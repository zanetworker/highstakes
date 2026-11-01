package explain

import (
	"fmt"
	"strings"

	"github.com/zanetworker/highstakes/internal/types"
)

// unassessed keys rules for files whose impact was not assessed by an LLM.
const unassessed types.Quadrant = ""

const (
	fc      = types.QuadrantFragileCritical
	protect = types.QuadrantProtect
	watch   = types.QuadrantWatch
	low     = types.QuadrantLow
)

// rule is one row of the action table: it fires for a file in one of its
// quadrants when text returns ok. Rules run in order, most important first.
type rule struct {
	id   string
	in   []types.Quadrant
	text func(s signals) (string, bool)
}

func always(t string) func(signals) (string, bool) {
	return func(signals) (string, bool) { return t, true }
}

var rules = []rule{
	{"assess-impact", []types.Quadrant{unassessed},
		always("Run analyze with an LLM to assess impact; these actions use likelihood only")},
	{"senior-review", []types.Quadrant{fc}, func(s signals) (string, bool) {
		return "Every PR: senior review and sign-off from a top owner" + ownerList(s), true
	}},
	{"owner-review", []types.Quadrant{protect}, func(s signals) (string, bool) {
		return "Owner review on every PR" + ownerList(s), true
	}},
	{"codeowners", []types.Quadrant{fc, protect}, always("Add to CODEOWNERS; never auto-merge")},
	{"change-carefully", []types.Quadrant{protect}, always("Change carefully: small PRs, tests first")},
	{"ai-review", []types.Quadrant{watch}, always("AI review is fine for routine changes")},
	{"light-review", []types.Quadrant{low}, always("Light review; AI review is fine")},
	{"split", []types.Quadrant{fc, unassessed}, func(s signals) (string, bool) {
		cx := s.f.Factors.Complexity
		switch {
		case s.fnTop > 0 && s.fnTop <= splitTop:
			return fmt.Sprintf("Split it: %d functions in one file", cx.FunctionCount), true
		case s.cycTop > 0 && s.cycTop <= splitTop:
			return fmt.Sprintf("Split it: cyclomatic %d in one file", cx.Cyclomatic), true
		}
		return "", false
	}},
	{"refactor", []types.Quadrant{watch}, func(s signals) (string, bool) {
		cx := s.f.Factors.Complexity
		return fmt.Sprintf("Refactor to cut complexity: cyclomatic %d across %d functions", cx.Cyclomatic, cx.FunctionCount),
			notable(s.complexTop())
	}},
	{"regression-tests", []types.Quadrant{fc, protect, unassessed}, func(s signals) (string, bool) {
		around := "the recent fix"
		if len(s.fixes) > 1 {
			around = fmt.Sprintf("the %d recent fixes", len(s.fixes))
		}
		return "Add regression tests around " + around + noTestsSuffix(s), fixTestsApply(s)
	}},
	{"add-tests", []types.Quadrant{watch}, func(s signals) (string, bool) {
		return fmt.Sprintf("Add tests: %d recent fixes suggest bugs recur", len(s.fixes)) + noTestsSuffix(s), fixTestsApply(s)
	}},
	{"stable-interface", []types.Quadrant{fc, protect}, func(s signals) (string, bool) {
		n := len(s.f.Dependencies.ImportedBy)
		return fmt.Sprintf("Keep its public interface stable: %d importers; add contract tests", n),
			s.importersTop > 0 && s.importersTop <= interfaceTop
	}},
	{"no-tests", []types.Quadrant{fc, protect, watch, unassessed}, func(s signals) (string, bool) {
		return "Add tests: none found next to this file", s.noTests() && !fixTestsApply(s)
	}},
	{"single-owner", []types.Quadrant{fc, protect, watch, unassessed}, func(s signals) (string, bool) {
		if len(s.owners) == 0 || s.owners[0].Share < singleOwnerShare || s.f.Factors.ChangeFrequency.CommitCount < singleOwnerMin {
			return "", false
		}
		return fmt.Sprintf("Single owner risk: %s wrote %.0f%% of it; have a second person review and learn it",
			s.owners[0].Handle, s.owners[0].Share*100), true
	}},
	{"knowledge-spread", []types.Quadrant{fc, protect, watch, unassessed}, func(s signals) (string, bool) {
		n := s.f.Factors.ChangeFrequency.UniqueAuthors
		return fmt.Sprintf("Spread knowledge: %d authors change it; document its invariants and keep an owner on review", n),
			n >= minAuthors && s.authorsTop > 0 && s.authorsTop <= authorsTop
	}},
}

// actions applies the rule table. Test files only get a light review.
func actions(s signals) []types.Action {
	q := s.f.Quadrant
	if !s.f.ImpactAssessed {
		q = unassessed
	}
	if s.isTest {
		q = low
	}
	var out []types.Action
	for _, r := range rules {
		if !hasQuadrant(r.in, q) {
			continue
		}
		if t, ok := r.text(s); ok {
			out = append(out, types.Action{ID: r.id, Text: t})
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

// fixTestsApply: one recent fix asks for regression tests on impactful or
// unassessed files; watch files need fixes to recur (two or more).
func fixTestsApply(s signals) bool {
	if s.f.ImpactAssessed && s.f.Quadrant == watch {
		return len(s.fixes) >= 2
	}
	return len(s.fixes) >= 1
}

func noTestsSuffix(s signals) string {
	if s.noTests() {
		return "; none found next to this file"
	}
	return ""
}

// ownerList names up to two top owners, e.g. " (@ana, @bo)".
func ownerList(s signals) string {
	var names []string
	for _, o := range s.owners {
		if o.Handle != "" && len(names) < 2 {
			names = append(names, o.Handle)
		}
	}
	if len(names) == 0 {
		return ""
	}
	return " (" + strings.Join(names, ", ") + ")"
}
