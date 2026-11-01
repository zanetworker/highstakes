package calibration

import (
	"fmt"
	"sort"
	"time"

	"github.com/zanetworker/highstakes/internal/gitanalyzer"
	"github.com/zanetworker/highstakes/internal/scorer"
	"github.com/zanetworker/highstakes/internal/tour"
	"github.com/zanetworker/highstakes/internal/types"
)

// Options controls a calibration run.
type Options struct {
	K             int       // size of the "top K hottest files" list
	From, To      time.Time // events are counted in (From, To]
	Holdout       bool      // From is a cutoff: rescore with history up to it
	SurpriseLimit int       // max entries per surprise list
	Tiers         types.TierThresholds
	// ExistingAtCutoff limits the population to files present at the cutoff
	// (holdout only). Nil means every file in the heatmap.
	ExistingAtCutoff map[string]bool
	Events           EventFilter // event kinds to count; empty means EventsAll
}

// Report is the result of a calibration run.
type Report struct {
	Mode               string         `json:"mode"` // "in-sample" or "holdout"
	EventFilter        EventFilter    `json:"event_filter"`
	From               time.Time      `json:"from"`
	To                 time.Time      `json:"to"`
	K                  int            `json:"k"`
	Files              int            `json:"files"`
	FilesWithEvents    int            `json:"files_with_events"`
	TooFewEvents       bool           `json:"too_few_events"` // under MinFilesWithEvents
	BaseRate           float64        `json:"base_rate"`
	Events             FileEvents     `json:"events"`
	UnscoredEventFiles int            `json:"unscored_event_files"`
	Tiers              []TierResult   `json:"tiers"`
	Signals            []SignalResult `json:"signals"`
	TopK               []FileResult   `json:"top_k"`
	Surprises          Surprises      `json:"surprises"`
	Caveats            []string       `json:"caveats"`
}

// TierResult is the observed event rate for one tier.
type TierResult struct {
	Tier            types.Tier `json:"tier"`
	Files           int        `json:"files"`
	FilesWithEvents int        `json:"files_with_events"`
	Events          int        `json:"events"`
	EventsPerFile   float64    `json:"events_per_file"`
	HitRate         float64    `json:"hit_rate"` // share of files with >= 1 event
}

// SignalResult is how well one score ranks files by observed events.
type SignalResult struct {
	Name         string   `json:"name"`
	Available    bool     `json:"available"`
	Note         string   `json:"note,omitempty"`
	PrecisionAtK float64  `json:"precision_at_k"`
	RecallAtK    float64  `json:"recall_at_k"`
	Lift         float64  `json:"lift"`
	Spearman     *float64 `json:"spearman"` // nil when undefined
}

// FileResult is one file with its score and events.
type FileResult struct {
	Path      string     `json:"path"`
	Tier      types.Tier `json:"tier"`
	HeatScore int        `json:"heat_score"`
	Events    FileEvents `json:"events"`
}

// Surprises are the files where the tiers and history disagree most.
type Surprises struct {
	LowTierWithEvents []FileResult `json:"low_tier_with_events"`
	HotWithoutEvents  []FileResult `json:"hot_without_events"`
}

// Caveat texts, shared with the CLI docs.
const (
	CaveatInSample = "In-sample: churn and incident history were computed from the same history the events come from, so they look better than they are. Use --holdout to score with history before a cutoff and count events after it."
	CaveatHoldout  = "Holdout: churn and incident history were recomputed from history up to the cutoff. Complexity, centrality, path heuristics and blast radius still come from today's file contents, so some leakage remains."
	CaveatSecurity = "Security-only: events are security fixes, a rough proxy for impact (how bad a break is). Read the blast_radius row first; heat and churn also rise with how often a file changes, so they score on any event kind."
	CaveatLabels   = "Events come from commit-message keywords and recorded incidents. They are noisy (\"fix typo\" counts as a fix) and measure how often code breaks, while blast radius measures the impact if it breaks, so a weak blast-radius correlation is not by itself a failure."
)

// MinFilesWithEvents is the fewest files with events for which the report's
// numbers mean anything; below it the report sets TooFewEvents.
const MinFilesWithEvents = 10

var tierOrder = []types.Tier{types.TierCritical, types.TierHigh, types.TierMedium, types.TierLow}

