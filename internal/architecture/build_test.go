package architecture

import (
	"encoding/json"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zanetworker/highstakes/internal/types"
)

var update = flag.Bool("update", false, "rewrite golden files")

// fixtureHeatmap is a small OpenShell-like repo: a gateway, a supervisor and
// a sandbox runtime, two compute drivers, a CLI, a helper bin and a shared
// core library everything links.
func fixtureHeatmap() *types.Heatmap {
	hm := &types.Heatmap{Files: map[string]*types.FileHeat{}, Metadata: types.Metadata{RepoPath: "/src/openshell", CommitSHA: "abcdef1234567"}}
	add := func(p, comp string, impact, likelihood int, q types.Quadrant, us []string, imports ...string) {
		hm.Files[p] = &types.FileHeat{Path: p, Component: comp, Units: us, Impact: impact, Likelihood: likelihood,
			ImpactAssessed: true, Quadrant: q,
			Explanation: &types.Explanation{Actions: []types.Action{{ID: "add-tests", Text: "Add tests for " + filepath.Base(p)}}}}
		for _, to := range imports {
			f := hm.Files[to]
			f.Dependencies.ImportedBy = append(f.Dependencies.ImportedBy, types.Dependency{Path: p})
		}
	}
	all := []string{"gw", "sup", "sbx", "drv-k8s", "drv-vm", "cli", "perf"}
	add("crates/core/src/lib.rs", "crates/core", 90, 40, types.QuadrantProtect, all)
	add("crates/core/src/policy.rs", "crates/core", 95, 70, types.QuadrantFragileCritical, all)
	add("crates/gw/src/main.rs", "crates/gw", 80, 20, types.QuadrantProtect, []string{"gw"}, "crates/core/src/lib.rs")
	add("crates/gw/src/grpc.rs", "crates/gw", 95, 60, types.QuadrantFragileCritical, []string{"gw"}, "crates/core/src/policy.rs")
	add("crates/gw/src/auth.rs", "crates/gw", 92, 10, types.QuadrantProtect, []string{"gw"}, "crates/core/src/policy.rs")
	add("crates/gw/src/util.rs", "crates/gw", 20, 5, types.QuadrantLow, []string{"gw"})
	add("crates/sup/src/main.rs", "crates/sup", 93, 50, types.QuadrantFragileCritical, []string{"sup"}, "crates/core/src/lib.rs", "crates/core/src/policy.rs")
	add("crates/sbx/src/main.rs", "crates/sbx", 88, 50, types.QuadrantFragileCritical, []string{"sbx", "perf"}, "crates/core/src/lib.rs")
	add("crates/sbx/src/bin/perf.rs", "crates/sbx", 30, 5, types.QuadrantLow, []string{"perf"}, "crates/sbx/src/main.rs")
	add("crates/drv-k8s/src/main.rs", "crates/drv-k8s", 85, 30, types.QuadrantProtect, []string{"drv-k8s"}, "crates/core/src/lib.rs")
	add("crates/drv-vm/src/main.rs", "crates/drv-vm", 70, 30, types.QuadrantLow, []string{"drv-vm", "sup"}, "crates/core/src/lib.rs")
	add("crates/sup/src/vm.rs", "crates/sup", 60, 10, types.QuadrantLow, []string{"sup"}, "crates/drv-vm/src/main.rs")
	add("crates/cli/src/main.rs", "crates/cli", 50, 30, types.QuadrantLow, []string{"cli"}, "crates/core/src/lib.rs")

	unit := func(name, entry, home string, impact, fc int, shared ...string) types.Unit {
		return types.Unit{Name: name, Kind: "rust-bin", Entry: entry, Home: home, MaxImpact: impact, ImpactAssessed: true,
			FragileCritical: fc, Components: append([]string{home}, shared...), Shared: shared}
	}
	gw := unit("gw", "crates/gw/src/main.rs", "crates/gw", 95, 2, "crates/core")
	gw.InferredPlacement = []types.Placement{{Kind: "dockerfile", Detail: "ENTRYPOINT of deploy/Dockerfile.gateway", Evidence: "deploy/Dockerfile.gateway:23"}}
	sbx := unit("sbx", "crates/sbx/src/main.rs", "crates/sbx", 95, 2, "crates/core")
	sbx.InferredPlacement = []types.Placement{{Kind: "dockerfile", Detail: "ENTRYPOINT of deploy/Dockerfile.sandbox", Evidence: "deploy/Dockerfile.sandbox:22"}}
	perf := unit("perf", "crates/sbx/src/bin/perf.rs", "crates/sbx", 95, 2, "crates/core")
	perf.Supporting = true
	hm.Metadata.Units = []types.Unit{
		gw, unit("sup", "crates/sup/src/main.rs", "crates/sup", 95, 3, "crates/core"), sbx, perf,
		unit("drv-k8s", "crates/drv-k8s/src/main.rs", "crates/drv-k8s", 95, 1, "crates/core"),
		unit("drv-vm", "crates/drv-vm/src/main.rs", "crates/drv-vm", 95, 1, "crates/core"),
		unit("cli", "crates/cli/src/main.rs", "crates/cli", 95, 1, "crates/core"),
	}
	return hm
}

