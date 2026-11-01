package explain

import (
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/zanetworker/highstakes/internal/gitanalyzer"
	"github.com/zanetworker/highstakes/internal/tour"
	"github.com/zanetworker/highstakes/internal/types"
)

// BriefInput is what an issue brief needs beyond the file and its action.
// Empty fields leave their line out.
type BriefInput struct {
	Posture         string // Harden now, Protect, Stabilize, Routine
	LikelyCutoff    int
	ImpactThreshold int
	Owners          []string // @handles of people who know the file; callers drop emails
	Autonomy        *types.Autonomy
	PR              int    // PR the issue is opened from; 0 leaves the link out
	PRURL           string // link to that PR
	CommitSHA       string
}

// Brief is a self-contained issue for one action on one file. Who,
// Importers, Autonomy, PR and Commit are the context lines ("" when unknown,
// except Who and Importers, which always read as a sentence).
type Brief struct {
	Title        string
	WhyItMatters []string
	WhyNow       []string
	Approach     []string // 2-4 concrete steps
	DoneWhen     []string // checklist items, "tests pass" last
	Who          string
	Importers    string
	Autonomy     string
	PR           string
	Commit       string
}

const (
	maxBriefFixes     = 3 // fix commits quoted in a brief
	maxBriefImporters = 3 // importing files named in a brief
)

var postureWords = map[string]string{
	"Harden now": "Harden now: likely to break and costly if it does",
	"Protect":    "Protect: costly if it breaks, though it rarely does",
	"Stabilize":  "Stabilize: likely to break, with limited damage",
	"Routine":    "Routine: neither likely to break nor costly",
}

// IssueBrief builds the issue for action a on file f: why it matters, why
// now, a suggested approach, a done-when checklist and context. It is pure
// and deterministic (no LLM): every line comes from the file's scores,
// history and the action kind.
func IssueBrief(f *types.FileHeat, a types.Action, in BriefInput) Brief {
	title, _ := TaskTitle(a.ID, f)
	fixes := fixCommits(f)
	b := Brief{
		Title:        title,
		WhyItMatters: whyItMatters(f),
		WhyNow:       whyNow(f, in, fixes),
		Approach:     approach(a, f, fixes),
		DoneWhen:     []string{strings.TrimPrefix(Acceptance(a.ID, f), "tests pass and "), "tests pass"},
		Who:          "someone who knows the file",
		Importers:    importersLine(f),
	}
	if b.DoneWhen[0] == "tests pass" {
		b.DoneWhen = b.DoneWhen[1:]
	}
	if len(in.Owners) > 0 {
		b.Who = strings.Join(in.Owners, ", ")
	}
	if in.Autonomy != nil {
		b.Autonomy = fmt.Sprintf("L%d %s", in.Autonomy.Level, in.Autonomy.Label)
		if m := autonomyMeaning(in.Autonomy.Level); m != "" {
			b.Autonomy += ": " + m
		}
	}
	if in.PR > 0 && in.PRURL != "" {
		b.PR = fmt.Sprintf("[PR #%d](%s)", in.PR, in.PRURL)
	}
	if in.CommitSHA != "" {
		b.Commit = "`" + in.CommitSHA + "`"
	}
	return b
}

// Markdown renders the brief as an issue body.
func (b Brief) Markdown() string {
	var s strings.Builder
	list := func(heading string, items []string, bullet func(int) string) {
		if len(items) == 0 {
			return
		}
		s.WriteString("## " + heading + "\n")
		for i, it := range items {
			s.WriteString(bullet(i) + it + "\n")
		}
		s.WriteString("\n")
	}
	dash := func(int) string { return "- " }
	list("Why it matters", b.WhyItMatters, dash)
	list("Why now", b.WhyNow, dash)
	list("Suggested approach", b.Approach, func(i int) string { return fmt.Sprintf("%d. ", i+1) })
	list("Done when", b.DoneWhen, func(int) string { return "- [ ] " })
	ctx := []string{"Review by: " + b.Who, b.Importers}
	for _, l := range []struct{ name, v string }{{"AI autonomy", b.Autonomy}, {"From", b.PR}, {"Commit", b.Commit}} {
		if l.v != "" {
			ctx = append(ctx, l.name+": "+l.v)
		}
	}
	list("Context", ctx, dash)
	s.WriteString("_Opened from the HighStakes PR comment_\n")
	return s.String()
}

func whyItMatters(f *types.FileHeat) []string {
	br := f.Factors.BlastRadius
	if !f.ImpactAssessed || !br.Assessed {
		return []string{fmt.Sprintf("Impact estimated (~%d from centrality and path keywords; not AI-assessed yet)", f.Impact)}
	}
	var out []string
	if br.Summary != "" {
		out = append(out, br.Summary)
	}
	if br.CriticalReason != "" && br.CriticalReason != br.Summary {
		out = append(out, br.CriticalReason)
	}
	if max(br.SecurityImpact, br.AvailabilityImpact, br.UserImpact, br.DataImpact) > 0 {
		out = append(out, "Top impact: "+impactReasons(f)[0])
	}
	return out
}

