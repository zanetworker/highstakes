package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zanetworker/highstakes/internal/architecture"
	"github.com/zanetworker/highstakes/internal/types"
	"github.com/zanetworker/highstakes/pkg/heatmap"
)

// writeArchHeatmap writes a heatmap with a gateway, a supervisor, a helper
// bin and a shared core library, plus an optional architecture.yaml.
func writeArchHeatmap(t *testing.T, decl string) string {
	t.Helper()
	dir := t.TempDir()
	act := &types.Explanation{Actions: []types.Action{{ID: "add-tests", Text: "Add tests"}},
		ImpactReasons: []string{"Every RPC passes here"}, LikelihoodReasons: []string{"Changed 30 times"}}
	mk := func(p, comp string, impact int, q types.Quadrant, us []string, importedBy ...string) *types.FileHeat {
		f := &types.FileHeat{Path: p, Component: comp, Units: us, Impact: impact, HeatScore: impact, Likelihood: 40,
			ImpactAssessed: true, Quadrant: q, Tier: types.TierHigh, Explanation: act,
			Factors: types.Factors{BlastRadius: types.BlastRadius{Assessed: true, Score: impact, CriticalReason: "Every RPC passes here"}}}
		for _, b := range importedBy {
			f.Dependencies.ImportedBy = append(f.Dependencies.ImportedBy, types.Dependency{Path: b})
		}
		return f
	}
	hm := &types.Heatmap{Version: "1", Files: map[string]*types.FileHeat{}, Metadata: types.Metadata{RepoPath: "/src/demo", CommitSHA: "abc1234"}}
	for _, f := range []*types.FileHeat{
		mk("crates/core/src/lib.rs", "crates/core", 90, types.QuadrantFragileCritical, []string{"gw", "sup", "perf"}, "crates/gw/src/main.rs", "crates/sup/src/main.rs"),
		mk("crates/gw/src/main.rs", "crates/gw", 95, types.QuadrantFragileCritical, []string{"gw"}),
		mk("crates/sup/src/main.rs", "crates/sup", 80, types.QuadrantProtect, []string{"sup", "perf"}),
		mk("crates/sup/src/bin/perf.rs", "crates/sup", 10, types.QuadrantLow, []string{"perf"}),
	} {
		hm.Files[f.Path] = f
	}
	hm.Metadata.Units = []types.Unit{
		{Name: "gw", Kind: "rust-bin", Entry: "crates/gw/src/main.rs", Evidence: "crates/gw/Cargo.toml:1", Home: "crates/gw",
			Components: []string{"crates/gw", "crates/core"}, Shared: []string{"crates/core"}, MaxImpact: 95, ImpactAssessed: true},
		{Name: "sup", Kind: "rust-bin", Entry: "crates/sup/src/main.rs", Evidence: "crates/sup/Cargo.toml:1", Home: "crates/sup",
			Components: []string{"crates/sup", "crates/core"}, Shared: []string{"crates/core"}, MaxImpact: 90, ImpactAssessed: true},
		{Name: "perf", Kind: "rust-bin", Entry: "crates/sup/src/bin/perf.rs", Evidence: "crates/sup/src/bin/perf.rs:1", Home: "crates/sup",
			Components: []string{"crates/sup", "crates/core"}, Shared: []string{"crates/core"}, MaxImpact: 90, Supporting: true},
	}
	if err := os.MkdirAll(filepath.Join(dir, ".heatmap"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := heatmap.Save(hm, filepath.Join(dir, ".heatmap", "heatmap.json")); err != nil {
		t.Fatal(err)
	}
	for _, c := range []string{"core", "gw", "sup"} { // crate markers, so the tour finds components
		if err := os.MkdirAll(filepath.Join(dir, "crates", c), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "crates", c, "Cargo.toml"), []byte("[package]\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if decl != "" {
		if err := os.WriteFile(filepath.Join(dir, ".heatmap", "architecture.yaml"), []byte(decl), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

const archDecl = `links:
  - from: sup
    to: gw
    label: gRPC control session
    both: true
    evidence: docs/runtimes.mdx:59
placement:
  - unit: sup
    runs_in: supervisor pod
`

func TestGetArchitecture(t *testing.T) {
	dir := writeArchHeatmap(t, archDecl)

	t.Run("seecode spec", func(t *testing.T) {
		out, err := runCLI(t, dir, "get", "architecture", "--format", "seecode")
		if err != nil {
			t.Fatal(err)
		}
		var spec architecture.Spec
		if err := json.Unmarshal([]byte(out), &spec); err != nil {
			t.Fatalf("invalid JSON %q: %v", out, err)
		}
		if spec.Type != "architecture" || len(spec.Nodes) != 3 {
			t.Fatalf("spec = %+v, want gw, sup and the core library (perf hidden)", spec)
		}
		var async int
		for _, e := range spec.Edges {
			if e.Kind == "async" && e.Both && e.Label == "gRPC control session" {
				async++
			}
		}
		if async != 1 {
			t.Errorf("edges = %+v, want the declared control session dashed", spec.Edges)
		}
		if len(spec.Groups) != 1 || spec.Groups[0].Label != "supervisor pod (declared)" {
			t.Errorf("groups = %+v, want the declared supervisor pod", spec.Groups)
		}
	})

	t.Run("json is the plain diagram", func(t *testing.T) {
		out, err := runCLI(t, dir, "get", "architecture", "--json")
		if err != nil {
			t.Fatal(err)
		}
		var d architecture.Diagram
		if err := json.Unmarshal([]byte(out), &d); err != nil {
			t.Fatalf("invalid JSON %q: %v", out, err)
		}
		if d.SupportingHidden != 1 || d.Nodes[0].ID != "gw" || d.Nodes[0].Files[0].Path != "crates/gw/src/main.rs" {
			t.Errorf("diagram = %+v", d)
		}
	})

	t.Run("out writes the file", func(t *testing.T) {
		out, err := runCLI(t, dir, "get", "architecture", "--format", "seecode", "--out", "arch.json")
		if err != nil {
			t.Fatal(err)
		}
		b, err := os.ReadFile(filepath.Join(dir, "arch.json"))
		if err != nil || !strings.Contains(string(b), `"type": "architecture"`) {
			t.Errorf("file = %q err = %v", b, err)
		}
		if strings.Contains(out, `"type"`) {
			t.Errorf("stdout = %q, want the spec only in the file", out)
		}
	})

	t.Run("include supporting", func(t *testing.T) {
		out, err := runCLI(t, dir, "get", "architecture", "--json", "--include-supporting")
		if err != nil || !strings.Contains(out, `"id": "perf"`) {
			t.Errorf("out = %q err = %v, want perf shown", out, err)
		}
	})
}

func TestGetArchitecture_Errors(t *testing.T) {
	tests := []struct {
		name     string
		decl     string
		args     []string
		wantCode int
		wantMsg  string
	}{
		{"bad format lists valid ones", "", []string{"--format", "png"}, 2, "valid: seecode, json"},
		{"max nodes too small", "", []string{"--max-nodes", "1"}, 2, "--max-nodes"},
		{"unknown unit in architecture.yaml", "links:\n  - from: supervisor\n    to: gw\n", nil, 2, "valid units: gw, perf, sup"},
		{"malformed architecture.yaml", "links: [\n", nil, 2, "architecture.yaml"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := writeArchHeatmap(t, tt.decl)
			_, err := runCLI(t, dir, append([]string{"get", "architecture"}, tt.args...)...)
			ee, ok := err.(*exitError)
			if !ok || ee.code != tt.wantCode || !strings.Contains(ee.msg, tt.wantMsg) {
				t.Errorf("err = %v, want exit %d containing %q", err, tt.wantCode, tt.wantMsg)
			}
		})
	}
	t.Run("no units", func(t *testing.T) {
		_, err := runCLI(t, writeUnitsHeatmap(t, nil), "get", "architecture")
		if ee, ok := err.(*exitError); !ok || ee.code != 4 {
			t.Errorf("err = %v, want exit 4", err)
		}
	})
}

func TestListUnits_SupportingAndDeclared(t *testing.T) {
	dir := writeArchHeatmap(t, archDecl)
	out, err := runCLI(t, dir, "list", "units", "--json=false")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "perf  [") || !strings.Contains(out, "1 supporting unit hidden (--include-supporting shows it)") {
		t.Errorf("out = %q, want perf hidden with a note", out)
	}
	for _, want := range []string{
		"declared link: <-> gw: gRPC control session [docs/runtimes.mdx:59]",
		"declared placement: supervisor pod",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("out missing %q:\n%s", want, out)
		}
	}
	out, err = runCLI(t, dir, "list", "units", "--include-supporting", "--json")
	if err != nil {
		t.Fatal(err)
	}
	var res struct {
		Units            []types.Unit               `json:"units"`
		SupportingHidden int                        `json:"supporting_hidden"`
		Declared         types.DeclaredArchitecture `json:"declared"`
	}
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatal(err)
	}
	if len(res.Units) != 3 || res.SupportingHidden != 0 || len(res.Declared.Links) != 1 {
		t.Errorf("res = %+v, want 3 units, none hidden, the declared link", res)
	}
}

func TestGetTour_Walkthrough(t *testing.T) {
	dir := writeArchHeatmap(t, "")
	out, err := runCLI(t, dir, "get", "tour", "--per-component", "1", "--format", "walkthrough", "--out", "w.data.json")
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "w.data.json"))
	if err != nil {
		t.Fatalf("%v (stdout %q)", err, out)
	}
	var page map[string]any
	if err := json.Unmarshal(b, &page); err != nil {
		t.Fatal(err)
	}
	flows, _ := page["flows"].(map[string]any)
	if page["start"] != "product" || flows["product"] == nil {
		t.Fatalf("page = %s", b)
	}
	if !strings.Contains(string(b), "Why it matters: Every RPC passes here") || !strings.Contains(string(b), "impact 90 · 3 units") {
		t.Errorf("page missing the impact reason or the library role:\n%s", b)
	}

	_, err = runCLI(t, dir, "get", "tour", "--format", "html")
	if ee, ok := err.(*exitError); !ok || ee.code != 2 || !strings.Contains(ee.msg, "valid: text, json, walkthrough") {
		t.Errorf("err = %v, want exit 2 listing formats", err)
	}
}

func TestComponentRoles(t *testing.T) {
	us := []types.Unit{
		{Name: "gw", Home: "crates/gw", Components: []string{"crates/gw", "crates/server", "crates/core", "crates/drv"}, Shared: []string{"crates/core"}},
		{Name: "sup", Home: "crates/sup", Components: []string{"crates/sup", "crates/core"}, Shared: []string{"crates/core"}},
		{Name: "drv", Home: "crates/drv", Components: []string{"crates/drv"}},
	}
	got := componentRoles(us)
	want := map[string]string{
		"crates/gw":     "gw",
		"crates/server": "gw", // linked only by gw: its own code
		"crates/sup":    "sup",
		"crates/drv":    "drv", // another unit's home stays with that unit
		"crates/core":   "shared by 2 units",
	}
	if len(got) != len(want) {
		t.Errorf("roles = %v, want %v", got, want)
	}
	for c, w := range want {
		if got[c] != w {
			t.Errorf("%s: role %q, want %q", c, got[c], w)
		}
	}
	if len(componentRoles(nil)) != 0 {
		t.Error("no units: want no roles")
	}
}
