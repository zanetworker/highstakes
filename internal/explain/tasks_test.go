package explain

import (
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
			task.Command != cut.Command || task.Signal != cut.Signal || !strings.Contains(task.Title, "ui/busy.go") {
			t.Errorf("task = %+v", task)
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
