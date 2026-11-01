package main

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zanetworker/highstakes/internal/types"
	"github.com/zanetworker/highstakes/pkg/heatmap"
)

func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=T", "-c", "user.email=t@x.com"}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// TestPRCheck_UnscoredFileUsesConfiguredTiers: a new file inherits heat 80 from
// core/; the default tiers call that HIGH, a repo config with critical: 70 must call it CRITICAL.
func TestPRCheck_UnscoredFileUsesConfiguredTiers(t *testing.T) {
	tests := []struct {
		name   string
		config string
		want   types.Tier
	}{
		{"no config uses default tiers", "", types.TierHigh},
		{"custom critical threshold applies", "tiers:\n  critical: 70\n  high: 50\n  medium: 20\n  low: 0\n", types.TierCritical},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := writeHeatmap(t)
			if tt.config != "" {
				if err := os.WriteFile(filepath.Join(dir, ".heatmap", "config.yaml"), []byte(tt.config), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			gitIn(t, dir, "init", "-q", "-b", "main")
			gitIn(t, dir, "add", "core/a.go")
			gitIn(t, dir, "commit", "-q", "-m", "base")
			gitIn(t, dir, "checkout", "-q", "-b", "feature")
			if err := os.WriteFile(filepath.Join(dir, "core", "new.go"), []byte("package core\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			gitIn(t, dir, "add", "core/new.go")
			gitIn(t, dir, "commit", "-q", "-m", "add new")

			out, err := runCLI(t, dir, "pr", "check", "--base", "main", "--json", "--shadow=false", "--fail-on=", "--author=t@x.com")
			if err != nil {
				t.Fatalf("pr check: %v\n%s", err, out)
			}
			var risk types.PRRisk
			if err := json.Unmarshal([]byte(out), &risk); err != nil {
				t.Fatalf("decode %q: %v", out, err)
			}
			if len(risk.FilesChanged) != 1 || !risk.FilesChanged[0].Unscored {
				t.Fatalf("files = %+v, want one unscored file", risk.FilesChanged)
			}
			if got := risk.FilesChanged[0].Tier; got != tt.want {
				t.Errorf("unscored file tier = %s, want %s", got, tt.want)
			}
		})
	}
}

// TestPRCheck_EmptyDiffStillGates: an empty diff reports LOW, and the gate and
// shadow log still apply to that LOW result.
func TestPRCheck_EmptyDiffStillGates(t *testing.T) {
	tests := []struct {
		name      string
		args      []string
		wantCode  int    // 0 = no error
		wantErr   string // substring of the error
		wantOut   string // substring of stdout
		wantShade bool   // shadow log written
	}{
		{"invalid --fail-on lists valid values", []string{"--fail-on=invalid", "--shadow=false"}, 2, "low", "", false},
		{"--fail-on low passes a LOW result with nothing open", []string{"--fail-on=low", "--shadow=false"}, 0, "", `"decision_reason": "all before-merge items done"`, false},
		{"--strict --fail-on low blocks a LOW result", []string{"--fail-on=low", "--strict", "--shadow=false"}, exitBlocked, "tier low", "", false},
		{"--fail-on critical passes", []string{"--fail-on=critical", "--shadow=false"}, 0, "", `"decision": "pass"`, false},
		{"--shadow writes its decision record", []string{"--fail-on=", "--shadow=true"}, 0, "", `"tier": "low"`, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := writeHeatmap(t)
			gitIn(t, dir, "init", "-q", "-b", "main")
			gitIn(t, dir, "add", "core/a.go")
			gitIn(t, dir, "commit", "-q", "-m", "base")

			args := append([]string{"pr", "check", "--base", "main", "--json", "--author=t@x.com"}, tt.args...)
			out, err := runCLI(t, dir, args...)
			if tt.wantCode == 0 {
				if err != nil {
					t.Fatalf("pr check: %v\n%s", err, out)
				}
			} else {
				var ee *exitError
				if !errors.As(err, &ee) || ee.code != tt.wantCode || !strings.Contains(ee.msg, tt.wantErr) {
					t.Fatalf("err = %v, want exit %d containing %q", err, tt.wantCode, tt.wantErr)
				}
			}
			if !strings.Contains(out, tt.wantOut) {
				t.Errorf("stdout = %q, want it to contain %q", out, tt.wantOut)
			}
			log, readErr := os.ReadFile(filepath.Join(dir, shadowLogPath))
			if tt.wantShade && (readErr != nil || !strings.Contains(string(log), `"tier":"low"`) || !strings.Contains(string(log), `"files":0`)) {
				t.Errorf("shadow log = %q (%v), want a LOW record with 0 files", log, readErr)
			}
			if !tt.wantShade && readErr == nil {
				t.Errorf("shadow log written without --shadow: %q", log)
			}
		})
	}
}

// checklistRepo is a git repo whose main has core/a.go (Harden now, with
// sign-off, regression-test and CODEOWNERS actions) plus the extra files,
// checked out on branch feature. write writes a file in it.
func checklistRepo(t *testing.T, extra ...string) (dir string, write func(rel, s string)) {
	t.Helper()
	dir = writeHeatmap(t)
	hm, err := heatmap.Load(filepath.Join(dir, ".heatmap", "heatmap.json"))
	if err != nil {
		t.Fatal(err)
	}
	a := hm.Files["core/a.go"]
	a.Tier, a.Quadrant, a.ImpactAssessed, a.Likelihood = types.TierCritical, types.QuadrantFragileCritical, true, 60
	a.Explanation = &types.Explanation{Verdict: "Harden now", Actions: []types.Action{
		{ID: "senior-review", Text: "Get sign-off from a senior owner (@ana)", Scope: types.ScopeBeforeMerge, Owners: []string{"@ana"}},
		{ID: "regression-tests", Text: "Add regression tests around the recent fix", Scope: types.ScopeBeforeMerge, Signal: "recent fix"},
		{ID: "codeowners", Text: "Add a CODEOWNERS line", Scope: types.ScopeBacklog, Command: "/core/a.go @ana"},
	}}
	if err := heatmap.Save(hm, filepath.Join(dir, ".heatmap", "heatmap.json")); err != nil {
		t.Fatal(err)
	}
	write = func(rel, s string) {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, rel)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, rel), []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	gitIn(t, dir, "init", "-q", "-b", "main")
	for _, rel := range extra {
		write(rel, "package core\n\nfunc TestX() {}\n")
	}
	gitIn(t, dir, append([]string{"add", "core/a.go"}, extra...)...)
	gitIn(t, dir, "commit", "-q", "-m", "base")
	gitIn(t, dir, "checkout", "-q", "-b", "feature")
	return dir, write
}

