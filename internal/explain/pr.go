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
	Paths      []string        // every changed path (renames as old and new path)
	Deleted    map[string]bool // paths the PR deletes, including a rename's old path
	Lines      int             // lines added plus deleted
	Components int             // distinct components among changed non-test files
	Approvals  []string        // GitHub logins that approved the PR (from CI)
	// MaintainerApprovals are approvals of the head from logins with write
	// access (from CI); any one of them ticks owner-review items.
	MaintainerApprovals []string
	Codeowned           func(string) bool // CODEOWNERS at HEAD covers a path; nil: nothing
	SignedOff           bool              // the PR carries the SignOffLabel
	AutoMerge           string            // AutoMergeOn, AutoMergeOff, else unknown
}

// SignOffLabel is the PR label a maintainer adds after reviewing; it ticks
// the sign-off and owner-review items, since GitHub does not let authors
// approve their own PRs (a sole maintainer could never satisfy them).
const SignOffLabel = "highstakes:signed-off"

// Auto-merge states of the PR, as pr check --auto-merge-enabled takes them.
const (
	AutoMergeOn      = "true"
	AutoMergeOff     = "false"
	AutoMergeUnknown = "unknown"
)

// reviewActions are satisfied by an owner's approval or the sign-off label.
var reviewActions = map[string]bool{"senior-review": true, "owner-review": true}

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
		applyPRState(&a, c)
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
		Text:   fmt.Sprintf("Ask for a smaller PR: %s lines across %d %s", Thousands(c.Lines), c.Components, unit),
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
	case a.ID == "owner-review" && len(c.MaintainerApprovals) > 0:
		// Protect: someone with write access looked; a senior sign-off on
		// Harden now still needs a named owner or the sign-off label.
		return true
	case reviewActions[a.ID]:
		return approvedByOwner(a.Owners, c.Approvals)
	}
	return false
}

// applyPRState settles items from PR state CI reports: the sign-off label
// ticks review items no owner approved yet; auto-merge off ticks the
// no-auto-merge item, and auto-merge on rewords it as a request.
func applyPRState(a *types.Action, c Change) {
	switch {
	case reviewActions[a.ID] && !a.Done && c.SignedOff:
		a.Done, a.Signal = true, "signed off with the "+SignOffLabel+" label"
	case a.ID == "no-auto-merge" && c.AutoMerge == AutoMergeOff:
		a.Done, a.Signal = true, "auto-merge is not enabled on this PR"
	case a.ID == "no-auto-merge" && c.AutoMerge == AutoMergeOn:
		a.Text, a.Signal = "Turn auto-merge off: it is enabled on this PR", "auto-merge is enabled on this PR"
	}
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

// FileActions are one changed file's PR actions. Blocking marks a Harden
// now or Protect file, whose before-merge items gate the merge.
type FileActions struct {
	Path     string
	Blocking bool
	Actions  []types.Action
}

// Checklist merges per-file actions into the PR's before-merge checklist
// (items of blocking files), the items worth doing that do not block
// (before-merge items of other files, such as Stabilize) and the backlog.
// Identical items (same text and command) collapse into one that lists
// every file and is done only when done for all of them. Routine files'
// "normal review" item is left out: routine stays quiet.
func Checklist(files []FileActions) (before, worth, backlog []types.Action) {
	index := map[string]int{}
	var all []types.Action
	var nonBlocking []bool
	for _, fa := range files {
		for _, a := range fa.Actions {
			if a.ID == "light-review" {
				continue
			}
			// single-owner asks for a second person, which no step in the PR
			// can complete, so it never blocks the merge.
			worthOnly := a.Scope != types.ScopeBacklog && (!fa.Blocking || a.ID == "single-owner")
			key := fmt.Sprint(a.Scope, "\x00", a.Text, "\x00", a.Command, "\x00", worthOnly)
			if a.ID == "no-auto-merge" {
				// Auto-merge is one PR-wide switch: one item, worded by the
				// first (most severe) file, however each posture phrases it.
				key = fmt.Sprint(a.Scope, "\x00no-auto-merge\x00", worthOnly)
			}
			if i, ok := index[key]; ok {
				all[i].Files = append(all[i].Files, fa.Path)
				all[i].Done = all[i].Done && a.Done
				continue
			}
			index[key] = len(all)
			a.Files = []string{fa.Path}
			all = append(all, a)
			nonBlocking = append(nonBlocking, worthOnly)
		}
	}
	for i, a := range all {
		switch {
		case a.Scope == types.ScopeBacklog:
			backlog = append(backlog, a)
		case nonBlocking[i]:
			worth = append(worth, a)
		default:
			before = append(before, a)
		}
	}
	return before, worth, backlog
}
