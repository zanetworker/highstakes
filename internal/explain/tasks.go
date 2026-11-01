package explain

import (
	"fmt"
	"sort"

	"github.com/zanetworker/highstakes/internal/scorer"
	"github.com/zanetworker/highstakes/internal/types"
)

// Task is one backlog item as a self-contained unit of work for an agent.
type Task struct {
	Title      string `json:"title"`
	File       string `json:"file"`
	ActionID   string `json:"action_id"`
	Signal     string `json:"signal"`
	Command    string `json:"command,omitempty"`
	Acceptance string `json:"acceptance"` // when the task counts as done
	Level      int    `json:"level"`      // the file's autonomy level
	LevelLabel string `json:"level_label"`
}

// Tasks lists the backlog items of files whose autonomy level (capped at
// maxLevel) is at least minLevel, most autonomous level first, then by path.
// assess-impact is left out: one analyze run with an LLM covers every file.
func Tasks(hm *types.Heatmap, minLevel, maxLevel int) []Task {
	var out []Task
	for path, f := range hm.Files {
		if f.Explanation == nil {
			continue
		}
		a := scorer.FileAutonomy(f, maxLevel)
		if a.Level < minLevel {
			continue
		}
		for _, act := range f.Explanation.Actions {
			if act.Scope != types.ScopeBacklog || act.ID == "assess-impact" {
				continue
			}
			out = append(out, Task{
				Title: act.Text + " (" + path + ")", File: path, ActionID: act.ID, Signal: act.Signal,
				Command: act.Command, Acceptance: acceptance(act.ID, f), Level: a.Level, LevelLabel: a.Label,
			})
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Level != out[j].Level {
			return out[i].Level > out[j].Level
		}
		return out[i].File < out[j].File
	})
	return out
}

// acceptance is a checkable "done when" for a backlog action.
func acceptance(id string, f *types.FileHeat) string {
	switch id {
	case "split":
		return "tests pass and the " + count(f.Factors.Complexity.FunctionCount, "function") + " of " + f.Path + " are spread over smaller files"
	case "complexity-issue":
		return fmt.Sprintf("tests pass and cyclomatic complexity drops below %d", f.Factors.Complexity.Cyclomatic)
	case "codeowners":
		return "CODEOWNERS covers " + f.Path
	case "test-gap":
		return "a test file next to " + f.Path + " passes"
	case "document-invariants":
		return "the invariants of " + f.Path + " are documented next to the code"
	case "ownership-issue":
		return "a second author reviews or changes " + f.Path
	}
	return "tests pass"
}
