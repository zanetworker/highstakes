package dashboard

import (
	"strings"
	"testing"

	"github.com/zanetworker/highstakes/internal/types"
)

func inboxFile(path string, q types.Quadrant, impact, likelihood int, acts ...types.Action) *types.FileHeat {
	return &types.FileHeat{Path: path, Quadrant: q, ImpactAssessed: true, Impact: impact, Likelihood: likelihood,
		Explanation: &types.Explanation{Verdict: q.Label(), Actions: acts}}
}

func TestBuildInbox(t *testing.T) {
	review := types.Action{ID: "senior-review", Text: "Get sign-off from a senior owner (@ana)", Scope: types.ScopeBeforeMerge,
		Signal: "Harden now: likely to break and costly when it does", Owners: []string{"@ana"}}
	tests := types.Action{ID: "regression-tests", Text: "Add regression tests around the recent fix", Scope: types.ScopeBeforeMerge, Signal: "recent fix"}
	split := types.Action{ID: "split", Text: "Split it into smaller files", Scope: types.ScopeBacklog, Signal: "479 functions", Command: "gh issue create --title 'Split a.rs'"}
	hm := &types.Heatmap{Files: map[string]*types.FileHeat{
		"a.rs":      inboxFile("a.rs", types.QuadrantFragileCritical, 92, 50, review, tests, split),
		"p.rs":      inboxFile("p.rs", types.QuadrantProtect, 95, 5, types.Action{ID: "owner-review", Text: "Get an owner's review", Scope: types.ScopeBeforeMerge, Signal: "Protect"}),
		"b.rs":      inboxFile("b.rs", types.QuadrantFragileCritical, 80, 60, tests),
		"s.rs":      inboxFile("s.rs", types.QuadrantWatch, 10, 90, types.Action{ID: "complexity-issue", Text: "Open an issue to cut its complexity", Scope: types.ScopeBacklog, Signal: "cyclomatic", Command: "gh issue create"}),
		"r.rs":      inboxFile("r.rs", types.QuadrantLow, 5, 5, types.Action{ID: "light-review", Text: "Normal review", Scope: types.ScopeBeforeMerge}),
		"old.rs":    {Path: "old.rs"}, // no explanation
		"x_test.rs": inboxFile("x_test.rs", types.QuadrantFragileCritical, 90, 90, tests),
	}}
	in := buildInbox(hm)

	var order []string
	for _, c := range in.BeforeMerge {
		order = append(order, c.Path)
	}
	if strings.Join(order, ",") != "a.rs,b.rs,p.rs" {
		t.Errorf("before-merge files = %v, want Harden now by impact, then Protect; no Stabilize, routine or tests", order)
	}
	first := in.BeforeMerge[0]
	if first.Text != review.Text || first.Signal != review.Signal || first.Owner != "@ana" || first.Verdict != "Harden now" || first.More != 1 {
		t.Errorf("first card = %+v, want the top before-merge action, its owner and one more", first)
	}
	if len(in.Backlog) != 2 || in.Backlog[0].Path != "a.rs" || in.Backlog[0].Command == "" || in.Backlog[0].Owner != "@ana" {
		t.Errorf("backlog = %+v, want one card per backlog action with command and file owner, most severe first", in.Backlog)
	}
	if in.Backlog[1].Path != "s.rs" {
		t.Errorf("Stabilize files belong on the backlog: %+v", in.Backlog[1])
	}
	t.Run("empty heatmap", func(t *testing.T) {
		in := buildInbox(&types.Heatmap{})
		if len(in.BeforeMerge) != 0 || len(in.Backlog) != 0 {
			t.Errorf("got %+v", in)
		}
	})
}
