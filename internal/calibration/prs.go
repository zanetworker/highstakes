package calibration

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/zanetworker/highstakes/internal/prdata"
	"github.com/zanetworker/highstakes/internal/tour"
	"github.com/zanetworker/highstakes/internal/types"
)

// Stakes signal names: evidence, independent of the LLM, that maintainers
// treated a break in a file as serious.
const (
	StakesBackported  = "backported"     // a PR touching the file reached a release/backport branch
	StakesFastRevert  = "fast_revert"    // a PR touching the file was reverted within 7 days
	StakesSeverity    = "severity_label" // a PR touching the file carried a severity label
	StakesReviewDepth = "review_depth"   // a PR touching the file had 2+ approvers or 3+ reviews
	StakesCodeOwners  = "codeowners"     // the file is covered by CODEOWNERS (file-level)
	StakesCombined    = "combined"       // number of distinct stakes signals per file

	// StakesActivity is a reference row, not a stakes signal: every mapped
	// PR touching the file. A stakes signal that correlates with blast radius
	// no better than this one mostly measures activity.
	StakesActivity = "pr_activity"
)

// CaveatStakes explains what the stakes comparison can and cannot show.
const CaveatStakes = "Stakes: blast radius (the LLM's impact score) is compared with what maintainers did, not with outages. Review depth and CODEOWNERS reflect how important maintainers judge a file, which is the point of the comparison, but they also rise with code volume and with how often a file changes (more PRs, more chances to be backported or deeply reviewed), so a positive correlation partly measures activity."

// FragilityEvents counts, per file, the bug-fix and revert PRs merged in
// (from, to]. A PR that is both counts once in Total. PRs touching more than
// MaxFilesPerEvent files are skipped, as in CountEvents.
func FragilityEvents(prs []prdata.LabeledPR, from, to time.Time) map[string]*FileEvents {
	out := map[string]*FileEvents{}
	for _, pr := range prs {
		if (!pr.BugFix && !pr.Revert) || !pr.MergedAt.After(from) || pr.MergedAt.After(to) || len(pr.Files) > MaxFilesPerEvent {
			continue
		}
		for _, f := range pr.Files {
			e, ok := out[f]
			if !ok {
				e = &FileEvents{}
				out[f] = e
			}
			e.Total++
			if pr.BugFix {
				e.BugFix++
			}
			if pr.Revert {
				e.Revert++
			}
		}
	}
	return out
}

// StakesCounts counts, per stakes signal and file, the PRs with that signal
// that touched the file. PRs touching more than MaxFilesPerEvent files are
// skipped. Signals with no PRs are absent.
func StakesCounts(prs []prdata.LabeledPR) map[string]map[string]int {
	out := map[string]map[string]int{}
	add := func(signal string, files []string) {
		m, ok := out[signal]
		if !ok {
			m = map[string]int{}
			out[signal] = m
		}
		for _, f := range files {
			m[f]++
		}
	}
	for _, pr := range prs {
		if len(pr.Files) > MaxFilesPerEvent {
			continue
		}
		if pr.Backported {
			add(StakesBackported, pr.Files)
		}
		if pr.FastRevert {
			add(StakesFastRevert, pr.Files)
		}
		if pr.Severity {
			add(StakesSeverity, pr.Files)
		}
		if pr.DeepReview {
			add(StakesReviewDepth, pr.Files)
		}
	}
	return out
}

// StakesInput is one stakes signal: per-file counts and whether the data to
// compute it existed (e.g. no release branches means no backport signal).
type StakesInput struct {
	Name      string
	Available bool
	Note      string
	Counts    map[string]int
}

// StakesResult is how well blast radius ranks files by one stakes signal.
type StakesResult struct {
	Name            string   `json:"name"`
	Available       bool     `json:"available"`
	Note            string   `json:"note,omitempty"`
	Files           int      `json:"files"`
	FilesWithEvents int      `json:"files_with_events"`
	TooFewEvents    bool     `json:"too_few_events"`
	BaseRate        float64  `json:"base_rate"`
	PrecisionAtK    float64  `json:"precision_at_k"`
	RecallAtK       float64  `json:"recall_at_k"`
	Lift            float64  `json:"lift"`
	Spearman        *float64 `json:"spearman"` // nil when undefined
}

// CombineStakes counts, per file, how many distinct available signals have a
// positive count for it. The StakesActivity reference is not a signal and is
// left out.
func CombineStakes(inputs []StakesInput) map[string]int {
	out := map[string]int{}
	for _, in := range inputs {
		if !in.Available || in.Name == StakesActivity {
			continue
		}
		for f, n := range in.Counts {
			if n > 0 {
				out[f]++
			}
		}
	}
	return out
}

