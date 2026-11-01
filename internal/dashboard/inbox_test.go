package dashboard

import (
	"fmt"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/zanetworker/highstakes/internal/types"
	"github.com/zanetworker/highstakes/pkg/heatmap"
)

func inboxFile(path string, q types.Quadrant, impact, likelihood int, acts ...types.Action) *types.FileHeat {
	return &types.FileHeat{Path: path, Quadrant: q, ImpactAssessed: true, Impact: impact, Likelihood: likelihood,
		Explanation: &types.Explanation{Verdict: q.Label(), Actions: acts}}
}

var (
	reviewAct = types.Action{ID: "senior-review", Text: "Get sign-off from a senior owner (@ana)", Scope: types.ScopeBeforeMerge,
		Signal: "Harden now: likely to break and costly when it does", Owners: []string{"@ana", "@bo"}}
	testsAct  = types.Action{ID: "regression-tests", Text: "Add regression tests around the recent fix", Scope: types.ScopeBeforeMerge, Signal: "recent fix"}
	ownerAct  = types.Action{ID: "owner-review", Text: "Get an owner's review", Scope: types.ScopeBeforeMerge, Signal: "Protect", Owners: []string{"@cy"}}
	splitAct  = types.Action{ID: "split", Text: "Split it into smaller files", Scope: types.ScopeBacklog, Signal: "479 functions", Command: "gh issue create --title 'Split a.rs'"}
	cutCxAct  = types.Action{ID: "complexity-issue", Text: "Open an issue to cut its complexity", Scope: types.ScopeBacklog, Signal: "cyclomatic", Command: "gh issue create"}
	invarAct  = types.Action{ID: "document-invariants", Text: "Document its invariants", Scope: types.ScopeBacklog, Signal: "9 authors", Command: "gh issue create --title 'Document'"}
	routineOk = types.Action{ID: "light-review", Text: "Normal review", Scope: types.ScopeBeforeMerge}
)

func codeownersAct(path string) types.Action {
	return types.Action{ID: "codeowners", Text: "Add a CODEOWNERS line", Scope: types.ScopeBacklog, Signal: "not covered by CODEOWNERS", Command: "/" + path + " @ana"}
}

func paths(cards []fileCard) []string {
	out := []string{}
	for _, c := range cards {
		out = append(out, c.Path)
	}
	return out
}

func TestBuildInbox_BeforeMergeOneCardPerFile(t *testing.T) {
	hm := &types.Heatmap{Files: map[string]*types.FileHeat{
		"a.rs":      inboxFile("a.rs", types.QuadrantFragileCritical, 92, 50, reviewAct, testsAct, splitAct),
		"p.rs":      inboxFile("p.rs", types.QuadrantProtect, 95, 5, ownerAct),
		"b.rs":      inboxFile("b.rs", types.QuadrantFragileCritical, 80, 60, testsAct),
		"c.rs":      inboxFile("c.rs", types.QuadrantFragileCritical, 80, 70, testsAct),
		"s.rs":      inboxFile("s.rs", types.QuadrantWatch, 10, 90, cutCxAct),
		"r.rs":      inboxFile("r.rs", types.QuadrantLow, 5, 5, routineOk),
		"old.rs":    {Path: "old.rs"}, // no explanation
		"x_test.rs": inboxFile("x_test.rs", types.QuadrantFragileCritical, 90, 90, testsAct),
	}}
	in := buildInbox(hm)

	t.Run("Harden now first, then impact, then likelihood; no Stabilize, routine or tests", func(t *testing.T) {
		if got, want := paths(in.BeforeMerge), []string{"a.rs", "c.rs", "b.rs", "p.rs"}; !reflect.DeepEqual(got, want) {
			t.Errorf("order = %v, want %v", got, want)
		}
	})
	t.Run("card carries every before-merge step and the owners", func(t *testing.T) {
		want := fileCard{Path: "a.rs", Verdict: "Harden now", Owners: []string{"@ana", "@bo"},
			Steps: []step{{Text: reviewAct.Text, Signal: reviewAct.Signal}, {Text: testsAct.Text, Signal: testsAct.Signal}}}
		if !reflect.DeepEqual(in.BeforeMerge[0], want) {
			t.Errorf("first card = %+v, want %+v", in.BeforeMerge[0], want)
		}
	})
	t.Run("fewer files than the default top", func(t *testing.T) {
		if in.Top != 4 {
			t.Errorf("top = %d, want all 4 files", in.Top)
		}
	})
	t.Run("empty heatmap", func(t *testing.T) {
		in := buildInbox(&types.Heatmap{})
		if len(in.BeforeMerge) != 0 || len(in.Backlog) != 0 || in.Top != 0 || in.BeforeMerge == nil || in.Backlog == nil {
			t.Errorf("got %+v, want empty non-nil lists and top 0", in)
		}
	})
}

