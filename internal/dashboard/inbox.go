package dashboard

import (
	"fmt"
	"sort"
	"strings"

	"github.com/zanetworker/highstakes/internal/tour"
	"github.com/zanetworker/highstakes/internal/types"
)

// inboxTop is how many before-merge files the inbox shows by default.
const inboxTop = 10

// step is one before-merge action on a file and the signal that fired it.
type step struct {
	Text   string `json:"text"`
	Signal string `json:"signal,omitempty"`
}

// fileCard is one Harden now or Protect file: everything a PR touching it
// needs before merge, and who should review it.
type fileCard struct {
	Path    string   `json:"path"`
	Verdict string   `json:"verdict"`
	Steps   []step   `json:"steps"`
	Owners  []string `json:"owners,omitempty"`
}

// groupItem is one file a backlog group applies to.
type groupItem struct {
	Path    string `json:"path"`
	Verdict string `json:"verdict"`
	Signal  string `json:"signal,omitempty"`
	Owner   string `json:"owner,omitempty"`
	Command string `json:"command,omitempty"`
}

// group is one backlog action kind across files, with every file's command
// joined into one copyable snippet (sorted).
type group struct {
	Kind    string      `json:"kind"`
	Title   string      `json:"title"`
	Harden  int         `json:"harden"` // Harden now files it covers
	Items   []groupItem `json:"items"`
	Snippet string      `json:"snippet,omitempty"`
}

// inbox is the dashboard's default view.
type inbox struct {
	BeforeMerge []fileCard `json:"before_merge"` // Harden now and Protect files, most severe first
	Top         int        `json:"top"`          // shown by default: inboxTop, plus files tied with the last
	Backlog     []group    `json:"backlog"`      // one group per backlog action kind
}

var postureRank = map[types.Quadrant]int{
	types.QuadrantFragileCritical: 3, types.QuadrantProtect: 2, types.QuadrantWatch: 1,
}

// groupTitles name a backlog kind by its file count (singular, plural).
var groupTitles = map[string][2]string{
	"codeowners":          {"Add %d CODEOWNERS line", "Add %d CODEOWNERS lines"},
	"split":               {"Split %d oversized file", "Split %d oversized files"},
	"document-invariants": {"Document invariants for %d file", "Document invariants for %d files"},
	"test-gap":            {"Add tests for %d untested file", "Add tests for %d untested files"},
	"complexity-issue":    {"Cut complexity in %d file", "Cut complexity in %d files"},
	"ownership-issue":     {"Spread ownership of %d file", "Spread ownership of %d files"},
}

// buildInbox ranks files most severe first (posture, then impact, then
// likelihood). Before merge gets one card per Harden now or Protect file
// with all its before-merge steps. Backlog groups identical actions across
// files, kinds covering the most Harden now files first. Test files,
// routine files and files without an explanation are left out.
func buildInbox(hm *types.Heatmap) inbox {
	var files []*types.FileHeat
	for _, f := range hm.Files {
		if f.Explanation != nil && f.ImpactAssessed && postureRank[f.Quadrant] > 0 && !tour.IsTestFile(f.Path) {
			files = append(files, f)
		}
	}
	sort.Slice(files, func(i, j int) bool {
		if c := compareSeverity(files[i], files[j]); c != 0 {
			return c > 0
		}
		return files[i].Path < files[j].Path
	})
	in := inbox{BeforeMerge: []fileCard{}, Backlog: []group{}}
	var cardFiles []*types.FileHeat
	groups := map[string]*group{}
	for _, f := range files {
		owners := fileOwners(f.Explanation)
		c := fileCard{Path: f.Path, Verdict: f.Quadrant.Label(), Owners: owners}
		for _, a := range f.Explanation.Actions {
			switch a.Scope {
			case types.ScopeBeforeMerge:
				c.Steps = append(c.Steps, step{Text: a.Text, Signal: a.Signal})
			case types.ScopeBacklog:
				g := groups[a.ID]
				if g == nil {
					g = &group{Kind: a.ID, Title: a.Text}
					groups[a.ID] = g
				}
				it := groupItem{Path: f.Path, Verdict: c.Verdict, Signal: a.Signal, Command: a.Command}
				if len(owners) > 0 {
					it.Owner = owners[0]
				}
				g.Items = append(g.Items, it)
				if f.Quadrant == types.QuadrantFragileCritical {
					g.Harden++
				}
			}
		}
		if len(c.Steps) > 0 && postureRank[f.Quadrant] >= postureRank[types.QuadrantProtect] {
			in.BeforeMerge = append(in.BeforeMerge, c)
			cardFiles = append(cardFiles, f)
		}
	}
	in.Top = topWithTies(cardFiles, inboxTop)
	for _, g := range groups {
		g.Title = groupTitle(g.Kind, g.Title, len(g.Items))
		g.Snippet = snippet(g.Items)
		in.Backlog = append(in.Backlog, *g)
	}
	sort.Slice(in.Backlog, func(i, j int) bool {
		a, b := in.Backlog[i], in.Backlog[j]
		if a.Harden != b.Harden {
			return a.Harden > b.Harden
		}
		if len(a.Items) != len(b.Items) {
			return len(a.Items) > len(b.Items)
		}
		return a.Kind < b.Kind
	})
	return in
}

// compareSeverity orders by posture, then impact, then likelihood: >0 when a
// is more severe, 0 when tied.
func compareSeverity(a, b *types.FileHeat) int {
	if ra, rb := postureRank[a.Quadrant], postureRank[b.Quadrant]; ra != rb {
		return ra - rb
	}
	if a.Impact != b.Impact {
		return a.Impact - b.Impact
	}
	return a.Likelihood - b.Likelihood
}

// topWithTies is n, or fewer when there are fewer files, extended past n
// while files tie with the nth so a cut never splits equals.
func topWithTies(sorted []*types.FileHeat, n int) int {
	if len(sorted) <= n {
		return len(sorted)
	}
	for n < len(sorted) && compareSeverity(sorted[n-1], sorted[n]) == 0 {
		n++
	}
	return n
}

// groupTitle counts the files a kind covers; unknown kinds keep the action text.
func groupTitle(kind, text string, n int) string {
	t, ok := groupTitles[kind]
	if !ok {
		t = [2]string{text + ": %d file", text + ": %d files"}
	}
	if n == 1 {
		return fmt.Sprintf(t[0], n)
	}
	return fmt.Sprintf(t[1], n)
}

// snippet joins the items' commands, sorted, one per line.
func snippet(items []groupItem) string {
	var cmds []string
	for _, it := range items {
		if it.Command != "" {
			cmds = append(cmds, it.Command)
		}
	}
	sort.Strings(cmds)
	return strings.Join(cmds, "\n")
}

// fileOwners are the distinct owners named by the file's actions, in order.
func fileOwners(e *types.Explanation) []string {
	var out []string
	seen := map[string]bool{}
	for _, a := range e.Actions {
		for _, o := range a.Owners {
			if !seen[o] {
				seen[o] = true
				out = append(out, o)
			}
		}
	}
	return out
}
