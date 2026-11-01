package explain

import (
	"fmt"
	"sort"
	"strings"

	"github.com/zanetworker/highstakes/internal/gitanalyzer"
	"github.com/zanetworker/highstakes/internal/ownership"
	"github.com/zanetworker/highstakes/internal/types"
)

// Thresholds, as "top N% of the repo's non-test files".
const (
	notableTop   = 20 // a signal is worth a reason
	splitTop     = 5  // functions or cyclomatic this high: split the file
	interfaceTop = 10 // importers this high: keep the interface stable
	authorsTop   = 10 // authors this high (and at least minAuthors): spread knowledge

	minAuthors       = 5
	dimensionFloor   = 60  // impact dimensions listed as reasons
	singleOwnerShare = 0.8 // top owner share that is a single-owner risk
	singleOwnerMin   = 5   // commits before single ownership means anything
	maxMessage       = 72  // fix messages quoted up to this many characters
)

// signals are the repo-relative facts reasons and actions are derived from.
type signals struct {
	f          *types.FileHeat
	owners     []ownership.Owner
	isTest     bool
	codeowned  bool // an existing CODEOWNERS rule covers the file
	soleAuthor bool // the repo has one author overall

	churnTop, authorsTop, cycTop, fnTop, importersTop, linesTop int
	fixes                                                       []types.Change // newest first
}

func notable(top int) bool { return top > 0 && top <= notableTop }

// complexTop is the better of the cyclomatic and function percentiles.
func (s signals) complexTop() int {
	switch {
	case s.cycTop == 0:
		return s.fnTop
	case s.fnTop == 0:
		return s.cycTop
	}
	return min(s.cycTop, s.fnTop)
}

// noTests is true only when the language has a convention and none was found.
func (s signals) noTests() bool {
	h := s.f.Factors.TestCoverage.HasTestFile
	return !s.isTest && h != nil && !*h
}

// Explain derives the headline, reasons and actions for one file. owners
// (ranked by share, from internal/ownership) may be nil.
func Explain(f *types.FileHeat, rs RepoStats, owners []ownership.Owner) types.Explanation {
	isTest := rs.isTest != nil && rs.isTest(f.Path)
	s := signals{
		f: f, owners: owners, isTest: isTest,
		codeowned:    rs.Codeowned != nil && rs.Codeowned(f.Path),
		soleAuthor:   rs.SoleAuthor,
		churnTop:     topPercent(rs.churn, f.Factors.ChangeFrequency.CommitsLast90d),
		authorsTop:   topPercent(rs.authors, f.Factors.ChangeFrequency.UniqueAuthors),
		cycTop:       topPercent(rs.cyclomatic, f.Factors.Complexity.Cyclomatic),
		fnTop:        topPercent(rs.functions, f.Factors.Complexity.FunctionCount),
		importersTop: topPercent(rs.importers, len(f.Dependencies.ImportedBy)),
		linesTop:     topPercent(rs.lines, f.Size.Lines),
	}
	for _, c := range f.RecentChanges {
		if gitanalyzer.IsBugFix(c.Message) {
			s.fixes = append(s.fixes, c)
		}
	}
	return types.Explanation{
		Headline:          headline(f, isTest, rs.LikelyCutoff, rs.ImpactThreshold),
		Verdict:           Verdict(f, isTest),
		ImpactHigh:        f.Impact >= rs.ImpactThreshold,
		Likely:            !isTest && f.Likelihood >= rs.LikelyCutoff,
		ImpactReasons:     impactReasons(f),
		LikelihoodReasons: likelihoodReasons(s),
		Actions:           actions(s),
	}
}

// Verdict is the posture a reader acts on: the quadrant label, "Routine"
// for test files, "Not assessed" when no LLM assessed impact.
func Verdict(f *types.FileHeat, isTest bool) string {
	switch {
	case isTest:
		return types.QuadrantLow.Label()
	case !f.ImpactAssessed || f.Quadrant.Label() == "":
		return "Not assessed"
	}
	return f.Quadrant.Label()
}

// headline names the verdict and both axes with the repo's cutoffs, so the
// numbers read relative to this repo. A zero cutoff is left out.
func headline(f *types.FileHeat, isTest bool, cutoff, impactCutoff int) string {
	breaks := fmt.Sprintf("will it break %d", f.Likelihood)
	if cutoff > 0 {
		breaks += fmt.Sprintf(", cutoff %d", cutoff)
	}
	bad := fmt.Sprintf("how bad %d", f.Impact)
	switch {
	case !f.ImpactAssessed:
		bad = fmt.Sprintf("how bad ~%d estimate", f.Impact)
	case impactCutoff > 0:
		bad += fmt.Sprintf(", cutoff %d", impactCutoff)
	}
	return fmt.Sprintf("%s (%s; %s)", strings.ToUpper(Verdict(f, isTest)), breaks, bad)
}