// Run scores the heatmap against events in the window.
func Run(hm *types.Heatmap, commits []gitanalyzer.CommitInfo, incidents []types.Incident, opts Options) *Report {
	if opts.Events == "" {
		opts.Events = EventsAll
	}
	events := CountEvents(commits, incidents, opts.From, opts.To, opts.Events)

	r := &Report{Mode: "in-sample", EventFilter: opts.Events, From: opts.From, To: opts.To, K: opts.K}
	if opts.Holdout {
		r.Mode = "holdout"
	}

	// Population, rescored at the cutoff in holdout mode.
	var files []*types.FileHeat
	for path, fh := range hm.Files {
		// Test files are capped below MEDIUM and are not shipped code; leave
		// them out so they do not dilute the ranking being measured.
		if tour.IsTestFile(path) {
			continue
		}
		if opts.Holdout {
			if opts.ExistingAtCutoff != nil && !opts.ExistingAtCutoff[path] {
				continue
			}
			fh = rescoreAtCutoff(fh, commits, incidents, opts.From, opts.Tiers)
		}
		files = append(files, fh)
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	if opts.Holdout {
		// Likelihood changed at the cutoff, so the repo-relative "likely"
		// set and the quadrant tiers of assessed files change with it.
		rescored := make(map[string]*types.FileHeat, len(files))
		for _, fh := range files {
			rescored[fh.Path] = fh
		}
		scorer.ApplyQuadrants(rescored, opts.Tiers, tour.IsTestFile)
	}

	inPop := map[string]bool{}
	evOf := func(path string) FileEvents {
		if e, ok := events[path]; ok {
			return *e
		}
		return FileEvents{}
	}
	for _, fh := range files {
		inPop[fh.Path] = true
		e := evOf(fh.Path)
		if e.Total > 0 {
			r.FilesWithEvents++
		}
		r.Events.Total += e.Total
		r.Events.Revert += e.Revert
		r.Events.BugFix += e.BugFix
		r.Events.Security += e.Security
		r.Events.Incident += e.Incident
	}
	for path := range events {
		if !inPop[path] {
			r.UnscoredEventFiles++
		}
	}
	r.Files = len(files)
	if r.Files > 0 {
		r.BaseRate = float64(r.FilesWithEvents) / float64(r.Files)
	}

	r.Tiers = tierResults(files, evOf)
	r.Signals = signalResults(files, evOf, opts.K)
	r.TopK = topK(files, evOf, opts.K)
	r.Surprises = surprises(files, evOf, opts.SurpriseLimit)

	if opts.Holdout {
		r.Caveats = append(r.Caveats, CaveatHoldout)
	} else {
		r.Caveats = append(r.Caveats, CaveatInSample)
	}
	if opts.Events == EventsSecurity {
		r.Caveats = append(r.Caveats, CaveatSecurity)
	}
	r.Caveats = append(r.Caveats, CaveatLabels)
	if r.FilesWithEvents < MinFilesWithEvents {
		r.TooFewEvents = true
		r.Caveats = append(r.Caveats, fmt.Sprintf("Only %d file(s) had %s events in the window (under %d): too few for these numbers to mean anything; treat them as anecdotes, not evidence.", r.FilesWithEvents, opts.Events, MinFilesWithEvents))
	}
	return r
}

// rescoreAtCutoff returns a copy of fh with churn and incident history
// recomputed from data up to cutoff, and the heat score and tier recalculated.
func rescoreAtCutoff(fh *types.FileHeat, commits []gitanalyzer.CommitInfo, incidents []types.Incident, cutoff time.Time, tiers types.TierThresholds) *types.FileHeat {
	out := *fh
	f := out.Factors

	recentStart := cutoff.AddDate(0, 0, -90)
	recent := 0
	for _, c := range commits {
		if !c.When.After(recentStart) || c.When.After(cutoff) {
			continue
		}
		for _, p := range c.Files {
			if p == fh.Path {
				recent++
				break
			}
		}
	}
	// The file existed at the cutoff, so it has history; CommitCount only needs
	// to be non-zero for the scorer to include the churn factor.
	f.ChangeFrequency = types.ChangeFrequency{
		Score:          scorer.CalculateChangeFrequencyScore(recent),
		CommitCount:    max(recent, 1),
		CommitsLast90d: recent,
	}

	var past []types.Incident
	for _, inc := range incidents {
		if inc.File == fh.Path && !inc.Date.After(cutoff) {
			past = append(past, inc)
		}
	}
	f.IncidentHistory = types.IncidentHistory{
		Score:         scorer.CalculateIncidentScore(past, cutoff),
		IncidentCount: len(past),
	}

	out.Factors = f
	out.Likelihood = scorer.CalculateLikelihood(f)
	out.HeatScore = scorer.CalculateHeatScore(f)
	out.Tier = scorer.CalculateTier(out.HeatScore, tiers)
	return &out
}

func tierResults(files []*types.FileHeat, evOf func(string) FileEvents) []TierResult {
	byTier := map[types.Tier]*TierResult{}
	for _, t := range tierOrder {
		byTier[t] = &TierResult{Tier: t}
	}
	for _, fh := range files {
		tr, ok := byTier[fh.Tier]
		if !ok {
			continue
		}
		e := evOf(fh.Path)
		tr.Files++
		tr.Events += e.Total
		if e.Total > 0 {
			tr.FilesWithEvents++
		}
	}
	out := make([]TierResult, 0, len(tierOrder))
	for _, t := range tierOrder {
		tr := byTier[t]
		if tr.Files > 0 {
			tr.EventsPerFile = float64(tr.Events) / float64(tr.Files)
			tr.HitRate = float64(tr.FilesWithEvents) / float64(tr.Files)
		}
		out = append(out, *tr)
	}
	return out
}

type signalDef struct {
	name  string
	score func(*types.FileHeat) float64
}

var signalDefs = []signalDef{
	{"heat", func(f *types.FileHeat) float64 { return float64(f.HeatScore) }},
	{"blast_radius", func(f *types.FileHeat) float64 { return float64(f.Factors.BlastRadius.Score) }},
	{"churn", func(f *types.FileHeat) float64 { return float64(f.Factors.ChangeFrequency.Score) }},
	{"complexity", func(f *types.FileHeat) float64 { return float64(f.Factors.Complexity.Score) }},
	{"centrality", func(f *types.FileHeat) float64 { return f.Factors.DependencyCentrality.Score * 100 }},
	// The two axes, computed from factors so heatmaps written before the
	// axes existed are measured the same way.
	{"likelihood", func(f *types.FileHeat) float64 { return float64(scorer.CalculateLikelihood(f.Factors)) }},
	{"impact", func(f *types.FileHeat) float64 { i, _ := scorer.CalculateImpact(f.Factors); return float64(i) }},
}

func signalResults(files []*types.FileHeat, evOf func(string) FileEvents, k int) []SignalResult {
	assessed := false
	for _, fh := range files {
		if fh.Factors.BlastRadius.Assessed {
			assessed = true
			break
		}
	}

	var out []SignalResult
	for _, def := range signalDefs {
		res := SignalResult{Name: def.name, Available: true}
		pop := files
		if def.name == "blast_radius" {
			if !assessed {
				res.Available = false
				res.Note = "no LLM assessment in this heatmap (analyzed with --no-llm or no API key)"
				out = append(out, res)
				continue
			}
			pop = assessedOnly(files)
			res.Note = fmt.Sprintf("assessed files only (%d of %d)", len(pop), len(files))
		}
		if def.name == "impact" {
			if assessed {
				pop = assessedOnly(files)
				res.Note = fmt.Sprintf("assessed files only (%d of %d)", len(pop), len(files))
			} else {
				res.Note = "estimate from centrality and path keywords (no LLM assessment)"
			}
		}

		items := make([]Item, len(pop))
		xs := make([]float64, len(pop))
		ys := make([]float64, len(pop))
		for i, fh := range pop {
			e := evOf(fh.Path).Total
			items[i] = Item{Path: fh.Path, Score: def.score(fh), Events: e}
			xs[i], ys[i] = items[i].Score, float64(e)
		}
		res.PrecisionAtK = PrecisionAtK(items, k)
		res.RecallAtK = RecallAtK(items, k)
		res.Lift = Lift(res.PrecisionAtK, BaseRate(items))
		if rho, ok := Spearman(xs, ys); ok {
			res.Spearman = &rho
		}
		out = append(out, res)
	}
	return out
}

func assessedOnly(files []*types.FileHeat) []*types.FileHeat {
	var out []*types.FileHeat
	for _, fh := range files {
		if fh.Factors.BlastRadius.Assessed {
			out = append(out, fh)
		}
	}
	return out
}

func toResult(fh *types.FileHeat, evOf func(string) FileEvents) FileResult {
	return FileResult{Path: fh.Path, Tier: fh.Tier, HeatScore: fh.HeatScore, Events: evOf(fh.Path)}
}

// topK lists the k hottest files (ties broken by path) for display.
func topK(files []*types.FileHeat, evOf func(string) FileEvents, k int) []FileResult {
	sorted := append([]*types.FileHeat(nil), files...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].HeatScore > sorted[j].HeatScore })
	if k > len(sorted) {
		k = len(sorted)
	}
	out := []FileResult{}
	for _, fh := range sorted[:max(k, 0)] {
		out = append(out, toResult(fh, evOf))
	}
	return out
}

