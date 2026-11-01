package explain

import (
	"strings"
	"testing"

	"github.com/zanetworker/highstakes/internal/types"
)

// explained returns hot() with its analyze-time explanation attached.
func explained(edit func(*types.FileHeat)) *types.FileHeat {
	f := hot()
	edit(f)
	e := Explain(f, repo(f), twoOwners)
	f.Explanation = &e
	return f
}

func TestForPR(t *testing.T) {
	small := Change{Paths: []string{"net/proxy.rs"}, Lines: 40, Components: 1}
	tests := []struct {
		name     string
		edit     func(*types.FileHeat)
		change   Change
		done     []string
		open     []string
		absent   []string
		smallerT string
		signals  map[string]string // id -> wanted signal
		texts    map[string]string // id -> wanted text
	}{
		{name: "nothing satisfied yet",
			edit: func(*types.FileHeat) {}, change: small,
			open: []string{"senior-review", "regression-tests", "stable-interface", "no-auto-merge", "codeowners"}, absent: []string{"smaller-pr"}},
		{name: "adjacent test modified marks test actions done",
			edit:   func(*types.FileHeat) {},
			change: Change{Paths: []string{"net/proxy.rs", "net/proxy/tests/compat.rs"}, Lines: 40, Components: 1},
			done:   []string{"regression-tests", "stable-interface"}, open: []string{"senior-review"}},
		{name: "deleted adjacent test does not count",
			edit: func(*types.FileHeat) {},
			change: Change{Paths: []string{"net/proxy.rs", "net/proxy/tests/compat.rs"}, Lines: 40, Components: 1,
				Deleted: map[string]bool{"net/proxy/tests/compat.rs": true}},
			open: []string{"regression-tests", "stable-interface"}},
		{name: "test renamed next to the file counts at its new path",
			edit: func(*types.FileHeat) {},
			change: Change{Paths: []string{"net/proxy.rs", "old/compat_test.rs", "net/proxy/tests/compat.rs"}, Lines: 40, Components: 1,
				Deleted: map[string]bool{"old/compat_test.rs": true}},
			done: []string{"regression-tests"}},
		{name: "test renamed away from the file does not count",
			edit: func(*types.FileHeat) {},
			change: Change{Paths: []string{"net/proxy.rs", "net/proxy/tests/compat.rs", "other/compat_test.rs"}, Lines: 40, Components: 1,
				Deleted: map[string]bool{"net/proxy/tests/compat.rs": true}},
			open: []string{"regression-tests"}},
		{name: "unrelated test does not count",
			edit:   func(*types.FileHeat) {},
			change: Change{Paths: []string{"net/proxy.rs", "other/x_test.go"}, Lines: 40, Components: 1},
			open:   []string{"regression-tests"}},
		{name: "approval by a suggested owner marks the sign-off done",
			edit:   func(*types.FileHeat) {},
			change: Change{Paths: small.Paths, Lines: 40, Components: 1, Approvals: []string{"zed", "Ana"}},
			done:   []string{"senior-review"}},
		{name: "approval by someone else does not",
			edit:   func(*types.FileHeat) {},
			change: Change{Paths: small.Paths, Lines: 40, Components: 1, Approvals: []string{"zed"}},
			open:   []string{"senior-review"}},
		{name: "CODEOWNERS covering the path at HEAD marks codeowners done",
			edit:   func(*types.FileHeat) {},
			change: Change{Paths: small.Paths, Lines: 40, Components: 1, Codeowned: func(p string) bool { return p == "net/proxy.rs" }},
			done:   []string{"codeowners"}},
		{name: "large PR on a Harden now file asks for a smaller PR",
			edit:     func(*types.FileHeat) {},
			change:   Change{Paths: small.Paths, Lines: 501, Components: 1},
			open:     []string{"smaller-pr"},
			smallerT: "Ask for a smaller PR: 501 lines across 1 component"},
		{name: "a huge PR reads with thousands separators",
			edit:     func(*types.FileHeat) {},
			change:   Change{Paths: small.Paths, Lines: 43274, Components: 4},
			open:     []string{"smaller-pr"},
			smallerT: "Ask for a smaller PR: 43,274 lines across 4 components"},
		{name: "500 lines is not large",
			edit:   func(*types.FileHeat) {},
			change: Change{Paths: small.Paths, Lines: 500, Components: 2},
			absent: []string{"smaller-pr"}},
		{name: "three components is unrelated work",
			edit:     func(*types.FileHeat) {},
			change:   Change{Paths: small.Paths, Lines: 10, Components: 3},
			open:     []string{"smaller-pr"},
			smallerT: "Ask for a smaller PR: 10 lines across 3 components"},
		{name: "sign-off label marks the senior sign-off done",
			edit:    func(*types.FileHeat) {},
			change:  Change{Paths: small.Paths, Lines: 40, Components: 1, SignedOff: true},
			done:    []string{"senior-review"},
			open:    []string{"regression-tests", "no-auto-merge"},
			signals: map[string]string{"senior-review": "signed off with the highstakes:signed-off label"}},
		{name: "sign-off label marks a Protect owner review done",
			edit:    func(f *types.FileHeat) { f.Quadrant = types.QuadrantProtect },
			change:  Change{Paths: small.Paths, Lines: 40, Components: 1, SignedOff: true},
			done:    []string{"owner-review"},
			signals: map[string]string{"owner-review": "signed off with the highstakes:signed-off label"}},
		{name: "an owner's approval keeps its own signal when also signed off",
			edit:    func(*types.FileHeat) {},
			change:  Change{Paths: small.Paths, Lines: 40, Components: 1, Approvals: []string{"ana"}, SignedOff: true},
			done:    []string{"senior-review"},
			signals: map[string]string{"senior-review": "Harden now: likely to break and costly when it does"}},
		{name: "auto-merge off marks keep auto-merge off done",
			edit:    func(*types.FileHeat) {},
			change:  Change{Paths: small.Paths, Lines: 40, Components: 1, AutoMerge: AutoMergeOff},
			done:    []string{"no-auto-merge"},
			open:    []string{"senior-review"},
			signals: map[string]string{"no-auto-merge": "auto-merge is not enabled on this PR"},
			texts:   map[string]string{"no-auto-merge": "Keep auto-merge off"}},
		{name: "auto-merge off marks a Protect block auto-merge done",
			edit:   func(f *types.FileHeat) { f.Quadrant = types.QuadrantProtect },
			change: Change{Paths: small.Paths, Lines: 40, Components: 1, AutoMerge: AutoMergeOff},
			done:   []string{"no-auto-merge"},
			texts:  map[string]string{"no-auto-merge": "Block auto-merge"}},
		{name: "auto-merge on stays open and says to turn it off",
			edit:    func(*types.FileHeat) {},
			change:  Change{Paths: small.Paths, Lines: 40, Components: 1, AutoMerge: AutoMergeOn},
			open:    []string{"no-auto-merge"},
			signals: map[string]string{"no-auto-merge": "auto-merge is enabled on this PR"},
			texts:   map[string]string{"no-auto-merge": "Turn auto-merge off: it is enabled on this PR"}},
		{name: "unknown auto-merge state stays open as before",
			edit:    func(*types.FileHeat) {},
			change:  Change{Paths: small.Paths, Lines: 40, Components: 1, AutoMerge: AutoMergeUnknown},
			open:    []string{"no-auto-merge"},
			signals: map[string]string{"no-auto-merge": "Harden now files never auto-merge"},
			texts:   map[string]string{"no-auto-merge": "Keep auto-merge off"}},
		{name: "protect files never ask for a smaller PR",
			edit:   func(f *types.FileHeat) { f.Quadrant = types.QuadrantProtect },
			change: Change{Paths: small.Paths, Lines: 900, Components: 5},
			absent: []string{"smaller-pr"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ForPR(explained(tt.edit), tt.change)
			for _, id := range tt.done {
				if a := find(got, id); a.ID == "" || !a.Done {
					t.Errorf("%s should be done: %+v", id, a)
				}
			}
			for _, id := range tt.open {
				if a := find(got, id); a.ID == "" || a.Done {
					t.Errorf("%s should be open: %+v (all: %v)", id, a, ids(got))
				}
			}
			for _, id := range tt.absent {
				if find(got, id).ID != "" {
					t.Errorf("unexpected %s", id)
				}
			}
			for id, want := range tt.signals {
				if a := find(got, id); a.Signal != want {
					t.Errorf("%s signal = %q, want %q", id, a.Signal, want)
				}
			}
			for id, want := range tt.texts {
				if a := find(got, id); a.Text != want {
					t.Errorf("%s text = %q, want %q", id, a.Text, want)
				}
			}
			if tt.smallerT != "" {
				a := find(got, "smaller-pr")
				if a.Text != tt.smallerT || a.Scope != types.ScopeBeforeMerge || a.Signal == "" {
					t.Errorf("smaller-pr = %+v, want text %q", a, tt.smallerT)
				}
			}
		})
	}
	t.Run("does not mutate the stored explanation", func(t *testing.T) {
		f := explained(func(*types.FileHeat) {})
		ForPR(f, Change{Paths: []string{"net/proxy_tests.rs"}, Approvals: []string{"ana"}})
		for _, a := range f.Explanation.Actions {
			if a.Done {
				t.Errorf("%s marked done in the heatmap", a.ID)
			}
		}
	})
	t.Run("no explanation yields no actions", func(t *testing.T) {
		if got := ForPR(hot(), small); got != nil {
			t.Errorf("got %v", got)
		}
	})
}

