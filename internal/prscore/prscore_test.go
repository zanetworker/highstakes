package prscore

import (
	"strings"
	"testing"

	"github.com/zanetworker/highstakes/internal/scorer"
	"github.com/zanetworker/highstakes/internal/types"
)

func makeHeatmap() *types.Heatmap {
	return &types.Heatmap{
		Files: map[string]*types.FileHeat{
			"src/auth/jwt.go":     {Path: "src/auth/jwt.go", HeatScore: 95, Tier: types.TierCritical, Language: "Go"},
			"src/api/handler.go":  {Path: "src/api/handler.go", HeatScore: 45, Tier: types.TierMedium, Language: "Go"},
			"src/utils/format.go": {Path: "src/utils/format.go", HeatScore: 5, Tier: types.TierLow, Language: "Go"},
		},
	}
}

func TestCalculatePRRisk_InheritsCriticalTier(t *testing.T) {
	hm := makeHeatmap()
	changes := []FileChange{
		{Path: "src/auth/jwt.go", LinesAdded: 10, LinesDeleted: 5},
		{Path: "src/utils/format.go", LinesAdded: 2, LinesDeleted: 1},
	}

	risk := CalculatePRRisk(1234, changes, hm, scorer.DefaultTierThresholds())

	if risk.Tier != types.TierCritical {
		t.Errorf("PR touching critical file should be critical, got %s", risk.Tier)
	}
	if risk.HeatScore != 95 {
		t.Errorf("PR heat should be max of files (95), got %d", risk.HeatScore)
	}
}

func TestCalculatePRRisk_LowRiskPR(t *testing.T) {
	hm := makeHeatmap()
	changes := []FileChange{
		{Path: "src/utils/format.go", LinesAdded: 2, LinesDeleted: 1},
	}

	risk := CalculatePRRisk(1235, changes, hm, scorer.DefaultTierThresholds())

	if risk.Tier != types.TierLow {
		t.Errorf("PR touching only low file should be low, got %s", risk.Tier)
	}
	if !risk.ReviewRequirements.AutoMerge {
		t.Error("low risk PR should allow auto-merge")
	}
}

func TestCalculatePRRisk_CriticalBlocksAutoMerge(t *testing.T) {
	hm := makeHeatmap()
	changes := []FileChange{
		{Path: "src/auth/jwt.go", LinesAdded: 1, LinesDeleted: 0},
	}

	risk := CalculatePRRisk(1236, changes, hm, scorer.DefaultTierThresholds())

	if risk.ReviewRequirements.AutoMerge {
		t.Error("critical PR should block auto-merge")
	}
	if risk.ReviewRequirements.MinReviewers < 2 {
		t.Errorf("critical PR should require 2 reviewers, got %d", risk.ReviewRequirements.MinReviewers)
	}
}

func TestCalculatePRRisk_UnscoredFiles(t *testing.T) {
	tests := []struct {
		name         string
		path         string
		wantHeat     int
		wantTier     types.Tier
		wantUnscored bool
		wantSignal   bool
	}{
		{"new file inherits max heat of its directory", "src/auth/oidc.go", 95, types.TierCritical, true, true},
		{"new file in new subdirectory inherits nearest ancestor", "src/api/v2/routes.go", 45, types.TierMedium, true, true},
		{"new file with no scored ancestor stays low but is flagged", "new_file.go", 0, types.TierLow, true, true},
		{"documentation file is not scored", "src/auth/README.md", 0, types.TierLow, false, false},
		{"plain text file is documentation", "src/auth/NOTES.txt", 0, types.TierLow, false, false},
		{"requirements.txt inherits directory heat", "src/auth/requirements.txt", 95, types.TierCritical, true, true},
		{"Cargo.lock inherits directory heat", "src/auth/Cargo.lock", 95, types.TierCritical, true, true},
		{"known file is scored normally", "src/utils/format.go", 5, types.TierLow, false, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			changes := []FileChange{{Path: tt.path, LinesAdded: 10, IsNew: true}}
			risk := CalculatePRRisk(1237, changes, makeHeatmap(), scorer.DefaultTierThresholds())

			fc := risk.FilesChanged[0]
			if fc.HeatScore != tt.wantHeat || fc.Tier != tt.wantTier {
				t.Errorf("file heat/tier = %d/%s, want %d/%s", fc.HeatScore, fc.Tier, tt.wantHeat, tt.wantTier)
			}
			if fc.Unscored != tt.wantUnscored {
				t.Errorf("Unscored = %v, want %v", fc.Unscored, tt.wantUnscored)
			}
			if risk.HeatScore != tt.wantHeat {
				t.Errorf("PR heat = %d, want %d", risk.HeatScore, tt.wantHeat)
			}
			gotSignal := false
			for _, s := range risk.CircuitBreakerSignals {
				if strings.Contains(s, "Unscored new file") && strings.Contains(s, tt.path) {
					gotSignal = true
				}
			}
			if gotSignal != tt.wantSignal {
				t.Errorf("unscored signal present = %v, want %v (signals: %v)", gotSignal, tt.wantSignal, risk.CircuitBreakerSignals)
			}
		})
	}
}

