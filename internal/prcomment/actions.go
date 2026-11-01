package prcomment

import (
	"fmt"
	"math"
	"net/url"
	"path"
	"regexp"
	"strings"

	"github.com/zanetworker/highstakes/internal/explain"
	"github.com/zanetworker/highstakes/internal/types"
)

// Links turns actions into links on GitHub. The zero value renders every
// action as plain text.
type Links struct {
	Repo string // owner/name; "" renders issue and CODEOWNERS actions as plain text
	Base string // branch the CODEOWNERS edit link opens; "" leaves that action plain text
	// Codeowners is the repo's CODEOWNERS path (.github/, root or docs/);
	// "" means none exists yet, so the link creates .github/CODEOWNERS.
	Codeowners string
}

// maxLinks caps the one-click links; maxURL keeps each under what browsers
// and GitHub accept comfortably.
const (
	maxLinks = 5
	maxURL   = 1500
)

var postureEmoji = map[string]string{"Harden now": "⛔", "Protect": "🛡", "Stabilize": "🔧", "Routine": "✅"}

// postureCounts is "⛔ 1 harden · 🛡 1 protect · ...", zero counts left out.
func postureCounts(files []types.FileChange) string {
	n := map[string]int{}
	for _, fc := range files {
		n[posture(fc)]++
	}
	var parts []string
	for _, p := range []string{"Harden now", "Protect", "Stabilize", "Routine"} {
		if n[p] > 0 {
			word := strings.ToLower(strings.TrimSuffix(p, " now"))
			parts = append(parts, fmt.Sprintf("%s %d %s", postureEmoji[p], n[p], word))
		}
	}
	return strings.Join(parts, " · ")
}

// step is how an action reads in the comment: its emoji and a short label
// for the table's Next step column.
type step struct{ emoji, short string }

var steps = map[string]step{
	"senior-review":       {"👀", "senior sign-off"},
	"owner-review":        {"👀", "owner review"},
	"single-owner":        {"👀", "second reviewer"},
	"regression-tests":    {"🧪", "add regression tests"},
	"no-tests":            {"🧪", "add tests"},
	"stable-interface":    {"🧪", "add contract tests"},
	"test-gap":            {"🧪", "add tests"},
	"split":               {"✂️", "split"},
	"smaller-pr":          {"✂️", "smaller PR"},
	"refactor":            {"✂️", "refactor"},
	"complexity-issue":    {"✂️", "cut complexity"},
	"no-auto-merge":       {"🚫", "no auto-merge"},
	"document-invariants": {"📝", "document invariants"},
	"codeowners":          {"👥", "add CODEOWNERS"},
	"ownership-issue":     {"👥", "spread ownership"},
	"assess-impact":       {"🔍", "run analyze with an LLM"},
}

func emojiOf(id string) string {
	if s, ok := steps[id]; ok {
		return s.emoji
	}
	return "🔍"
}

// handles keeps the owners that are GitHub handles (noreply addresses
// become @handles); private emails are dropped, never named.
func handles(owners []string) []string {
	var out []string
	for _, o := range owners {
		switch m := noreplyRe.FindStringSubmatch(o); {
		case strings.HasPrefix(o, "@"):
			out = append(out, o)
		case m != nil:
			out = append(out, "@"+m[1])
		}
	}
	return out
}

// reviewText phrases a review item as a request naming who to ask.
func reviewText(a types.Action) string {
	verb := "Request a review from"
	if a.ID == "senior-review" {
		verb = "Get sign-off from"
	}
	who := "someone who knows"
	if h := handles(a.Owners); len(h) > 0 {
		who = strings.Join(h, ", ") + " on"
	}
	return fmt.Sprintf("%s %s %s", verb, who, codeList(a.Files))
}

// checklistLine is one before-merge item: emoji, the request and its files.
// No-auto-merge is PR-wide, so it lists no files.
func checklistLine(a types.Action) string {
	box := " "
	if a.Done {
		box = "x"
	}
	text := a.Text + " (" + codeList(a.Files) + ")"
	switch a.ID {
	case "senior-review", "owner-review":
		text = reviewText(a)
	case "no-auto-merge":
		text = a.Text
	}
	return fmt.Sprintf("- [%s] %s %s\n", box, emojiOf(a.ID), text)
}

var (
	fixesRe = regexp.MustCompile(`^(\d+ recent fixes)`)
	cutRe   = regexp.MustCompile(`( \(|; |, last on ).*$`)
)

