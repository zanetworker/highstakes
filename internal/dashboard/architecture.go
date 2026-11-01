package dashboard

import (
	"path"
	"sort"
	"strings"

	"github.com/zanetworker/highstakes/internal/scorer"
	"github.com/zanetworker/highstakes/internal/tour"
	"github.com/zanetworker/highstakes/internal/types"
	"github.com/zanetworker/highstakes/internal/units"
)

// archNode is one directory (Go package) in the Architecture view.
type archNode struct {
	ID      string `json:"id"`       // repo-relative directory, "." for root
	Files   int    `json:"files"`    // file count
	Lines   int    `json:"lines"`    // total lines; drives node size
	MaxHeat int    `json:"max_heat"` // heat of the hottest file
	Tier    string `json:"tier"`     // most severe tier in the directory
	Hottest string `json:"hottest"`  // path of the hottest file
	Level   int    `json:"level"`    // dependency depth; 0 = imports no other directory
	// MaxImpact is the highest LLM-assessed impact (0 when none assessed).
	// ImpactTier, the node colour, is the tier of MaxImpact, or Tier when no
	// file in the directory was assessed. Likely counts likely files and
	// drives the fragility marker.
	MaxImpact  int    `json:"max_impact"`
	ImpactTier string `json:"impact_tier"`
	Likely     int    `json:"likely"`
	// Unit view only: Kind is "unit" or "shared", Label the display name,
	// Members the components drawn in the box, Placement a short inferred
	// placement ("Dockerfile.worker, helm worker").
	Kind      string   `json:"kind,omitempty"`
	Label     string   `json:"label,omitempty"`
	Members   []string `json:"members,omitempty"`
	Placement string   `json:"placement,omitempty"`
}

// maxEdgeLinks caps the file links embedded per edge.
const maxEdgeLinks = 200

// archEdge says Count distinct files in From import a file in To. Links are
// the file-level imports (importer, imported) behind the arrow, at most
// maxEdgeLinks of LinksTotal.
type archEdge struct {
	From       string      `json:"from"`
	To         string      `json:"to"`
	Count      int         `json:"count"`
	Links      [][2]string `json:"links"`
	LinksTotal int         `json:"links_total"`
}

type architecture struct {
	Nodes []archNode `json:"nodes"`
	Edges []archEdge `json:"edges"`
}

var tierRank = map[types.Tier]int{types.TierLow: 0, types.TierMedium: 1, types.TierHigh: 2, types.TierCritical: 3}

// aggregate groups non-test files into nodes by key ("" = left out).
func aggregate(hm *types.Heatmap, key func(*types.FileHeat) string) map[string]*archNode {
	nodes := map[string]*archNode{}
	assessed := map[string]bool{}
	for _, f := range hm.Files {
		if tour.IsTestFile(f.Path) {
			continue
		}
		d := key(f)
		if d == "" {
			continue
		}
		n := nodes[d]
		if n == nil {
			n = &archNode{ID: d, MaxHeat: -1, Tier: string(types.TierLow)}
			nodes[d] = n
		}
		n.Files++
		n.Lines += f.Size.Lines
		if f.HeatScore > n.MaxHeat || (f.HeatScore == n.MaxHeat && f.Path < n.Hottest) {
			n.MaxHeat, n.Hottest = f.HeatScore, f.Path
		}
		if tierRank[f.Tier] > tierRank[types.Tier(n.Tier)] {
			n.Tier = string(f.Tier)
		}
		if f.ImpactAssessed {
			assessed[d] = true
			n.MaxImpact = max(n.MaxImpact, f.Impact)
		}
		if cutoff := hm.Metadata.LikelyCutoff; cutoff > 0 && f.Likelihood >= cutoff {
			n.Likely++
		}
	}
	for d, n := range nodes {
		n.ImpactTier = n.Tier
		if assessed[d] {
			n.ImpactTier = string(scorer.CalculateTier(n.MaxImpact, scorer.DefaultTierThresholds()))
		}
	}
	return nodes
}

// edges aggregates the file import graph to node edges, keeping the file
// links behind each. key maps a file to its node ("" = left out).
func edges(hm *types.Heatmap, key func(string) string) []archEdge {
	type pair struct{ from, to string }
	links := map[pair][][2]string{}
	importers := map[pair]map[string]bool{}
	for from, tos := range units.Imports(hm) {
		a := key(from)
		for _, to := range tos {
			b := key(to)
			if a == "" || b == "" || a == b {
				continue
			}
			k := pair{a, b}
			links[k] = append(links[k], [2]string{from, to})
			if importers[k] == nil {
				importers[k] = map[string]bool{}
			}
			importers[k][from] = true
		}
	}
	out := []archEdge{}
	for k, ls := range links {
		sort.Slice(ls, func(i, j int) bool {
			if ls[i][0] != ls[j][0] {
				return ls[i][0] < ls[j][0]
			}
			return ls[i][1] < ls[j][1]
		})
		e := archEdge{From: k.from, To: k.to, Count: len(importers[k]), Links: ls, LinksTotal: len(ls)}
		if len(ls) > maxEdgeLinks {
			e.Links = ls[:maxEdgeLinks]
		}
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].From != out[j].From {
			return out[i].From < out[j].From
		}
		return out[i].To < out[j].To
	})
	return out
}