func whyNow(f *types.FileHeat, in BriefInput, fixes []types.Change) []string {
	var out []string
	if w := postureWords[in.Posture]; w != "" {
		out = append(out, w)
	}
	bad := fmt.Sprintf("%d (%s, cutoff %d)", f.Impact, strings.ToLower(types.AxisLevel(f.Impact, in.ImpactThreshold)), in.ImpactThreshold)
	if !f.ImpactAssessed {
		bad = fmt.Sprintf("~%d (estimate, cutoff %d)", f.Impact, in.ImpactThreshold)
	}
	out = append(out, fmt.Sprintf("%s %d (%s, cutoff %d); %s %s", types.AxisLikelihood, f.Likelihood,
		strings.ToLower(types.AxisLevel(f.Likelihood, in.LikelyCutoff)), in.LikelyCutoff, types.AxisImpact, bad))
	if len(fixes) > 0 {
		out = append(out, "Recent fixes: "+fixList(fixes))
	}
	if f.Explanation != nil {
		for _, r := range f.Explanation.LikelihoodReasons {
			// The fix list above already quotes the fixes.
			if len(fixes) > 0 && (strings.HasPrefix(r, "recent fix:") || strings.Contains(r, "recent fixes; latest:")) {
				continue
			}
			out = append(out, r)
		}
	}
	return out
}

// fixCommits are the file's recent bug-fix commits, newest first, at most
// maxBriefFixes.
func fixCommits(f *types.FileHeat) []types.Change {
	var out []types.Change
	for _, c := range f.RecentChanges {
		if gitanalyzer.IsBugFix(c.Message) && len(out) < maxBriefFixes {
			out = append(out, c)
		}
	}
	return out
}

func fixList(fixes []types.Change) string {
	parts := make([]string, len(fixes))
	for i, c := range fixes {
		parts[i] = describeChange(c)
	}
	return strings.Join(parts, "; ")
}

// approach is 2-4 concrete steps for the action kind.
func approach(a types.Action, f *types.FileHeat, fixes []types.Change) []string {
	cx := f.Factors.Complexity
	switch a.ID {
	case "regression-tests":
		first := "Write a failing test that reproduces each recent fix"
		if len(fixes) > 0 {
			first += ": " + fixList(fixes)
		}
		return []string{first,
			"Check each test fails on the commit before its fix, then passes now",
			"Keep the tests next to the file (" + testFileHint(f.Path) + ")"}
	case "split":
		groups := "Identify cohesive groups of functions"
		if cx.FunctionCount > 0 {
			groups = "Identify cohesive groups of the " + count(cx.FunctionCount, "function")
		}
		return []string{groups + " (functions that share data or callers)",
			"Move one group per PR into a new file in the same package",
			"Keep the public API unchanged so importers need no edits",
			"Run the tests after each move"}
	case "complexity-issue":
		return []string{"Add tests that pin the current behavior first",
			"Find the most complex functions (cyclomatic " + Thousands(cx.Cyclomatic) + " across " + count(cx.FunctionCount, "function") + ") and extract their branches into small helpers",
			"Run the tests after each extraction"}
	case "test-gap":
		cover := "Cover the exported functions"
		if n := f.Factors.DependencyCentrality.ExportedSymbols; n > 0 {
			cover += " (" + count(n, "exported symbol") + ")"
		}
		return []string{"Add " + testFileHint(f.Path), cover + ", including an error path"}
	case "document-invariants":
		check := "Check the contract against the code that depends on it"
		if top := topImporters(f); len(top) > 0 {
			check = "Check the contract against its importers: " + strings.Join(top, ", ")
		}
		return []string{"Write the contract (inputs, outputs, invariants, errors) next to the code", check,
			"Ask someone who knows the file to review it"}
	}
	return []string{a.Text, "Make sure tests pass"}
}

// testFileHint names the test file a language convention expects next to p.
func testFileHint(p string) string {
	base := path.Base(p)
	ext := path.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	switch ext {
	case ".go":
		return "a test file `" + stem + "_test.go`"
	case ".py":
		return "a test file `test_" + base + "`"
	case ".ts", ".tsx", ".js", ".jsx", ".mjs":
		return "a test file `" + stem + ".test" + ext + "`"
	case ".java", ".kt":
		return "a test file `" + stem + "Test" + ext + "`"
	case ".rs":
		return "a `#[cfg(test)]` module in the file"
	}
	return "a test file following the project's test convention"
}

// codeImporters are the importing files that are not tests: a test importing
// the file says little about who depends on it in production.
func codeImporters(f *types.FileHeat) []types.Dependency {
	var out []types.Dependency
	for _, d := range f.Dependencies.ImportedBy {
		if !tour.IsTestFile(d.Path) {
			out = append(out, d)
		}
	}
	return out
}

// topImporters are the hottest maxBriefImporters non-test importing files, in backticks.
func topImporters(f *types.FileHeat) []string {
	deps := codeImporters(f)
	sort.SliceStable(deps, func(i, j int) bool {
		if deps[i].HeatScore != deps[j].HeatScore {
			return deps[i].HeatScore > deps[j].HeatScore
		}
		return deps[i].Path < deps[j].Path
	})
	var out []string
	for _, d := range deps[:min(len(deps), maxBriefImporters)] {
		out = append(out, "`"+d.Path+"`")
	}
	return out
}

func importersLine(f *types.FileHeat) string {
	n := len(codeImporters(f))
	tests := ""
	if t := len(f.Dependencies.ImportedBy) - n; t > 0 {
		tests = " (plus " + count(t, "test") + ")"
	}
	switch {
	case n == 0:
		return "No file imports it" + tests
	case n <= maxBriefImporters:
		return importedBy(n) + ": " + strings.Join(topImporters(f), ", ") + tests
	}
	return importedBy(n) + ", including " + strings.Join(topImporters(f), ", ") + tests
}

// autonomyMeaning is the level's meaning as a clause: "agents may ...".
func autonomyMeaning(level int) string {
	for _, l := range types.AutonomyLevels {
		if l.Level == level {
			m := strings.TrimSuffix(l.Meaning, ".")
			return strings.ToLower(m[:1]) + m[1:]
		}
	}
	return ""
}
