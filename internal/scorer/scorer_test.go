package scorer

import (
	"testing"
	"time"

	"github.com/zanetworker/code-heatmap/internal/types"
)

func TestCalculateHeatScore_AllZero(t *testing.T) {
	factors := types.Factors{}
	score := CalculateHeatScore(factors)

	if score != 0 {
		t.Errorf("expected 0 for all-zero factors, got %d", score)
	}
}

func TestCalculateHeatScore_MaxFactors(t *testing.T) {
	factors := types.Factors{
		DependencyCentrality: types.DependencyCentrality{Score: 1.0},
		IncidentHistory:      types.IncidentHistory{Score: 100, IncidentCount: 1},
		ChangeFrequency:      types.ChangeFrequency{Score: 100, CommitCount: 1},
		UserImpact:           types.UserImpact{Score: 100},
		DataSensitivity:      types.DataSensitivity{Score: 100},
		TestCoverage:         types.TestCoverage{Score: 100}, // No coverage = max risk
		Complexity:           types.Complexity{Score: 100},
	}
	score := CalculateHeatScore(factors)

	// With all factors at max and all active, score should be very high
	if score < 90 {
		t.Errorf("expected >= 90 for max factors, got %d", score)
	}
}

func TestCalculateHeatScore_HighCoverageReducesScore(t *testing.T) {
	base := types.Factors{
		DependencyCentrality: types.DependencyCentrality{Score: 0.5},
		IncidentHistory:      types.IncidentHistory{Score: 50},
		ChangeFrequency:      types.ChangeFrequency{Score: 50},
		UserImpact:           types.UserImpact{Score: 50},
		DataSensitivity:      types.DataSensitivity{Score: 50},
		Complexity:           types.Complexity{Score: 50},
	}

	noCoverage := base
	noCoverage.TestCoverage = types.TestCoverage{Score: 100} // No coverage = high risk score

	highCoverage := base
	highCoverage.TestCoverage = types.TestCoverage{Score: 0} // Full coverage = low risk score

	scoreNoCoverage := CalculateHeatScore(noCoverage)
	scoreHighCoverage := CalculateHeatScore(highCoverage)

	if scoreHighCoverage >= scoreNoCoverage {
		t.Errorf("high coverage (%d) should produce lower score than no coverage (%d)",
			scoreHighCoverage, scoreNoCoverage)
	}
}

func TestCalculateHeatScore_ClampedToZero(t *testing.T) {
	// High test coverage with zero everything else should clamp to 0
	factors := types.Factors{
		TestCoverage: types.TestCoverage{Score: 100},
	}
	score := CalculateHeatScore(factors)

	if score < 0 {
		t.Errorf("score should be clamped to 0, got %d", score)
	}
}

func TestCalculateTier(t *testing.T) {
	thresholds := DefaultTierThresholds()

	tests := []struct {
		score    int
		expected types.Tier
	}{
		{100, types.TierCritical},
		{86, types.TierCritical},
		{85, types.TierHigh},
		{61, types.TierHigh},
		{60, types.TierMedium},
		{31, types.TierMedium},
		{30, types.TierLow},
		{0, types.TierLow},
	}

	for _, tt := range tests {
		tier := CalculateTier(tt.score, thresholds)
		if tier != tt.expected {
			t.Errorf("score %d: expected tier %q, got %q", tt.score, tt.expected, tier)
		}
	}
}

func TestCalculateReviewRequirements_Critical(t *testing.T) {
	req := CalculateReviewRequirements(types.TierCritical)

	if req.MinReviewers != 2 {
		t.Errorf("critical tier should require 2 reviewers, got %d", req.MinReviewers)
	}
	if !req.RequiresSenior {
		t.Error("critical tier should require senior reviewer")
	}
	if !req.RequiresSecurityScan {
		t.Error("critical tier should require security scan")
	}
	if req.AutoMerge {
		t.Error("critical tier should not allow auto-merge")
	}
}

func TestCalculateReviewRequirements_Low(t *testing.T) {
	req := CalculateReviewRequirements(types.TierLow)

	if req.MinReviewers != 0 {
		t.Errorf("low tier should require 0 reviewers, got %d", req.MinReviewers)
	}
	if !req.AutoMerge {
		t.Error("low tier should allow auto-merge")
	}
}

func TestCalculateIncidentScore_NoIncidents(t *testing.T) {
	score := CalculateIncidentScore(nil, time.Now())

	if score != 0 {
		t.Errorf("expected 0 for no incidents, got %d", score)
	}
}

func TestCalculateIncidentScore_RecentHighSeverity(t *testing.T) {
	now := time.Now()
	incidents := []types.Incident{
		{Severity: "high", Date: now.AddDate(0, 0, -30)},
		{Severity: "high", Date: now.AddDate(0, 0, -60)},
	}

	score := CalculateIncidentScore(incidents, now)

	// 2 incidents * 20 = 40 base + 20 recency + 10 severity = 70
	if score < 50 {
		t.Errorf("2 recent high incidents should score high, got %d", score)
	}
}

