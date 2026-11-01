package dashboard

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/zanetworker/highstakes/internal/types"
)

func fh(path string, heat int, tier types.Tier, lines int, importedBy ...string) *types.FileHeat {
	f := &types.FileHeat{Path: path, HeatScore: heat, Tier: tier, Size: types.FileSize{Lines: lines}}
	for _, p := range importedBy {
		f.Dependencies.ImportedBy = append(f.Dependencies.ImportedBy, types.Dependency{Path: p})
	}
	return f
}

func archHeatmap() *types.Heatmap {
	files := []*types.FileHeat{
		fh("cmd/main.go", 40, types.TierMedium, 300),
		fh("api/a.go", 90, types.TierCritical, 100, "cmd/main.go"),
		fh("api/b.go", 20, types.TierLow, 50, "cmd/main.go"),
		fh("types/t.go", 10, types.TierLow, 80, "api/a.go", "api/b.go", "cmd/main.go"),
		fh("README.md", 5, types.TierLow, 20),
	}
	hm := &types.Heatmap{Files: map[string]*types.FileHeat{}}
	for _, f := range files {
		hm.Files[f.Path] = f
	}
	return hm
}

func TestBuildArchitecture_Nodes(t *testing.T) {
	arch := buildArchitecture(archHeatmap())
	byID := map[string]archNode{}
	for _, n := range arch.Nodes {
		byID[n.ID] = n
	}
	tests := []struct {
		id   string
		want archNode
	}{
		{"api", archNode{ID: "api", Files: 2, Lines: 150, MaxHeat: 90, Tier: "critical", ImpactTier: "critical", Hottest: "api/a.go", Level: 1}},
		{"types", archNode{ID: "types", Files: 1, Lines: 80, MaxHeat: 10, Tier: "low", ImpactTier: "low", Hottest: "types/t.go", Level: 0}},
		{"cmd", archNode{ID: "cmd", Files: 1, Lines: 300, MaxHeat: 40, Tier: "medium", ImpactTier: "medium", Hottest: "cmd/main.go", Level: 2}},
		{".", archNode{ID: ".", Files: 1, Lines: 20, MaxHeat: 5, Tier: "low", ImpactTier: "low", Hottest: "README.md", Level: 0}},
	}
	if len(arch.Nodes) != len(tests) {
		t.Fatalf("got %d nodes, want %d: %+v", len(arch.Nodes), len(tests), arch.Nodes)
	}
	for _, tt := range tests {
		t.Run(tt.id, func(t *testing.T) {
			if got := byID[tt.id]; !reflect.DeepEqual(got, tt.want) {
				t.Errorf("node = %+v, want %+v", got, tt.want)
			}
		})
	}
	// Ordered by level, then heat: types/. (0), api (1), cmd (2).
	var order []string
	for _, n := range arch.Nodes {
		order = append(order, n.ID)
	}
	if want := []string{"types", ".", "api", "cmd"}; !reflect.DeepEqual(order, want) {
		t.Errorf("order = %v, want %v", order, want)
	}
}

func TestBuildArchitecture_Edges(t *testing.T) {
	arch := buildArchitecture(archHeatmap())
	want := []archEdge{
		{From: "api", To: "types", Count: 2, Links: [][2]string{{"api/a.go", "types/t.go"}, {"api/b.go", "types/t.go"}}, LinksTotal: 2},
		// one importing file, two file links: the arrow's panel lists both
		{From: "cmd", To: "api", Count: 1, Links: [][2]string{{"cmd/main.go", "api/a.go"}, {"cmd/main.go", "api/b.go"}}, LinksTotal: 2},
		{From: "cmd", To: "types", Count: 1, Links: [][2]string{{"cmd/main.go", "types/t.go"}}, LinksTotal: 1},
	}
	if !reflect.DeepEqual(arch.Edges, want) {
		t.Errorf("edges = %+v, want %+v", arch.Edges, want)
	}
}

func TestBuildArchitecture_Empty(t *testing.T) {
	arch := buildArchitecture(&types.Heatmap{Files: map[string]*types.FileHeat{}})
	if arch.Nodes == nil || arch.Edges == nil || len(arch.Nodes) != 0 || len(arch.Edges) != 0 {
		t.Errorf("empty heatmap should give empty non-nil slices, got %+v", arch)
	}
}

func TestBuildArchitecture_TierIsMaxSeverity(t *testing.T) {
	// An annotation override can make a lower-heat file a higher tier.
	hm := &types.Heatmap{Files: map[string]*types.FileHeat{
		"x/hot.go":  fh("x/hot.go", 70, types.TierHigh, 10),
		"x/crit.go": fh("x/crit.go", 50, types.TierCritical, 10),
	}}
	n := buildArchitecture(hm).Nodes[0]
	if n.Tier != "critical" || n.Hottest != "x/hot.go" || n.MaxHeat != 70 {
		t.Errorf("node = %+v", n)
	}
}

func TestGenerate_EmbedsArchitecture(t *testing.T) {
	out := filepath.Join(t.TempDir(), "d.html")
	if err := Generate(archHeatmap(), types.DeclaredArchitecture{}, out); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	html := string(b)
	for _, want := range []string{`"from":"api","to":"types","count":2`, `setMode('arch',this)`, `id="arch"`} {
		if !strings.Contains(html, want) {
			t.Errorf("dashboard missing %q", want)
		}
	}
	if strings.Contains(html, "/*__ARCH__*/") {
		t.Error("architecture placeholder not replaced")
	}
}

