package explain

import (
	"fmt"
	"strings"

	"github.com/zanetworker/highstakes/internal/types"
)

// unassessed keys rules for files whose impact was not assessed by an LLM.
const unassessed types.Quadrant = ""

const (
	harden    = types.QuadrantFragileCritical
	protect   = types.QuadrantProtect
	stabilize = types.QuadrantWatch
	routine   = types.QuadrantLow

	beforeMerge = types.ScopeBeforeMerge
	backlog     = types.ScopeBacklog
)

// fired is what a rule produces when it applies: the imperative, the signal
// that triggered it and, for backlog items, a copyable command.
type fired struct {
	text, signal, command string
	owners                []string
}

// rule is one row of the action table: it fires for a file in one of its
// quadrants when apply returns ok. Rules run in order, before-merge first.
type rule struct {
	id    string
	scope string
	in    []types.Quadrant
	apply func(s signals) (fired, bool)
}

var rules = []rule{
	// Before merge: what this PR needs.
	{"senior-review", beforeMerge, []types.Quadrant{harden}, func(s signals) (fired, bool) {
		return fired{text: "Get sign-off from a senior owner" + ownerList(s),
			signal: "Harden now: likely to break and costly when it does", owners: topOwners(s)}, true
	}},
	{"owner-review", beforeMerge, []types.Quadrant{protect}, func(s signals) (fired, bool) {
		return fired{text: "Get an owner's review" + ownerList(s),
			signal: "Protect: costly if it breaks", owners: topOwners(s)}, true
	}},
	{"regression-tests", beforeMerge, []types.Quadrant{harden, stabilize, unassessed}, func(s signals) (fired, bool) {
		around := "the recent fix"
		if len(s.fixes) > 1 {
			around = fmt.Sprintf("the %d recent fixes", len(s.fixes))
		}
		return fired{text: "Add regression tests around " + around, signal: fixSignal(s)}, fixTestsApply(s)
	}},
	{"no-tests", beforeMerge, []types.Quadrant{harden, protect, unassessed}, func(s signals) (fired, bool) {
		return fired{text: "Add tests before merge: none next to this file", signal: "no test file next to it"},
			s.noTests() && !fixTestsApply(s)
	}},
	{"stable-interface", beforeMerge, []types.Quadrant{harden, protect}, func(s signals) (fired, bool) {
		return fired{text: "Add contract tests for its public interface", signal: importersSignal(s)},
			s.importersTop > 0 && s.importersTop <= interfaceTop
	}},
	{"refactor", beforeMerge, []types.Quadrant{stabilize}, func(s signals) (fired, bool) {
		cx := s.f.Factors.Complexity
		return fired{text: fmt.Sprintf("Refactor the function you touch: cyclomatic %d across %s", cx.Cyclomatic, count(cx.FunctionCount, "function")),
			signal: fmt.Sprintf("complexity in the top %d%% of this repo", s.complexTop())}, notable(s.complexTop())
	}},
	{"single-owner", beforeMerge, []types.Quadrant{stabilize, unassessed}, func(s signals) (fired, bool) {
		sig, ok := singleOwner(s)
		return fired{text: "Add a second reviewer: " + sig, signal: sig}, ok
	}},
	{"no-auto-merge", beforeMerge, []types.Quadrant{harden}, func(signals) (fired, bool) {
		return fired{text: "Keep auto-merge off", signal: "Harden now files never auto-merge"}, true
	}},
	{"no-auto-merge", beforeMerge, []types.Quadrant{protect}, func(signals) (fired, bool) {
		return fired{text: "Block auto-merge", signal: "Protect files never auto-merge"}, true
	}},
	{"light-review", beforeMerge, []types.Quadrant{routine}, func(signals) (fired, bool) {
		return fired{text: "Normal review", signal: "Routine: neither likely to break nor costly"}, true
	}},

	// Backlog: what to do about the file over time.
	{"assess-impact", backlog, []types.Quadrant{unassessed}, func(signals) (fired, bool) {
		return fired{text: "Run analyze with an LLM to assess impact", signal: "impact not assessed by an LLM; actions use likelihood only",
			command: "highstakes analyze"}, true
	}},
	{"split", backlog, []types.Quadrant{harden, unassessed}, func(s signals) (fired, bool) {
		cx := s.f.Factors.Complexity
		var sig string
		switch {
		case s.fnTop > 0 && s.fnTop <= splitTop:
			sig = fmt.Sprintf("%s in one file (top %d%%)", count(cx.FunctionCount, "function"), s.fnTop)
		case s.cycTop > 0 && s.cycTop <= splitTop:
			sig = fmt.Sprintf("cyclomatic %d in one file (top %d%%)", cx.Cyclomatic, s.cycTop)
		default:
			return fired{}, false
		}
		return fired{text: "Split it into smaller files", signal: sig, command: issue("Split "+s.f.Path, sig)}, true
	}},
	{"codeowners", backlog, []types.Quadrant{harden, protect}, func(s signals) (fired, bool) {
		if s.codeowned {
			return fired{}, false
		}
		return fired{text: "Add a CODEOWNERS line", signal: "not covered by CODEOWNERS", command: codeownersLine(s)}, true
	}},
	{"document-invariants", backlog, []types.Quadrant{harden}, func(s signals) (fired, bool) {
		sig, ok := manyAuthors(s)
		if !ok {
			sig, ok = singleOwner(s)
		}
		return fired{text: "Document its invariants", signal: sig,
			command: issue("Document the invariants of "+s.f.Path, sig)}, ok
	}},
	{"test-gap", backlog, []types.Quadrant{protect}, func(s signals) (fired, bool) {
		sig := "no test file next to it"
		return fired{text: "Open a test-gap issue", signal: sig, command: issue("Add tests for "+s.f.Path, sig)}, s.noTests()
	}},
	{"complexity-issue", backlog, []types.Quadrant{stabilize}, func(s signals) (fired, bool) {
		cx := s.f.Factors.Complexity
		sig := fmt.Sprintf("cyclomatic %d across %s (top %d%%)", cx.Cyclomatic, count(cx.FunctionCount, "function"), s.complexTop())
		return fired{text: "Open an issue to cut its complexity", signal: sig,
			command: issue("Cut the complexity of "+s.f.Path, sig)}, notable(s.complexTop())
	}},
	{"ownership-issue", backlog, []types.Quadrant{stabilize}, func(s signals) (fired, bool) {
		sig, ok := singleOwner(s)
		return fired{text: "Open an issue to spread ownership", signal: sig,
			command: issue("Spread ownership of "+s.f.Path, sig)}, ok
	}},
}

