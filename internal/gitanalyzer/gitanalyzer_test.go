package gitanalyzer

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
)

var t0 = time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

type testRepo struct {
	t    *testing.T
	dir  string
	repo *git.Repository
	wt   *git.Worktree
}

func newTestRepo(t *testing.T) *testRepo {
	t.Helper()
	dir := t.TempDir()
	repo, err := git.PlainInit(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	return &testRepo{t: t, dir: dir, repo: repo, wt: wt}
}

// commit writes files (path -> content; empty content deletes) and commits.
func (r *testRepo) commit(msg string, when time.Time, files map[string]string, parents ...plumbing.Hash) plumbing.Hash {
	r.t.Helper()
	for p, content := range files {
		full := filepath.Join(r.dir, p)
		if content == "" {
			if _, err := r.wt.Remove(p); err != nil {
				r.t.Fatal(err)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			r.t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			r.t.Fatal(err)
		}
		if _, err := r.wt.Add(p); err != nil {
			r.t.Fatal(err)
		}
	}
	sig := &object.Signature{Name: "dev", Email: "dev@example.com", When: when}
	opts := &git.CommitOptions{Author: sig, Committer: sig, AllowEmptyCommits: true}
	if len(parents) > 0 {
		opts.Parents = parents
	}
	h, err := r.wt.Commit(msg, opts)
	if err != nil {
		r.t.Fatal(err)
	}
	return h
}

func TestIsBugFix(t *testing.T) {
	tests := []struct {
		msg  string
		want bool
	}{
		{"fix: nil pointer in parser", true},
		{"Hotfix for login", true},
		{"feat: add dashboard", false},
		{"", false},
	}
	for _, tt := range tests {
		t.Run(tt.msg, func(t *testing.T) {
			if got := IsBugFix(tt.msg); got != tt.want {
				t.Errorf("IsBugFix(%q) = %v, want %v", tt.msg, got, tt.want)
			}
		})
	}
}

func TestCommitsSince(t *testing.T) {
	r := newTestRepo(t)
	r.commit("initial", t0, map[string]string{"a.go": "a", "b.go": "b"})
	h2 := r.commit("fix: a", t0.AddDate(0, 0, 10), map[string]string{"a.go": "a2"})
	r.commit("remove b, add c", t0.AddDate(0, 0, 20), map[string]string{"b.go": "", "c.go": "c"})
	// Merge commit (two parents) must be skipped.
	r.commit("Merge pull request #1 from x/fix", t0.AddDate(0, 0, 30), nil, h2, mustHead(t, r))

	g, err := New(r.dir)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name      string
		since     time.Time
		wantMsgs  []string
		wantFiles map[string][]string
	}{
		{
			name:     "all history includes root commit, skips merge",
			since:    time.Time{},
			wantMsgs: []string{"fix: a", "initial", "remove b, add c"},
			wantFiles: map[string][]string{
				"initial":         {"a.go", "b.go"},
				"fix: a":          {"a.go"},
				"remove b, add c": {"b.go", "c.go"},
			},
		},
		{
			name:      "since filters by date (boundary inclusive)",
			since:     t0.AddDate(0, 0, 10),
			wantMsgs:  []string{"fix: a", "remove b, add c"},
			wantFiles: map[string][]string{"fix: a": {"a.go"}, "remove b, add c": {"b.go", "c.go"}},
		},
		{
			name:     "since after all commits",
			since:    t0.AddDate(1, 0, 0),
			wantMsgs: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			commits, err := g.CommitsSince(tt.since)
			if err != nil {
				t.Fatal(err)
			}
			var msgs []string
			for _, c := range commits {
				msgs = append(msgs, c.Message)
				if c.SHA == "" || c.When.IsZero() {
					t.Errorf("commit %q missing SHA or date", c.Message)
				}
				if want, ok := tt.wantFiles[c.Message]; ok {
					got := append([]string(nil), c.Files...)
					sort.Strings(got)
					if !equal(got, want) {
						t.Errorf("files of %q = %v, want %v", c.Message, got, want)
					}
				}
			}
			sort.Strings(msgs)
			if !equal(msgs, tt.wantMsgs) {
				t.Errorf("messages = %v, want %v", msgs, tt.wantMsgs)
			}
		})
	}
}

