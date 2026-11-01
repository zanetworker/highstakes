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

func find(as []types.Action, id string) types.Action {
	for _, a := range as {
		if a.ID == id {
			return a
		}
	}
	return types.Action{}
}

func text(as []types.Action, id string) string { return find(as, id).Text }

var twoOwners = []ownership.Owner{{Handle: "@ana", Share: 0.5}, {Handle: "@bo", Share: 0.3}, {Handle: "@cy", Share: 0.2}}

func TestActions_HardenNow(t *testing.T) {
	f := hot()
	got := Explain(f, repo(f), twoOwners).Actions
	want := []string{"senior-review", "regression-tests", "stable-interface", "no-auto-merge", "split", "codeowners", "document-invariants"}
	if strings.Join(ids(got), ",") != strings.Join(want, ",") {
		t.Fatalf("ids = %v, want %v", ids(got), want)
	}
	tests := []struct {
		id, text, scope, signal, command string
	}{
		{"senior-review", "Get sign-off from a senior owner (@ana, @bo)", types.ScopeBeforeMerge,
			"Harden now: likely to break and costly when it does", ""},
		{"regression-tests", "Add regression tests around the recent fix", types.ScopeBeforeMerge,
			`recent fix: "fix(network): align destination checks" (2026-10-28)`, ""},
		{"stable-interface", "Add contract tests for its public interface", types.ScopeBeforeMerge,
			"11 files import it (top 1%)", ""},
		{"no-auto-merge", "Keep auto-merge off", types.ScopeBeforeMerge, "Harden now files never auto-merge", ""},
		{"split", "Split it into smaller files", types.ScopeBacklog, "479 functions in one file (top 1%)",
			`gh issue create --title 'Split net/proxy.rs' --body '479 functions in one file (top 1%). Found by highstakes.'`},
		{"codeowners", "Add a CODEOWNERS line", types.ScopeBacklog, "not covered by CODEOWNERS", "/net/proxy.rs @ana @bo"},
		{"document-invariants", "Document its invariants", types.ScopeBacklog, "28 authors change it (top 1%)",
			`gh issue create --title 'Document the invariants of net/proxy.rs' --body '28 authors change it (top 1%). Found by highstakes.'`},
	}
	for _, tt := range tests {
		t.Run(tt.id, func(t *testing.T) {
			a := find(got, tt.id)
			if a.Text != tt.text || a.Scope != tt.scope || a.Signal != tt.signal || a.Command != tt.command {
				t.Errorf("got %+v\nwant text=%q scope=%q signal=%q command=%q", a, tt.text, tt.scope, tt.signal, tt.command)
			}
			if a.Done {
				t.Error("analyze-time actions are never done")
			}
		})
	}
	t.Run("review actions carry owners", func(t *testing.T) {
		if o := find(got, "senior-review").Owners; strings.Join(o, ",") != "@ana,@bo" {
			t.Errorf("owners = %v", o)
		}
	})
}