func TestCalculatePRRisk_EmptyChanges(t *testing.T) {
	hm := makeHeatmap()
	risk := CalculatePRRisk(1238, []FileChange{}, hm, scorer.DefaultTierThresholds())

	if risk.HeatScore != 0 {
		t.Errorf("empty PR should have heat 0, got %d", risk.HeatScore)
	}
	if len(risk.FilesChanged) != 0 {
		t.Errorf("empty PR should have 0 files changed, got %d", len(risk.FilesChanged))
	}
}

func TestCircuitBreaker_LargeDiff(t *testing.T) {
	hm := makeHeatmap()
	changes := []FileChange{
		{Path: "src/api/handler.go", LinesAdded: 300, LinesDeleted: 250},
	}

	signals := checkCircuitBreakers(changes, hm)

	found := false
	for _, s := range signals {
		if s == "Large diff (>500 lines)" {
			found = true
		}
	}
	if !found {
		t.Error("should flag large diff (>500 lines)")
	}
}

func TestCircuitBreaker_SmallDiff(t *testing.T) {
	hm := makeHeatmap()
	changes := []FileChange{
		{Path: "src/api/handler.go", LinesAdded: 10, LinesDeleted: 5},
	}

	signals := checkCircuitBreakers(changes, hm)

	for _, s := range signals {
		if s == "Large diff (>500 lines)" {
			t.Error("should not flag small diff")
		}
	}
}

func TestCircuitBreaker_LowTestRatio(t *testing.T) {
	hm := makeHeatmap()
	changes := []FileChange{
		{Path: "src/api/handler.go", LinesAdded: 100, LinesDeleted: 0},
		// No test files
	}

	signals := checkCircuitBreakers(changes, hm)

	found := false
	for _, s := range signals {
		if s == "Low test ratio (<0.3)" {
			found = true
		}
	}
	if !found {
		t.Error("should flag low test ratio when no tests")
	}
}

func TestCircuitBreaker_GoodTestRatio(t *testing.T) {
	hm := makeHeatmap()
	changes := []FileChange{
		{Path: "src/api/handler.go", LinesAdded: 100, LinesDeleted: 0},
		{Path: "src/api/handler_test.go", LinesAdded: 50, LinesDeleted: 0},
	}

	signals := checkCircuitBreakers(changes, hm)

	for _, s := range signals {
		if s == "Low test ratio (<0.3)" {
			t.Error("should not flag when test ratio >= 0.3")
		}
	}
}

func TestIsTestFile(t *testing.T) {
	tests := []struct {
		path     string
		expected bool
	}{
		{"handler_test.go", true},
		{"handler.test.ts", true},
		{"handler.spec.js", true},
		{"test_handler.py", true},
		{"handler.go", false},
		{"main.go", false},
		{"testdata/fixture.json", false},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			result := isTestFile(tt.path)
			if result != tt.expected {
				t.Errorf("isTestFile(%q) = %v, want %v", tt.path, result, tt.expected)
			}
		})
	}
}

func TestCalculatePRRisk_UnscoredInheritsQuadrantTier(t *testing.T) {
	hm := &types.Heatmap{Files: map[string]*types.FileHeat{
		// Quadrant tiers that the numeric thresholds would not give.
		"core/store.go":  {Path: "core/store.go", HeatScore: 75, Tier: types.TierCritical, ImpactAssessed: true, Quadrant: types.QuadrantFragileCritical},
		"core/cache.go":  {Path: "core/cache.go", HeatScore: 40, Tier: types.TierMedium},
		"tools/lint.go":  {Path: "tools/lint.go", HeatScore: 90, Tier: types.TierMedium, ImpactAssessed: true, Quadrant: types.QuadrantWatch},
		"tools/hooks.go": {Path: "tools/hooks.go", HeatScore: 20, Tier: types.TierLow},
		// Stale stored tier: unassessed files are re-tiered with the thresholds.
		"flat/old.go": {Path: "flat/old.go", HeatScore: 70, Tier: types.TierLow},
	}}
	tests := []struct {
		name     string
		path     string
		wantHeat int
		wantTier types.Tier
	}{
		{"fragile-critical neighbour keeps the new file critical", "core/wal.go", 75, types.TierCritical},
		{"nearest ancestor's quadrant tier carries down", "core/v2/wal.go", 75, types.TierCritical},
		{"hot but low-impact neighbour does not raise the tier", "tools/fmt.go", 90, types.TierMedium},
		{"no scored ancestor stays low", "other/new.go", 0, types.TierLow},
		{"unassessed neighbour is tiered from heat", "flat/new.go", 70, types.TierHigh},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			risk := CalculatePRRisk(1, []FileChange{{Path: tt.path, LinesAdded: 5, IsNew: true}}, hm, scorer.DefaultTierThresholds())
			fc := risk.FilesChanged[0]
			if fc.HeatScore != tt.wantHeat || fc.Tier != tt.wantTier {
				t.Errorf("file heat/tier = %d/%s, want %d/%s", fc.HeatScore, fc.Tier, tt.wantHeat, tt.wantTier)
			}
			if risk.Tier != tt.wantTier {
				t.Errorf("PR tier = %s, want %s", risk.Tier, tt.wantTier)
			}
		})
	}
}
