package explain

import (
	"strings"
	"testing"

	"github.com/zanetworker/highstakes/internal/ownership"
	"github.com/zanetworker/highstakes/internal/types"
)

func ids(as []types.Action) []string {
	out := make([]string, len(as))
	for i, a := range as {
		out[i] = a.ID
	}
	return out
}

func text(as []types.Action, id string) string {
	for _, a := range as {
		if a.ID == id {
			return a.Text
		}
	}
	return ""
}

var twoOwners = []ownership.Owner{{Handle: "@ana", Share: 0.5}, {Handle: "@bo", Share: 0.3}, {Handle: "@cy", Share: 0.2}}

func TestActions_FragileCritical(t *testing.T) {
	f := hot()
	got := Explain(f, repo(f), twoOwners).Actions
	want := []string{"senior-review", "codeowners", "split", "regression-tests", "stable-interface", "knowledge-spread"}
	if strings.Join(ids(got), ",") != strings.Join(want, ",") {
		t.Fatalf("ids = %v, want %v", ids(got), want)
	}
	for id, w := range map[string]string{
		"senior-review":    "Every PR: senior review and sign-off from a top owner (@ana, @bo)",
		"codeowners":       "Add to CODEOWNERS; never auto-merge",
		"split":            "Split it: 479 functions in one file",
		"regression-tests": "Add regression tests around the recent fix",
		"stable-interface": "Keep its public interface stable: 11 importers; add contract tests",
		"knowledge-spread": "Spread knowledge: 28 authors change it; document its invariants and keep an owner on review",
	} {
		if g := text(got, id); g != w {
			t.Errorf("%s = %q, want %q", id, g, w)
		}
	}
}