// breaksOften answers "will it break?": 🔴 and the strongest signal in a few
// words when the file is at or above the repo cutoff, else 🟢 rarely.
func breaksOften(fc types.FileChange, hm *types.Heatmap, cutoff int) string {
	if fc.Unscored {
		return "🆕 new file"
	}
	if fc.Likelihood < cutoff {
		return "🟢 rarely"
	}
	f, ok := hm.Files[fc.Path]
	if !ok || f.Explanation == nil || len(f.Explanation.LikelihoodReasons) == 0 {
		return "🔴 yes"
	}
	reasons := f.Explanation.LikelihoodReasons
	for _, r := range reasons { // repeated fixes say most about the next break
		if m := fixesRe.FindStringSubmatch(r); m != nil {
			return "🔴 " + m[1]
		}
		if strings.HasPrefix(r, "recent fix:") {
			return "🔴 1 recent fix"
		}
	}
	r := cutRe.ReplaceAllString(reasons[0], "")
	if i := strings.Index(r, " in the last 90 days"); i >= 0 {
		r = r[:i+len(" in the last 90 days")]
	}
	return "🔴 " + short(r)
}

// next is a file's next step: the label for the table and the action it
// came from (zero when none applies).
type next struct {
	label  string
	action types.Action
}

// nextStep is the file's top open action: before merge first, then worth
// doing, then backlog. Keep-auto-merge-off is PR-wide, so it is the step
// only when nothing else is open.
func nextStep(fc types.FileChange, risk *types.PRRisk) next {
	var fallback *types.Action
	for _, list := range [][]types.Action{risk.Checklist, risk.WorthDoing, risk.Backlog} {
		for i, a := range list {
			if a.Done || !contains(a.Files, fc.Path) {
				continue
			}
			if a.ID == "no-auto-merge" {
				if fallback == nil {
					fallback = &list[i]
				}
				continue
			}
			return next{label: label(a), action: a}
		}
	}
	if fallback != nil {
		return next{label: label(*fallback), action: *fallback}
	}
	if posture(fc) == "Routine" {
		return next{label: "normal review"}
	}
	return next{label: "careful review"}
}

func label(a types.Action) string {
	if s, ok := steps[a.ID]; ok {
		return s.emoji + " " + s.short
	}
	return emojiOf(a.ID) + " " + short(a.Text)
}

// issueShaped are the worth-doing and backlog items one issue can track and
// an agent can take: split, complexity, tests, invariants.
var issueShaped = map[string]bool{"split": true, "complexity-issue": true, "test-gap": true,
	"document-invariants": true, "regression-tests": true}

// issueURL is a prefilled GitHub new-issue link; ok is false without a repo
// or when the link would be too long.
func issueURL(repo, title, body string) (string, bool) {
	if repo == "" {
		return "", false
	}
	u := "https://github.com/" + repo + "/issues/new?title=" + url.QueryEscape(title) + "&body=" + url.QueryEscape(body)
	return u, len(u) <= maxURL
}

// issueLink is one one-click issue: url is "" when it renders as plain text.
type issueLink struct {
	file, title, url string
}

// issueLinks are up to maxLinks issue-shaped items of the changed files,
// riskiest file first: each file's first item, then the files' further
// items. Emails are scrubbed before encoding: an encoded address would slip
// past ScrubEmails.
func issueLinks(risk *types.PRRisk, files []types.FileChange, hm *types.Heatmap, repo string) []issueLink {
	var first, rest []issueLink
	for _, fc := range files {
		f, ok := hm.Files[fc.Path]
		if !ok {
			f = &types.FileHeat{Path: fc.Path}
		}
		n := 0
		for _, list := range [][]types.Action{risk.WorthDoing, risk.Backlog} {
			for _, a := range list {
				if a.Done || !issueShaped[a.ID] || !contains(a.Files, fc.Path) {
					continue
				}
				title, _ := explain.TaskTitle(a.ID, f)
				title = ScrubEmails(title)
				body := ScrubEmails(fmt.Sprintf("File: `%s`\nWhy: %s\nDone when: %s\n\nOpened from the HighStakes PR comment",
					fc.Path, a.Signal, explain.Acceptance(a.ID, f)))
				u, ok := issueURL(repo, title, body)
				if !ok {
					u = ""
				}
				if l := (issueLink{file: fc.Path, title: title, url: u}); n == 0 {
					first = append(first, l)
				} else {
					rest = append(rest, l)
				}
				n++
			}
		}
	}
	out := append(first, rest...) //nolint:gocritic // first is not used again
	return out[:min(len(out), maxLinks)]
}

