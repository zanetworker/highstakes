package dashboard

import (
	"sort"

	"github.com/zanetworker/highstakes/internal/tour"
	"github.com/zanetworker/highstakes/internal/types"
)

// archNode is one directory (Go package) in the Architecture view.
type archNode struct {
	ID      string `json:"id"`       // repo-relative directory, "." for root
	Files   int    `json:"files"`    // file count
	Lines   int    `json:"lines"`    // total lines; drives node size
	MaxHeat int    `json:"max_heat"` // heat of the hottest file
	Tier    string `json:"tier"`     // most severe tier in the directory; drives color
	Hottest string `json:"hottest"`  // path of the hottest file
	Level   int    `json:"level"`    // dependency depth; 0 = imports no other directory
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
