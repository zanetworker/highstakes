package explain

import (
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
			title, ok := TaskTitle(act.ID, f)
			if !ok || (a.Level == 0 && !assistOnly[act.ID]) {
				continue
			}
			// Agents do the work itself, so issue-filing commands are dropped;
			// only a CODEOWNERS line is a direct change worth handing over.
			cmd := ""
			if act.ID == "codeowners" {
				cmd = act.Command
			}
			out = append(out, Task{
				Title: title, File: path, ActionID: act.ID, Signal: act.Signal,
				Command: cmd, Acceptance: Acceptance(act.ID, f), Level: a.Level, LevelLabel: a.Label,
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

// assistOnly are the tasks allowed at L0 ("assist only"): tests and
// explanations, never changes to the code or its ownership.
var assistOnly = map[string]bool{"test-gap": true, "document-invariants": true}

// TaskTitle phrases an action as work an agent does on the file, so the
// instruction matches its acceptance. ok is false for actions that need a
// person rather than an agent (a second reviewer).
func TaskTitle(id string, f *types.FileHeat) (string, bool) {
	switch id {
	case "split":
		return "Split " + f.Path + " into smaller files", true
	case "complexity-issue":
		return "Cut the cyclomatic complexity of " + f.Path, true
	case "test-gap":
		return "Add tests next to " + f.Path, true
	case "document-invariants":
		return "Document the invariants of " + f.Path + " next to the code", true
	case "codeowners":
		return "Add a CODEOWNERS line for " + f.Path, true
	case "regression-tests":
		return "Add regression tests for the recent fixes to " + f.Path, true
	case "ownership-issue":
		return "", false
	}
	return "Address the backlog item on " + f.Path, true
}

// Acceptance is a checkable "done when" for an action.
func Acceptance(id string, f *types.FileHeat) string {
	switch id {
	case "split":
		fns := "functions"
		if n := f.Factors.Complexity.FunctionCount; n > 0 {
			fns = count(n, "function")
		}
		return "tests pass and the " + fns + " of " + f.Path + " are spread over smaller files"
	case "complexity-issue":
		return "tests pass and cyclomatic complexity drops below " + Thousands(f.Factors.Complexity.Cyclomatic)
	case "codeowners":
		return "CODEOWNERS covers " + f.Path
	case "test-gap":
		return "a test file next to " + f.Path + " passes"
	case "document-invariants":
		return "the invariants of " + f.Path + " are documented next to the code"
	case "regression-tests":
		return "a test next to " + f.Path + " covers the recent fixes and passes"
	case "ownership-issue":
		return "a second author reviews or changes " + f.Path
	}
	return "tests pass"
}