func TestFilesAt(t *testing.T) {
	r := newTestRepo(t)
	r.commit("initial", t0, map[string]string{"a.go": "a", "b.go": "b"})
	r.commit("remove b, add c", t0.AddDate(0, 0, 20), map[string]string{"b.go": "", "c.go": "c"})

	g, err := New(r.dir)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		at   time.Time
		want []string
	}{
		{"before any commit", t0.AddDate(0, 0, -1), nil},
		{"exactly at first commit", t0, []string{"a.go", "b.go"}},
		{"between commits", t0.AddDate(0, 0, 5), []string{"a.go", "b.go"}},
		{"after last commit", t0.AddDate(0, 1, 0), []string{"a.go", "c.go"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			files, err := g.FilesAt(tt.at)
			if err != nil {
				t.Fatal(err)
			}
			var got []string
			for f := range files {
				got = append(got, f)
			}
			sort.Strings(got)
			if !equal(got, tt.want) {
				t.Errorf("FilesAt = %v, want %v", got, tt.want)
			}
		})
	}
}

func mustHead(t *testing.T, r *testRepo) plumbing.Hash {
	t.Helper()
	ref, err := r.repo.Head()
	if err != nil {
		t.Fatal(err)
	}
	return ref.Hash()
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestNew_LinkedWorktree checks that a `git worktree add` checkout (where .git
// is a file pointing at the main repository) can be read.
func TestNew_LinkedWorktree(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git binary not available")
	}
	r := newTestRepo(t)
	r.commit("fix: initial", t0, map[string]string{"a.go": "a"})
	wtDir := filepath.Join(t.TempDir(), "wt")
	out, err := exec.Command("git", "-C", r.dir, "worktree", "add", "-q", "-b", "side", wtDir).CombinedOutput()
	if err != nil {
		t.Fatalf("git worktree add: %v: %s", err, out)
	}

	g, err := New(wtDir)
	if err != nil {
		t.Fatalf("New(worktree): %v", err)
	}
	commits, err := g.CommitsSince(time.Time{})
	if err != nil {
		t.Fatalf("CommitsSince(worktree): %v", err)
	}
	if len(commits) != 1 || commits[0].Message != "fix: initial" {
		t.Errorf("commits = %+v, want the one initial commit", commits)
	}
}

// commitAs is commit with a chosen author email.
func (r *testRepo) commitAs(email, msg string, when time.Time, files map[string]string) {
	r.t.Helper()
	for p, content := range files {
		if err := os.WriteFile(filepath.Join(r.dir, p), []byte(content), 0o644); err != nil {
			r.t.Fatal(err)
		}
		if _, err := r.wt.Add(p); err != nil {
			r.t.Fatal(err)
		}
	}
	sig := &object.Signature{Name: email, Email: email, When: when}
	if _, err := r.wt.Commit(msg, &git.CommitOptions{Author: sig, Committer: sig}); err != nil {
		r.t.Fatal(err)
	}
}

// fastImportRepo builds a repo with git fast-import: nFiles files, then
// nCommits commits where commit k rewrites file k%nFiles. It returns the dir
// and the expected commit count per path.
func fastImportRepo(t *testing.T, nFiles, nCommits int) (string, map[string]int) {
	t.Helper()
	dir := t.TempDir()
	if out, err := exec.Command("git", "init", "-q", dir).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
	var b strings.Builder
	want := map[string]int{}
	when := time.Now().AddDate(0, 0, -300).Unix()
	path := func(i int) string { return "pkg/d" + strconv.Itoa(i%20) + "/f" + strconv.Itoa(i) + ".go" }
	writeCommit := func(n int, msg string, files map[string]string) {
		fmt.Fprintf(&b, "commit refs/heads/main\nmark :%d\nauthor dev <dev@x.io> %d +0000\ncommitter dev <dev@x.io> %d +0000\ndata %d\n%s\n",
			n, when, when, len(msg), msg)
		if n > 1 {
			fmt.Fprintf(&b, "from :%d\n", n-1)
		}
		for p, c := range files {
			fmt.Fprintf(&b, "M 100644 inline %s\ndata %d\n%s\n", p, len(c), c)
			want[p]++
		}
		when += 60
	}
	initial := map[string]string{}
	for i := 0; i < nFiles; i++ {
		initial[path(i)] = "v0"
	}
	writeCommit(1, "initial", initial)
	for k := 0; k < nCommits; k++ {
		writeCommit(k+2, "change "+strconv.Itoa(k), map[string]string{path(k % nFiles): "v" + strconv.Itoa(k+1)})
	}
	cmd := exec.Command("git", "-C", dir, "fast-import", "--quiet")
	cmd.Stdin = strings.NewReader(b.String())
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("fast-import: %v: %s", err, out)
	}
	if out, err := exec.Command("git", "-C", dir, "symbolic-ref", "HEAD", "refs/heads/main").CombinedOutput(); err != nil {
		t.Fatalf("symbolic-ref: %v: %s", err, out)
	}
	return dir, want
}