func fixtureDeclared() types.DeclaredArchitecture {
	return types.DeclaredArchitecture{
		Links: []types.DeclaredLink{
			{From: "sup", To: "gw", Label: "gRPC control session", Both: true, Evidence: "docs/overview.mdx:1070"},
			{From: "sup", To: "sbx", Label: "boundary protocol", Both: true, Evidence: "crates/core/src/boundary.rs:8"},
			{From: "gw", To: "drv-k8s", Label: "creates sandboxes"},
			{From: "gw", To: "drv-vm", Label: "creates sandboxes"},
		},
		Placement: []types.DeclaredPlacement{
			{Unit: "sbx", RunsIn: "workload pod", Evidence: "crates/drv-k8s/src/shared.rs:47"},
			{Unit: "sup", RunsIn: "supervisor pod"},
		},
	}
}

func source(string) ([]byte, error) { return []byte("// header\n\nfn main() {}\n"), nil }

func nodeByID(d *Diagram, id string) *Node {
	for i := range d.Nodes {
		if d.Nodes[i].ID == id {
			return &d.Nodes[i]
		}
	}
	return nil
}

func TestBuild_AllUnitsFit(t *testing.T) {
	d, err := Build(fixtureHeatmap(), fixtureDeclared(), Options{MaxNodes: 9, Source: source})
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, n := range d.Nodes {
		ids = append(ids, n.ID)
	}
	// six product units plus the shared library; the supporting helper bin is hidden
	if got := strings.Join(ids, ","); len(d.Nodes) != 7 || nodeByID(d, "perf") != nil || nodeByID(d, "lib-core") == nil || nodeByID(d, "other") != nil {
		t.Fatalf("nodes = %s, want 6 units and lib-core, no perf, no other", got)
	}
	if d.SupportingHidden != 1 {
		t.Errorf("supporting hidden = %d, want 1", d.SupportingHidden)
	}

	t.Run("focal is the highest impact unit (tie: most fragile-critical)", func(t *testing.T) {
		for _, n := range d.Nodes {
			if n.Focal != (n.ID == "sup") {
				t.Errorf("%s focal = %v, want only sup focal", n.ID, n.Focal)
			}
		}
	})

	t.Run("evidence is the top 3 files by impact with quadrant and first action", func(t *testing.T) {
		gw := nodeByID(d, "gw")
		if len(gw.Files) != 3 {
			t.Fatalf("gw files = %+v, want 3", gw.Files)
		}
		if gw.Files[0].Path != "crates/gw/src/grpc.rs" || gw.Files[1].Path != "crates/gw/src/auth.rs" || gw.Files[2].Path != "crates/gw/src/main.rs" {
			t.Errorf("gw files = %+v, want grpc, auth, main", gw.Files)
		}
		if f := gw.Files[0]; f.Line != 3 || f.Note != "fragile-critical: Add tests for grpc.rs" {
			t.Errorf("top file = %+v, want line 3 and note quadrant: action", f)
		}
		if lib := nodeByID(d, "lib-core"); len(lib.Files) != 2 || lib.Files[0].Path != "crates/core/src/policy.rs" {
			t.Errorf("lib files = %+v, want policy.rs first", lib.Files)
		}
	})

	t.Run("edges: imports labelled with file counts, declared marked", func(t *testing.T) {
		find := func(from, to, source string) *Edge {
			for i, e := range d.Edges {
				if e.From == from && e.To == to && e.Source == source {
					return &d.Edges[i]
				}
			}
			return nil
		}
		if e := find("gw", "lib-core", SourceImports); e == nil || e.Count != 3 || e.Label != "3 files" {
			t.Errorf("gw -> lib-core = %+v, want 3 importing files", e)
		}
		if e := find("sup", "drv-vm", SourceImports); e == nil || e.Label != "1 file" {
			t.Errorf("sup -> drv-vm = %+v, want 1 file (sup's vm.rs imports the driver crate)", e)
		}
		if e := find("sup", "gw", SourceDeclared); e == nil || !e.Both || e.Label != "gRPC control session" || e.Evidence != "docs/overview.mdx:1070" {
			t.Errorf("declared sup <-> gw = %+v", e)
		}
		if len(d.Edges) > 12 {
			t.Errorf("%d edges, want at most 12", len(d.Edges))
		}
		if d.Edges[0].Source != SourceDeclared {
			t.Errorf("first edge = %+v, want declared links kept first", d.Edges[0])
		}
	})

	t.Run("groups: declared placement wins over inferred", func(t *testing.T) {
		groups := map[string]Group{}
		for _, g := range d.Groups {
			groups[g.ID] = g
		}
		sbx, gw := nodeByID(d, "sbx"), nodeByID(d, "gw")
		if g := groups[sbx.Group]; g.Label != "workload pod" || g.Source != SourceDeclared || g.Evidence != "crates/drv-k8s/src/shared.rs:47" {
			t.Errorf("sbx group = %+v, want declared workload pod", g)
		}
		if g := groups[gw.Group]; g.Label != "Dockerfile.gateway" || g.Source != SourceInferred || g.Evidence != "deploy/Dockerfile.gateway:23" {
			t.Errorf("gw group = %+v, want inferred Dockerfile.gateway", g)
		}
		if nodeByID(d, "cli").Group != "" {
			t.Errorf("cli has no placement, want no group")
		}
	})

	t.Run("layout: callers above what they use, one cell per node", func(t *testing.T) {
		cells := map[[2]int]string{}
		for _, n := range d.Nodes {
			k := [2]int{n.Row, n.Col}
			if cells[k] != "" {
				t.Errorf("%s and %s share row %d col %d", n.ID, cells[k], n.Row, n.Col)
			}
			cells[k] = n.ID
		}
		if lib, gw, sup := nodeByID(d, "lib-core"), nodeByID(d, "gw"), nodeByID(d, "sup"); lib.Row <= gw.Row || gw.Row <= sup.Row {
			t.Errorf("rows: sup %d, gw %d, lib %d; want sup above gw above the library", sup.Row, gw.Row, lib.Row)
		}
		// row 2 holds 4 boxes at cols 0, 2, 4, 6; a lone box is centered at 3
		if sup, lib := nodeByID(d, "sup"), nodeByID(d, "lib-core"); sup.Col != 3 || lib.Col != 3 {
			t.Errorf("cols: sup %d, lib %d; want both centered at 3", sup.Col, lib.Col)
		}
	})
}