func impactReasons(f *types.FileHeat) []string {
	br := f.Factors.BlastRadius
	if !f.ImpactAssessed || !br.Assessed {
		return []string{fmt.Sprintf("~%d is an estimate from centrality and path keywords; run analyze with an LLM to assess it", f.Impact)}
	}
	dims := []struct {
		name  string
		score int
	}{{"security", br.SecurityImpact}, {"availability", br.AvailabilityImpact}, {"user", br.UserImpact}, {"data", br.DataImpact}}
	sort.SliceStable(dims, func(i, j int) bool { return dims[i].score > dims[j].score })
	var high []string
	for _, d := range dims {
		if d.score >= dimensionFloor {
			high = append(high, fmt.Sprintf("%s %d", d.name, d.score))
		}
	}
	var out []string
	if len(high) > 0 {
		out = append(out, strings.Join(high, " · "))
	} else {
		out = append(out, fmt.Sprintf("no impact dimension reaches %d (highest: %s %d)", dimensionFloor, dims[0].name, dims[0].score))
	}
	if br.Summary != "" {
		out = append(out, fmt.Sprintf("%q", br.Summary))
	}
	if br.CriticalReason != "" && br.CriticalReason != br.Summary {
		out = append(out, fmt.Sprintf("%q", br.CriticalReason))
	}
	return out
}

func likelihoodReasons(s signals) []string {
	f := s.f
	var out []string
	first := true
	pct := func(top int) string { // the first percentile names the population
		if first {
			first = false
			return fmt.Sprintf("(top %d%% of this repo)", top)
		}
		return fmt.Sprintf("(top %d%%)", top)
	}
	cf := f.Factors.ChangeFrequency
	switch {
	case notable(s.churnTop):
		out = append(out, fmt.Sprintf("%s in the last 90 days, %s overall %s", count(cf.CommitsLast90d, "commit"), count(cf.UniqueAuthors, "author"), pct(s.churnTop)))
	case notable(s.authorsTop):
		out = append(out, fmt.Sprintf("%s overall %s", count(cf.UniqueAuthors, "author"), pct(s.authorsTop)))
	}
	cx := f.Factors.Complexity
	switch {
	case notable(s.complexTop()):
		out = append(out, fmt.Sprintf("%s, cyclomatic %d in one file %s", count(cx.FunctionCount, "function"), cx.Cyclomatic, pct(s.complexTop())))
	case notable(s.linesTop):
		out = append(out, fmt.Sprintf("%s in one file %s", count(f.Size.Lines, "line"), pct(s.linesTop)))
	}
	if notable(s.importersTop) {
		out = append(out, fmt.Sprintf("%s %s", importedBy(len(f.Dependencies.ImportedBy)), pct(s.importersTop)))
	}
	switch n := len(s.fixes); {
	case n == 1:
		out = append(out, "recent fix: "+describeChange(s.fixes[0]))
	case n > 1:
		out = append(out, fmt.Sprintf("%d recent fixes; latest: %s", n, describeChange(s.fixes[0])))
	}
	if ih := f.Factors.IncidentHistory; ih.IncidentCount > 0 {
		r := count(ih.IncidentCount, "incident") + " recorded"
		if ih.LastIncident != nil {
			r += ", last on " + ih.LastIncident.Format("2006-01-02")
		}
		out = append(out, r)
	}
	if s.noTests() {
		out = append(out, "no tests found next to this file")
	}
	if len(out) == 0 {
		out = append(out, "no notable churn, complexity, importers or fixes")
	}
	return out
}

// count is "1 author" or "N authors".
func count(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return fmt.Sprintf("%d %ss", n, word)
}

// importedBy is "1 file imports it" or "N files import it".
func importedBy(n int) string {
	if n == 1 {
		return "1 file imports it"
	}
	return fmt.Sprintf("%d files import it", n)
}

// describeChange quotes a commit subject (truncated) with its date.
func describeChange(c types.Change) string {
	msg := c.Message
	if r := []rune(msg); len(r) > maxMessage {
		msg = string(r[:maxMessage]) + "..."
	}
	if c.Date.IsZero() {
		return fmt.Sprintf("%q", msg)
	}
	return fmt.Sprintf("%q (%s)", msg, c.Date.Format("2006-01-02"))
}

// Titles are the section headings every frontend uses for the two reason
// lists, so the CLI, TUI and dashboard read the same.
func Titles(e types.Explanation, assessed bool) (impact, likelihood string) {
	level := func(high bool) string {
		if high {
			return "HIGH"
		}
		return "LOW"
	}
	impact = "Break cost (" + level(e.ImpactHigh) + ")"
	if !assessed {
		impact = "Break cost (estimate)"
	}
	return impact, "Break signals (" + level(e.Likely) + ")"
}
