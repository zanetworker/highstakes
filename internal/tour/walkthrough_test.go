package tour

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/zanetworker/highstakes/internal/types"
)

// wtStop builds a stop whose Why is why (the LLM critical reason or
// summary) and whose first impact reason is the dimension scores line.
func wtStop(order int, p string, q types.Quadrant, why, likelyWhy string, actions ...string) Stop {
	s := Stop{Order: order, Path: p, Line: 7, Anchor: fmt.Sprintf("%s:7", p), Impact: 90, Likelihood: 50, ImpactAssessed: q != "", Quadrant: q, Why: why}
	e := &types.Explanation{ImpactReasons: []string{"security 90 · data 80"}}
	if likelyWhy != "" {
		e.LikelihoodReasons = []string{likelyWhy}
	}
	for _, a := range actions {
		e.Actions = append(e.Actions, types.Action{ID: "x", Text: a})
	}
	s.Explanation = e
	return s
}

func wtTour() *Tour {
	return &Tour{Repo: "/src/openshell", Commit: "abcdef1234", TotalStops: 4, ProductComponents: 3, Sections: []Section{
		{Order: 1, Component: "crates/openshell-core", Kind: KindProduct, Stops: []Stop{
			wtStop(1, "crates/openshell-core/src/policy.rs", types.QuadrantFragileCritical, "Every unit loads policy <here>", "Changed 40 times", "Add tests", "Pair review"),
		}},
		{Order: 2, Component: "crates/openshell-gateway", Kind: KindProduct, Stops: []Stop{
			wtStop(2, "crates/openshell-gateway/src/grpc.rs", types.QuadrantProtect, "Serves every RPC", ""),
			wtStop(3, "crates/openshell-gateway/src/auth.rs", "", "", "Complex"),
		}},
		{Order: 3, Component: "examples/demo", Kind: KindSupporting, Stops: []Stop{
			wtStop(4, "examples/demo/src/main.rs", types.QuadrantLow, "Demo only", "Rarely changed", "Leave it"),
		}},
	}}
}

func TestWalkthrough(t *testing.T) {
	roles := map[string]string{"crates/openshell-gateway": "openshell-gateway", "crates/openshell-core": "shared by 7 units"}
	p, err := Walkthrough(wtTour(), roles)
	if err != nil {
		t.Fatal(err)
	}

	t.Run("one node per component, inside the map", func(t *testing.T) {
		if len(p.Nodes) != 3 {
			t.Fatalf("nodes = %+v, want 3", p.Nodes)
		}
		for id, n := range p.Nodes {
			if n.X < 100 || n.X > 1180 || n.Y < gridTop || n.Y > gridBottom {
				t.Errorf("node %s at (%v, %v) is outside the 1280x680 map with padding", id, n.X, n.Y)
			}
		}
		gw := p.Nodes[p.NodeOf["crates/openshell-gateway"]]
		if gw.Name != "gateway" || gw.Sub != "impact 90 · gateway" {
			t.Errorf("gateway node = %+v, want the repo-name prefix stripped from the name and the owning unit", gw)
		}
		if core := p.Nodes[p.NodeOf["crates/openshell-core"]]; !core.Tp {
			t.Errorf("core = %+v, want tp (shared library)", core)
		}
	})

	t.Run("tour order becomes hops; product and supporting flows", func(t *testing.T) {
		prod, sup := p.Flows["product"], p.Flows["supporting"]
		if len(prod.Hops) != 3 || len(sup.Hops) != 1 || p.Start != "product" {
			t.Fatalf("flows = %+v, want 3 product hops and 1 supporting", p.Flows)
		}
		core, gw := p.NodeOf["crates/openshell-core"], p.NodeOf["crates/openshell-gateway"]
		h := prod.Hops
		if h[0].F != core || h[0].T != core || h[0].Ch != "s" {
			t.Errorf("hop 1 = %+v, want a self hop on core", h[0])
		}
		if h[1].F != core || h[1].T != gw || h[1].Ch != "b" {
			t.Errorf("hop 2 = %+v, want core -> gateway", h[1])
		}
		if h[2].F != gw || h[2].T != gw || h[2].Ch != "s" {
			t.Errorf("hop 3 = %+v, want a self hop on gateway (same component)", h[2])
		}
	})

	t.Run("captions: why impact and first likelihood reason, escaped", func(t *testing.T) {
		h := p.Flows["product"].Hops[0]
		if !strings.Contains(h.Txt, "Every unit loads policy &lt;here&gt;") || !strings.Contains(h.Txt, "Changed 40 times") {
			t.Errorf("caption = %q, want the escaped why and likelihood reason", h.Txt)
		}
		if h.Pay != "policy.rs:7" || h.Short != "do: Add tests" || h.Plane != "fragile" {
			t.Errorf("hop = %+v, want anchor payload, first action, fragile plane", h)
		}
		if h := p.Flows["product"].Hops[2]; !strings.Contains(h.Txt, "security 90 · data 80") || h.Plane != "estimate" || h.Short != "read auth.rs" {
			t.Errorf("hop 3 = %+v, want the first impact reason when the stop has no why, estimate plane", h)
		}
	})

	t.Run("every plane used has a color and a key entry", func(t *testing.T) {
		keys := map[string]bool{}
		for _, k := range p.PlaneKey {
			keys[k[0]] = true
		}
		for _, f := range p.Flows {
			for _, h := range f.Hops {
				if p.Colors[h.Plane] == "" || !keys[h.Plane] {
					t.Errorf("plane %q has no color or key", h.Plane)
				}
			}
		}
	})

	t.Run("JSON has the walkthrough_build.py fields", func(t *testing.T) {
		b, err := json.Marshal(p)
		if err != nil {
			t.Fatal(err)
		}
		var m map[string]any
		if err := json.Unmarshal(b, &m); err != nil {
			t.Fatal(err)
		}
		for _, k := range []string{"title", "heading", "kicker", "intro", "colors", "planeKey", "zones", "nodes", "frames", "flows", "start"} {
			if _, ok := m[k]; !ok {
				t.Errorf("missing %s", k)
			}
		}
		if _, ok := m["nodeOf"]; ok {
			t.Error("nodeOf must not be serialized (the build refuses unknown fields only on nodes, but keep the page clean)")
		}
	})
}

