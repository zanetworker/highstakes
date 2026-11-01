package dashboard

import (
	"sort"

	"github.com/zanetworker/highstakes/internal/tour"
	"github.com/zanetworker/highstakes/internal/types"
)

// card is one item of the action inbox: an imperative, the signal that
// triggered it, the file, an owner and (backlog) a copyable command.
type card struct {
	Path    string `json:"path"`
	Verdict string `json:"verdict"`
	Text    string `json:"text"`
	Signal  string `json:"signal"`
	Owner   string `json:"owner,omitempty"`
	Command string `json:"command,omitempty"`
	More    int    `json:"more,omitempty"` // before merge: further before-merge actions on the file
}

// inbox is the dashboard's default view.
type inbox struct {
	BeforeMerge []card `json:"before_merge"` // Harden now and Protect files, one card each
	Backlog     []card `json:"backlog"`      // every backlog action
}

var postureRank = map[types.Quadrant]int{
	types.QuadrantFragileCritical: 3, types.QuadrantProtect: 2, types.QuadrantWatch: 1,
}

// buildInbox ranks files most severe first (posture, then impact, then
// likelihood). Before merge gets one card per Harden now or Protect file:
// its top before-merge action. Backlog gets every backlog action. Test
// files, routine files and files without an explanation are left out.
func buildInbox(hm *types.Heatmap) inbox {
	var files []*types.FileHeat
	for _, f := range hm.Files {
		if f.Explanation != nil && f.ImpactAssessed && postureRank[f.Quadrant] > 0 && !tour.IsTestFile(f.Path) {
			files = append(files, f)
		}
	}
	sort.Slice(files, func(i, j int) bool {
		a, b := files[i], files[j]
		if ra, rb := postureRank[a.Quadrant], postureRank[b.Quadrant]; ra != rb {
			return ra > rb
		}
		if a.Impact != b.Impact {
			return a.Impact > b.Impact
		}
		if a.Likelihood != b.Likelihood {
			return a.Likelihood > b.Likelihood
		}
		return a.Path < b.Path
	})
	in := inbox{BeforeMerge: []card{}, Backlog: []card{}}
	for _, f := range files {
		owner := fileOwner(f.Explanation)
		var before []types.Action
		for _, a := range f.Explanation.Actions {
			switch a.Scope {
			case types.ScopeBeforeMerge:
				before = append(before, a)
			case types.ScopeBacklog:
				in.Backlog = append(in.Backlog, card{Path: f.Path, Verdict: f.Quadrant.Label(), Text: a.Text,
					Signal: a.Signal, Owner: owner, Command: a.Command})
			}
		}
		if len(before) > 0 && postureRank[f.Quadrant] >= postureRank[types.QuadrantProtect] {
			in.BeforeMerge = append(in.BeforeMerge, card{Path: f.Path, Verdict: f.Quadrant.Label(), Text: before[0].Text,
				Signal: before[0].Signal, Owner: owner, More: len(before) - 1})
		}
	}
	return in
}

// fileOwner is the first owner named by any of the file's actions.
func fileOwner(e *types.Explanation) string {
	for _, a := range e.Actions {
		if len(a.Owners) > 0 {
			return a.Owners[0]
		}
	}
	return ""
}
