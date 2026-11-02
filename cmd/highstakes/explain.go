package main

import (
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/zanetworker/highstakes/internal/explain"
	"github.com/zanetworker/highstakes/internal/gitanalyzer"
	"github.com/zanetworker/highstakes/internal/prscore"
	"github.com/zanetworker/highstakes/internal/tour"
	"github.com/zanetworker/highstakes/internal/types"
)

// explanationBlock renders a file for `get`: verdict, the two questions
// with repo cutoffs, before-merge and backlog checklists, why, latest fix.
// Empty for heatmaps written before explanations existed.
func explanationBlock(f *types.FileHeat, likelyCutoff, impactCutoff int) string {
	e := f.Explanation
	if e == nil {
		return ""
	}
	var b strings.Builder
	verdict := e.Verdict
	if verdict == "" {
		verdict = explain.Verdict(f, false)
	}
	b.WriteString(f.Path + "\n" + strings.ToUpper(verdict) + "\n")
	b.WriteString(axisLine(types.AxisLikelihood, f.Likelihood, likelyCutoff))
	if f.ImpactAssessed {
		b.WriteString(axisLine(types.AxisImpact, f.Impact, impactCutoff))
	} else {
		fmt.Fprintf(&b, "%-22s %s\n", types.AxisImpact, fmt.Sprintf("~%d estimate (run analyze with an LLM to assess)", f.Impact))
	}
	b.WriteString(types.AxisNote + "\n" + types.AxisEvidence + "\n")

	checklists := []struct{ title, scope string }{
		{"Do before the next merge", types.ScopeBeforeMerge}, {"Put on the backlog", types.ScopeBacklog},
	}
	for i, sec := range checklists {
		first := true
		for _, a := range e.Actions {
			if a.Scope != sec.scope {
				continue
			}
			if first {
				if i == 0 || !hasScope(e.Actions, types.ScopeBeforeMerge) {
					b.WriteString("\n")
				}
				b.WriteString(sec.title + "\n")
				first = false
			}
			b.WriteString("  " + checkbox(a.Done) + " " + a.Text + "\n")
			if a.Signal != "" {
				b.WriteString("      why: " + a.Signal + "\n")
			}
			if a.Command != "" {
				b.WriteString("      " + a.Command + "\n")
			}
		}
	}

	if len(e.LikelihoodReasons) > 0 || len(e.ImpactReasons) > 0 {
		b.WriteString("\nWhy\n")
		if len(e.LikelihoodReasons) > 0 {
			b.WriteString("  Break signals: " + strings.Join(e.LikelihoodReasons, "; ") + "\n")
		}
		if len(e.ImpactReasons) > 0 {
			b.WriteString("  Break cost: " + strings.Join(e.ImpactReasons, "; ") + "\n")
		}
	}
	for _, c := range f.RecentChanges { // newest first
		if gitanalyzer.IsBugFix(c.Message) {
			fmt.Fprintf(&b, "Latest fix  %s  %s\n", c.Date.Format("2006-01-02"), c.Message)
			break
		}
	}
	return b.String()
}

// axisLine is "Will it break?         50  HIGH  repo cutoff 33".
func axisLine(question string, score, cutoff int) string {
	return fmt.Sprintf("%-22s %-3d %-5s repo cutoff %d\n", question, score, types.AxisLevel(score, cutoff), cutoff)
}

func checkbox(done bool) string {
	if done {
		return "[x]"
	}
	return "[ ]"
}

func hasScope(as []types.Action, scope string) bool {
	for _, a := range as {
		if a.Scope == scope {
			return true
		}
	}
	return false
}

// buildChange collects what explain.ForPR needs to know about the PR.
// codeowned (may be nil) reports CODEOWNERS coverage at HEAD.
func buildChange(changes []prscore.FileChange, hm *types.Heatmap, approvals []string, codeowned func(string) bool) explain.Change {
	c := explain.Change{Approvals: approvals, Codeowned: codeowned, Deleted: map[string]bool{}}
	components := map[string]bool{}
	for _, ch := range changes {
		c.Paths = append(c.Paths, ch.Path)
		if ch.Deleted {
			c.Deleted[ch.Path] = true
		}
		c.Lines += ch.LinesAdded + ch.LinesDeleted
		if tour.IsTestFile(ch.Path) {
			continue
		}
		comp := path.Dir(ch.Path)
		if f, ok := hm.Files[ch.Path]; ok && f.Component != "" {
			comp = f.Component
		}
		components[comp] = true
	}
	c.Components = len(components)
	return c
}