// RunStakes compares blast radius with each stakes signal, then with the
// combined signal (CombineStakes over the available inputs). The population
// is the non-test files with an LLM assessment; without any, every result is
// unavailable.
func RunStakes(hm *types.Heatmap, inputs []StakesInput, k int) []StakesResult {
	var pop []*types.FileHeat
	for p, fh := range hm.Files {
		if !tour.IsTestFile(p) && fh.Factors.BlastRadius.Assessed {
			pop = append(pop, fh)
		}
	}
	sort.Slice(pop, func(i, j int) bool { return pop[i].Path < pop[j].Path })

	var used []string
	for _, in := range inputs {
		if in.Available && in.Name != StakesActivity {
			used = append(used, in.Name)
		}
	}
	combined := StakesInput{Name: StakesCombined, Available: len(used) > 0, Counts: CombineStakes(inputs),
		Note: "distinct signals per file among: " + strings.Join(used, ", ")}
	if len(used) == 0 {
		combined.Note = "no stakes signal available"
	}

	out := make([]StakesResult, 0, len(inputs)+1)
	for _, in := range append(append([]StakesInput(nil), inputs...), combined) {
		res := StakesResult{Name: in.Name, Available: in.Available, Note: in.Note}
		if len(pop) == 0 {
			res.Available = false
			res.Note = "no LLM assessment in this heatmap (analyzed with --no-llm or no API key)"
		}
		if !res.Available {
			out = append(out, res)
			continue
		}
		items := make([]Item, len(pop))
		xs := make([]float64, len(pop))
		ys := make([]float64, len(pop))
		for i, fh := range pop {
			n := in.Counts[fh.Path]
			items[i] = Item{Path: fh.Path, Score: float64(fh.Factors.BlastRadius.Score), Events: n}
			xs[i], ys[i] = items[i].Score, float64(n)
		}
		res.Files = len(pop)
		res.FilesWithEvents = countPositives(items)
		res.TooFewEvents = res.FilesWithEvents < MinFilesWithEvents
		res.BaseRate = BaseRate(items)
		res.PrecisionAtK = PrecisionAtK(items, k)
		res.RecallAtK = RecallAtK(items, k)
		res.Lift = Lift(res.PrecisionAtK, res.BaseRate)
		if rho, ok := Spearman(xs, ys); ok {
			res.Spearman = &rho
		}
		out = append(out, res)
	}
	return out
}

// PRReport is the result of a calibration run against merged pull requests.
// The embedded Report holds the fragility comparison (PR bug fixes and
// reverts against likelihood, heat and the other signals), so the JSON keeps
// every field of a commit-sourced report and adds the rest.
type PRReport struct {
	*Report
	GitHub        PRSummary      `json:"github"`
	CommitKeyword *Report        `json:"commit_keyword"` // the commit-message source on the same window
	Stakes        []StakesResult `json:"stakes"`
}

// PRSummary describes the PR data behind a PRReport.
type PRSummary struct {
	Repo  string             `json:"repo"`
	Fetch prdata.FetchResult `json:"fetch"`
	// PRs merged in the window, how many mapped to a local commit, and the
	// counts of each label among mapped PRs.
	InWindow         int      `json:"in_window"`
	Mapped           int      `json:"mapped"`
	BugFix           int      `json:"bugfix"`
	Revert           int      `json:"revert"`
	Backported       int      `json:"backported"`
	FastRevert       int      `json:"fast_revert"`
	Severity         int      `json:"severity_label"`
	DeepReview       int      `json:"review_depth"`
	BackportBranches []string `json:"backport_branches"`
	CodeOwnersFile   string   `json:"codeowners_file,omitempty"`
}

// Summarize counts the labels among mapped PRs.
func Summarize(repo string, fetch prdata.FetchResult, prs []prdata.LabeledPR, branches []string, codeOwnersFile string) PRSummary {
	s := PRSummary{Repo: repo, Fetch: fetch, InWindow: len(fetch.PRs), Mapped: len(prs),
		BackportBranches: append([]string{}, branches...), CodeOwnersFile: codeOwnersFile}
	for _, pr := range prs {
		s.BugFix += b2i(pr.BugFix)
		s.Revert += b2i(pr.Revert)
		s.Backported += b2i(pr.Backported)
		s.FastRevert += b2i(pr.FastRevert)
		s.Severity += b2i(pr.Severity)
		s.DeepReview += b2i(pr.DeepReview)
	}
	return s
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

// StakesInputs builds the stakes signals from labelled PRs, the backport
// branches found (none makes the backport signal unavailable) and an optional
// CODEOWNERS file applied to every file in the heatmap.
func StakesInputs(hm *types.Heatmap, prs []prdata.LabeledPR, branches []string, co *prdata.CodeOwners, codeOwnersFile string) []StakesInput {
	counts := StakesCounts(prs)
	activity := map[string]int{}
	for _, pr := range prs {
		if len(pr.Files) <= MaxFilesPerEvent {
			for _, f := range pr.Files {
				activity[f]++
			}
		}
	}
	backport := StakesInput{Name: StakesBackported, Available: len(branches) > 0, Counts: counts[StakesBackported],
		Note: fmt.Sprintf("%d release/backport branch(es)", len(branches))}
	if len(branches) == 0 {
		backport.Note = "no release or backport branches in the local clone (fetch them to enable)"
	}
	owners := StakesInput{Name: StakesCodeOwners, Available: co != nil, Counts: map[string]int{}, Note: codeOwnersFile}
	if co == nil {
		owners.Note = "no CODEOWNERS file"
	} else {
		for p := range hm.Files {
			if co.Covers(p) {
				owners.Counts[p] = 1
			}
		}
	}
	return []StakesInput{
		backport,
		{Name: StakesFastRevert, Available: true, Counts: counts[StakesFastRevert], Note: "reverted within 7 days of merge"},
		{Name: StakesSeverity, Available: true, Counts: counts[StakesSeverity]},
		{Name: StakesReviewDepth, Available: true, Counts: counts[StakesReviewDepth], Note: "2+ distinct approvers or 3+ reviews"},
		owners,
		{Name: StakesActivity, Available: true, Counts: activity, Note: "reference, not a stakes signal: all mapped PRs (activity)"},
	}
}
