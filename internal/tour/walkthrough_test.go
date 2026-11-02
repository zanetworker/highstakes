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
	for i, a := range actions {
		scope := types.ScopeBeforeMerge
		if i == len(actions)-1 && len(actions) > 1 {
			scope = types.ScopeBacklog
		}
		e.Actions = append(e.Actions, types.Action{ID: "x", Text: a, Scope: scope})
	}
	s.Explanation = e
	s.Verdict, s.TopBeforeMerge, s.TopBacklog = q.Label(), e.Top(types.ScopeBeforeMerge), e.Top(types.ScopeBacklog)
	return s
}

func wtTour() *Tour {
	return &Tour{Repo: "/src/acme", Commit: "abcdef1234", TotalStops: 4, LikelyCutoff: 33, ImpactThreshold: 78, ProductComponents: 3, Sections: []Section{
		{Order: 1, Component: "crates/acme-core", Kind: KindProduct, Stops: []Stop{
			wtStop(1, "crates/acme-core/src/policy.rs", types.QuadrantFragileCritical, "Every unit loads policy <here>", "Changed 40 times", "Add tests", "Pair review"),
		}},
		{Order: 2, Component: "crates/acme-gateway", Kind: KindProduct, Stops: []Stop{
			wtStop(2, "crates/acme-gateway/src/grpc.rs", types.QuadrantProtect, "Serves every RPC", ""),
			wtStop(3, "crates/acme-gateway/src/auth.rs", "", "", "Complex"),
		}},
		{Order: 3, Component: "examples/demo", Kind: KindSupporting, Stops: []Stop{
			wtStop(4, "examples/demo/src/main.rs", types.QuadrantLow, "Demo only", "Rarely changed", "Leave it"),
		}},
	}}
}