func TestBuildInbox_TopWithTies(t *testing.T) {
	// n Harden now files with distinct impacts 99, 98, ..., then tied files
	// that all equal the last distinct one.
	build := func(distinct, tied int) *types.Heatmap {
		hm := &types.Heatmap{Files: map[string]*types.FileHeat{}}
		for i := 0; i < distinct; i++ {
			p := fmt.Sprintf("d%02d.rs", i)
			hm.Files[p] = inboxFile(p, types.QuadrantFragileCritical, 99-i, 50, testsAct)
		}
		for i := 0; i < tied; i++ {
			p := fmt.Sprintf("t%02d.rs", i)
			hm.Files[p] = inboxFile(p, types.QuadrantFragileCritical, 99-distinct+1, 50, testsAct)
		}
		return hm
	}
	tests := []struct {
		name             string
		distinct, tied   int
		wantTop, wantAll int
	}{
		{"exactly ten", 10, 0, 10, 10},
		{"eleven distinct: cut at ten", 11, 0, 10, 11},
		{"ties across the boundary are kept together", 9, 3, 12, 12},
		{"tie ends before the boundary", 5, 1, 10, 15},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			hm := build(tc.distinct, tc.tied)
			if tc.name == "tie ends before the boundary" {
				for i := 0; i < 9; i++ { // more files below the tie
					p := fmt.Sprintf("z%02d.rs", i)
					hm.Files[p] = inboxFile(p, types.QuadrantFragileCritical, 50-i, 50, testsAct)
				}
			}
			in := buildInbox(hm)
			if in.Top != tc.wantTop || len(in.BeforeMerge) != tc.wantAll {
				t.Errorf("top = %d of %d, want %d of %d", in.Top, len(in.BeforeMerge), tc.wantTop, tc.wantAll)
			}
		})
	}
}

func TestBuildInbox_BacklogGroups(t *testing.T) {
	hm := &types.Heatmap{Files: map[string]*types.FileHeat{
		"z.rs": inboxFile("z.rs", types.QuadrantFragileCritical, 90, 80, reviewAct, codeownersAct("z.rs"), invarAct),
		"a.rs": inboxFile("a.rs", types.QuadrantFragileCritical, 70, 80, codeownersAct("a.rs"), invarAct, splitAct),
		"m.rs": inboxFile("m.rs", types.QuadrantProtect, 95, 5, codeownersAct("m.rs")),
		"n.rs": inboxFile("n.rs", types.QuadrantProtect, 94, 5, codeownersAct("n.rs")),
		"s.rs": inboxFile("s.rs", types.QuadrantWatch, 10, 90, cutCxAct),
	}}
	in := buildInbox(hm)

	t.Run("one group per action kind, most Harden now files first, then most files", func(t *testing.T) {
		var got []string
		for _, g := range in.Backlog {
			got = append(got, g.Kind)
		}
		want := []string{"codeowners", "document-invariants", "split", "complexity-issue"}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("kinds = %v, want %v", got, want)
		}
	})
	byKind := map[string]group{}
	for _, g := range in.Backlog {
		byKind[g.Kind] = g
	}
	t.Run("CODEOWNERS group has one sorted snippet with every line", func(t *testing.T) {
		g := byKind["codeowners"]
		if g.Title != "Add 4 CODEOWNERS lines" || g.Harden != 2 {
			t.Errorf("title %q harden %d, want \"Add 4 CODEOWNERS lines\" and 2", g.Title, g.Harden)
		}
		if want := "/a.rs @ana\n/m.rs @ana\n/n.rs @ana\n/z.rs @ana"; g.Snippet != want {
			t.Errorf("snippet = %q, want %q", g.Snippet, want)
		}
		var files []string
		for _, it := range g.Items {
			files = append(files, it.Path)
		}
		if want := []string{"z.rs", "a.rs", "m.rs", "n.rs"}; !reflect.DeepEqual(files, want) {
			t.Errorf("items = %v, want most severe first %v", files, want)
		}
	})
	t.Run("items keep signal, verdict, owner and command", func(t *testing.T) {
		it := byKind["split"].Items[0]
		want := groupItem{Path: "a.rs", Verdict: "Harden now", Signal: splitAct.Signal, Command: splitAct.Command}
		if !reflect.DeepEqual(it, want) {
			t.Errorf("item = %+v, want %+v", it, want)
		}
		if z := byKind["document-invariants"].Items[0]; z.Owner != "@ana" {
			t.Errorf("owner = %q, want the file's first owner @ana", z.Owner)
		}
	})
	t.Run("titles count files with singular and plural", func(t *testing.T) {
		for kind, want := range map[string]string{
			"split": "Split 1 oversized file", "document-invariants": "Document invariants for 2 files",
			"complexity-issue": "Cut complexity in 1 file",
		} {
			if got := byKind[kind].Title; got != want {
				t.Errorf("%s title = %q, want %q", kind, got, want)
			}
		}
	})
	t.Run("unknown kind falls back to the action text", func(t *testing.T) {
		odd := types.Action{ID: "new-kind", Text: "Do the new thing", Scope: types.ScopeBacklog}
		in := buildInbox(&types.Heatmap{Files: map[string]*types.FileHeat{"q.rs": inboxFile("q.rs", types.QuadrantProtect, 90, 5, odd)}})
		if len(in.Backlog) != 1 || in.Backlog[0].Title != "Do the new thing: 1 file" {
			t.Errorf("backlog = %+v", in.Backlog)
		}
	})
}