func TestActions_Rules(t *testing.T) {
	no := false
	tests := []struct {
		name      string
		edit      func(*types.FileHeat)
		owners    []ownership.Owner
		codeowned bool
		has       []string
		hasnt     []string
		texts     map[string]string
		signals   map[string]string
	}{
		{name: "senior review without known owners",
			edit: func(*types.FileHeat) {}, has: []string{"senior-review"},
			texts: map[string]string{"senior-review": "Get sign-off from a senior owner"}},
		{name: "split by cyclomatic when functions are not top 5%",
			edit:    func(f *types.FileHeat) { f.Factors.Complexity.FunctionCount = 1 },
			signals: map[string]string{"split": "cyclomatic 1092 in one file (top 1%)"}},
		{name: "no split, no contract tests when signals are ordinary",
			edit: func(f *types.FileHeat) {
				f.Factors.Complexity = types.Complexity{Cyclomatic: 1, FunctionCount: 1}
				f.Dependencies.ImportedBy = f.Dependencies.ImportedBy[:1]
			},
			hasnt: []string{"split", "stable-interface"}},
		{name: "codeowners left out when CODEOWNERS already covers the file",
			edit: func(*types.FileHeat) {}, codeowned: true, hasnt: []string{"codeowners"}},
		{name: "no regression tests without a fix; missing tests asks for tests first",
			edit: func(f *types.FileHeat) {
				f.RecentChanges = nil
				f.Factors.TestCoverage.HasTestFile = &no
			},
			has: []string{"no-tests"}, hasnt: []string{"regression-tests"},
			texts:   map[string]string{"no-tests": "Add tests before merge: none next to this file"},
			signals: map[string]string{"no-tests": "no test file next to it"}},
		{name: "several fixes are counted",
			edit: func(f *types.FileHeat) {
				f.RecentChanges = append(f.RecentChanges, types.Change{Message: "fix: again"})
			},
			texts: map[string]string{"regression-tests": "Add regression tests around the 2 recent fixes"}},
		{name: "one dominant owner asks to document invariants",
			edit:    func(f *types.FileHeat) { f.Factors.ChangeFrequency.UniqueAuthors = 2 },
			owners:  []ownership.Owner{{Handle: "@solo", Share: 0.85}},
			signals: map[string]string{"document-invariants": "@solo wrote 85% of it"}},
		{name: "few authors and no dominant owner: no invariants doc",
			edit:  func(f *types.FileHeat) { f.Factors.ChangeFrequency.UniqueAuthors = 4 },
			hasnt: []string{"document-invariants"}},
		{name: "protect",
			edit: func(f *types.FileHeat) {
				f.Quadrant = types.QuadrantProtect
				f.Factors.TestCoverage.HasTestFile = &no
			},
			owners: twoOwners,
			has:    []string{"owner-review", "no-auto-merge", "no-tests", "stable-interface", "codeowners", "test-gap"},
			hasnt:  []string{"senior-review", "split", "regression-tests", "document-invariants"},
			texts: map[string]string{
				"owner-review":  "Get an owner's review (@ana, @bo)",
				"no-auto-merge": "Block auto-merge",
				"test-gap":      "Open a test-gap issue",
			}},
		{name: "protect with tests next to it has no test gap",
			edit:  func(f *types.FileHeat) { f.Quadrant = types.QuadrantProtect },
			hasnt: []string{"no-tests", "test-gap"}},
		{name: "stabilize with recurring fixes, complexity and a single owner",
			edit: func(f *types.FileHeat) {
				f.Quadrant = types.QuadrantWatch
				f.RecentChanges = append(f.RecentChanges, types.Change{Message: "fix: again"})
			},
			owners: []ownership.Owner{{Handle: "@solo", Share: 0.8}},
			has:    []string{"regression-tests", "refactor", "single-owner", "complexity-issue", "ownership-issue"},
			hasnt:  []string{"codeowners", "senior-review", "no-auto-merge", "stable-interface"},
			texts: map[string]string{
				"refactor":     "Refactor the function you touch: cyclomatic 1092 across 479 functions",
				"single-owner": "Add a second reviewer: @solo wrote 80% of it",
			}},
		{name: "stabilize with one fix does not ask for tests",
			edit:  func(f *types.FileHeat) { f.Quadrant = types.QuadrantWatch },
			hasnt: []string{"regression-tests"}},
		{name: "79% is not a single owner",
			edit:   func(f *types.FileHeat) { f.Quadrant = types.QuadrantWatch },
			owners: []ownership.Owner{{Handle: "@solo", Share: 0.79}},
			hasnt:  []string{"single-owner", "ownership-issue"}},
		{name: "single owner needs 5 commits",
			edit: func(f *types.FileHeat) {
				f.Quadrant = types.QuadrantWatch
				f.Factors.ChangeFrequency.CommitCount = 4
			},
			owners: []ownership.Owner{{Handle: "@solo", Share: 1}},
			hasnt:  []string{"single-owner"}},
		{name: "routine stays quiet",
			edit:  func(f *types.FileHeat) { f.Quadrant = types.QuadrantLow },
			texts: map[string]string{"light-review": "Normal review; auto-merge allowed by policy"}},
		{name: "not assessed asks for an LLM run first",
			edit:  func(f *types.FileHeat) { f.ImpactAssessed = false; f.Quadrant = "" },
			has:   []string{"assess-impact", "split", "regression-tests"},
			hasnt: []string{"senior-review", "codeowners", "no-auto-merge"}},
		{name: "test file is routine",
			edit:  func(f *types.FileHeat) { f.Path = "net/proxy_test.go" },
			texts: map[string]string{"light-review": "Normal review; auto-merge allowed by policy"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := hot()
			tt.edit(f)
			rs := repo(f)
			if tt.codeowned {
				rs.Codeowned = func(string) bool { return true }
			}
			got := Explain(f, rs, tt.owners).Actions
			idset := map[string]bool{}
			for _, a := range got {
				if idset[a.ID] {
					t.Errorf("duplicate action %s", a.ID)
				}
				idset[a.ID] = true
				if a.Scope != types.ScopeBeforeMerge && a.Scope != types.ScopeBacklog {
					t.Errorf("%s: scope %q", a.ID, a.Scope)
				}
				if a.Signal == "" {
					t.Errorf("%s: no signal", a.ID)
				}
				if a.Scope == types.ScopeBacklog && a.Command == "" {
					t.Errorf("%s: backlog item without a command", a.ID)
				}
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
			for id, w := range tt.signals {
				if g := find(got, id).Signal; g != w {
					t.Errorf("%s signal = %q, want %q", id, g, w)
				}
			}
		})
	}
	t.Run("routine and test files have exactly one before-merge action and no backlog", func(t *testing.T) {
		for _, edit := range []func(*types.FileHeat){
			func(f *types.FileHeat) { f.Quadrant = types.QuadrantLow },
			func(f *types.FileHeat) { f.Path = "x_test.go" },
		} {
			f := hot()
			edit(f)
			got := Explain(f, repo(f), nil).Actions
			if len(got) != 1 || got[0].Scope != types.ScopeBeforeMerge {
				t.Errorf("actions = %+v, want one before-merge action", got)
			}
		}
	})
	t.Run("before-merge actions come before backlog", func(t *testing.T) {
		f := hot()
		got := Explain(f, repo(f), twoOwners).Actions
		seenBacklog := false
		for _, a := range got {
			if a.Scope == types.ScopeBacklog {
				seenBacklog = true
			} else if seenBacklog {
				t.Errorf("%s (before merge) listed after a backlog item", a.ID)
			}
		}
	})
}

func TestShellQuote(t *testing.T) {
	tests := []struct{ in, want string }{
		{"plain", "'plain'"},
		{"it's", `'it'\''s'`},
		{"", "''"},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			if got := shellQuote(tt.in); got != tt.want {
				t.Errorf("shellQuote(%q) = %s, want %s", tt.in, got, tt.want)
			}
		})
	}
}

func TestScopeOf(t *testing.T) {
	tests := []struct{ id, want string }{
		{"senior-review", types.ScopeBeforeMerge},
		{"no-auto-merge", types.ScopeBeforeMerge},
		{"codeowners", types.ScopeBacklog},
		{"ownership-issue", types.ScopeBacklog},
		{"retired-rule", types.ScopeBeforeMerge},
		{"", types.ScopeBeforeMerge},
	}
	for _, tt := range tests {
		t.Run(tt.id, func(t *testing.T) {
			if got := ScopeOf(tt.id); got != tt.want {
				t.Errorf("ScopeOf(%q) = %q, want %q", tt.id, got, tt.want)
			}
		})
	}
}
