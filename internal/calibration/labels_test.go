package calibration

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/zanetworker/highstakes/internal/gitanalyzer"
	"github.com/zanetworker/highstakes/internal/types"
)

func TestClassifyMessage(t *testing.T) {
	tests := []struct {
		name string
		msg  string
		want Labels
	}{
		{"revert title suppresses bugfix", `Revert "fix: nil deref"`, Labels{Revert: true}},
		{"revert body trailer", "undo change\n\nThis reverts commit abc123.", Labels{Revert: true}},
		{"plain bug fix", "fix: nil deref in parser", Labels{BugFix: true}},
		{"cve only", "Bump golang.org/x/net to address CVE-2026-1234", Labels{Security: true}},
		{"ghsa fix is both", "fix GHSA-abcd-efgh-ijkl path traversal", Labels{BugFix: true, Security: true}},
		{"vuln keyword", "harden vuln scanner config", Labels{Security: true}},
		{"security keyword case-insensitive", "Security: tighten token checks", Labels{Security: true}},
		{"feature", "feat: add tour command", Labels{}},
		{"fix keyword only in body is not a fix", "feat: add tour\n\nAlso fixes a typo and a security note.", Labels{}},
		{"advisory id in body counts", "chore(deps): bump x/net\n\nAddresses GHSA-abcd-efgh-ijkl.", Labels{Security: true}},
		{"empty", "", Labels{}},
		// Security keyword false positives seen in benchmark repos.
		{"docs type is not a security fix", "docs(security): add private reporting workflows (#4832)", Labels{}},
		{"ci type is not a security fix", "ci(security): gate tagged releases on scans (#3523)", Labels{}},
		{"test type is not a security fix", "test(server): cover service endpoint plaintext security (#1352)", Labels{}},
		{"chore type is not a security fix", "chore(security): document Kubernetes runtime RBAC (#3328)", Labels{}},
		{"SECURITY.md edit", "Update SECURITY.md", Labels{}},
		{"CSP header name", "browse: fix Content-Security-Policy warnings in Firefox (#6443)", Labels{BugFix: true}},
		{"identifier podSecurityContext", "fix(helm): omit podSecurityContext block when value is null", Labels{BugFix: true}},
		{"identifier SecurityException", "Return the default enum if a SecurityException is caught", Labels{}},
		{"constant JAVA_SECURITY_KERBEROS_CONF", "Add SystemProperties.JAVA_SECURITY_KERBEROS_CONF", Labels{}},
		// True positives that must survive the tightening.
		{"fix(security) scope", "fix(security): harden Vault credential transport (#3329)", Labels{BugFix: true, Security: true}},
		{"vulnerability word", "fastcgi: fix HTTPoxy vulnerability (#7934)", Labels{BugFix: true, Security: true}},
		{"hyphenated vulnerable", "Set an arbitrary upper limit on ReDoS-vulnerable regexps", Labels{Security: true}},
		{"security at end of subject", "caddyhttp: enhancements for client IP parsing security", Labels{Security: true}},
		{"cve id in docs commit still counts", "docs: note CVE-2026-1234 workaround", Labels{Security: true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ClassifyMessage(tt.msg)
			if got != tt.want {
				t.Errorf("ClassifyMessage(%q) = %+v, want %+v", tt.msg, got, tt.want)
			}
			if got.Any() != (tt.want != Labels{}) {
				t.Errorf("Any() = %v for %+v", got.Any(), got)
			}
		})
	}
}

var base = time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)

func TestCountEvents(t *testing.T) {
	commits := []gitanalyzer.CommitInfo{
		{SHA: "1", Message: "fix: a", When: base.AddDate(0, 0, 1), Files: []string{"a.go", "b.go"}},
		{SHA: "2", Message: `Revert "feat: x"`, When: base.AddDate(0, 0, 2), Files: []string{"a.go"}},
		{SHA: "3", Message: "fix CVE-2026-1", When: base.AddDate(0, 0, 3), Files: []string{"a.go"}},
		{SHA: "4", Message: "feat: y", When: base.AddDate(0, 0, 4), Files: []string{"a.go", "c.go"}},
		{SHA: "5", Message: "fix: too old", When: base, Files: []string{"c.go"}},                  // at from: excluded
		{SHA: "6", Message: "fix: too new", When: base.AddDate(0, 1, 0), Files: []string{"c.go"}}, // after to
		{SHA: "7", Message: "fix: bulk rename", When: base.AddDate(0, 0, 6), Files: manyFiles(MaxFilesPerEvent + 1)},
		{SHA: "8", Message: "fix: wide but allowed", When: base.AddDate(0, 0, 7), Files: append(manyFiles(MaxFilesPerEvent-1), "b.go")},
	}
	incs := []types.Incident{
		{File: "c.go", Date: base.AddDate(0, 0, 5)},
		{File: "c.go", Date: base.AddDate(-1, 0, 0)}, // outside window
	}
	got := CountEvents(commits, incs, base, base.AddDate(0, 0, 10), EventsAll)

	want := map[string]FileEvents{
		"a.go": {Total: 3, Revert: 1, BugFix: 2, Security: 1},
		"b.go": {Total: 2, BugFix: 2},
		"c.go": {Total: 1, Incident: 1},
	}
	// Commit 8 touches exactly MaxFilesPerEvent files and counts; commit 7 is
	// one over the limit and is skipped as a bulk change.
	if len(got) != len(want)+MaxFilesPerEvent-1 {
		t.Fatalf("got %d files %v, want %d", len(got), got, len(want))
	}
	for path, w := range want {
		t.Run(path, func(t *testing.T) {
			g, ok := got[path]
			if !ok {
				t.Fatalf("missing %s", path)
			}
			if *g != w {
				t.Errorf("%s = %+v, want %+v", path, *g, w)
			}
		})
	}

	if empty := CountEvents(nil, nil, base, base, EventsAll); len(empty) != 0 {
		t.Errorf("empty input gave %v", empty)
	}
}