func TestChecklist(t *testing.T) {
	files := []FileActions{
		{Path: "a.rs", Blocking: true, Actions: []types.Action{
			{ID: "no-auto-merge", Text: "Keep auto-merge off", Scope: types.ScopeBeforeMerge, Done: false},
			{ID: "regression-tests", Text: "Add regression tests around the recent fix", Scope: types.ScopeBeforeMerge, Done: true},
			{ID: "codeowners", Text: "Add a CODEOWNERS line", Scope: types.ScopeBacklog, Command: "/a.rs @ana"},
		}},
		{Path: "b.rs", Blocking: true, Actions: []types.Action{
			{ID: "no-auto-merge", Text: "Keep auto-merge off", Scope: types.ScopeBeforeMerge},
			{ID: "regression-tests", Text: "Add regression tests around the recent fix", Scope: types.ScopeBeforeMerge, Done: false},
			{ID: "codeowners", Text: "Add a CODEOWNERS line", Scope: types.ScopeBacklog, Command: "/b.rs @bo"},
		}},
		{Path: "c.rs", Actions: []types.Action{
			{ID: "light-review", Text: "Normal review; auto-merge allowed by policy", Scope: types.ScopeBeforeMerge},
		}},
		{Path: "s.go", Actions: []types.Action{
			{ID: "refactor", Text: "Refactor the function you touch", Scope: types.ScopeBeforeMerge},
			{ID: "regression-tests", Text: "Add regression tests around the recent fix", Scope: types.ScopeBeforeMerge},
			{ID: "complexity-issue", Text: "Open an issue to cut its complexity", Scope: types.ScopeBacklog, Command: "gh issue create"},
		}},
	}
	before, worth, backlog := Checklist(files)
	if len(before) != 2 {
		t.Fatalf("before = %+v, want 2 deduplicated items from blocking files only", before)
	}
	if before[0].ID != "no-auto-merge" || strings.Join(before[0].Files, ",") != "a.rs,b.rs" {
		t.Errorf("first item = %+v", before[0])
	}
	if before[1].Done {
		t.Error("an item is done only when it is done for every file")
	}
	if len(worth) != 2 || worth[0].ID != "refactor" || strings.Join(worth[1].Files, ",") != "s.go" {
		t.Errorf("worth = %+v, want refactor and regression-tests for s.go only", worth)
	}
	if len(backlog) != 3 {
		t.Errorf("backlog = %+v, want one per file (commands differ)", backlog)
	}
	t.Run("empty input", func(t *testing.T) {
		b, w, l := Checklist(nil)
		if b != nil || w != nil || l != nil {
			t.Errorf("got %v %v %v", b, w, l)
		}
	})
}
