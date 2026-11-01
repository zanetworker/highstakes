// Package prcomment renders `pr check` as the Markdown PR comment the
// triage workflow posts: verdict first, then the before-merge checklist, a
// short file table, and the reasons folded away.
package prcomment

import (
	"fmt"
	"sort"
	"strings"

	"github.com/zanetworker/highstakes/internal/types"
)

// Marker starts every comment so the workflow can find and update its own.
const Marker = "<!-- highstakes-triage -->"

// maxRows is how many non-routine files the table lists; the rest get one
// "need action" row and stay in the details.
const maxRows = 5

var headings = map[string]string{
	"Harden now": "⛔ Harden before merge",
	"Protect":    "🛡 Protect",
	"Stabilize":  "🔧 Stabilize",
	"Routine":    "✅ Routine",
}

// MaxCommentChars keeps the comment under GitHub's 65,536-character limit for
// issue comments, with headroom; past it the API rejects the post (422).
const MaxCommentChars = 60000

// reserve is room kept for the omitted notice, reviewers, gate and closing tags.
const reserve = 2000

// fits reports whether adding s keeps the comment within budget.
func fits(b *strings.Builder, s string) bool { return b.Len()+len(s) <= MaxCommentChars-reserve }

var severity = map[string]int{"Harden now": 3, "Protect": 2, "Stabilize": 1, "Routine": 0}

// posture is a changed file's posture: its quadrant label, else the
// posture of its tier (unassessed or unscored files).
func posture(fc types.FileChange) string {
	if l := fc.Quadrant.Label(); l != "" {
		return l
	}
	return types.TierPosture(fc.Tier)
}

// Render returns the comment for a scored PR. hm supplies per-file reasons;
// files missing from it simply have none.
func Render(risk *types.PRRisk, hm *types.Heatmap) string {
	verdict := risk.Verdict
	if verdict == "" {
		verdict = types.TierPosture(risk.Tier)
	}
	files := ranked(risk.FilesChanged)
	var b strings.Builder
	b.WriteString(Marker + "\n")
	fmt.Fprintf(&b, "## %s\n\n", headings[verdict])
	b.WriteString(summary(verdict, files, hm) + "\n\n")

	if len(risk.Checklist) > 0 {
		b.WriteString("### Do before merge\n\n")
		for i, a := range risk.Checklist {
			box := " "
			if a.Done {
				box = "x"
			}
			line := fmt.Sprintf("- [%s] %s (%s)\n", box, a.Text, codeList(a.Files))
			if !fits(&b, line) {
				fmt.Fprintf(&b, "- _%s omitted to fit GitHub's comment size limit; run `highstakes pr check` for the full list_\n", plural(len(risk.Checklist)-i, "item"))
				break
			}
			b.WriteString(line)
		}
		b.WriteString("\n")
	}

	// Circuit breakers (unscored files, large diffs, low test ratio) stay
	// visible: they flag where the scores themselves are not to be trusted.
	if len(risk.CircuitBreakerSignals) > 0 {
		b.WriteString("### Warnings\n\n")
		for _, s := range risk.CircuitBreakerSignals {
			line := "- " + s + "\n"
			if room := MaxCommentChars - reserve - b.Len(); len(line) > room/2 {
				// One signal can list thousands of paths: keep its start, cut the rest.
				line = "- " + s[:max(0, room/2-200)] + "... (omitted to fit GitHub's comment size limit)\n"
			}
			b.WriteString(line)
		}
		b.WriteString("\n")
	}

	writeTable(&b, files, risk.LikelyCutoff, risk.ImpactThreshold)
	writeDetails(&b, risk, files, hm)
	return b.String()
}

// ranked orders files most severe first, then by impact and likelihood.
func ranked(fcs []types.FileChange) []types.FileChange {
	out := append([]types.FileChange(nil), fcs...)
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if sa, sb := severity[posture(a)], severity[posture(b)]; sa != sb {
			return sa > sb
		}
		if a.Impact != b.Impact {
			return a.Impact > b.Impact
		}
		if a.Likelihood != b.Likelihood {
			return a.Likelihood > b.Likelihood
		}
		return a.Path < b.Path
	})
	return out
}

// summary is the one sentence under the verdict: the riskiest file(s), why.
func summary(verdict string, files []types.FileChange, hm *types.Heatmap) string {
	var top []string
	for _, fc := range files {
		if posture(fc) == verdict {
			top = append(top, fc.Path)
		}
	}
	if verdict == "Routine" || len(top) == 0 {
		return "No changed file is likely to break or costly if it does. Normal review; auto-merge allowed by policy."
	}
	names := "`" + top[0] + "` is"
	switch {
	case len(top) == 2:
		names = "`" + top[0] + "` and `" + top[1] + "` are"
	case len(top) > 2:
		names = fmt.Sprintf("`%s` and %d other files are", top[0], len(top)-1)
	}
	why := map[string]string{
		"Harden now": "likely to break and costly if broken",
		"Protect":    "costly if broken",
		"Stabilize":  "likely to break, with limited damage",
	}[verdict]
	s := names + " " + why
	if f, ok := hm.Files[top[0]]; ok && f.Factors.BlastRadius.Summary != "" {
		s += ": " + strings.TrimSuffix(f.Factors.BlastRadius.Summary, ".")
	}
	return s + "."
}

