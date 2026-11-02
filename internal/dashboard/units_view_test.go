package dashboard

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/zanetworker/highstakes/internal/types"
)

func unitsHeatmap() *types.Heatmap {
	mk := func(p, comp string, impact int, units []string, importedBy ...string) *types.FileHeat {
		f := fh(p, impact, types.TierLow, 10, importedBy...)
		f.Impact, f.ImpactAssessed, f.Component, f.Units = impact, true, comp, units
		return f
	}
	both := []string{"sbx", "sup"}
	files := []*types.FileHeat{
		mk("crates/sup/src/main.rs", "crates/sup", 30, []string{"sup"}),
		mk("crates/net/src/proxy.rs", "crates/net", 92, []string{"sup"}, "crates/sup/src/main.rs"),
		mk("crates/core/src/policy.rs", "crates/core", 80, both, "crates/net/src/proxy.rs", "crates/core/src/lib.rs"),
		mk("crates/core/src/lib.rs", "crates/core", 60, both, "crates/sbx/src/main.rs"),
		mk("crates/sbx/src/main.rs", "crates/sbx", 20, []string{"sbx"}),
		// sbx's crate also holds the sbx-perf bin: shared files of the crate
		// stay in the owner's box, perf.rs in its own.
		mk("crates/sbx/src/util.rs", "crates/sbx", 10, []string{"sbx", "sbx-perf"}),
		mk("crates/sbx/src/bin/perf.rs", "crates/sbx", 5, []string{"sbx-perf"}),
		mk("crates/tool/src/x.rs", "crates/tool", 50, nil),
		mk("crates/sup/tests/it.rs", "crates/sup", 0, nil),
	}
	hm := &types.Heatmap{Files: map[string]*types.FileHeat{}, Metadata: types.Metadata{Units: []types.Unit{
		{Name: "sbx", Kind: "rust-bin", Entry: "crates/sbx/src/main.rs", Home: "crates/sbx",
			Components: []string{"crates/sbx", "crates/core"}, Shared: []string{"crates/core"}, Files: 3, MaxImpact: 80,
			InferredPlacement: []types.Placement{{Kind: "dockerfile", Detail: "ENTRYPOINT of deploy/Dockerfile.sandbox", Evidence: "deploy/Dockerfile.sandbox:22"}}},
		{Name: "sup", Kind: "rust-bin", Entry: "crates/sup/src/main.rs", Home: "crates/sup",
			Components: []string{"crates/sup", "crates/core", "crates/net"}, Shared: []string{"crates/core"}, Files: 4, MaxImpact: 92},
		{Name: "sbx-perf", Kind: "rust-bin", Entry: "crates/sbx/src/bin/perf.rs", Home: "crates/sbx",
			Components: []string{"crates/sbx"}, Shared: []string{}, Files: 2, MaxImpact: 10},
	}}}
	for _, f := range files {
		hm.Files[f.Path] = f
	}
	return hm
}

func TestBuildUnitView(t *testing.T) {
	view, fileNode := buildUnitView(unitsHeatmap())
	byID := map[string]archNode{}
	for _, n := range view.Nodes {
		byID[n.ID] = n
	}
	tests := []struct {
		id        string
		kind      string
		label     string
		files     int
		maxImpact int
		level     int
		members   []string
		placement string
	}{
		{"u:sbx", "unit", "sbx", 2, 20, 1, []string{"crates/sbx"}, "Dockerfile.sandbox"},
		{"u:sbx-perf", "unit", "sbx-perf", 1, 5, 1, nil, ""},
		{"u:sup", "unit", "sup", 2, 92, 1, []string{"crates/sup", "crates/net"}, ""},
		{"c:crates/core", "shared", "crates/core", 2, 80, 0, []string{"crates/core"}, ""},
	}
	if len(view.Nodes) != len(tests) {
		t.Fatalf("nodes = %+v, want %d", view.Nodes, len(tests))
	}
	for _, tt := range tests {
		t.Run(tt.id, func(t *testing.T) {
			n := byID[tt.id]
			if n.Kind != tt.kind || n.Label != tt.label || n.Files != tt.files || n.MaxImpact != tt.maxImpact || n.Level != tt.level ||
				!reflect.DeepEqual(n.Members, tt.members) || n.Placement != tt.placement {
				t.Errorf("node = %+v", n)
			}
		})
	}
	wantEdges := []archEdge{
		{From: "u:sbx", To: "c:crates/core", Count: 1, Links: [][2]string{{"crates/sbx/src/main.rs", "crates/core/src/lib.rs"}}, LinksTotal: 1, Key: true, KeyAll: true},
		{From: "u:sup", To: "c:crates/core", Count: 1, Links: [][2]string{{"crates/net/src/proxy.rs", "crates/core/src/policy.rs"}}, LinksTotal: 1, Key: true, KeyAll: true},
	}
	if !reflect.DeepEqual(view.Edges, wantEdges) {
		t.Errorf("edges = %+v\nwant %+v", view.Edges, wantEdges)
	}
	if view.Unlinked != 1 {
		t.Errorf("unlinked = %d, want 1 (crates/tool; tests do not count)", view.Unlinked)
	}
	for p, want := range map[string]string{
		"crates/net/src/proxy.rs": "u:sup", "crates/core/src/lib.rs": "c:crates/core", "crates/tool/src/x.rs": "",
		"crates/sbx/src/util.rs": "u:sbx", "crates/sbx/src/bin/perf.rs": "u:sbx-perf",
	} {
		if fileNode[p] != want {
			t.Errorf("node of %s = %q, want %q", p, fileNode[p], want)
		}
	}
}