func TestCalculateIncidentScore_OldIncidents(t *testing.T) {
	now := time.Now()
	recent := []types.Incident{
		{Severity: "medium", Date: now.AddDate(0, 0, -30)},
	}
	old := []types.Incident{
		{Severity: "medium", Date: now.AddDate(0, -6, 0)},
	}

	recentScore := CalculateIncidentScore(recent, now)
	oldScore := CalculateIncidentScore(old, now)

	if oldScore >= recentScore {
		t.Errorf("old incident (%d) should score lower than recent (%d)", oldScore, recentScore)
	}
}

func TestCalculateIncidentScore_CappedAt100(t *testing.T) {
	now := time.Now()
	incidents := make([]types.Incident, 20)
	for i := range incidents {
		incidents[i] = types.Incident{Severity: "critical", Date: now.AddDate(0, 0, -1)}
	}

	score := CalculateIncidentScore(incidents, now)

	if score > 100 {
		t.Errorf("score should cap at 100, got %d", score)
	}
}

func TestCalculateChangeFrequencyScore_Stable(t *testing.T) {
	score := CalculateChangeFrequencyScore(0)

	if score != 0 {
		t.Errorf("stable file (0 commits) should score 0, got %d", score)
	}
}

func TestCalculateChangeFrequencyScore_HighChurn(t *testing.T) {
	score := CalculateChangeFrequencyScore(90) // 1 commit/day

	if score < 40 {
		t.Errorf("high churn (90 commits/90d) should score high, got %d", score)
	}
}

func TestCalculateChangeFrequencyScore_CappedAt100(t *testing.T) {
	score := CalculateChangeFrequencyScore(500) // Extreme churn

	if score > 100 {
		t.Errorf("score should cap at 100, got %d", score)
	}
}

func TestCalculateUserImpactScore(t *testing.T) {
	tests := []struct {
		name        string
		userFacing  bool
		auth        bool
		data        bool
		payments    bool
		minExpected int
	}{
		{"none", false, false, false, false, 0},
		{"user-facing only", true, false, false, false, 40},
		{"auth path", true, true, false, false, 70},
		{"payment flow", true, true, false, true, 100},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score := CalculateUserImpactScore(tt.userFacing, tt.auth, tt.data, tt.payments)
			if score < tt.minExpected {
				t.Errorf("expected at least %d, got %d", tt.minExpected, score)
			}
		})
	}
}

func TestCalculateUserImpactScore_CappedAt100(t *testing.T) {
	score := CalculateUserImpactScore(true, true, true, true) // All flags

	if score > 100 {
		t.Errorf("score should cap at 100, got %d", score)
	}
}

func TestCalculateDataSensitivityScore(t *testing.T) {
	none := CalculateDataSensitivityScore(false, false, false)
	piiOnly := CalculateDataSensitivityScore(true, false, false)
	allSensitive := CalculateDataSensitivityScore(true, true, true)

	if none != 0 {
		t.Errorf("no sensitive data should score 0, got %d", none)
	}
	if piiOnly != 50 {
		t.Errorf("PII only should score 50, got %d", piiOnly)
	}
	if allSensitive > 100 {
		t.Errorf("score should cap at 100, got %d", allSensitive)
	}
}

func TestCalculateTestCoverageScore(t *testing.T) {
	noTests := CalculateTestCoverageScore(0, false)
	halfCoverage := CalculateTestCoverageScore(50, false)
	fullCoverage := CalculateTestCoverageScore(100, false)
	fullWithIntegration := CalculateTestCoverageScore(100, true)

	if noTests != 100 {
		t.Errorf("0%% coverage should score 100, got %d", noTests)
	}
	if halfCoverage != 50 {
		t.Errorf("50%% coverage should score 50, got %d", halfCoverage)
	}
	if fullCoverage != 0 {
		t.Errorf("100%% coverage should score 0, got %d", fullCoverage)
	}
	if fullWithIntegration != 0 {
		t.Errorf("100%% + integration should score 0, got %d", fullWithIntegration)
	}
}

func TestCalculateComplexityScore(t *testing.T) {
	simple := CalculateComplexityScore(1, 1)
	moderate := CalculateComplexityScore(10, 15)
	extreme := CalculateComplexityScore(50, 50)

	if simple >= moderate {
		t.Errorf("simple (%d) should be less than moderate (%d)", simple, moderate)
	}
	if moderate >= extreme {
		t.Errorf("moderate (%d) should be less than extreme (%d)", moderate, extreme)
	}
	if extreme > 100 {
		t.Errorf("extreme should cap at 100, got %d", extreme)
	}
}