func TestWalkthrough(t *testing.T) {
	roles := map[string]string{"crates/acme-gateway": "acme-gateway", "crates/acme-core": "shared by 7 units"}
	links := []Link{
		{From: "crates/acme-gateway", To: "crates/acme-core", Files: 12},
		{From: "examples/demo", To: "crates/acme-gateway", Files: 1},
		{From: "crates/acme-gateway", To: "crates/not-drawn", Files: 30}, // no node: left out
	}
	p, err := Walkthrough(wtTour(), roles, links)
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
		gw := p.Nodes[p.NodeOf["crates/acme-gateway"]]
		if gw.Name != "gateway" || gw.Sub != "impact 90 · gateway" {
			t.Errorf("gateway node = %+v, want the repo-name prefix stripped from the name and the owning unit", gw)
		}
		if core := p.Nodes[p.NodeOf["crates/acme-core"]]; !core.Tp {
			t.Errorf("core = %+v, want tp (shared library)", core)
		}
	})

	t.Run("tour order becomes hops; product and supporting flows", func(t *testing.T) {
		prod, sup := p.Flows["product"], p.Flows["supporting"]
		if len(prod.Hops) != 3 || len(sup.Hops) != 1 || p.Start != "product" {
			t.Fatalf("flows = %+v, want 3 product hops and 1 supporting", p.Flows)
		}
		core, gw := p.NodeOf["crates/acme-core"], p.NodeOf["crates/acme-gateway"]
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
		for _, want := range []string{"Harden now", "Will it break? 50 HIGH, cutoff 33", "How bad if it breaks? 90 HIGH, cutoff 78",
			"Before merge: Add tests", "Backlog: Pair review"} {
			if !strings.Contains(h.Txt, want) {
				t.Errorf("caption = %q, want %q", h.Txt, want)
			}
		}
		if h := p.Flows["product"].Hops[2]; !strings.Contains(h.Txt, "security 90 · data 80") || h.Plane != "estimate" || h.Short != "read auth.rs" {
			t.Errorf("hop 3 = %+v, want the first impact reason when the stop has no why, estimate plane", h)
		}
	})

	t.Run("import links become a flow between drawn components", func(t *testing.T) {
		f, ok := p.Flows[linksFlowID]
		if !ok {
			t.Fatalf("flows = %v, want a links flow", p.Flows)
		}
		if f.Label != "How components connect (2 links)" || len(f.Hops) != 2 {
			t.Fatalf("links flow = %+v, want 2 hops (the link to an undrawn component dropped)", f)
		}
		gw, core := p.NodeOf["crates/acme-gateway"], p.NodeOf["crates/acme-core"]
		h := f.Hops[0]
		if h.F != gw || h.T != core || h.Ch != "f" || h.Pay != "12 files" || h.Plane != "imports" {
			t.Errorf("first link hop = %+v, want gateway -> core (importer to dependency), thin wire, 12 files", h)
		}
		if !strings.Contains(h.Short, "gateway imports core") {
			t.Errorf("short = %q, want it to name the direction", h.Short)
		}
		if f.Hops[1].Pay != "1 file" {
			t.Errorf("second hop payload = %q, want singular", f.Hops[1].Pay)
		}
		if p.Start != "product" {
			t.Errorf("start = %q, want the product tour first", p.Start)
		}
		// The build renders flow buttons in JSON key order, which encoding/json sorts.
		if linksFlowID <= KindProduct || linksFlowID <= KindSupporting {
			t.Errorf("links flow id %q sorts before the tour flows; its button would come first", linksFlowID)
		}
	})

	t.Run("legend is HighStakes' own, not the template's channel key", func(t *testing.T) {
		for _, want := range []string{"shared library", "imports", "reading order", `.legend .lcol > span:not(.hs){display:none}`, `.caption .chan{display:none}`} {
			if !strings.Contains(p.LegendExtra, want) {
				t.Errorf("legendExtra = %q, missing %q", p.LegendExtra, want)
			}
		}
		for _, k := range p.PlaneKey {
			if k[0] == "imports" {
				t.Errorf("planeKey %v: imports is a wire, keyed in legendExtra, not a quadrant dot", k)
			}
		}
		if p.Colors["imports"] == "" {
			t.Error("imports plane has no color")
		}
	})

	t.Run("every plane used has a color and a key entry", func(t *testing.T) {
		keys := map[string]bool{}
		for _, k := range p.PlaneKey {
			keys[k[0]] = true
		}
		for _, f := range p.Flows {
			for _, h := range f.Hops {
				if p.Colors[h.Plane] == "" || (!keys[h.Plane] && h.Plane != "imports") {
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
	if _, err := Walkthrough(&Tour{}, nil, nil); !errors.Is(err, ErrNoFiles) {
		t.Errorf("empty tour: err = %v, want ErrNoFiles", err)
	}
}

func TestWalkthrough_Capacity(t *testing.T) {
	tr := &Tour{Repo: "r"}
	for i := 0; i < 50; i++ {
		c := fmt.Sprintf("crates/c%02d", i)
		tr.Sections = append(tr.Sections, Section{Component: c, Kind: KindProduct, Stops: []Stop{wtStop(i+1, c+"/lib.rs", types.QuadrantLow, "r", "l")}})
	}
	p, err := Walkthrough(tr, nil, nil)
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
		{"acme-gateway", "gateway"},
		{"shared by 16 units", "16 units"},
		{"acme", "acme"}, // the repo-named unit keeps its name
		{"", ""},         // no unit links the component
	}
	for _, tt := range tests {
		if got := roleText(tt.role, "acme"); got != tt.want {
			t.Errorf("roleText(%q) = %q, want %q", tt.role, got, tt.want)
		}
	}
}

func TestComponentLinks(t *testing.T) {
	imp := func(ps ...string) types.Dependencies {
		var d types.Dependencies
		for _, p := range ps {
			d.Imports = append(d.Imports, types.Dependency{Path: p})
		}
		return d
	}
	markers := map[string]bool{"crates/api/Cargo.toml": true, "crates/core/Cargo.toml": true, "crates/db/Cargo.toml": true}
	hm := &types.Heatmap{Files: map[string]*types.FileHeat{
		"crates/api/src/a.rs":        {Dependencies: imp("crates/core/src/x.rs", "crates/core/src/y.rs", "crates/api/src/b.rs")},
		"crates/api/src/b.rs":        {Dependencies: imp("crates/core/src/x.rs", "crates/db/src/d.rs")},
		"crates/db/src/d.rs":         {Dependencies: imp("crates/core/src/x.rs")},
		"crates/api/tests/t_test.rs": {Dependencies: imp("crates/db/src/d.rs")}, // tests do not link components
		"crates/core/src/x.rs":       {},
		"crates/core/src/y.rs":       {},
	}}
	for p, f := range hm.Files {
		f.Path = p
	}
	got := ComponentLinks(hm, markers)
	want := []Link{
		{From: "crates/api", To: "crates/core", Files: 2}, // importing files, not import lines
		{From: "crates/api", To: "crates/db", Files: 1},
		{From: "crates/db", To: "crates/core", Files: 1},
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("ComponentLinks =\n%v\nwant\n%v", got, want)
	}
	if got := ComponentLinks(&types.Heatmap{}, nil); len(got) != 0 {
		t.Errorf("empty heatmap: %v, want none", got)
	}
}

func TestWalkthrough_LinkCap(t *testing.T) {
	tr := &Tour{Repo: "r"}
	var links []Link
	for i := 0; i < 12; i++ {
		c := fmt.Sprintf("c%02d", i)
		tr.Sections = append(tr.Sections, Section{Component: c, Kind: KindProduct, Stops: []Stop{wtStop(i+1, c+"/lib.rs", types.QuadrantLow, "r", "l")}})
	}
	for i := 0; i < 12; i++ {
		for j := 0; j < 12; j++ {
			if i != j {
				links = append(links, Link{From: fmt.Sprintf("c%02d", i), To: fmt.Sprintf("c%02d", j), Files: 200 - i*12 - j})
			}
		}
	}
	p, err := Walkthrough(tr, nil, links)
	if err != nil {
		t.Fatal(err)
	}
	hops := p.Flows[linksFlowID].Hops
	if len(hops) != maxWalkthroughLinks {
		t.Fatalf("link hops = %d, want capped at %d", len(hops), maxWalkthroughLinks)
	}
	if hops[0].Pay != "199 files" {
		t.Errorf("first link = %+v, want the strongest (199 files)", hops[0])
	}
}