func TestBuildArchitecture_IgnoresTestFiles(t *testing.T) {
	hm := archHeatmap()
	hm.Files["api/a_test.go"] = fh("api/a_test.go", 99, types.TierCritical, 500)
	hm.Files["e2e/flow_test.go"] = fh("e2e/flow_test.go", 50, types.TierMedium, 200)
	hm.Files["types/t.go"].Dependencies.ImportedBy = append(hm.Files["types/t.go"].Dependencies.ImportedBy,
		types.Dependency{Path: "e2e/flow_test.go"})

	arch := buildArchitecture(hm)
	for _, n := range arch.Nodes {
		switch n.ID {
		case "api":
			if n.Hottest != "api/a.go" || n.Files != 2 || n.Lines != 150 || n.Tier != string(types.TierCritical) {
				t.Errorf("api node = %+v, want hottest api/a.go, 2 files, 150 lines, critical", n)
			}
		case "e2e":
			t.Errorf("test-only dir e2e should not be a node: %+v", n)
		}
	}
	for _, e := range arch.Edges {
		if e.From == "e2e" {
			t.Errorf("edge from test-only dir: %+v", e)
		}
	}
}

func TestBuildArchitecture_ImpactAndFragility(t *testing.T) {
	axes := func(path string, tier types.Tier, impact, likelihood int, assessed bool) *types.FileHeat {
		f := fh(path, max(impact, likelihood), tier, 10)
		f.Impact, f.Likelihood, f.ImpactAssessed = impact, likelihood, assessed
		return f
	}
	hm := &types.Heatmap{Metadata: types.Metadata{LikelyCutoff: 70}, Files: map[string]*types.FileHeat{
		"core/auth.go":       axes("core/auth.go", types.TierHigh, 90, 10, true),      // protect
		"core/parse.go":      axes("core/parse.go", types.TierMedium, 30, 80, true),   // watch: likely
		"core/parse_test.go": axes("core/parse_test.go", types.TierLow, 30, 95, true), // tests never count
		"ui/view.go":         axes("ui/view.go", types.TierMedium, 50, 70, false),     // no LLM: likely at the cutoff
		"lib/x.go":           axes("lib/x.go", types.TierLow, 70, 10, true),           // impact 70: high
	}}
	byID := map[string]archNode{}
	for _, n := range buildArchitecture(hm).Nodes {
		byID[n.ID] = n
	}
	tests := []struct {
		id         string
		maxImpact  int
		impactTier string
		likely     int
	}{
		{"core", 90, "critical", 1},
		{"ui", 0, "medium", 1}, // unassessed: colour falls back to the heat tier
		{"lib", 70, "high", 0},
	}
	for _, tt := range tests {
		t.Run(tt.id, func(t *testing.T) {
			n := byID[tt.id]
			if n.MaxImpact != tt.maxImpact || n.ImpactTier != tt.impactTier || n.Likely != tt.likely {
				t.Errorf("node = %+v, want max_impact %d impact_tier %s likely %d", n, tt.maxImpact, tt.impactTier, tt.likely)
			}
		})
	}

	t.Run("no cutoff in old heatmaps: nothing is likely", func(t *testing.T) {
		hm.Metadata.LikelyCutoff = 0
		for _, n := range buildArchitecture(hm).Nodes {
			if n.Likely != 0 {
				t.Errorf("node %s likely = %d, want 0", n.ID, n.Likely)
			}
		}
	})
}

func TestGenerate_EmbedsQuadrantView(t *testing.T) {
	hm := archHeatmap()
	hm.Metadata.LikelyCutoff, hm.Metadata.ImpactThreshold = 55, 61
	f := hm.Files["api/a.go"]
	f.Likelihood, f.Impact, f.ImpactAssessed, f.Quadrant = 60, 90, true, types.QuadrantFragileCritical
	out := filepath.Join(t.TempDir(), "d.html")
	if err := Generate(hm, types.DeclaredArchitecture{}, out); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	html := string(b)
	for _, want := range []string{
		`setMode('quadrant',this)`, `id="quadrant"`, `function renderQuadrant`,
		`"likely_cutoff":55`, `"impact_threshold":61`,
		`"likelihood":60,"impact":90,"impact_assessed":true,"quadrant":"fragile-critical"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("dashboard missing %q", want)
		}
	}
}

func TestGenerate_EmbedsExplanation(t *testing.T) {
	hm := archHeatmap()
	f := hm.Files["api/a.go"]
	f.ImpactAssessed = true
	f.Explanation = &types.Explanation{
		Headline: "PROTECT (impact 90, likelihood 10)", ImpactHigh: true,
		ImpactReasons:     []string{"security 90"},
		LikelihoodReasons: []string{`recent fix: "<b>x</b>"`},
		Actions:           []types.Action{{ID: "codeowners", Text: "Add to CODEOWNERS; never auto-merge"}},
	}
	out := filepath.Join(t.TempDir(), "d.html")
	if err := Generate(hm, types.DeclaredArchitecture{}, out); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	html := string(b)
	for _, want := range []string{
		`"impact_title":"Why impact is high"`,
		`"likely_title":"Likelihood signals (below the likely cutoff)"`,
		`"text":"Add to CODEOWNERS; never auto-merge"`,
		`What to do`, `function htmlEsc`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("dashboard missing %q", want)
		}
	}
	if strings.Contains(html, `"<b>x</b>"`) {
		t.Error("reason text must stay JSON-escaped, not raw HTML")
	}
}
