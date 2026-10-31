package calibration

import (
	"math"
	"testing"
	"time"

	"github.com/zanetworker/highstakes/internal/gitanalyzer"
	"github.com/zanetworker/highstakes/internal/scorer"
	"github.com/zanetworker/highstakes/internal/types"
)

func fileHeat(path string, heat int, tier types.Tier, churn, complexity int, centrality float64) *types.FileHeat {
	return &types.FileHeat{
		Path: path, HeatScore: heat, Tier: tier,
		Factors: types.Factors{
			ChangeFrequency:      types.ChangeFrequency{Score: churn, CommitCount: 1},
			Complexity:           types.Complexity{Score: complexity},
			DependencyCentrality: types.DependencyCentrality{Score: centrality},
		},
	}
}

func testHeatmap() *types.Heatmap {
	return &types.Heatmap{Files: map[string]*types.FileHeat{
		"hot.go":   fileHeat("hot.go", 90, types.TierCritical, 50, 80, 0.9),
		"warm.go":  fileHeat("warm.go", 70, types.TierHigh, 0, 10, 0.1),
		"mid.go":   fileHeat("mid.go", 40, types.TierMedium, 20, 50, 0.5),
		"cold.go":  fileHeat("cold.go", 10, types.TierLow, 80, 0, 0.2),
		"cold2.go": fileHeat("cold2.go", 5, types.TierLow, 0, 0, 0),
	}}
}

func fixes(path string, n int, start time.Time) []gitanalyzer.CommitInfo {
	var out []gitanalyzer.CommitInfo
	for i := 0; i < n; i++ {
		out = append(out, gitanalyzer.CommitInfo{Message: "fix: bug", When: start.Add(time.Duration(i+1) * time.Hour), Files: []string{path}})
	}
	return out
}

func signal(t *testing.T, r *Report, name string) SignalResult {
	t.Helper()
	for _, s := range r.Signals {
		if s.Name == name {
			return s
		}
	}
	t.Fatalf("signal %q missing", name)
	return SignalResult{}
}

func TestRun_InSample(t *testing.T) {
	commits := append(fixes("hot.go", 2, base), fixes("cold.go", 3, base)...)
	commits = append(commits, fixes("gone.go", 1, base)...)
	r := Run(testHeatmap(), commits, nil, Options{
		K: 2, From: base, To: base.AddDate(0, 0, 30), SurpriseLimit: 5,
		Tiers: scorer.DefaultTierThresholds(),
	})

	if r.Mode != "in-sample" || r.Files != 5 || r.FilesWithEvents != 2 || !near(r.BaseRate, 0.4) {
		t.Fatalf("header = mode %s files %d with %d base %v", r.Mode, r.Files, r.FilesWithEvents, r.BaseRate)
	}
	if r.UnscoredEventFiles != 1 {
		t.Errorf("UnscoredEventFiles = %d, want 1", r.UnscoredEventFiles)
	}
	if r.Events.Total != 5 || r.Events.BugFix != 5 {
		t.Errorf("Events = %+v, want 5 bug fixes", r.Events)
	}

	tests := []struct {
		name              string
		precision, recall float64
		lift              float64
		spearman          float64
		spearmanDefined   bool
		available         bool
	}{
		{"heat", 0.5, 0.5, 1.25, 1 / math.Sqrt(80), true, true},
		{"churn", 1, 1, 2.5, 0, true, true},
		{"complexity", 0.5, 0.5, 1.25, 0, true, true},
		{"blast_radius", 0, 0, 0, 0, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := signal(t, r, tt.name)
			if s.Available != tt.available {
				t.Fatalf("available = %v, want %v", s.Available, tt.available)
			}
			if !tt.available {
				if s.Note == "" {
					t.Error("unavailable signal should explain why")
				}
				return
			}
			if !near(s.PrecisionAtK, tt.precision) || !near(s.RecallAtK, tt.recall) || !near(s.Lift, tt.lift) {
				t.Errorf("P/R/lift = %v/%v/%v, want %v/%v/%v", s.PrecisionAtK, s.RecallAtK, s.Lift, tt.precision, tt.recall, tt.lift)
			}
			if (s.Spearman != nil) != tt.spearmanDefined {
				t.Fatalf("spearman defined = %v", s.Spearman != nil)
			}
			if tt.name == "heat" && !near(*s.Spearman, tt.spearman) {
				t.Errorf("spearman = %v, want %v", *s.Spearman, tt.spearman)
			}
		})
	}

	wantTiers := map[types.Tier]TierResult{
		types.TierCritical: {Tier: types.TierCritical, Files: 1, FilesWithEvents: 1, Events: 2, EventsPerFile: 2, HitRate: 1},
		types.TierHigh:     {Tier: types.TierHigh, Files: 1},
		types.TierMedium:   {Tier: types.TierMedium, Files: 1},
		types.TierLow:      {Tier: types.TierLow, Files: 2, FilesWithEvents: 1, Events: 3, EventsPerFile: 1.5, HitRate: 0.5},
	}
	if len(r.Tiers) != 4 || r.Tiers[0].Tier != types.TierCritical || r.Tiers[3].Tier != types.TierLow {
		t.Fatalf("tiers out of order: %+v", r.Tiers)
	}
	for _, tr := range r.Tiers {
		if tr != wantTiers[tr.Tier] {
			t.Errorf("tier %s = %+v, want %+v", tr.Tier, tr, wantTiers[tr.Tier])
		}
	}

	if len(r.Surprises.LowTierWithEvents) != 1 || r.Surprises.LowTierWithEvents[0].Path != "cold.go" || r.Surprises.LowTierWithEvents[0].Events.Total != 3 {
		t.Errorf("LowTierWithEvents = %+v", r.Surprises.LowTierWithEvents)
	}
	if len(r.Surprises.HotWithoutEvents) != 1 || r.Surprises.HotWithoutEvents[0].Path != "warm.go" {
		t.Errorf("HotWithoutEvents = %+v", r.Surprises.HotWithoutEvents)
	}
	if len(r.TopK) != 2 || r.TopK[0].Path != "hot.go" || r.TopK[1].Path != "warm.go" {
		t.Errorf("TopK = %+v", r.TopK)
	}
	if len(r.Caveats) == 0 {
		t.Error("in-sample report must carry the leakage caveat")
	}
}