// attachPRActions sets each scored file's before-merge actions (routine
// files stay quiet) and the PR's deduplicated checklist (Harden now and
// Protect files), worth-doing items (the rest) and backlog, most severe
// file first.
func attachPRActions(risk *types.PRRisk, hm *types.Heatmap, c explain.Change) {
	order := make([]int, len(risk.FilesChanged))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(i, j int) bool {
		a, b := risk.FilesChanged[order[i]], risk.FilesChanged[order[j]]
		if ra, rb := tierOrder[a.Tier], tierOrder[b.Tier]; ra != rb {
			return ra > rb
		}
		return a.Impact > b.Impact
	})
	var all []explain.FileActions
	for _, i := range order {
		fc := &risk.FilesChanged[i]
		f, ok := hm.Files[fc.Path]
		switch {
		case !ok:
			// A new file that inherits a blocking tier was never assessed, so it
			// still needs a review; without one the gate would read "all done".
			if !fc.Unscored || !types.Blocking(fc.Posture()) || tour.IsTestFile(fc.Path) {
				continue
			}
			f = unscoredReview(fc, directoryOwners(hm, fc.Path))
		case f.Explanation == nil && types.Blocking(fc.Posture()) && !tour.IsTestFile(fc.Path):
			// A heatmap from before actions existed: no data is not "done".
			f = unscoredReview(fc, directoryOwners(hm, fc.Path))
		}
		if types.Blocking(fc.Posture()) && !tour.IsTestFile(fc.Path) {
			f = withSafeguards(f, fc, directoryOwners(hm, fc.Path))
		}
		acts := explain.ForPR(f, c)
		fc.Actions = nil
		for _, a := range acts {
			if a.Scope == types.ScopeBeforeMerge && a.ID != "light-review" {
				fc.Actions = append(fc.Actions, a)
			}
		}
		all = append(all, explain.FileActions{Path: fc.Path, Blocking: types.Blocking(fc.Posture()), Actions: acts})
	}
	risk.Checklist, risk.WorthDoing, risk.Backlog = explain.Checklist(all)
}

var tierOrder = map[types.Tier]int{types.TierLow: 0, types.TierMedium: 1, types.TierHigh: 2, types.TierCritical: 3}

// unscoredReview is a stand-in record for a new, unassessed file that
// inherits Harden now or Protect: one review item that approvals and the
// sign-off label can satisfy like any other.
func unscoredReview(fc *types.FileChange, owners []string) *types.FileHeat {
	kind, signal := "the new file", "new file: not scored yet, treated like its directory ("+fc.Posture()+")"
	if !fc.Unscored {
		kind, signal = "the file", fc.Posture()+" file without action data: run analyze again"
	}
	id, who := "owner-review", "someone who knows its directory"
	if fc.Posture() == "Harden now" {
		id, who = "senior-review", "a senior owner of its directory"
	}
	noAuto := types.Action{ID: "no-auto-merge", Scope: types.ScopeBeforeMerge, Text: "Block auto-merge", Signal: "Protect files never auto-merge"}
	if fc.Posture() == "Harden now" {
		noAuto.Text, noAuto.Signal = "Keep auto-merge off", "Harden now files never auto-merge"
	}
	return &types.FileHeat{Path: fc.Path, Explanation: &types.Explanation{Actions: []types.Action{{
		ID: id, Scope: types.ScopeBeforeMerge, Owners: owners,
		Text:   "Review " + kind + " `" + fc.Path + "` with " + who,
		Signal: signal,
	}, noAuto}}}
}

// directoryOwners are the review owners of scored files in the file's
// directory, or the nearest parent with any, so an owner's approval can
// satisfy a new file's review item. At most three, in first-seen order.
func directoryOwners(hm *types.Heatmap, p string) []string {
	for dir := path.Dir(p); ; dir = path.Dir(dir) {
		var paths []string
		for q := range hm.Files {
			if path.Dir(q) == dir {
				paths = append(paths, q)
			}
		}
		sort.Strings(paths)
		var out []string
		seen := map[string]bool{}
		for _, q := range paths {
			f := hm.Files[q]
			if f.Explanation == nil {
				continue
			}
			for _, a := range f.Explanation.Actions {
				if a.ID != "senior-review" && a.ID != "owner-review" {
					continue
				}
				for _, o := range a.Owners {
					if !seen[o] && len(out) < 3 {
						seen[o] = true
						out = append(out, o)
					}
				}
			}
		}
		if len(out) > 0 || dir == "." || dir == "/" {
			return out
		}
	}
}

// withSafeguards returns f with a review item and the auto-merge item added
// when its own actions lack them: every Harden now or Protect file needs
// both, or an unassessed file's empty checklist would read "all done".
func withSafeguards(f *types.FileHeat, fc *types.FileChange, owners []string) *types.FileHeat {
	if f.Explanation == nil {
		return f
	}
	has := map[string]bool{}
	for _, a := range f.Explanation.Actions {
		has[a.ID] = true
	}
	review := has["senior-review"] || has["owner-review"]
	if review && has["no-auto-merge"] {
		return f
	}
	stand := unscoredReview(fc, owners).Explanation.Actions // [review, no-auto-merge]
	acts := append([]types.Action(nil), f.Explanation.Actions...)
	if !review {
		acts = append(acts, stand[0])
	}
	if !has["no-auto-merge"] {
		acts = append(acts, stand[1])
	}
	e := *f.Explanation
	e.Actions = acts
	out := *f
	out.Explanation = &e
	return &out
}
