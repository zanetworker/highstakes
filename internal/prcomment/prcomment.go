// Package prcomment renders `pr check` as the Markdown PR comment the
// triage workflow posts: verdict first, then the before-merge checklist, a
// short file table, and the reasons folded away.
package prcomment

import (
	"fmt"
	"regexp"
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
func posture(fc types.FileChange) string { return fc.Posture() }

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
	if risk.AutonomyReason != "" {
		fmt.Fprintf(&b, "AI may auto-merge: %s (%s)\n\n", map[bool]string{true: "yes", false: "no"}[risk.AutoMergeAllowed], risk.AutonomyReason)
	}

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

	writeTable(&b, files, hm, risk.LikelyCutoff)
	writeDetails(&b, risk, files, hm)
	return ScrubEmails(b.String())
}

var (
	emailRe   = regexp.MustCompile(`[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}`)
	noreplyRe = regexp.MustCompile(`^(?:\d+\+)?([A-Za-z0-9-]+)@users\.noreply\.github\.com$`)
)

// hasPrivateEmail reports an address that ScrubEmails would turn into prose
// (anything but a GitHub noreply address).
func hasPrivateEmail(s string) bool {
	for _, e := range emailRe.FindAllString(s, -1) {
		if !noreplyRe.MatchString(e) {
			return true
		}
	}
	return false
}

// ScrubEmails keeps email addresses out of public comments: GitHub noreply
// addresses become @handles, any other address becomes "a top owner".
// Owners with an @handle in the config owners map are shown by handle.
func ScrubEmails(s string) string {
	return emailRe.ReplaceAllStringFunc(s, func(e string) string {
		if m := noreplyRe.FindStringSubmatch(e); m != nil {
			return "@" + m[1]
		}
		return "a top owner"
	})
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
		return "No changed file is likely to break or costly if it does. Normal review."
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

func writeTable(b *strings.Builder, files []types.FileChange, hm *types.Heatmap, likelyCutoff int) {
	b.WriteString("| File | If it breaks | Breaks often? | Do |\n|---|---|---|---|\n")
	shown, more, rest := 0, 0, 0
	for _, fc := range files {
		switch {
		case posture(fc) == "Routine":
			rest++
		case shown == maxRows:
			more++
		default:
			shown++
			fmt.Fprintf(b, "| `%s` | %s | %s | %s |\n", cell(fc.Path), cell(ifBreaks(fc, hm)), cell(breaksOften(fc, hm, likelyCutoff)), posture(fc))
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
		fmt.Fprintf(b, "| %d other %s | limited damage | rarely | Routine |\n", rest, plural(rest, "file"))
	case rest > 0:
		fmt.Fprintf(b, "| %d %s | limited damage | rarely | Routine |\n", rest, plural(rest, "file"))
	}
	b.WriteString("\n")
}

// maxCell keeps table cells to one readable line.
const maxCell = 90

func short(s string) string {
	s = strings.Join(strings.Fields(s), " ") // one line: newlines would split the table row
	s = strings.Trim(s, `"`)
	if len(s) > maxCell {
		s = s[:maxCell-3] + "..."
	}
	return s
}

// ifBreaks is what goes wrong if the file breaks, in words: the LLM's
// summary, else its first impact reason.
func ifBreaks(fc types.FileChange, hm *types.Heatmap) string {
	if fc.Unscored {
		return "new file: treated like its directory (" + strings.ToLower(posture(fc)) + ")"
	}
	f, ok := hm.Files[fc.Path]
	switch {
	case ok && f.Factors.BlastRadius.Assessed && f.Factors.BlastRadius.Summary != "":
		return short(f.Factors.BlastRadius.Summary)
	case !fc.ImpactAssessed:
		return "not AI-assessed yet"
	case ok && f.Explanation != nil && len(f.Explanation.ImpactReasons) > 0:
		return short(f.Explanation.ImpactReasons[0])
	case posture(fc) == "Harden now" || posture(fc) == "Protect":
		return "costly (no description; run analyze again)"
	}
	return "limited damage"
}

// breaksOften answers "will it break?" in words: the strongest signal when
// the file is above the repo cutoff.
func breaksOften(fc types.FileChange, hm *types.Heatmap, cutoff int) string {
	if fc.Unscored {
		return "new file"
	}
	if fc.Likelihood < cutoff {
		return "No, rarely changes"
	}
	if f, ok := hm.Files[fc.Path]; ok && f.Explanation != nil && len(f.Explanation.LikelihoodReasons) > 0 {
		return "Yes: " + short(f.Explanation.LikelihoodReasons[0])
	}
	return "Yes"
}

func writeDetails(b *strings.Builder, risk *types.PRRisk, files []types.FileChange, hm *types.Heatmap) {
	b.WriteString("<details><summary>Why these actions?</summary>\n\n")
	fmt.Fprintf(b, "%s %s Scores and repo cutoffs: Will it break? %d, How bad if it breaks? %d (a score at or above its cutoff counts as high).\n\n",
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
	if !fc.Unscored {
		bad := fmt.Sprintf("%d (%s, cutoff %d)", fc.Impact, strings.ToLower(types.AxisLevel(fc.Impact, risk.ImpactThreshold)), risk.ImpactThreshold)
		if !fc.ImpactAssessed {
			bad = fmt.Sprintf("~%d (estimate, cutoff %d)", fc.Impact, risk.ImpactThreshold)
		}
		fmt.Fprintf(b, "- Scores: Will it break? %d (%s, cutoff %d); How bad if it breaks? %s\n",
			fc.Likelihood, strings.ToLower(types.AxisLevel(fc.Likelihood, risk.LikelyCutoff)), risk.LikelyCutoff, bad)
	}
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
	if len(risk.WorthDoing) > 0 {
		b.WriteString("**Worth doing (does not block)**\n")
		for i, a := range risk.WorthDoing {
			item := fmt.Sprintf("- %s (%s): %s\n", a.Text, codeList(a.Files), a.Signal)
			if !fits(b, item) {
				fmt.Fprintf(b, "- _%s omitted to fit GitHub's comment size limit_\n", plural(len(risk.WorthDoing)-i, "item"))
				break
			}
			b.WriteString(item)
		}
		b.WriteString("\n")
	}
	if len(risk.Backlog) > 0 {
		b.WriteString("**Backlog** (the file over time, not this PR)\n")
		for i, a := range risk.Backlog {
			item := fmt.Sprintf("- %s (%s): %s\n", a.Text, codeList(a.Files), a.Signal)
			switch {
			case a.Command != "" && hasPrivateEmail(a.Command):
				// Redacting the email would leave an invalid rule; say how to get one.
				item += "  (add an `owners` alias in .heatmap/config.yaml to get a copyable CODEOWNERS line)\n"
			case a.Command != "":
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