func writeTable(b *strings.Builder, files []types.FileChange, likelyCutoff, impactCutoff int) {
	b.WriteString("| File | Will it break? | How bad? | Action |\n|---|---|---|---|\n")
	shown, more, rest := 0, 0, 0
	for _, fc := range files {
		switch {
		case posture(fc) == "Routine":
			rest++
		case shown == maxRows:
			more++
		default:
			shown++
			fmt.Fprintf(b, "| `%s` | %s | %s | %s |\n", cell(fc.Path), breaks(fc, likelyCutoff), howBad(fc, impactCutoff), posture(fc))
		}
	}
	if more > 0 {
		need := "files need"
		if more == 1 {
			need = "file needs"
		}
		fmt.Fprintf(b, "| %d more %s action |  |  | see details |\n", more, need)
	}
	switch {
	case rest > 0 && shown > 0:
		fmt.Fprintf(b, "| %d other %s | low | low | Routine |\n", rest, plural(rest, "file"))
	case rest > 0:
		fmt.Fprintf(b, "| %d %s | low | low | Routine |\n", rest, plural(rest, "file"))
	}
	b.WriteString("\n")
}

func breaks(fc types.FileChange, cutoff int) string {
	if fc.Unscored {
		return "new file"
	}
	return fmt.Sprintf("%d %s", fc.Likelihood, strings.ToLower(types.AxisLevel(fc.Likelihood, cutoff)))
}

func howBad(fc types.FileChange, cutoff int) string {
	switch {
	case fc.Unscored:
		return "inherited: " + strings.ToLower(posture(fc))
	case !fc.ImpactAssessed:
		return fmt.Sprintf("~%d estimate", fc.Impact)
	}
	return fmt.Sprintf("%d %s", fc.Impact, strings.ToLower(types.AxisLevel(fc.Impact, cutoff)))
}

func writeDetails(b *strings.Builder, risk *types.PRRisk, files []types.FileChange, hm *types.Heatmap) {
	b.WriteString("<details><summary>Why these actions?</summary>\n\n")
	fmt.Fprintf(b, "%s %s Repo cutoffs: will it break %d, how bad %d (a score at or above its cutoff is high).\n\n",
		types.AxisNote, types.AxisEvidence, risk.LikelyCutoff, risk.ImpactThreshold)
	omittedFiles := 0
	for _, fc := range files {
		if posture(fc) == "Routine" {
			continue
		}
		var e strings.Builder
		writeFileReasons(&e, risk, fc, hm)
		if omittedFiles > 0 || !fits(b, e.String()) {
			omittedFiles++
			continue
		}
		b.WriteString(e.String())
	}
	if omittedFiles > 0 {
		fmt.Fprintf(b, "_%s omitted to fit GitHub's comment size limit; run `highstakes pr check` for the full list._\n\n", plural(omittedFiles, "more file"))
	}
	writeReviewersBacklogGate(b, risk)
	b.WriteString("</details>\n")
}

// writeFileReasons writes one non-routine file's reasons and checklist signals.
func writeFileReasons(b *strings.Builder, risk *types.PRRisk, fc types.FileChange, hm *types.Heatmap) {
	fmt.Fprintf(b, "**`%s`** (%s)\n", fc.Path, posture(fc))
	if f, ok := hm.Files[fc.Path]; ok && f.Explanation != nil {
		if r := f.Explanation.LikelihoodReasons; len(r) > 0 {
			fmt.Fprintf(b, "- Break signals: %s\n", strings.Join(r, "; "))
		}
		if r := f.Explanation.ImpactReasons; len(r) > 0 {
			fmt.Fprintf(b, "- Break cost: %s\n", strings.Join(r, "; "))
		}
	} else if fc.Unscored {
		b.WriteString("- New file: not scored yet, so it takes the tier of its directory\n")
	}
	for _, a := range risk.Checklist {
		if contains(a.Files, fc.Path) && a.Signal != "" {
			fmt.Fprintf(b, "- %s: %s\n", a.Text, a.Signal)
		}
	}
	b.WriteString("\n")
}

// writeReviewersBacklogGate writes the tail of the details block.
func writeReviewersBacklogGate(b *strings.Builder, risk *types.PRRisk) {
	if len(risk.SuggestedReviewers) > 0 {
		var names []string
		for _, r := range risk.SuggestedReviewers {
			names = append(names, r.Handle)
		}
		fmt.Fprintf(b, "**Suggested reviewers:** %s\n\n", strings.Join(names, ", "))
	}
	if len(risk.Backlog) > 0 {
		b.WriteString("**Backlog** (the file over time, not this PR)\n")
		for i, a := range risk.Backlog {
			item := fmt.Sprintf("- %s (%s): %s\n", a.Text, codeList(a.Files), a.Signal)
			if a.Command != "" {
				item += fmt.Sprintf("  `%s`\n", a.Command)
			}
			if !fits(b, item) {
				fmt.Fprintf(b, "- _%s omitted to fit GitHub's comment size limit_\n", plural(len(risk.Backlog)-i, "backlog item"))
				break
			}
			b.WriteString(item)
		}
		b.WriteString("\n")
	}
	if risk.Decision != "" {
		mode := ""
		if risk.Shadow {
			mode = " (shadow: not enforced)"
		}
		fmt.Fprintf(b, "Gate: %s at --fail-on %s%s.\n\n", risk.Decision, risk.FailOn, mode)
	}
}

func codeList(paths []string) string {
	quoted := make([]string, len(paths))
	for i, p := range paths {
		quoted[i] = "`" + p + "`"
	}
	return strings.Join(quoted, ", ")
}

func cell(s string) string { return strings.ReplaceAll(s, "|", `\|`) }

func plural(n int, word string) string {
	if n == 1 {
		return word
	}
	return word + "s"
}

func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}