// graphOf turns edges into the count graph tour.Levels expects.
func graphOf(es []archEdge) map[string]map[string]int {
	g := map[string]map[string]int{}
	for _, e := range es {
		if g[e.From] == nil {
			g[e.From] = map[string]int{}
		}
		g[e.From][e.To] = e.Count
	}
	return g
}

// sortNodes orders nodes by level, then max heat, then id.
func sortNodes(ns []archNode) {
	sort.Slice(ns, func(i, j int) bool {
		a, b := ns[i], ns[j]
		if a.Level != b.Level {
			return a.Level < b.Level
		}
		if a.MaxHeat != b.MaxHeat {
			return a.MaxHeat > b.MaxHeat
		}
		return a.ID < b.ID
	})
}

// buildArchitecture aggregates files to directory nodes and the import graph
// to directory edges. Nodes are ordered by level, then max heat, then name.
func buildArchitecture(hm *types.Heatmap) architecture {
	nodes := aggregate(hm, func(f *types.FileHeat) string { return tour.Dir(f.Path) })
	arch := architecture{Nodes: []archNode{}, Edges: edges(hm, tour.Dir)}
	ids := make([]string, 0, len(nodes))
	for id := range nodes {
		ids = append(ids, id)
	}
	levels := tour.Levels(ids, graphOf(arch.Edges))
	for _, id := range ids {
		nodes[id].Level = levels[id]
		arch.Nodes = append(arch.Nodes, *nodes[id])
	}
	sortNodes(arch.Nodes)
	return arch
}

// unitView is the Architecture view grouped by runtime unit: one node per
// unit (its own components) and one per shared component, drawn as a band
// below the units.
type unitView struct {
	Nodes    []archNode `json:"nodes"`
	Edges    []archEdge `json:"edges"`
	Unlinked int        `json:"unlinked"` // non-test files no unit links
}

// buildUnitView groups files into unit and shared-component nodes (see
// units.Assign). It also returns each file's node id ("" when no unit links
// it).
func buildUnitView(hm *types.Heatmap) (unitView, map[string]string) {
	view := unitView{Nodes: []archNode{}, Edges: []archEdge{}}
	fileNode, unlinked := units.Assign(hm)
	if len(hm.Metadata.Units) == 0 {
		return view, fileNode
	}
	view.Unlinked = unlinked
	shared := map[string]bool{}
	for _, u := range hm.Metadata.Units {
		for _, c := range u.Shared {
			shared[c] = true
		}
	}
	owner := units.Owners(hm.Metadata.Units)
	nodes := aggregate(hm, func(f *types.FileHeat) string { return fileNode[f.Path] })
	view.Edges = edges(hm, func(p string) string { return fileNode[p] })

	var sharedIDs []string
	for c := range shared {
		id := "c:" + c
		if nodes[id] == nil {
			nodes[id] = &archNode{ID: id, Tier: string(types.TierLow), ImpactTier: string(types.TierLow)}
		}
		nodes[id].Kind, nodes[id].Label, nodes[id].Members = "shared", c, []string{c}
		sharedIDs = append(sharedIDs, id)
	}
	sharedGraph := map[string]map[string]int{}
	for _, e := range view.Edges {
		if strings.HasPrefix(e.From, "c:") && strings.HasPrefix(e.To, "c:") {
			if sharedGraph[e.From] == nil {
				sharedGraph[e.From] = map[string]int{}
			}
			sharedGraph[e.From][e.To] = e.Count
		}
	}
	levels := tour.Levels(sharedIDs, sharedGraph)
	top := 0
	for _, id := range sharedIDs {
		nodes[id].Level = levels[id]
		top = max(top, levels[id]+1)
	}
	for _, u := range hm.Metadata.Units {
		id := "u:" + u.Name
		if nodes[id] == nil {
			nodes[id] = &archNode{ID: id, Tier: string(types.TierLow), ImpactTier: string(types.TierLow), Hottest: u.Entry}
		}
		n := nodes[id]
		n.Kind, n.Label, n.Level, n.Placement = "unit", u.Name, top, placementLabel(u.InferredPlacement)
		for _, c := range u.Components {
			if !shared[c] && (owner[c] == "" || owner[c] == u.Name) {
				n.Members = append(n.Members, c)
			}
		}
	}
	for _, n := range nodes {
		view.Nodes = append(view.Nodes, *n)
	}
	sortNodes(view.Nodes)
	return view, fileNode
}

// placementLabel shortens inferred placements for a box label: the
// Dockerfile name, "helm <values key>", or "k8s <manifest name>".
func placementLabel(ps []types.Placement) string {
	var out []string
	seen := map[string]bool{}
	for _, p := range ps {
		file, _, _ := strings.Cut(p.Evidence, ":")
		l := path.Base(file)
		switch p.Kind {
		case "helm":
			if _, rest, ok := strings.Cut(p.Detail, ": "); ok && len(strings.Fields(rest)) > 0 {
				l = "helm " + strings.Fields(rest)[0]
			}
		case "k8s":
			l = "k8s " + l
		}
		if !seen[l] {
			seen[l] = true
			out = append(out, l)
		}
	}
	return strings.Join(out, ", ")
}