func TestActions_Rules(t *testing.T) {
	no := false
	tests := []struct {
		name   string
		edit   func(*types.FileHeat)
		owners []ownership.Owner
		has    []string
		hasnt  []string
		texts  map[string]string
	}{
		{name: "senior review without known owners",
			edit: func(*types.FileHeat) {}, has: []string{"senior-review"},
			texts: map[string]string{"senior-review": "Every PR: senior review and sign-off from a top owner"}},
		{name: "split by cyclomatic when functions are not top 5%",
			edit:  func(f *types.FileHeat) { f.Factors.Complexity.FunctionCount = 1 },
			texts: map[string]string{"split": "Split it: cyclomatic 1092 in one file"}},
		{name: "no split, no stable interface when signals are ordinary",
			edit: func(f *types.FileHeat) {
				f.Factors.Complexity = types.Complexity{Cyclomatic: 1, FunctionCount: 1}
				f.Dependencies.ImportedBy = f.Dependencies.ImportedBy[:1]
			},
			hasnt: []string{"split", "stable-interface"}},
		{name: "no regression tests without a fix; missing tests asks for tests",
			edit: func(f *types.FileHeat) {
				f.RecentChanges = nil
				f.Factors.TestCoverage.HasTestFile = &no
			},
			has: []string{"no-tests"}, hasnt: []string{"regression-tests"},
			texts: map[string]string{"no-tests": "Add tests: none found next to this file"}},
		{name: "fix and no tests fold into one regression-tests action",
			edit: func(f *types.FileHeat) { f.Factors.TestCoverage.HasTestFile = &no },
			has:  []string{"regression-tests"}, hasnt: []string{"no-tests"},
			texts: map[string]string{"regression-tests": "Add regression tests around the recent fix; none found next to this file"}},
		{name: "single owner at 80% with enough commits",
			edit:   func(*types.FileHeat) {},
			owners: []ownership.Owner{{Handle: "@solo", Share: 0.8}, {Handle: "@x", Share: 0.2}},
			texts:  map[string]string{"single-owner": "Single owner risk: @solo wrote 80% of it; have a second person review and learn it"}},
		{name: "79% is not a single owner",
			edit:   func(*types.FileHeat) {},
			owners: []ownership.Owner{{Handle: "@solo", Share: 0.79}},
			hasnt:  []string{"single-owner"}},
		{name: "single owner needs 5 commits",
			edit:   func(f *types.FileHeat) { f.Factors.ChangeFrequency.CommitCount = 4 },
			owners: []ownership.Owner{{Handle: "@solo", Share: 1}},
			hasnt:  []string{"single-owner"}},
		{name: "knowledge spread needs at least 5 authors",
			edit:  func(f *types.FileHeat) { f.Factors.ChangeFrequency.UniqueAuthors = 4 },
			hasnt: []string{"knowledge-spread"}},
		{name: "protect",
			edit:   func(f *types.FileHeat) { f.Quadrant = types.QuadrantProtect },
			owners: twoOwners,
			has:    []string{"owner-review", "codeowners", "change-carefully", "stable-interface"},
			hasnt:  []string{"senior-review", "split"},
			texts:  map[string]string{"owner-review": "Owner review on every PR (@ana, @bo)"}},
		{name: "watch with recurring fixes",
			edit: func(f *types.FileHeat) {
				f.Quadrant = types.QuadrantWatch
				f.RecentChanges = append(f.RecentChanges, types.Change{Message: "fix: again"})
			},
			has:   []string{"ai-review", "refactor", "add-tests"},
			hasnt: []string{"codeowners", "senior-review", "regression-tests", "stable-interface"},
			texts: map[string]string{
				"refactor":  "Refactor to cut complexity: cyclomatic 1092 across 479 functions",
				"add-tests": "Add tests: 2 recent fixes suggest bugs recur",
			}},
		{name: "several fixes are counted in the regression-tests action",
			edit: func(f *types.FileHeat) {
				f.RecentChanges = append(f.RecentChanges, types.Change{Message: "fix: again"})
			},
			texts: map[string]string{"regression-tests": "Add regression tests around the 2 recent fixes"}},
		{name: "watch with one fix does not ask for tests",
			edit:  func(f *types.FileHeat) { f.Quadrant = types.QuadrantWatch },
			hasnt: []string{"add-tests", "regression-tests"}},
		{name: "low stakes is light review only",
			edit:  func(f *types.FileHeat) { f.Quadrant = types.QuadrantLow },
			texts: map[string]string{"light-review": "Light review; AI review is fine"}},
		{name: "not assessed asks for an LLM run first",
			edit:  func(f *types.FileHeat) { f.ImpactAssessed = false; f.Quadrant = "" },
			has:   []string{"assess-impact", "split", "regression-tests"},
			hasnt: []string{"senior-review", "codeowners"}},
		{name: "test file is light review only",
			edit:  func(f *types.FileHeat) { f.Path = "net/proxy_test.go" },
			texts: map[string]string{"light-review": "Light review; AI review is fine"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := hot()
			tt.edit(f)
			got := Explain(f, repo(f), tt.owners).Actions
			idset := map[string]bool{}
			for _, id := range ids(got) {
				if idset[id] {
					t.Errorf("duplicate action %s", id)
				}
				idset[id] = true
			}
			for _, id := range tt.has {
				if !idset[id] {
					t.Errorf("missing %s in %v", id, ids(got))
				}
			}
			for _, id := range tt.hasnt {
				if idset[id] {
					t.Errorf("unexpected %s in %v", id, ids(got))
				}
			}
			for id, w := range tt.texts {
				if g := text(got, id); g != w {
					t.Errorf("%s = %q, want %q", id, g, w)
				}
			}
		})
	}
	t.Run("low stakes and test files have exactly one action", func(t *testing.T) {
		for _, edit := range []func(*types.FileHeat){
			func(f *types.FileHeat) { f.Quadrant = types.QuadrantLow },
			func(f *types.FileHeat) { f.Path = "x_test.go" },
		} {
			f := hot()
			edit(f)
			if got := Explain(f, repo(f), nil).Actions; len(got) != 1 {
				t.Errorf("actions = %v, want one", ids(got))
			}
		}
	})
}