// codeownersAction is the CODEOWNERS backlog item as an action line: a link
// to edit CODEOWNERS when every owner is an @handle, else plain text.
func codeownersAction(a types.Action, links Links) string {
	if hasPrivateEmail(a.Command) {
		// Redacting the email would leave an invalid rule; say how to get one.
		return "- 👥 Add an `owners` alias in .heatmap/config.yaml, then a CODEOWNERS line for " + codeList(a.Files)
	}
	line := "Add `" + ScrubEmails(a.Command) + "` to CODEOWNERS"
	if links.Repo == "" || links.Base == "" {
		return "- 👥 " + line
	}
	if links.Codeowners == "" {
		rule := ScrubEmails(a.Command)
		q := url.Values{"filename": {".github/CODEOWNERS"}, "value": {rule + "\n"}}
		return fmt.Sprintf("- [👥 Create CODEOWNERS with `%s`](https://github.com/%s/new/%s?%s)", rule, links.Repo, url.PathEscape(links.Base), q.Encode())
	}
	return fmt.Sprintf("- [👥 %s](https://github.com/%s/edit/%s/%s)", line, links.Repo, url.PathEscape(links.Base), links.Codeowners)
}

const unscoredPrefix = "Unscored new file(s), heat inherited from directory: "

// breakerLine turns a circuit-breaker signal into an action line.
func breakerLine(signal string, risk *types.PRRisk) string {
	switch {
	case strings.HasPrefix(signal, "Large diff"):
		lines := 0
		for _, fc := range risk.FilesChanged {
			lines += fc.LinesAdded + fc.LinesDeleted
		}
		return fmt.Sprintf("✂️ Large diff (%s lines): consider splitting this PR", thousands(lines))
	case strings.HasPrefix(signal, unscoredPrefix):
		return "🆕 Unscored new file(s), treated like their directory until the next analyze scores them: " + strings.TrimPrefix(signal, unscoredPrefix)
	case strings.HasPrefix(signal, "Low test ratio"):
		return "🧪 Low test ratio (<0.3 test lines per code line added): add tests"
	}
	return "⚠️ " + signal
}

// thousands formats n with comma separators: 1234 -> "1,234".
func thousands(n int) string {
	s := fmt.Sprint(n)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}

const maxChartPoints = 15

var unsafeLabel = regexp.MustCompile(`[^A-Za-z0-9._ -]`)

// quadrantChart is a Mermaid quadrant chart of up to maxChartPoints scored
// non-routine files, "" when there are none. Mermaid splits its axes at
// 0.5, so each score is scaled to put the repo cutoff there, and a point is
// kept off the midline in its posture's half (unassessed files take their
// posture from the tier, so their scores can sit on the other side).
func quadrantChart(files []types.FileChange, risk *types.PRRisk) string {
	var b strings.Builder
	seen := map[string]int{}
	points := 0
	for _, fc := range files {
		if posture(fc) == "Routine" || fc.Unscored || points == maxChartPoints {
			continue
		}
		points++
		name := strings.TrimSpace(unsafeLabel.ReplaceAllString(path.Base(fc.Path), ""))
		if name == "" {
			name = "file"
		}
		if seen[name]++; seen[name] > 1 {
			name = fmt.Sprintf("%s %d", name, seen[name])
		}
		p := posture(fc)
		x := inHalf(chartPos(fc.Likelihood, risk.LikelyCutoff), p == "Harden now" || p == "Stabilize")
		y := inHalf(chartPos(fc.Impact, risk.ImpactThreshold), p == "Harden now" || p == "Protect")
		fmt.Fprintf(&b, "  %s: [%.2f, %.2f]\n", name, x, y)
	}
	if points == 0 {
		return ""
	}
	return "```mermaid\nquadrantChart\n  title Changed files\n  x-axis Rarely breaks --> Breaks often\n  y-axis Limited damage --> Costly if it breaks\n" +
		"  quadrant-1 Harden now\n  quadrant-2 Protect\n  quadrant-3 Routine\n  quadrant-4 Stabilize\n" + b.String() + "```\n"
}

// inHalf keeps v in the upper half (high) or lower half of the axis.
func inHalf(v float64, high bool) float64 {
	if high {
		return math.Max(v, 0.52)
	}
	return math.Min(v, 0.48)
}

// chartPos maps a 0-100 score to [0.02, 0.98] with the cutoff at 0.5; an
// unknown cutoff falls back to score/100.
func chartPos(score, cutoff int) float64 {
	v := float64(score) / 100
	if cutoff > 0 && cutoff < 100 {
		c := float64(cutoff)
		if s := float64(score); s < c {
			v = s / c * 0.5
		} else {
			v = 0.5 + (s-c)/(100-c)*0.5
		}
	}
	return math.Min(0.98, math.Max(0.02, v))
}
