package explain

import (
	"fmt"
	"strings"

	"github.com/zanetworker/highstakes/internal/analyzer"
	"github.com/zanetworker/highstakes/internal/types"
)

// largePRLines and unrelatedComponents make a PR "large": more changed
// lines than the circuit breaker allows, or work spread over 3+ components.
const (
	largePRLines        = 500
	unrelatedComponents = 3
)

// Change is what pr check knows about the PR. Whether exported symbols
// changed is not detected (it would need a per-language diff parser).
type Change struct {
	Paths      []string          // every changed path (renames as old and new path)
	Deleted    map[string]bool   // paths the PR deletes, including a rename's old path
	Lines      int               // lines added plus deleted
	Components int               // distinct components among changed non-test files
	Approvals  []string          // GitHub logins that approved the PR (from CI)
	Codeowned  func(string) bool // CODEOWNERS at HEAD covers a path; nil: nothing
}

// testActions are satisfied by adding or changing a test next to the file;
// deleting one does not count.
var testActions = map[string]bool{"regression-tests": true, "no-tests": true, "stable-interface": true}

// ForPR returns a copy of f's actions for this PR: the stored ones plus the
// change-dependent smaller-pr item for Harden now files, with Done set when
// the PR already satisfies an item. It returns nil without an explanation.
func ForPR(f *types.FileHeat, c Change) []types.Action {
	if f.Explanation == nil {
		return nil
	}
	var out []types.Action
	added := false
	for _, a := range f.Explanation.Actions {
		if !added && a.Scope == types.ScopeBacklog {
			out, added = appendSmallerPR(out, f, c), true
		}
		a.Done = satisfied(a, f.Path, c)
		out = append(out, a)
	}
	if !added {
		out = appendSmallerPR(out, f, c)
	}
	return out
}

func appendSmallerPR(out []types.Action, f *types.FileHeat, c Change) []types.Action {
	if !f.ImpactAssessed || f.Quadrant != types.QuadrantFragileCritical ||
		(c.Lines <= largePRLines && c.Components < unrelatedComponents) {
		return out
	}
	unit := "components"
	if c.Components == 1 {
		unit = "component"
	}
	return append(out, types.Action{
		ID:     "smaller-pr",
		Text:   fmt.Sprintf("Ask for a smaller PR: %d lines across %d %s", c.Lines, c.Components, unit),
		Scope:  types.ScopeBeforeMerge,
		Signal: fmt.Sprintf("large change to a Harden now file (over %d lines or %d+ components)", largePRLines, unrelatedComponents),
	})
}

// satisfied reports whether the PR already does what a is asking.
func satisfied(a types.Action, file string, c Change) bool {
	switch {
	case testActions[a.ID]:
		for _, p := range c.Paths {
			if !c.Deleted[p] && analyzer.IsAdjacentTest(p, file) {
				return true
			}
		}
	case a.ID == "codeowners":
		return c.Codeowned != nil && c.Codeowned(file)
	case a.ID == "senior-review" || a.ID == "owner-review":
		return approvedByOwner(a.Owners, c.Approvals)
	}
	return false
}

// approvedByOwner matches GitHub logins against owner handles, ignoring
// case and the leading "@".
func approvedByOwner(owners, approvals []string) bool {
	norm := func(s string) string { return strings.ToLower(strings.TrimPrefix(strings.TrimSpace(s), "@")) }
	for _, o := range owners {
		for _, a := range approvals {
			if norm(a) != "" && norm(o) == norm(a) {
				return true
			}
		}
	}
	return false
}

// FileActions are one changed file's PR actions.
type FileActions struct {
	Path    string
	Actions []types.Action
}

// Checklist merges per-file actions into the PR's before-merge checklist and
// backlog: identical items (same text and command) collapse into one that
// lists every file and is done only when done for all of them. Routine
// files' "normal review" item is left out: routine stays quiet.
func Checklist(files []FileActions) (before, backlog []types.Action) {
	index := map[string]int{}
	var all []types.Action
	for _, fa := range files {
		for _, a := range fa.Actions {
			if a.ID == "light-review" {
				continue
			}
			key := a.Scope + "\x00" + a.Text + "\x00" + a.Command
			if i, ok := index[key]; ok {
				all[i].Files = append(all[i].Files, fa.Path)
				all[i].Done = all[i].Done && a.Done
				continue
			}
			index[key] = len(all)
			a.Files = []string{fa.Path}
			all = append(all, a)
		}
	}
	for _, a := range all {
		if a.Scope == types.ScopeBacklog {
			backlog = append(backlog, a)
		} else {
			before = append(before, a)
		}
	}
	return before, backlog
}
