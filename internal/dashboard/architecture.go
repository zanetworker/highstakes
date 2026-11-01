package dashboard

import (
	"sort"

	"github.com/zanetworker/highstakes/internal/scorer"
	"github.com/zanetworker/highstakes/internal/tour"
	"github.com/zanetworker/highstakes/internal/types"
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
}

// archEdge says Count distinct files in From import a file in To.
type archEdge struct {
	From  string `json:"from"`
	To    string `json:"to"`
	Count int    `json:"count"`
}

type architecture struct {
	Nodes []archNode `json:"nodes"`
	Edges []archEdge `json:"edges"`
}

var tierRank = map[types.Tier]int{types.TierLow: 0, types.TierMedium: 1, types.TierHigh: 2, types.TierCritical: 3}

// buildArchitecture aggregates files to directory nodes and the import graph
// to directory edges. Nodes are ordered by level, then max heat, then name.
func buildArchitecture(hm *types.Heatmap) architecture {
	nodes := map[string]*archNode{}
	assessed := map[string]bool{}
	for _, f := range hm.Files {
		if tour.IsTestFile(f.Path) {
			continue
		}
		d := tour.Dir(f.Path)
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

	graph := tour.DirGraph(hm)
	ids := make([]string, 0, len(nodes))
	for id := range nodes {
		ids = append(ids, id)
	}
	levels := tour.Levels(ids, graph)

	arch := architecture{Nodes: []archNode{}, Edges: []archEdge{}}
	for _, id := range ids {
		nodes[id].Level = levels[id]
		arch.Nodes = append(arch.Nodes, *nodes[id])
	}
	sort.Slice(arch.Nodes, func(i, j int) bool {
		a, b := arch.Nodes[i], arch.Nodes[j]
		if a.Level != b.Level {
			return a.Level < b.Level
		}
		if a.MaxHeat != b.MaxHeat {
			return a.MaxHeat > b.MaxHeat
		}
		return a.ID < b.ID
	})

	for from, tos := range graph {
		for to, c := range tos {
			arch.Edges = append(arch.Edges, archEdge{From: from, To: to, Count: c})
		}
	}
	sort.Slice(arch.Edges, func(i, j int) bool {
		if arch.Edges[i].From != arch.Edges[j].From {
			return arch.Edges[i].From < arch.Edges[j].From
		}
		return arch.Edges[i].To < arch.Edges[j].To
	})
	return arch
}
