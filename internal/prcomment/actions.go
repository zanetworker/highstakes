package prcomment

import (
	"fmt"
	"math"
	"net/url"
	"regexp"
	"slices"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

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
	SHA        string // commit the issues name; "" leaves it out
}

// maxLinks caps the one-click links; maxURL keeps each under what GitHub
// accepts (about 8,000 characters), so a long brief is trimmed, not dropped.
const (
	maxLinks = 5
	maxURL   = 6000
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
			parts = append(parts, fmt.Sprintf("%s %s %s", postureEmoji[p], explain.Thousands(n[p]), word))
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

// prLevel are actions about the whole PR, not one file: they never become
// a file's next step (smaller-pr is rendered once with the one-click actions).
var prLevel = map[string]bool{"no-auto-merge": true, "smaller-pr": true}

// nextStep is the file's top open action: before merge first, then worth
// doing, then backlog. PR-level actions are skipped; a file with none left
// gets its posture's default step, never a vague "careful review".
func nextStep(fc types.FileChange, risk *types.PRRisk, hm *types.Heatmap) next {
	for _, list := range [][]types.Action{risk.Checklist, risk.WorthDoing, risk.Backlog} {
		for _, a := range list {
			if !a.Done && !prLevel[a.ID] && contains(a.Files, fc.Path) {
				return next{label: label(a), action: a}
			}
		}
	}
	switch posture(fc) {
	case "Routine":
		return next{label: "normal review"}
	case "Harden now":
		return next{label: "👀 senior sign-off"}
	case "Protect":
		return next{label: "👀 owner review"}
	case "Stabilize":
		if hasFixes(hm, fc.Path) {
			return next{label: "🧪 regression tests"}
		}
	}
	return next{label: "🔍 review the change"}
}

// hasFixes reports whether the file's break signals name a recent fix.
func hasFixes(hm *types.Heatmap, p string) bool {
	f, ok := hm.Files[p]
	if !ok || f.Explanation == nil {
		return false
	}
	for _, r := range f.Explanation.LikelihoodReasons {
		if strings.HasPrefix(r, "recent fix:") || fixesRe.MatchString(r) {
			return true
		}
	}
	return false
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
	brief            explain.Brief
}

// issueLinks are up to maxLinks issue-shaped items of the changed files,
// riskiest file first: each file's first item, then the files' further
// items. Each carries a full brief (explain.IssueBrief). Emails are
// scrubbed before encoding: an encoded address would slip past ScrubEmails.
func issueLinks(risk *types.PRRisk, files []types.FileChange, hm *types.Heatmap, links Links) []issueLink {
	var first, rest []issueLink
	for _, fc := range files {
		f, ok := hm.Files[fc.Path]
		if !ok {
			f = &types.FileHeat{Path: fc.Path}
		}
		in := briefInput(risk, fc, f, links)
		n := 0
		for _, list := range [][]types.Action{risk.WorthDoing, risk.Backlog} {
			for _, a := range list {
				if a.Done || !issueShaped[a.ID] || !contains(a.Files, fc.Path) {
					continue
				}
				b := explain.IssueBrief(f, a, in)
				b.Title = ScrubEmails(b.Title)
				u, _ := fitIssueURL(links.Repo, b)
				if l := (issueLink{file: fc.Path, title: b.Title, url: u, brief: b}); n == 0 {
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

// briefInput gathers a file's brief context: cutoffs, owners as @handles
// (its review items, else suggested reviewers of the file), autonomy, and
// the PR and commit when known.
func briefInput(risk *types.PRRisk, fc types.FileChange, f *types.FileHeat, links Links) explain.BriefInput {
	in := explain.BriefInput{Posture: posture(fc), LikelyCutoff: risk.LikelyCutoff, ImpactThreshold: risk.ImpactThreshold,
		Autonomy: fc.Autonomy, CommitSHA: links.SHA}
	if in.Autonomy == nil {
		in.Autonomy = f.Autonomy
	}
	for _, a := range risk.Checklist {
		if (a.ID == "senior-review" || a.ID == "owner-review") && contains(a.Files, fc.Path) {
			in.Owners = append(in.Owners, handles(a.Owners)...)
		}
	}
	if len(in.Owners) == 0 {
		for _, r := range risk.SuggestedReviewers {
			if contains(r.Files, fc.Path) {
				in.Owners = append(in.Owners, handles([]string{r.Handle})...)
			}
		}
	}
	if risk.PRNumber > 0 && links.Repo != "" {
		in.PR, in.PRURL = risk.PRNumber, fmt.Sprintf("https://github.com/%s/pull/%d", links.Repo, risk.PRNumber)
	}
	return in
}

// fitIssueURL is the issue link for brief b, trimmed until it fits maxURL:
// the longest section loses its last item, then the longest item is cut in
// half. ok is false without a repo or when even the trimmed brief is too long.
func fitIssueURL(repo string, b explain.Brief) (string, bool) {
	// Trimming cuts items in place: work on copies so the caller's brief stays whole.
	b.WhyNow, b.WhyItMatters = slices.Clone(b.WhyNow), slices.Clone(b.WhyItMatters)
	b.Approach, b.DoneWhen = slices.Clone(b.Approach), slices.Clone(b.DoneWhen)
	for {
		u, ok := issueURL(repo, b.Title, ScrubEmails(b.Markdown()))
		if ok || repo == "" || !trimBrief(&b) {
			if !ok {
				return "", false
			}
			return u, true
		}
	}
}

// maxTrimmedItem is the length an item is cut down to, at most.
const maxTrimmedItem = 160

// trimBrief makes b shorter; false when nothing is left to trim.
func trimBrief(b *explain.Brief) bool {
	sections := []*[]string{&b.WhyNow, &b.WhyItMatters, &b.Approach}
	var longest *[]string
	size := func(xs []string) int { return len(strings.Join(xs, "")) }
	for _, sec := range sections {
		if len(*sec) > 1 && (longest == nil || size(*sec) > size(*longest)) {
			longest = sec
		}
	}
	if longest != nil {
		*longest = (*longest)[:len(*longest)-1]
		return true
	}
	var item *string
	for _, sec := range append(sections, &b.DoneWhen) {
		for i := range *sec {
			if s := &(*sec)[i]; len(*s) > maxTrimmedItem && (item == nil || len(*s) > len(*item)) {
				item = s
			}
		}
	}
	if item == nil {
		return false
	}
	*item = cutRunes(*item, max(maxTrimmedItem, len(*item)/2))
	return true
}

// cutRunes shortens s to at most n bytes on a rune boundary, adding "...".
func cutRunes(s string, n int) string {
	if len(s) <= n {
		return s
	}
	n -= len("...")
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n] + "..."
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
		return fmt.Sprintf("✂️ Large diff (%s lines): consider splitting this PR", explain.Thousands(lines))
	case strings.HasPrefix(signal, unscoredPrefix):
		return "🆕 Unscored new file(s), treated like their directory until the next analyze scores them: " + strings.TrimPrefix(signal, unscoredPrefix)
	case strings.HasPrefix(signal, "Low test ratio"):
		return "🧪 Low test ratio (<0.3 test lines per code line added): add tests"
	}
	return "⚠️ " + signal
}

const maxChartPoints = 15

// Within its half of an axis a point keeps chartMargin from the midline
// and from the edge, so it never sits on a border or under a quadrant title.
const chartMargin = 0.08

// quadrantChart is a Mermaid quadrant chart of up to maxChartPoints scored
// non-routine files, numbered in the given order, with a legend under it
// ("1. ⛔ `path`: next step"); "" when there are none. Each point is placed
// inside its posture's quadrant by rank, not by raw score (see grid), so
// ties never overlap and a file on a cutoff still sits clearly inside its
// quadrant. Unassessed files take their posture from the tier.
func quadrantChart(files []types.FileChange, risk *types.PRRisk, hm *types.Heatmap) string {
	var plotted []types.FileChange
	for _, fc := range files {
		if posture(fc) != "Routine" && !fc.Unscored && len(plotted) < maxChartPoints {
			plotted = append(plotted, fc)
		}
	}
	if len(plotted) == 0 {
		return ""
	}
	byPosture := map[string][]int{}
	for i, fc := range plotted {
		byPosture[posture(fc)] = append(byPosture[posture(fc)], i)
	}
	xs, ys := make([]float64, len(plotted)), make([]float64, len(plotted))
	for p, idx := range byPosture {
		grid(plotted, idx, xs, ys, p == "Harden now" || p == "Stabilize", p == "Harden now" || p == "Protect")
	}
	var pts, legend strings.Builder
	for i, fc := range plotted {
		fmt.Fprintf(&pts, "  %d: [%.2f, %.2f]\n", i+1, xs[i], ys[i])
		fmt.Fprintf(&legend, "%d. %s `%s`: %s\n", i+1, postureEmoji[posture(fc)], fc.Path, stepText(nextStep(fc, risk, hm).label))
	}
	return "```mermaid\nquadrantChart\n  title Changed files\n  x-axis Rarely breaks --> Breaks often\n  y-axis Limited damage --> Costly if it breaks\n" +
		"  quadrant-1 Harden now\n  quadrant-2 Protect\n  quadrant-3 Routine\n  quadrant-4 Stabilize\n" + pts.String() + "```\n\n" + legend.String()
}

// grid places one quadrant's points (idx into fs) on an even grid inside
// the quadrant: files ranked by likelihood fill columns left to right,
// and within a column rank by impact bottom to top. Ranks keep the axis
// order; the grid keeps points apart even when the scores correlate.
func grid(fs []types.FileChange, idx []int, xs, ys []float64, highX, highY bool) {
	order := append([]int(nil), idx...)
	sort.SliceStable(order, func(a, b int) bool {
		i, j := fs[order[a]], fs[order[b]]
		return less(i.Likelihood, j.Likelihood, i.Impact, j.Impact, i.Path, j.Path)
	})
	cols := int(math.Ceil(math.Sqrt(float64(len(order)))))
	perCol := (len(order) + cols - 1) / cols
	cols = (len(order) + perCol - 1) / perCol // drop empty columns
	for c := 0; c < cols; c++ {
		col := order[c*perCol : min(len(order), (c+1)*perCol)]
		sort.SliceStable(col, func(a, b int) bool {
			i, j := fs[col[a]], fs[col[b]]
			return less(i.Impact, j.Impact, i.Likelihood, j.Likelihood, i.Path, j.Path)
		})
		for r, i := range col {
			xs[i] = inQuadrant(c, cols, highX)
			ys[i] = inQuadrant(r, len(col), highY)
		}
	}
}

// inQuadrant is slot k of n spaced evenly in the upper (high) or lower half
// of an axis, chartMargin from its borders.
func inQuadrant(k, n int, high bool) float64 {
	lo := chartMargin
	if high {
		lo += 0.5
	}
	return lo + (0.5-2*chartMargin)*float64(k+1)/float64(n+1)
}

// less orders by a, then b, then path.
func less(a1, a2, b1, b2 int, p1, p2 string) bool {
	if a1 != a2 {
		return a1 < a2
	}
	if b1 != b2 {
		return b1 < b2
	}
	return p1 < p2
}

// stepText is a next-step label without its leading emoji.
func stepText(label string) string {
	if r, _ := utf8.DecodeRuneInString(label); !unicode.IsLetter(r) {
		if _, rest, ok := strings.Cut(label, " "); ok {
			return rest
		}
	}
	return label
}