func TestBuild_Budget(t *testing.T) {
	d, err := Build(fixtureHeatmap(), fixtureDeclared(), Options{MaxNodes: 5, Source: source})
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Nodes) != 5 {
		t.Fatalf("%d nodes, want 5", len(d.Nodes))
	}
	other := nodeByID(d, "other")
	if other == nil || other.Kind != KindOther || strings.Join(other.Members, ",") != "cli,drv-k8s,drv-vm" {
		t.Fatalf("other = %+v, want drv-k8s, drv-vm, cli folded in", other)
	}
	for _, id := range []string{"sup", "gw", "sbx", "lib-core"} {
		if nodeByID(d, id) == nil {
			t.Errorf("missing %s", id)
		}
	}
	var declToOther int
	for _, e := range d.Edges {
		if e.From == e.To {
			t.Errorf("self edge %+v", e)
		}
		if e.Source == SourceDeclared && e.To == "other" {
			declToOther++
		}
	}
	if declToOther != 1 {
		t.Errorf("declared edges into other = %d, want the two gw -> driver links merged into 1", declToOther)
	}
}

func TestBuild_EdgeCap(t *testing.T) {
	hm := fixtureHeatmap()
	decl := fixtureDeclared()
	d, err := Build(hm, decl, Options{MaxNodes: 9, MaxEdges: 3, Source: source})
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Edges) != 3 || d.DroppedEdges == 0 {
		t.Errorf("edges = %d dropped = %d, want 3 kept and the rest counted", len(d.Edges), d.DroppedEdges)
	}
	// Layout ranks on every edge, drawn or not: the cli -> core import is
	// dropped, yet core still sits below cli.
	if lib, cli := nodeByID(d, "lib-core"), nodeByID(d, "cli"); lib.Row <= cli.Row {
		t.Errorf("rows: cli %d, lib %d; want the library below cli", cli.Row, lib.Row)
	}
}