func TestRun_SurpriseLimitAndEmpty(t *testing.T) {
	hm := testHeatmap()
	r := Run(hm, nil, nil, Options{K: 3, From: base, To: base.AddDate(0, 0, 1), SurpriseLimit: 1, Tiers: scorer.DefaultTierThresholds()})
	if r.FilesWithEvents != 0 || r.BaseRate != 0 {
		t.Errorf("no events: with=%d base=%v", r.FilesWithEvents, r.BaseRate)
	}
	// Two hot files without events, limit 1 keeps the hottest.
	if len(r.Surprises.HotWithoutEvents) != 1 || r.Surprises.HotWithoutEvents[0].Path != "hot.go" {
		t.Errorf("HotWithoutEvents = %+v", r.Surprises.HotWithoutEvents)
	}
	if s := signal(t, r, "heat"); s.Spearman != nil || s.PrecisionAtK != 0 {
		t.Errorf("heat with no events = %+v", s)
	}
}

func TestRun_Holdout(t *testing.T) {
	cutoff := base
	commits := fixes("cold2.go", 45, cutoff.AddDate(0, 0, -30)) // churn before cutoff
	commits = append(commits, fixes("mid.go", 2, cutoff)...)    // events after cutoff
	commits = append(commits, fixes("cold.go", 4, cutoff)...)   // file did not exist at cutoff
	existing := map[string]bool{"hot.go": true, "warm.go": true, "mid.go": true, "cold2.go": true}

	r := Run(testHeatmap(), commits, nil, Options{
		K: 1, From: cutoff, To: cutoff.AddDate(0, 0, 30), Holdout: true, ExistingAtCutoff: existing,
		SurpriseLimit: 5, Tiers: scorer.DefaultTierThresholds(),
	})
	if r.Mode != "holdout" || r.Files != 4 || r.FilesWithEvents != 1 {
		t.Fatalf("mode %s files %d with %d", r.Mode, r.Files, r.FilesWithEvents)
	}
	// cold2.go had 45 commits in the 90 days before the cutoff: churn 25 is now
	// the top churn signal, and it has no events after the cutoff.
	if s := signal(t, r, "churn"); s.PrecisionAtK != 0 {
		t.Errorf("holdout churn precision = %v, want 0 (cold2 churned before cutoff, no later fixes)", s.PrecisionAtK)
	}
}

func TestRescoreAtCutoff(t *testing.T) {
	cutoff := base
	mk := func() *types.FileHeat {
		return &types.FileHeat{Path: "f.go", Factors: types.Factors{
			DependencyCentrality: types.DependencyCentrality{Score: 0.5},
			Complexity:           types.Complexity{Score: 40},
			TestCoverage:         types.TestCoverage{Score: 50},
			ChangeFrequency:      types.ChangeFrequency{Score: 100, CommitCount: 500, CommitsLast90d: 400},
		}}
	}
	old := gitanalyzer.CommitInfo{Message: "x", When: cutoff.AddDate(0, 0, -200), Files: []string{"f.go"}}
	later := gitanalyzer.CommitInfo{Message: "x", When: cutoff.AddDate(0, 0, 1), Files: []string{"f.go"}}
	in45 := fixes("f.go", 45, cutoff.AddDate(0, 0, -60))

	tests := []struct {
		name      string
		commits   []gitanalyzer.CommitInfo
		incidents []types.Incident
		wantHeat  int
		wantChurn int
		wantTier  types.Tier
	}{
		// (50*20 + 0*10 keywords + 40*15 + 50*10 + 25*10) / 65 = 36.2
		{"45 commits in the 90 days before", append([]gitanalyzer.CommitInfo{old, later}, in45...), nil, 36, 25, types.TierMedium},
		// churn factor still counts (file has history) but scores 0: 2100/65 = 32.3
		{"no recent commits", []gitanalyzer.CommitInfo{old, later}, nil, 32, 0, types.TierMedium},
		// incident before cutoff: 20 + 20 recency + 5 high = 45 at weight 25; the later one is ignored.
		// (2100 + 45*25) / 90 = 35.8
		{"incident before cutoff counts, after does not", nil, []types.Incident{
			{File: "f.go", Severity: "high", Date: cutoff.AddDate(0, 0, -10)},
			{File: "f.go", Severity: "critical", Date: cutoff.AddDate(0, 0, 5)},
			{File: "other.go", Severity: "critical", Date: cutoff.AddDate(0, 0, -5)},
		}, 35, 0, types.TierMedium},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fh := mk()
			got := rescoreAtCutoff(fh, tt.commits, tt.incidents, cutoff, scorer.DefaultTierThresholds())
			if got.HeatScore != tt.wantHeat || got.Factors.ChangeFrequency.Score != tt.wantChurn || got.Tier != tt.wantTier {
				t.Errorf("heat=%d churn=%d tier=%s, want heat=%d churn=%d tier=%s",
					got.HeatScore, got.Factors.ChangeFrequency.Score, got.Tier, tt.wantHeat, tt.wantChurn, tt.wantTier)
			}
			if fh.Factors.ChangeFrequency.Score != 100 {
				t.Error("rescoreAtCutoff must not mutate its input")
			}
		})
	}
}