// TestPRCheck_MarkdownChecklist: --format markdown renders the PR comment;
// an adjacent test in the diff and an owner approval tick their items.
func TestPRCheck_MarkdownChecklist(t *testing.T) {
	dir, write := checklistRepo(t)
	write("core/a.go", "package core\n\nfunc A() { _ = 1 }\n")
	gitIn(t, dir, "commit", "-q", "-am", "change a")
	base := []string{"pr", "check", "--base", "main", "--format", "markdown", "--shadow=false", "--fail-on=", "--author=t@x.com", "--reviewers=0"}

	out, err := runCLI(t, dir, base...)
	if err != nil {
		t.Fatalf("pr check: %v\n%s", err, out)
	}
	for _, want := range []string{"## ⛔ Harden before merge", "🤖 AI may auto-merge: ❌ no (touches Harden now files)",
		"- [ ] 👀 Get sign-off from @ana on `core/a.go`", "- [ ] 🧪 Add regression tests", "/core/a.go @ana"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}

	write("core/a_test.go", "package core\n")
	gitIn(t, dir, "add", "core/a_test.go")
	gitIn(t, dir, "commit", "-q", "-m", "test a")
	out, err = runCLI(t, dir, append(base, "--approvals", "zed,ANA")...)
	if err != nil {
		t.Fatalf("pr check: %v\n%s", err, out)
	}
	for _, want := range []string{"- [x] 👀 Get sign-off from @ana on `core/a.go`", "- [x] 🧪 Add regression tests"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}

	out, err = runCLI(t, dir, append(base, "--repo", "acme/core")...)
	if err != nil {
		t.Fatalf("pr check --repo: %v\n%s", err, out)
	}
	if want := "- [👥 Create CODEOWNERS with `/core/a.go @ana`](https://github.com/acme/core/new/main?filename=.github%2FCODEOWNERS&value=%2Fcore%2Fa.go+%40ana%0A)"; !strings.Contains(out, want) {
		t.Errorf("--repo should link the CODEOWNERS action (no CODEOWNERS yet: a create link); missing %q in:\n%s", want, out)
	}
	_, err = runCLI(t, dir, append(base, "--repo", "acme")...)
	if ee := (*exitError)(nil); !errors.As(err, &ee) || ee.code != 2 {
		t.Errorf("invalid --repo: err = %v, want exit 2", err)
	}

	out, err = runCLI(t, dir, "pr", "check", "--base", "main", "--json", "--shadow=false", "--fail-on=", "--reviewers=0")
	if err != nil {
		t.Fatalf("pr check --json: %v\n%s", err, out)
	}
	if !strings.Contains(out, `"autonomy_level": 0`) || !strings.Contains(out, `"auto_merge_allowed": false`) {
		t.Errorf("JSON lacks autonomy_level 0 and auto_merge_allowed false:\n%s", out)
	}

	_, err = runCLI(t, dir, "pr", "check", "--base", "main", "--format", "yaml")
	var ee *exitError
	if !errors.As(err, &ee) || ee.code != 2 || !strings.Contains(ee.msg, "text, markdown, json") {
		t.Errorf("invalid --format: err = %v, want exit 2 listing valid values", err)
	}
}