// TestBuildInbox_LegacyHeatmap: actions stored before scopes existed still
// fill both inbox sections once loaded.
func TestBuildInbox_LegacyHeatmap(t *testing.T) {
	hm, err := heatmap.Load(filepath.Join("..", "..", "pkg", "heatmap", "testdata", "legacy-heatmap.json"))
	if err != nil {
		t.Fatal(err)
	}
	in := buildInbox(hm)
	if len(in.BeforeMerge) != 1 || len(in.BeforeMerge[0].Steps) != 2 {
		t.Errorf("before merge = %+v, want one card with 2 steps", in.BeforeMerge)
	}
	var kinds []string
	for _, g := range in.Backlog {
		kinds = append(kinds, g.Kind)
	}
	if len(kinds) != 2 {
		t.Errorf("backlog kinds = %v, want codeowners and split", kinds)
	}
}

func TestBuildInbox_StaticOnly(t *testing.T) {
	noTestsAct := types.Action{ID: "no-tests", Text: "Add tests before merge: none next to this file", Scope: types.ScopeBeforeMerge, Signal: "no test file next to it"}
	assessAct := types.Action{ID: "assess-impact", Text: "Run analyze with an LLM to assess impact", Scope: types.ScopeBacklog, Command: "highstakes analyze"}
	estimated := func(path string, impact, likelihood int) *types.FileHeat {
		return &types.FileHeat{Path: path, Impact: impact, Likelihood: likelihood,
			Explanation: &types.Explanation{Verdict: "Not assessed", Actions: []types.Action{noTestsAct, assessAct}}}
	}
	meta := types.Metadata{LikelyCutoff: 60, ImpactThreshold: 70}

	t.Run("no LLM: cards by estimated quadrant, marked as estimates", func(t *testing.T) {
		hm := &types.Heatmap{Metadata: meta, Files: map[string]*types.FileHeat{
			"hot.go":    estimated("hot.go", 80, 90),    // likely, impactful: Harden now
			"core.go":   estimated("core.go", 75, 10),   // impactful only: Protect
			"churn.go":  estimated("churn.go", 20, 95),  // likely only: Stabilize, backlog only
			"quiet.go":  estimated("quiet.go", 10, 10),  // Routine: left out
			"x_test.go": estimated("x_test.go", 99, 99), // tests: left out
		}}
		in := buildInbox(hm)
		if got, want := paths(in.BeforeMerge), []string{"hot.go", "core.go"}; !reflect.DeepEqual(got, want) {
			t.Fatalf("before merge = %v, want %v", got, want)
		}
		if c := in.BeforeMerge[0]; c.Verdict != "Harden now" || !c.Estimated {
			t.Errorf("first card = %+v, want Harden now marked estimated", c)
		}
		if in.BeforeMerge[1].Verdict != "Protect" {
			t.Errorf("second card verdict = %q, want Protect", in.BeforeMerge[1].Verdict)
		}
		if in.EstimateNote != estimateNote {
			t.Errorf("estimate note = %q, want %q", in.EstimateNote, estimateNote)
		}
		if len(in.Backlog) != 1 || len(in.Backlog[0].Items) != 3 || in.Backlog[0].Harden != 1 || !in.Backlog[0].Items[0].Estimated {
			t.Errorf("backlog = %+v, want one assess-impact group over 3 estimated files, 1 Harden now", in.Backlog)
		}
	})
	t.Run("LLM-assessed files rank before estimated ones", func(t *testing.T) {
		hm := &types.Heatmap{Metadata: meta, Files: map[string]*types.FileHeat{
			"hot.go": estimated("hot.go", 95, 95),
			"p.rs":   inboxFile("p.rs", types.QuadrantProtect, 72, 5, ownerAct),
		}}
		in := buildInbox(hm)
		if got, want := paths(in.BeforeMerge), []string{"p.rs", "hot.go"}; !reflect.DeepEqual(got, want) {
			t.Errorf("before merge = %v, want %v", got, want)
		}
		if in.BeforeMerge[0].Estimated || !in.BeforeMerge[1].Estimated {
			t.Errorf("estimated flags = %v, %v; want false, true", in.BeforeMerge[0].Estimated, in.BeforeMerge[1].Estimated)
		}
	})
	t.Run("no cutoffs recorded: nothing to estimate from", func(t *testing.T) {
		hm := &types.Heatmap{Files: map[string]*types.FileHeat{"hot.go": estimated("hot.go", 95, 95)}}
		if in := buildInbox(hm); len(in.BeforeMerge) != 0 || in.EstimateNote != "" {
			t.Errorf("got %+v, want an empty inbox without the estimate note", in)
		}
	})
}
