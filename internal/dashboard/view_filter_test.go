package dashboard

import (
	"reflect"
	"testing"

	"github.com/zanetworker/highstakes/internal/types"
)

func TestBuildArchitecture_Supporting(t *testing.T) {
	lang := func(p, l string) *types.FileHeat {
		f := fh(p, 10, types.TierLow, 10)
		f.Language = l
		return f
	}
	hm := &types.Heatmap{Files: map[string]*types.FileHeat{}}
	for _, f := range []*types.FileHeat{
		lang("cmd/main.go", "Go"),
		lang("api/a.go", "Go"),
		lang("api/cfg.yaml", "YAML"), // code next to config: still product
		lang("docs/gen.go", "Go"),
		lang("internal/x/testdata/a.go", "Go"),
		lang("bench/run.go", "Go"),
		lang(".github/workflows/ci.yml", "YAML"),
		lang("charts/app/values.yaml", "YAML"), // config only: non-code
		lang("charts/app/Dockerfile", "Dockerfile"),
		lang("config/defaults.toml", "TOML"),
		lang("schema/s.json", "JSON"),
		lang("legacy/x", ""), // unknown language is not assumed non-code
	} {
		hm.Files[f.Path] = f
	}
	want := map[string]bool{
		"cmd": false, "api": false, "legacy": false,
		"docs": true, "internal/x/testdata": true, "bench": true, ".github/workflows": true,
		"charts/app": true, "config": true, "schema": true,
	}
	got := map[string]bool{}
	for _, n := range buildArchitecture(hm).Nodes {
		got[n.ID] = n.Supporting
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("supporting = %v, want %v", got, want)
	}
}

func TestBuildUnitView_Supporting(t *testing.T) {
	hm := unitsHeatmap()
	hm.Metadata.Units[2].Supporting = true // sbx-perf, a helper bin
	view, _ := buildUnitView(hm)
	got := map[string]bool{}
	for _, n := range view.Nodes {
		got[n.ID] = n.Supporting
	}
	want := map[string]bool{"u:sbx": false, "u:sup": false, "u:sbx-perf": true, "c:crates/core": false}
	for id, w := range want {
		if g, ok := got[id]; !ok || g != w {
			t.Errorf("%s supporting = %v (present %v), want %v", id, g, ok, w)
		}
	}
}

func TestKeyEdges(t *testing.T) {
	node := func(id, impactTier string, supporting bool) archNode {
		return archNode{ID: id, ImpactTier: impactTier, Supporting: supporting}
	}
	nodes := []archNode{
		node("a", "low", false), node("b", "low", false), node("c", "medium", false),
		node("hot", "critical", false), node("prot", "high", false), node("docs", "low", true),
	}
	edges := []archEdge{
		{From: "a", To: "b", Count: 9},       // 0: strongest
		{From: "a", To: "c", Count: 5},       // 1: tied, wins on name
		{From: "b", To: "c", Count: 5},       // 2: tied, loses on name
		{From: "c", To: "hot", Count: 3},     // 3: Harden now's strongest edge
		{From: "b", To: "hot", Count: 1},     // 4: Harden now's weaker edge
		{From: "prot", To: "a", Count: 1},    // 5: Protect's only edge
		{From: "docs", To: "a", Count: 20},   // 6: supporting endpoint
		{From: "docs", To: "prot", Count: 2}, // 7: supporting, Protect's strongest when shown
	}
	tests := []struct {
		name           string
		top            int
		withSupporting bool
		want           []bool
	}{
		{"top 2 plus the strongest edge of each risky node", 2, false, []bool{true, true, false, true, false, true, false, false}},
		{"top 0 keeps one edge per risky node", 0, false, []bool{false, false, false, true, false, true, false, false}},
		{"top above edge count keeps every visible edge", 50, false, []bool{true, true, true, true, true, true, false, false}},
		{"supporting shown competes for the top and for risky nodes", 2, true, []bool{true, false, false, true, false, false, true, true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := keyEdges(nodes, edges, tt.top, tt.withSupporting); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("keyEdges = %v, want %v", got, tt.want)
			}
		})
	}
	t.Run("no edges", func(t *testing.T) {
		if got := keyEdges(nodes, nil, 15, false); len(got) != 0 {
			t.Errorf("keyEdges(nil) = %v, want empty", got)
		}
	})
}

func TestBuildArchitecture_MarksKeyEdges(t *testing.T) {
	arch := buildArchitecture(archHeatmap())
	// Three edges, all among product packages: all within the top 15.
	for _, e := range arch.Edges {
		if !e.Key || !e.KeyAll {
			t.Errorf("edge %s->%s key=%v key_all=%v, want both true", e.From, e.To, e.Key, e.KeyAll)
		}
	}
}

func TestDefaultGroup(t *testing.T) {
	tests := []struct {
		name  string
		nodes []archNode
		want  string
	}{
		{"no units", nil, "package"},
		{"only shared components", []archNode{{ID: "c:x", Kind: "shared"}}, "package"},
		{"only supporting units", []archNode{{ID: "u:perf", Kind: "unit", Supporting: true}}, "package"},
		{"one product unit holds everything: packages say more", []archNode{{ID: "u:perf", Kind: "unit", Supporting: true}, {ID: "u:api", Kind: "unit"}}, "package"},
		{"two product units", []archNode{{ID: "u:api", Kind: "unit"}, {ID: "u:worker", Kind: "unit"}, {ID: "c:lib", Kind: "shared"}}, "unit"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := defaultGroup(unitView{Nodes: tt.nodes}); got != tt.want {
				t.Errorf("defaultGroup = %q, want %q", got, tt.want)
			}
		})
	}
}