// TestAnalyzeAll_EveryFileGetsHistory guards against the old 30s overall and
// 2s per-file timeouts, which silently dropped history on large repos: every
// file must come back with its full commit count.
func TestAnalyzeAll_EveryFileGetsHistory(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git binary not available")
	}
	dir, want := fastImportRepo(t, 1000, 6000)
	g, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	paths := make([]string, 0, len(want))
	for p := range want {
		paths = append(paths, p)
	}
	start := time.Now()
	got, err := g.AnalyzeAll(paths, 365)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("AnalyzeAll on %d files took %s", len(paths), time.Since(start))
	if len(got) != len(want) {
		t.Fatalf("history for %d files, want %d", len(got), len(want))
	}
	for p, n := range want {
		if got[p].CommitCount != n {
			t.Errorf("%s: CommitCount = %d, want %d", p, got[p].CommitCount, n)
		}
	}
}

func TestAnalyzeAll(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git binary not available")
	}
	now := time.Now()
	r := newTestRepo(t)
	r.commitAs("a@x.io", "initial", now.AddDate(0, 0, -400), map[string]string{"a.go": "a", "b.go": "b", "c.go": "c"})
	r.commitAs("b@x.io", "feat: b", now.AddDate(0, 0, -91), map[string]string{"b.go": "b2"})
	r.commitAs("b@x.io", "fix: b (#42)", now.AddDate(0, 0, -89), map[string]string{"b.go": "b3"})
	for i := 0; i < 12; i++ {
		r.commitAs("c@x.io", "tweak c "+strconv.Itoa(i), now.AddDate(0, 0, -30+i), map[string]string{"c.go": "c" + strconv.Itoa(i)})
	}

	g, err := New(r.dir)
	if err != nil {
		t.Fatal(err)
	}
	got, err := g.AnalyzeAll([]string{"a.go", "b.go", "c.go", "untracked.go"}, 365)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name        string
		path        string
		commits     int
		last90      int
		authors     int
		recent      int
		firstMsg    string
		firstPR     int
		firstBugFix bool
	}{
		{"commit older than lookback counts but is not a recent change", "a.go", 1, 0, 1, 0, "", 0, false},
		{"90-day boundary: 91 days out, 89 days in", "b.go", 3, 1, 2, 2, "fix: b (#42)", 42, true},
		{"recent changes capped at 10, newest first", "c.go", 13, 12, 2, 10, "tweak c 11", 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h, ok := got[tt.path]
			if !ok {
				t.Fatalf("no history for %s", tt.path)
			}
			if h.CommitCount != tt.commits || h.CommitsLast90d != tt.last90 || len(h.UniqueAuthors) != tt.authors || len(h.RecentChanges) != tt.recent {
				t.Errorf("got commits=%d last90=%d authors=%d recent=%d, want %d %d %d %d",
					h.CommitCount, h.CommitsLast90d, len(h.UniqueAuthors), len(h.RecentChanges),
					tt.commits, tt.last90, tt.authors, tt.recent)
			}
			if tt.recent == 0 {
				return
			}
			first := h.RecentChanges[0]
			if first.Message != tt.firstMsg || first.HadIncident != tt.firstBugFix || len(first.SHA) != 7 {
				t.Errorf("first change = %+v, want message %q bugfix %v", first, tt.firstMsg, tt.firstBugFix)
			}
			if (tt.firstPR == 0) != (first.PRNumber == nil) || (first.PRNumber != nil && *first.PRNumber != tt.firstPR) {
				t.Errorf("PR number = %v, want %d", first.PRNumber, tt.firstPR)
			}
		})
	}

	t.Run("file with no history is absent", func(t *testing.T) {
		if _, ok := got["untracked.go"]; ok {
			t.Error("untracked.go should have no history entry")
		}
	})
}

func TestAnalyzeAll_NoCommits(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git binary not available")
	}
	r := newTestRepo(t)
	g, err := New(r.dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := g.AnalyzeAll([]string{"a.go"}, 365); err == nil {
		t.Error("AnalyzeAll on a repo without commits: want error, got nil")
	}
}