// ScopeOf is the scope of the rule with this id, else before_merge. It
// fills in actions stored by releases that did not record a scope.
func ScopeOf(id string) string {
	for _, r := range rules {
		if r.id == id {
			return r.scope
		}
	}
	return beforeMerge
}

// actions applies the rule table. Test files are routine.
func actions(s signals) []types.Action {
	q := s.f.Quadrant
	if !s.f.ImpactAssessed {
		q = unassessed
	}
	if s.isTest {
		q = routine
	}
	var out []types.Action
	for _, r := range rules {
		if !hasQuadrant(r.in, q) {
			continue
		}
		if f, ok := r.apply(s); ok {
			out = append(out, types.Action{ID: r.id, Text: f.text, Scope: r.scope, Signal: f.signal,
				Command: f.command, Owners: f.owners})
		}
	}
	return out
}

func hasQuadrant(qs []types.Quadrant, q types.Quadrant) bool {
	for _, x := range qs {
		if x == q {
			return true
		}
	}
	return false
}

// fixTestsApply: one recent fix asks for regression tests on Harden now or
// unassessed files; Stabilize files need fixes to recur (two or more);
// Protect files never get them (they rarely change).
func fixTestsApply(s signals) bool {
	switch {
	case !s.f.ImpactAssessed:
		return len(s.fixes) >= 1
	case s.f.Quadrant == stabilize:
		return len(s.fixes) >= 2
	case s.f.Quadrant == protect:
		return false
	}
	return len(s.fixes) >= 1
}

func fixSignal(s signals) string {
	if len(s.fixes) == 0 {
		return ""
	}
	if len(s.fixes) == 1 {
		return "recent fix: " + describeChange(s.fixes[0])
	}
	return fmt.Sprintf("%d recent fixes; latest: %s", len(s.fixes), describeChange(s.fixes[0]))
}

func importersSignal(s signals) string {
	return fmt.Sprintf("%s (top %d%%)", importedBy(len(s.f.Dependencies.ImportedBy)), s.importersTop)
}

// singleOwner reports one author owning at least 80% of a file with enough
// commits for the share to mean something, in a repo with more than one author.
func singleOwner(s signals) (string, bool) {
	if s.soleAuthor || len(s.owners) == 0 || s.owners[0].Share < singleOwnerShare || s.f.Factors.ChangeFrequency.CommitCount < singleOwnerMin {
		return "", false
	}
	return fmt.Sprintf("%s wrote %.0f%% of it", s.owners[0].Handle, s.owners[0].Share*100), true
}

// manyAuthors reports an author count in the repo's top 10% (at least 5).
func manyAuthors(s signals) (string, bool) {
	n := s.f.Factors.ChangeFrequency.UniqueAuthors
	return fmt.Sprintf("%d authors change it (top %d%%)", n, s.authorsTop),
		n >= minAuthors && s.authorsTop > 0 && s.authorsTop <= authorsTop
}

// topOwners names up to two top owners.
func topOwners(s signals) []string {
	var names []string
	for _, o := range s.owners {
		if o.Handle != "" && len(names) < 2 {
			names = append(names, o.Handle)
		}
	}
	return names
}

// ownerList renders topOwners as " (@ana, @bo)", or "" when none are known.
func ownerList(s signals) string {
	names := topOwners(s)
	if len(names) == 0 {
		return ""
	}
	return " (" + strings.Join(names, ", ") + ")"
}

// codeownersLine is an anchored CODEOWNERS rule naming the top owners, or a
// placeholder team when no owner is known.
func codeownersLine(s signals) string {
	owners := topOwners(s)
	if len(owners) == 0 {
		owners = []string{"@your-team"}
	}
	return "/" + s.f.Path + " " + strings.Join(owners, " ")
}

// issue is a copyable `gh issue create` command.
func issue(title, body string) string {
	return "gh issue create --title " + shellQuote(title) + " --body " + shellQuote(body+". Found by highstakes.")
}

// shellQuote single-quotes s for a POSIX shell.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