func surprises(files []*types.FileHeat, evOf func(string) FileEvents, limit int) Surprises {
	s := Surprises{LowTierWithEvents: []FileResult{}, HotWithoutEvents: []FileResult{}}
	for _, fh := range files {
		e := evOf(fh.Path)
		switch {
		case fh.Tier == types.TierLow && e.Total > 0:
			s.LowTierWithEvents = append(s.LowTierWithEvents, toResult(fh, evOf))
		case (fh.Tier == types.TierCritical || fh.Tier == types.TierHigh) && e.Total == 0:
			s.HotWithoutEvents = append(s.HotWithoutEvents, toResult(fh, evOf))
		}
	}
	// files are sorted by path, so stable sorts keep path order within ties.
	sort.SliceStable(s.LowTierWithEvents, func(i, j int) bool {
		return s.LowTierWithEvents[i].Events.Total > s.LowTierWithEvents[j].Events.Total
	})
	sort.SliceStable(s.HotWithoutEvents, func(i, j int) bool {
		return s.HotWithoutEvents[i].HeatScore > s.HotWithoutEvents[j].HeatScore
	})
	if limit >= 0 {
		if len(s.LowTierWithEvents) > limit {
			s.LowTierWithEvents = s.LowTierWithEvents[:limit]
		}
		if len(s.HotWithoutEvents) > limit {
			s.HotWithoutEvents = s.HotWithoutEvents[:limit]
		}
	}
	return s
}
