// Package explain turns a file's scores into reasons and actions: why its
// impact is high, why it is likely to break, and what to do. It is pure and
// deterministic (no LLM, no I/O): every reason quotes the file's numbers and
// its percentile among the repo's non-test files.
package explain

import (
	"sort"

	"github.com/zanetworker/highstakes/internal/types"
)

// RepoStats holds the repo distributions that make a file's numbers
// meaningful ("top 2% of this repo"). Build it with BuildRepoStats.
type RepoStats struct {
	LikelyCutoff    int // lowest likelihood counted as likely
	ImpactThreshold int // lowest impact counted as impactful
	// Codeowned reports whether an existing CODEOWNERS rule covers a path;
	// nil means nothing is covered. Callers set it after BuildRepoStats.
	Codeowned func(string) bool

	churn, authors, cyclomatic, functions, importers, lines []int // sorted ascending
	isTest                                                  func(string) bool
}

// BuildRepoStats collects per-metric distributions over non-test files.
// isTest may be nil, in which case every file counts.
func BuildRepoStats(hm *types.Heatmap, isTest func(string) bool) RepoStats {
	if isTest == nil {
		isTest = func(string) bool { return false }
	}
	s := RepoStats{
		LikelyCutoff:    hm.Metadata.LikelyCutoff,
		ImpactThreshold: hm.Metadata.ImpactThreshold,
		isTest:          isTest,
	}
	for _, f := range hm.Files {
		if isTest(f.Path) {
			continue
		}
		s.churn = append(s.churn, f.Factors.ChangeFrequency.CommitsLast90d)
		s.authors = append(s.authors, f.Factors.ChangeFrequency.UniqueAuthors)
		s.cyclomatic = append(s.cyclomatic, f.Factors.Complexity.Cyclomatic)
		s.functions = append(s.functions, f.Factors.Complexity.FunctionCount)
		s.importers = append(s.importers, len(f.Dependencies.ImportedBy))
		s.lines = append(s.lines, f.Size.Lines)
	}
	for _, xs := range [][]int{s.churn, s.authors, s.cyclomatic, s.functions, s.importers, s.lines} {
		sort.Ints(xs)
	}
	return s
}

// topPercent is the share of files (1-100, rounded up) whose value is at
// least v, so the repo maximum in 1000 files is "top 1%". It is 0 when v is
// not positive or the repo is empty, meaning "not notable".
func topPercent(sorted []int, v int) int {
	if v <= 0 || len(sorted) == 0 {
		return 0
	}
	atOrAbove := len(sorted) - sort.SearchInts(sorted, v)
	if atOrAbove == 0 {
		atOrAbove = 1 // above every file in the repo: the file itself
	}
	return (atOrAbove*100 + len(sorted) - 1) / len(sorted)
}