func TestParseEventFilter(t *testing.T) {
	tests := []struct {
		in      string
		want    EventFilter
		wantErr bool
	}{
		{"all", EventsAll, false},
		{"fixes", EventsFixes, false},
		{"reverts", EventsReverts, false},
		{"security", EventsSecurity, false},
		{"SECURITY", EventsSecurity, false},
		{"", "", true},
		{"bugs", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := ParseEventFilter(tt.in)
			if (err != nil) != tt.wantErr || got != tt.want {
				t.Fatalf("ParseEventFilter(%q) = (%q, %v), want (%q, err=%v)", tt.in, got, err, tt.want, tt.wantErr)
			}
			if err != nil && !strings.Contains(err.Error(), "all, fixes, reverts, security") {
				t.Errorf("error %q must list valid values", err)
			}
		})
	}
}

func TestCountEvents_Filter(t *testing.T) {
	commits := []gitanalyzer.CommitInfo{
		{Message: "fix: nil deref", When: base.AddDate(0, 0, 1), Files: []string{"a.go"}},
		{Message: `Revert "feat: x"`, When: base.AddDate(0, 0, 2), Files: []string{"a.go", "r.go"}},
		{Message: "fix CVE-2026-1 token leak", When: base.AddDate(0, 0, 3), Files: []string{"a.go", "s.go"}},
		{Message: "harden vuln scanner", When: base.AddDate(0, 0, 4), Files: []string{"v.go"}},
	}
	incs := []types.Incident{{File: "i.go", Date: base.AddDate(0, 0, 5)}}
	to := base.AddDate(0, 0, 10)
	tests := []struct {
		filter EventFilter
		want   map[string]FileEvents
	}{
		{EventsAll, map[string]FileEvents{
			"a.go": {Total: 3, Revert: 1, BugFix: 2, Security: 1},
			"r.go": {Total: 1, Revert: 1},
			"s.go": {Total: 1, BugFix: 1, Security: 1},
			"v.go": {Total: 1, Security: 1},
			"i.go": {Total: 1, Incident: 1},
		}},
		// fixes: bug-fix or security commits; a CVE fix is counted once.
		{EventsFixes, map[string]FileEvents{
			"a.go": {Total: 2, BugFix: 2, Security: 1},
			"s.go": {Total: 1, BugFix: 1, Security: 1},
			"v.go": {Total: 1, Security: 1},
		}},
		{EventsReverts, map[string]FileEvents{
			"a.go": {Total: 1, Revert: 1},
			"r.go": {Total: 1, Revert: 1},
		}},
		{EventsSecurity, map[string]FileEvents{
			"a.go": {Total: 1, Security: 1},
			"s.go": {Total: 1, Security: 1},
			"v.go": {Total: 1, Security: 1},
		}},
	}
	for _, tt := range tests {
		t.Run(string(tt.filter), func(t *testing.T) {
			got := CountEvents(commits, incs, base, to, tt.filter)
			if len(got) != len(tt.want) {
				t.Fatalf("got %d files %v, want %v", len(got), got, tt.want)
			}
			for path, w := range tt.want {
				if g := got[path]; g == nil || *g != w {
					t.Errorf("%s = %+v, want %+v", path, g, w)
				}
			}
		})
	}
}

// TestCountEvents_RealRepo builds a git repo with go-git and checks that events
// are found end to end through gitanalyzer.CommitsSince.
func TestCountEvents_RealRepo(t *testing.T) {
	dir := t.TempDir()
	repo, err := git.PlainInit(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	commit := func(msg string, when time.Time, files map[string]string) {
		t.Helper()
		for p, c := range files {
			full := filepath.Join(dir, p)
			if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(full, []byte(c), 0o644); err != nil {
				t.Fatal(err)
			}
			if _, err := wt.Add(p); err != nil {
				t.Fatal(err)
			}
		}
		sig := &object.Signature{Name: "d", Email: "d@x", When: when}
		if _, err := wt.Commit(msg, &git.CommitOptions{Author: sig, Committer: sig}); err != nil {
			t.Fatal(err)
		}
	}
	commit("initial", base, map[string]string{"auth/token.go": "v1", "ui/view.go": "v1", "docs/x.md": "v1"})
	commit("feat: refresh tokens", base.AddDate(0, 0, 1), map[string]string{"auth/token.go": "v2"})
	commit("Revert \"feat: refresh tokens\"\n\nThis reverts commit deadbeef.", base.AddDate(0, 0, 2), map[string]string{"auth/token.go": "v1"})
	commit("fix: GHSA-1234 token leak", base.AddDate(0, 0, 3), map[string]string{"auth/token.go": "v3", "ui/view.go": "v2"})
	commit("docs: typo", base.AddDate(0, 0, 4), map[string]string{"docs/x.md": "v2"})

	g, err := gitanalyzer.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	commits, err := g.CommitsSince(base)
	if err != nil {
		t.Fatal(err)
	}
	got := CountEvents(commits, nil, base, base.AddDate(0, 0, 30), EventsAll)

	want := map[string]FileEvents{
		"auth/token.go": {Total: 2, Revert: 1, BugFix: 1, Security: 1},
		"ui/view.go":    {Total: 1, BugFix: 1, Security: 1},
	}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for path, w := range want {
		if g := got[path]; g == nil || *g != w {
			t.Errorf("%s = %+v, want %+v", path, g, w)
		}
	}
}

func manyFiles(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = fmt.Sprintf("gen/f%03d.go", i)
	}
	return out
}
