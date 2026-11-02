package explain

import (
	"sort"
	"strings"
	"testing"

	"github.com/zanetworker/highstakes/internal/types"
)

func TestTasks(t *testing.T) {
	withBacklog := func(q types.Quadrant, assessed bool, acts ...types.Action) *types.FileHeat {
		f := &types.FileHeat{ImpactAssessed: assessed, Quadrant: q, Tier: types.TierMedium,
			Explanation: &types.Explanation{Actions: acts}}
		f.Factors.Complexity.Cyclomatic = 40
		return f
	}
	cut := types.Action{ID: "complexity-issue", Text: "Open an issue to cut its complexity", Scope: types.ScopeBacklog,
		Signal: "cyclomatic 40 across 12 functions (top 5%)", Command: "gh issue create --title x"}
	review := types.Action{ID: "refactor", Text: "Refactor the function you touch", Scope: types.ScopeBeforeMerge, Signal: "s"}
	hm := &types.Heatmap{Files: map[string]*types.FileHeat{
		"ui/busy.go":  withBacklog(types.QuadrantWatch, true, review, cut),
		"core/p.go":   withBacklog(types.QuadrantProtect, true, types.Action{ID: "codeowners", Text: "Add a CODEOWNERS line", Scope: types.ScopeBacklog, Signal: "not covered by CODEOWNERS", Command: "/core/p.go @ana"}),
		"est/e.go":    withBacklog("", false, types.Action{ID: "assess-impact", Text: "Run analyze with an LLM to assess impact", Scope: types.ScopeBacklog, Signal: "impact not assessed", Command: "highstakes analyze"}),
		"no/expl.go":  {ImpactAssessed: true, Quadrant: types.QuadrantLow},
		"ui/quiet.go": withBacklog(types.QuadrantLow, true),
	}}
	for f, h := range hm.Files {
		h.Path = f
	}
	t.Run("default level 2: only what agents may do with at most one approval", func(t *testing.T) {
		got := Tasks(hm, 2, 3)
		if len(got) != 1 {
			t.Fatalf("tasks = %+v, want the Stabilize file's backlog item only", got)
		}
		task := got[0]
		if task.File != "ui/busy.go" || task.ActionID != "complexity-issue" || task.Level != 2 || task.LevelLabel != "auto-fix, human merges" ||
			task.Signal != cut.Signal {
			t.Errorf("task = %+v", task)
		}
		// An agent does the work: the title says what to change and there is no
		// issue-filing command, so the instruction matches the acceptance.
		if task.Title != "Cut the cyclomatic complexity of ui/busy.go" || task.Command != "" {
			t.Errorf("title/command = %q / %q, want implementation work without an issue command", task.Title, task.Command)
		}
		if task.Acceptance != "tests pass and cyclomatic complexity drops below 40" {
			t.Errorf("acceptance = %q", task.Acceptance)
		}
	})
	t.Run("level 1 adds Protect files, most autonomous first; assess-impact is one repo-wide run, not a task", func(t *testing.T) {
		got := Tasks(hm, 1, 3)
		var files []string
		for _, task := range got {
			files = append(files, task.File)
		}
		if strings.Join(files, ",") != "ui/busy.go,core/p.go" {
			t.Errorf("files = %v", files)
		}
		if got[1].Acceptance != "CODEOWNERS covers core/p.go" {
			t.Errorf("codeowners acceptance = %q", got[1].Acceptance)
		}
	})
	t.Run("max level caps files below the requested level", func(t *testing.T) {
		if got := Tasks(hm, 2, 1); len(got) != 0 {
			t.Errorf("with max_level 1 nothing reaches L2: %+v", got)
		}
	})
	t.Run("level 3 has no tasks here", func(t *testing.T) {
		if got := Tasks(hm, 3, 3); len(got) != 0 {
			t.Errorf("tasks = %+v", got)
		}
	})
}

func TestTaskWork(t *testing.T) {
	f := &types.FileHeat{Path: "a/b.go"}
	f.Factors.Complexity.FunctionCount = 30
	tests := []struct {
		id, wantTitle, wantDone string
		agentWork               bool
	}{
		{"split", "Split a/b.go into smaller files", "tests pass and the 30 functions of a/b.go are spread over smaller files", true},
		{"complexity-issue", "Cut the cyclomatic complexity of a/b.go", "tests pass and cyclomatic complexity drops below 0", true},
		{"test-gap", "Add tests next to a/b.go", "a test file next to a/b.go passes", true},
		{"document-invariants", "Document the invariants of a/b.go next to the code", "the invariants of a/b.go are documented next to the code", true},
		{"codeowners", "Add a CODEOWNERS line for a/b.go", "CODEOWNERS covers a/b.go", true},
		{"regression-tests", "Add regression tests for the recent fixes to a/b.go", "a test next to a/b.go covers the recent fixes and passes", true},
		{"ownership-issue", "", "a second author reviews or changes a/b.go", false}, // needs a second person, not an agent
		{"unknown", "Address the backlog item on a/b.go", "tests pass", true},
	}
	for _, tt := range tests {
		t.Run(tt.id, func(t *testing.T) {
			title, ok := TaskTitle(tt.id, f)
			if ok != tt.agentWork || title != tt.wantTitle {
				t.Errorf("TaskTitle(%q) = %q, %v; want %q, %v", tt.id, title, ok, tt.wantTitle, tt.agentWork)
			}
			if got := Acceptance(tt.id, f); got != tt.wantDone {
				t.Errorf("Acceptance(%q) = %q, want %q", tt.id, got, tt.wantDone)
			}
		})
	}
}

func TestTasks_L0IsAssistOnly(t *testing.T) {
	acts := []types.Action{
		{ID: "split", Text: "Split it", Scope: types.ScopeBacklog},
		{ID: "complexity-issue", Text: "Cut complexity", Scope: types.ScopeBacklog},
		{ID: "codeowners", Text: "Add a CODEOWNERS line", Scope: types.ScopeBacklog, Command: "/h.go @a"},
		{ID: "test-gap", Text: "Add tests", Scope: types.ScopeBacklog},
		{ID: "document-invariants", Text: "Document invariants", Scope: types.ScopeBacklog},
	}
	harden := &types.FileHeat{Path: "core/h.go", ImpactAssessed: true, Quadrant: types.QuadrantFragileCritical, Tier: types.TierCritical,
		Explanation: &types.Explanation{Actions: acts}}
	routine := &types.FileHeat{Path: "ui/r.go", ImpactAssessed: true, Quadrant: types.QuadrantLow, Tier: types.TierLow,
		Explanation: &types.Explanation{Actions: acts}}
	tests := []struct {
		name     string
		f        *types.FileHeat
		maxLevel int
		want     []string
	}{
		{"Harden now file (L0) gets only tests and docs", harden, 3, []string{"document-invariants", "test-gap"}},
		{"max_level 0 caps every file to assist-only", routine, 0, []string{"document-invariants", "test-gap"}},
		{"Routine file (L3) gets every task", routine, 3, []string{"codeowners", "complexity-issue", "document-invariants", "split", "test-gap"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got []string
			for _, task := range Tasks(&types.Heatmap{Files: map[string]*types.FileHeat{tt.f.Path: tt.f}}, 0, tt.maxLevel) {
				got = append(got, task.ActionID)
			}
			sort.Strings(got)
			if strings.Join(got, ",") != strings.Join(tt.want, ",") {
				t.Errorf("tasks = %v, want %v", got, tt.want)
			}
		})
	}
}