func TestWalkthrough_Errors(t *testing.T) {
	if _, err := Walkthrough(&Tour{}, nil); !errors.Is(err, ErrNoFiles) {
		t.Errorf("empty tour: err = %v, want ErrNoFiles", err)
	}
}

func TestWalkthrough_Capacity(t *testing.T) {
	tr := &Tour{Repo: "r"}
	for i := 0; i < 50; i++ {
		c := fmt.Sprintf("crates/c%02d", i)
		tr.Sections = append(tr.Sections, Section{Component: c, Kind: KindProduct, Stops: []Stop{wtStop(i+1, c+"/lib.rs", types.QuadrantLow, "r", "l")}})
	}
	p, err := Walkthrough(tr, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Nodes) != maxWalkthroughNodes || len(p.Flows["product"].Hops) != maxWalkthroughNodes {
		t.Errorf("nodes = %d hops = %d, want both capped at %d", len(p.Nodes), len(p.Flows["product"].Hops), maxWalkthroughNodes)
	}
	if !strings.Contains(strings.Join(p.Intro, " "), "22 more components") {
		t.Errorf("intro = %v, want a note about the 22 components left out", p.Intro)
	}
	seen := map[[2]float64]bool{}
	maxY := 0.0
	for _, n := range p.Nodes {
		k := [2]float64{n.X, n.Y}
		if seen[k] {
			t.Errorf("two nodes at %v", k)
		}
		seen[k] = true
		maxY = max(maxY, n.Y)
	}
	for _, n := range p.Nodes {
		if n.Y == maxY && n.X-boxHalfWidth < keyRight {
			t.Errorf("a bottom-row node at %v, %v sits under the key", n.X, n.Y)
		}
	}
}

func TestGridCells(t *testing.T) {
	tests := []struct {
		n, wantCols, wantRows int
	}{
		{1, 2, 1}, // ceil(sqrt(1.9)) = 2 columns, one used
		{4, 3, 2},
		{12, 5, 3}, // 3 rows: the bottom row uses only the 3 columns clear of the key
		{20, 5, 5},
		{maxWalkthroughNodes, 5, 6},
	}
	for _, tt := range tests {
		cells := gridCells(tt.n)
		if len(cells) != tt.n {
			t.Fatalf("gridCells(%d) = %d cells", tt.n, len(cells))
		}
		xs, ys := map[float64]bool{}, map[float64]bool{}
		for _, c := range cells {
			xs[c[0]], ys[c[1]] = true, true
		}
		if tt.n > 1 && (len(xs) != tt.wantCols || len(ys) != tt.wantRows) {
			t.Errorf("gridCells(%d): %d cols x %d rows, want %d x %d", tt.n, len(xs), len(ys), tt.wantCols, tt.wantRows)
		}
	}
}

func TestRoleText(t *testing.T) {
	tests := []struct{ role, want string }{
		{"openshell-gateway", "gateway"},
		{"shared by 16 units", "16 units"},
		{"openshell", "openshell"}, // the repo-named unit keeps its name
		{"", ""},                   // no unit links the component
	}
	for _, tt := range tests {
		if got := roleText(tt.role, "openshell"); got != tt.want {
			t.Errorf("roleText(%q) = %q, want %q", tt.role, got, tt.want)
		}
	}
}