// TestPRCheck_GateUnblocksWhenItemsDone: --fail-on critical blocks a Harden
// now PR while items are open and passes once the adjacent test lands and a
// maintainer signs off with the label; --strict still blocks.
func TestPRCheck_GateUnblocksWhenItemsDone(t *testing.T) {
	dir, write := checklistRepo(t)
	write("core/a.go", "package core\n\nfunc A() { _ = 1 }\n")
	gitIn(t, dir, "commit", "-q", "-am", "change a")
	base := []string{"pr", "check", "--base", "main", "--format", "markdown", "--shadow=false", "--fail-on=critical", "--author=t@x.com", "--reviewers=0", "--auto-merge-enabled=false"}

	out, err := runCLI(t, dir, base...)
	var ee *exitError
	if !errors.As(err, &ee) || ee.code != exitBlocked || !strings.Contains(ee.msg, "2 before-merge items open") {
		t.Fatalf("all open: err = %v, want exit 5 with 2 items open", err)
	}
	for _, want := range []string{"### ✅ Do before merge (1 of 3 done)", "⛔ Merge blocked: 2 before-merge items open",
		"- [ ] 👀 Get sign-off from @ana on `core/a.go` (a maintainer adds the `highstakes:signed-off` label after reviewing)"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}

	write("core/a_test.go", "package core\n")
	gitIn(t, dir, "add", "core/a_test.go")
	gitIn(t, dir, "commit", "-q", "-m", "test a")
	_, err = runCLI(t, dir, base...)
	if !errors.As(err, &ee) || ee.code != exitBlocked || !strings.Contains(ee.msg, "1 before-merge item open") {
		t.Fatalf("one open: err = %v, want exit 5 with 1 item open", err)
	}

	out, err = runCLI(t, dir, append(base, "--signed-off")...)
	if err != nil {
		t.Fatalf("all done: %v\n%s", err, out)
	}
	for _, want := range []string{"### ✅ Do before merge (3 of 3 done)", "✅ All before-merge items done: ready to merge",
		"- [x] 👀 Get sign-off from @ana on `core/a.go`\n", "signed off with the highstakes:signed-off label"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}

	out, err = runCLI(t, dir, "pr", "check", "--base", "main", "--json", "--shadow=false", "--fail-on=critical", "--reviewers=0", "--signed-off", "--auto-merge-enabled=false")
	if err != nil || !strings.Contains(out, `"open_items": 0`) || !strings.Contains(out, `"decision_reason": "all before-merge items done"`) {
		t.Errorf("JSON: err = %v, want open_items 0 and the reason:\n%s", err, out)
	}

	out, err = runCLI(t, dir, "pr", "check", "--base", "main", "--shadow=false", "--fail-on=critical", "--reviewers=0", "--signed-off", "--auto-merge-enabled=false")
	if err != nil || !strings.Contains(out, "Decision: PASS (fail-on critical): all before-merge items done\n") {
		t.Errorf("text: err = %v, want the decision with its reason:\n%s", err, out)
	}

	_, err = runCLI(t, dir, append(base, "--signed-off", "--strict")...)
	if !errors.As(err, &ee) || ee.code != exitBlocked || !strings.Contains(ee.msg, "--strict") {
		t.Errorf("--strict: err = %v, want exit 5 naming --strict", err)
	}

	_, err = runCLI(t, dir, append(base, "--auto-merge-enabled", "maybe")...)
	if !errors.As(err, &ee) || ee.code != 2 {
		t.Errorf("invalid --auto-merge-enabled: err = %v, want exit 2", err)
	}
}

// TestPRCheck_DeletedTestDoesNotTick: deleting the adjacent test leaves
// "add tests" open; a test renamed next to the file ticks it at its new path.
func TestPRCheck_DeletedTestDoesNotTick(t *testing.T) {
	tests := []struct {
		name  string
		extra []string
		edit  func(t *testing.T, dir string)
		want  string
	}{
		{name: "deleted adjacent test", extra: []string{"core/a_test.go"},
			edit: func(t *testing.T, dir string) { gitIn(t, dir, "rm", "-q", "core/a_test.go") },
			want: "- [ ] 🧪 Add regression tests"},
		{name: "test renamed away from the file", extra: []string{"core/a_test.go"},
			edit: func(t *testing.T, dir string) { gitIn(t, dir, "mv", "core/a_test.go", "other/a_test.go") },
			want: "- [ ] 🧪 Add regression tests"},
		{name: "test renamed next to the file", extra: []string{"other/a_test.go"},
			edit: func(t *testing.T, dir string) { gitIn(t, dir, "mv", "other/a_test.go", "core/a_test.go") },
			want: "- [x] 🧪 Add regression tests"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir, write := checklistRepo(t, tt.extra...)
			if err := os.MkdirAll(filepath.Join(dir, "other"), 0o755); err != nil {
				t.Fatal(err)
			}
			tt.edit(t, dir)
			write("core/a.go", "package core\n\nfunc A() { _ = 1 }\n")
			gitIn(t, dir, "commit", "-q", "-am", "change a, move tests")
			out, err := runCLI(t, dir, "pr", "check", "--base", "main", "--format", "markdown", "--shadow=false", "--fail-on=", "--author=t@x.com", "--reviewers=0")
			if err != nil {
				t.Fatalf("pr check: %v\n%s", err, out)
			}
			if !strings.Contains(out, tt.want) {
				t.Errorf("missing %q in:\n%s", tt.want, out)
			}
		})
	}
}