func TestBuild_Errors(t *testing.T) {
	tests := []struct {
		name string
		hm   *types.Heatmap
		opts Options
		want error
	}{
		{name: "no units", hm: &types.Heatmap{Files: map[string]*types.FileHeat{}}, opts: Options{}, want: ErrNoUnits},
		{name: "max nodes below 2", hm: fixtureHeatmap(), opts: Options{MaxNodes: 1}, want: ErrInvalidMaxNodes},
		{name: "max nodes above 24", hm: fixtureHeatmap(), opts: Options{MaxNodes: 25}, want: ErrInvalidMaxNodes},
		{name: "only supporting units", hm: func() *types.Heatmap {
			hm := fixtureHeatmap()
			for i := range hm.Metadata.Units {
				hm.Metadata.Units[i].Supporting = true
			}
			return hm
		}(), want: ErrOnlySupporting},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := Build(tt.hm, types.DeclaredArchitecture{}, tt.opts); !errors.Is(err, tt.want) {
				t.Errorf("err = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestBuild_IncludeSupporting(t *testing.T) {
	d, err := Build(fixtureHeatmap(), types.DeclaredArchitecture{}, Options{MaxNodes: 9, IncludeSupporting: true})
	if err != nil {
		t.Fatal(err)
	}
	if nodeByID(d, "perf") == nil || d.SupportingHidden != 0 {
		t.Errorf("perf missing or supporting hidden = %d, want perf shown", d.SupportingHidden)
	}
}

func TestSeeCode_Golden(t *testing.T) {
	d, err := Build(fixtureHeatmap(), fixtureDeclared(), Options{MaxNodes: 9, Source: source})
	if err != nil {
		t.Fatal(err)
	}
	got, err := json.MarshalIndent(SeeCode(d), "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	golden := filepath.Join("testdata", "openshell-like.seecode.json")
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(golden, append(got, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("%v (run go test ./internal/architecture -update)", err)
	}
	if strings.TrimSpace(string(want)) != string(got) {
		t.Errorf("SeeCode spec differs from %s (go test ./internal/architecture -update rewrites it):\n%s", golden, got)
	}
}

func TestSeeCode_Shape(t *testing.T) {
	d, err := Build(fixtureHeatmap(), fixtureDeclared(), Options{MaxNodes: 5, Source: source})
	if err != nil {
		t.Fatal(err)
	}
	spec := SeeCode(d)
	if spec.Type != "architecture" || len(spec.Nodes) != 5 {
		t.Fatalf("spec = %+v", spec)
	}
	kinds := map[string]string{}
	for _, n := range spec.Nodes {
		kinds[n.ID] = n.Kind
	}
	if kinds["other"] != "muted" {
		t.Errorf("other kind = %q, want muted", kinds["other"])
	}
	asyncEdges := 0
	for _, e := range spec.Edges {
		if e.Kind == "async" {
			asyncEdges++
		}
		if len([]rune(e.Label)) > maxEdgeLabel {
			t.Errorf("edge label %q over SeeCode's %d chars", e.Label, maxEdgeLabel)
		}
	}
	declared := 0
	for _, e := range d.Edges {
		if e.Source == SourceDeclared {
			declared++
		}
	}
	if asyncEdges != declared || declared == 0 {
		t.Errorf("%d async edges, want one per declared link (%d)", asyncEdges, declared)
	}
	for _, g := range spec.Groups {
		if strings.HasSuffix(g.Label, "(declared)") != (g.Style == "dashed") {
			t.Errorf("group %+v: declared groups are dashed", g)
		}
	}
	ev := map[string]int{}
	for _, e := range spec.Evidence {
		ev[e.ID]++
	}
	if ev["gw"] != 3 || ev["other"] == 0 {
		t.Errorf("evidence per node = %v, want 3 for gw and some for other", ev)
	}
}

func TestClip(t *testing.T) {
	tests := []struct {
		in   string
		n    int
		want string
	}{
		{"short", 28, "short"},
		{"exactly-five", 12, "exactly-five"},
		{"gRPC control session (long)", 10, "gRPC cont…"},
		{"", 5, ""},
	}
	for _, tt := range tests {
		if got := clip(tt.in, tt.n); got != tt.want {
			t.Errorf("clip(%q, %d) = %q, want %q", tt.in, tt.n, got, tt.want)
		}
	}
}

func TestBuild_LibraryIsTheMostDirectlyImported(t *testing.T) {
	// schema is linked by every unit (through core) and has a higher impact,
	// but no unit imports it directly: core is the hub the picture should show.
	hm := fixtureHeatmap()
	all := []string{"gw", "sup", "sbx", "drv-k8s", "drv-vm", "cli", "perf"}
	hm.Files["crates/schema/src/lib.rs"] = &types.FileHeat{Path: "crates/schema/src/lib.rs", Component: "crates/schema", Units: all,
		Impact: 99, ImpactAssessed: true, Quadrant: types.QuadrantProtect,
		Dependencies: types.Dependencies{ImportedBy: []types.Dependency{{Path: "crates/core/src/lib.rs"}}}}
	for i := range hm.Metadata.Units {
		hm.Metadata.Units[i].Shared = append(hm.Metadata.Units[i].Shared, "crates/schema")
	}
	d, err := Build(hm, types.DeclaredArchitecture{}, Options{MaxNodes: 9})
	if err != nil {
		t.Fatal(err)
	}
	if nodeByID(d, "lib-core") == nil || nodeByID(d, "lib-schema") != nil {
		t.Errorf("nodes = %+v, want lib-core, not lib-schema", d.Nodes)
	}
}