func TestBuildUnitView_NoUnits(t *testing.T) {
	view, _ := buildUnitView(archHeatmap())
	if view.Nodes == nil || view.Edges == nil || len(view.Nodes) != 0 {
		t.Errorf("no units: view = %+v, want empty non-nil slices", view)
	}
}

func TestEdgeLinksAreCapped(t *testing.T) {
	hm := &types.Heatmap{Files: map[string]*types.FileHeat{}}
	var importers []string
	for i := 0; i < maxEdgeLinks+5; i++ {
		p := "a/f" + strings.Repeat("x", i) + ".go"
		hm.Files[p] = fh(p, 1, types.TierLow, 1)
		importers = append(importers, p)
	}
	hm.Files["b/t.go"] = fh("b/t.go", 1, types.TierLow, 1, importers...)
	e := buildArchitecture(hm).Edges[0]
	if len(e.Links) != maxEdgeLinks || e.LinksTotal != maxEdgeLinks+5 || e.Count != maxEdgeLinks+5 {
		t.Errorf("links = %d total = %d count = %d, want %d %d %d", len(e.Links), e.LinksTotal, e.Count, maxEdgeLinks, maxEdgeLinks+5, maxEdgeLinks+5)
	}
}

func TestPlacementLabel(t *testing.T) {
	tests := []struct {
		name string
		in   []types.Placement
		want string
	}{
		{"none", nil, ""},
		{"dockerfile and helm, deduplicated", []types.Placement{
			{Kind: "dockerfile", Evidence: "deploy/docker/Dockerfile.sandbox:22"},
			{Kind: "dockerfile", Evidence: "deploy/docker/Dockerfile.sandbox:30"},
			{Kind: "helm", Detail: "helm chart deploy/helm/os: sandboxRuntime image openshell/sandbox", Evidence: "deploy/helm/os/values.yaml:49"},
			{Kind: "k8s", Detail: "image x in deploy/kube/pod.yaml", Evidence: "deploy/kube/pod.yaml:5"},
		}, "Dockerfile.sandbox, helm sandboxRuntime, k8s pod.yaml"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := placementLabel(tt.in); got != tt.want {
				t.Errorf("placementLabel = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestGenerate_EmbedsUnitView(t *testing.T) {
	out := filepath.Join(t.TempDir(), "d.html")
	if err := Generate(unitsHeatmap(), types.DeclaredArchitecture{}, out); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	html := string(b)
	for _, want := range []string{`"id":"u:sup"`, `"unit_node":"u:sup"`, `setGroup('unit',this)`, `function archFit`, `function selectEdge`} {
		if !strings.Contains(html, want) {
			t.Errorf("dashboard missing %q", want)
		}
	}
	if strings.Contains(html, "/*__UNITS__*/") {
		t.Error("units placeholder not replaced")
	}
}

func TestGenerate_DeclaredAndSupporting(t *testing.T) {
	hm := unitsHeatmap()
	hm.Metadata.Units[2].Supporting = true // sbx-perf, a helper bin
	decl := types.DeclaredArchitecture{
		Links:     []types.DeclaredLink{{From: "sup", To: "sbx", Label: "boundary protocol", Both: true, Evidence: "docs/x.md:3"}},
		Placement: []types.DeclaredPlacement{{Unit: "sbx", RunsIn: "workload pod"}},
	}
	out := filepath.Join(t.TempDir(), "d.html")
	if err := Generate(hm, decl, out); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	html := string(b)
	for _, want := range []string{
		`"declared":{"links":[{"from":"sup","to":"sbx","label":"boundary protocol","both":true,"evidence":"docs/x.md:3"}]`,
		`"runs_in":"workload pod"`,
		`"supporting":true`,
		`function toggleSupporting`, // the unit view hides supporting units until toggled
		`class="ae declared`,        // declared links draw dashed, apart from imports
	} {
		if !strings.Contains(html, want) {
			t.Errorf("dashboard missing %q", want)
		}
	}
}

func TestGenerate_Autonomy(t *testing.T) {
	hm := unitsHeatmap()
	for _, f := range hm.Files {
		f.Autonomy = &types.Autonomy{Level: 2, Label: "auto-fix, human merges", Reason: "Stabilize: likely to break, with limited damage"}
		break
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
		`"autonomy":{"level":2,"label":"auto-fix, human merges"`, // per-file data for the detail panel
		`data-auto="2" onclick="setAutonomy('2',this)"`,          // filter chip
		`Agent autonomy`,                                         // detail panel row
	} {
		if !strings.Contains(html, want) {
			t.Errorf("dashboard missing %q", want)
		}
	}
}
