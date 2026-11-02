package prdata

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestSubjectPRNumber(t *testing.T) {
	tests := []struct {
		subject string
		want    int
		ok      bool
	}{
		{"fix: nil deref (#123)", 123, true},
		{"fix: nil deref (#123)  ", 123, true},
		{"Merge pull request #45 from user/branch", 45, true},
		{"[1.43.x] fix foo (#7000) (#7010)", 7010, true}, // the trailing number is the PR that landed
		{"mentions (#9) mid-subject only", 0, false},
		{"plain commit", 0, false},
		{"", 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.subject, func(t *testing.T) {
			got, ok := subjectPRNumber(tt.subject)
			if got != tt.want || ok != tt.ok {
				t.Errorf("subjectPRNumber(%q) = (%d, %v), want (%d, %v)", tt.subject, got, ok, tt.want, tt.ok)
			}
		})
	}
}

func TestParseLog(t *testing.T) {
	raw := "\x1eaaa\x1fp1 p2\x1f1700000000\x1fMerge pull request #4 from x\n\nbody\n\x1f\n\na.go\nb/c.go\n" +
		"\x1ebbb\x1fp0\x1f1690000000\x1ffix: thing (#3)\n\x1f\n" // no files
	got, err := parseLog(raw)
	if err != nil {
		t.Fatal(err)
	}
	want := []Commit{
		{SHA: "aaa", Parents: []string{"p1", "p2"}, When: time.Unix(1700000000, 0), Message: "Merge pull request #4 from x\n\nbody", Files: []string{"a.go", "b/c.go"}},
		{SHA: "bbb", Parents: []string{"p0"}, When: time.Unix(1690000000, 0), Message: "fix: thing (#3)"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("parseLog =\n%+v\nwant\n%+v", got, want)
	}
	if _, err := parseLog("\x1eonly\x1ftwo"); err == nil {
		t.Error("parseLog accepted a truncated record")
	}
	if got, err := parseLog(""); err != nil || len(got) != 0 {
		t.Errorf("parseLog(\"\") = (%v, %v), want empty", got, err)
	}
}

func TestMapPRs(t *testing.T) {
	commits := []Commit{
		{SHA: "c3", Message: "Merge pull request #30 from a/b"},
		{SHA: "c2", Message: "feat: thing (#20)"},
		{SHA: "c1", Message: "unrelated subject"},
		{SHA: "c0", Message: "older duplicate (#20)"},
	}
	prs := []PR{
		{Number: 10, MergeCommit: "c1"}, // by merge commit SHA
		{Number: 20, MergeCommit: "zz"}, // SHA unknown locally: falls back to the subject, newest wins
		{Number: 30},
		{Number: 40, MergeCommit: "nope"}, // not on the default branch
	}
	got := MapPRs(prs, commits)
	want := map[int]string{10: "c1", 20: "c2", 30: "c3"}
	if len(got) != len(want) {
		t.Fatalf("MapPRs mapped %d PRs, want %d: %+v", len(got), len(want), got)
	}
	for n, sha := range want {
		if got[n].SHA != sha {
			t.Errorf("PR #%d -> %q, want %q", n, got[n].SHA, sha)
		}
	}
	if len(MapPRs(nil, commits)) != 0 || len(MapPRs(prs, nil)) != 0 {
		t.Error("MapPRs with no PRs or no commits should map nothing")
	}
}

func TestMapPRs_LandedFiles(t *testing.T) {
	// First-parent history, newest first: r3..r1 are PR #7 rebase-merged as
	// three commits; m is a merge commit; s is a squash merge.
	commits := []Commit{
		{SHA: "r3", Parents: []string{"r2"}, Message: "third step", Files: []string{"c.go"}},
		{SHA: "r2", Parents: []string{"r1"}, Message: "second step", Files: []string{"b.go"}},
		{SHA: "r1", Parents: []string{"m"}, Message: "first step", Files: []string{"a.go", "c.go"}},
		{SHA: "m", Parents: []string{"s", "side"}, Message: "Merge pull request #5 from x/y", Files: []string{"m.go"}},
		{SHA: "s", Parents: []string{"o"}, Message: "fix: thing (#4)", Files: []string{"s.go"}},
		{SHA: "o", Parents: []string{"z"}, Message: "older", Files: []string{"o.go"}},
	}
	steps := []string{"first step", "second step", "third step"}
	tests := []struct {
		name string
		pr   PR
		sha  string
		want []string
	}{
		{"squash merge", PR{Number: 4, MergeCommit: "s", Commits: 3}, "s", []string{"s.go"}},
		{"merge commit uses the diff against the first parent", PR{Number: 5, MergeCommit: "m", Commits: 4}, "m", []string{"m.go"}},
		{"rebase merge unions every landed commit", PR{Number: 7, MergeCommit: "r3", Commits: 3, CommitHeadlines: steps}, "r3", []string{"a.go", "b.go", "c.go"}},
		{"rebase merge with commit count unknown", PR{Number: 7, MergeCommit: "r3", CommitHeadlines: steps}, "r3", []string{"c.go"}},
		{"rebase merge with headlines unknown does not walk", PR{Number: 7, MergeCommit: "r3", Commits: 3}, "r3", []string{"c.go"}},
		{"rebase merge stops at an unrelated commit in between", PR{Number: 7, MergeCommit: "r3", Commits: 3, CommitHeadlines: []string{"first step", "third step"}}, "r3", []string{"c.go"}},
		{"squash with a custom message does not walk into unrelated ancestors", PR{Number: 9, Title: "second step", MergeCommit: "r2", Commits: 2, CommitHeadlines: []string{"wip", "address review"}}, "r2", []string{"b.go"}},
		{"commit count past the read history stops at its end", PR{Number: 8, MergeCommit: "o", Commits: 3, CommitHeadlines: []string{"older", "x"}}, "o", []string{"o.go"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := MapPRs([]PR{tt.pr}, commits)[tt.pr.Number]
			if !ok {
				t.Fatalf("PR #%d not mapped", tt.pr.Number)
			}
			if got.SHA != tt.sha || !reflect.DeepEqual(got.Files, tt.want) {
				t.Errorf("PR #%d -> %s %v, want %s %v", tt.pr.Number, got.SHA, got.Files, tt.sha, tt.want)
			}
		})
	}
}

func TestIsBackportBranch(t *testing.T) {
	tests := []struct {
		ref  string
		want bool
	}{
		{"origin/release-1.2", true},
		{"origin/release/v0.0.116-docs", true},
		{"origin/backport-123", true},
		{"origin/stable", true},
		{"origin/v1", true},
		{"origin/v0.2.x", true},
		{"origin/tokio-1.43.x", true}, // LTS line
		{"origin/main", false},
		{"origin/HEAD", false},
		{"origin/ci-upload-release-wheels/pm", false},
		{"origin/vendor-update", false},
		{"origin/dependabot/go_modules/x-1.2.x", false},
	}
	for _, tt := range tests {
		t.Run(tt.ref, func(t *testing.T) {
			if got := IsBackportBranch(tt.ref); got != tt.want {
				t.Errorf("IsBackportBranch(%q) = %v, want %v", tt.ref, got, tt.want)
			}
		})
	}
}

func TestBackportedPRs(t *testing.T) {
	prCommit := map[int]Commit{
		1: {SHA: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
		2: {SHA: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"},
		3: {SHA: "cccccccccccccccccccccccccccccccccccccccc"},
		// Fixed on the release line first (#8634), then merged forward as #8736.
		4: {SHA: "dddddddddddddddddddddddddddddddddddddddd", Message: "rt: fix shutdown (#8634) (#8736)"},
	}
	branch := []Commit{
		{Message: "rt: fix shutdown (#8634)"},
		{Message: "fix: x\n\n(cherry picked from commit aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa)"},
		{Message: "[1.x] fix: y (#2) (#99)"},
		{Message: "unrelated"},
	}
	got := BackportedPRs(branch, prCommit)
	want := map[int]bool{1: true, 2: true, 4: true}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("BackportedPRs = %v, want %v", got, want)
	}
	if len(BackportedPRs(nil, prCommit)) != 0 {
		t.Error("no branch commits should give no backports")
	}
}

func TestBranchOnly(t *testing.T) {
	head := []Commit{{SHA: "m3"}, {SHA: "m2"}, {SHA: "m1"}}
	branch := []Commit{{SHA: "b2"}, {SHA: "b1"}, {SHA: "m1"}} // forked from m1
	got := BranchOnly(branch, head)
	if len(got) != 2 || got[0].SHA != "b2" || got[1].SHA != "b1" {
		t.Errorf("BranchOnly = %+v, want b2, b1", got)
	}
	if len(BranchOnly(nil, head)) != 0 || len(BranchOnly(branch, nil)) != 3 {
		t.Error("BranchOnly edge cases wrong")
	}
}

func TestFastReverts(t *testing.T) {
	base := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	prs := []PR{
		{Number: 1, MergedAt: base},
		{Number: 2, MergedAt: base},
		{Number: 3, MergedAt: base},
		{Number: 4, MergedAt: base},
	}
	prCommit := map[int]Commit{
		1: {SHA: "1111111111111111111111111111111111111111"},
		2: {SHA: "2222222222222222222222222222222222222222"},
		3: {SHA: "3333333333333333333333333333333333333333"},
		4: {SHA: "4444444444444444444444444444444444444444"},
	}
	commits := []Commit{
		{When: base.Add(2 * 24 * time.Hour), Message: "Revert \"x\"\n\nThis reverts commit 1111111111111111111111111111111111111111."},
		{When: base.Add(10 * 24 * time.Hour), Message: "Revert \"y\"\n\nThis reverts commit 2222222."}, // too late
		{When: base.Add(7 * 24 * time.Hour), Message: "Revert \"z\" (#50)\n\nReverts org/repo#3"},      // exactly 7 days
		{When: base.Add(time.Hour), Message: "Revert w\n\nReverts #4"},
	}
	got := FastReverts(prs, prCommit, commits, 7*24*time.Hour)
	want := map[int]bool{1: true, 3: true, 4: true}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("FastReverts = %v, want %v", got, want)
	}
}

func TestReadCommits_RealRepo(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_DATE=2026-05-01T00:00:00Z", "GIT_COMMITTER_DATE=2026-05-01T00:00:00Z")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	write := func(name, body string) {
		t.Helper()
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	run("init", "-q", "-b", "main")
	run("config", "user.email", "t@example.com")
	run("config", "user.name", "t")
	write("a.go", "a")
	run("add", ".")
	run("commit", "-q", "-m", "init")
	run("checkout", "-q", "-b", "feature")
	write("pkg/b.go", "b")
	run("add", ".")
	run("commit", "-q", "-m", "add b")
	run("checkout", "-q", "main")
	run("merge", "-q", "--no-ff", "feature", "-m", "Merge pull request #7 from u/feature")

	got, err := ReadCommits(dir, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), true, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("first-parent log has %d commits, want 2 (merge and init): %+v", len(got), got)
	}
	merge := got[0]
	if !strings.HasPrefix(merge.Message, "Merge pull request #7") || !reflect.DeepEqual(merge.Files, []string{"pkg/b.go"}) {
		t.Errorf("merge commit = %+v, want files [pkg/b.go] diffed against the first parent", merge)
	}
	if n, ok := subjectPRNumber(merge.Message); !ok || n != 7 {
		t.Errorf("merge subject maps to PR %d (ok=%v), want 7", n, ok)
	}
	late, err := ReadCommits(dir, time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC), true, "HEAD")
	if err != nil || len(late) != 0 {
		t.Errorf("since after every commit = (%d commits, %v), want none", len(late), err)
	}
	if _, err := ReadCommits(dir, time.Time{}, true, "no-such-ref"); err == nil {
		t.Error("ReadCommits accepted an unknown revision")
	}
}
