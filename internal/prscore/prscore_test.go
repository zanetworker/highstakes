package prscore

import (
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

func TestCalculatePRRisk_UnknownFile(t *testing.T) {
	hm := makeHeatmap()
	changes := []FileChange{
		{Path: "new_file.go", LinesAdded: 50, LinesDeleted: 0, IsNew: true},
	}

	risk := CalculatePRRisk(1237, changes, hm, scorer.DefaultTierThresholds())

	if risk.Tier != types.TierLow {
		t.Errorf("new file not in heatmap should be low, got %s", risk.Tier)
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
