package scorer

import (
	"time"

	"github.com/zanetworker/code-heatmap/internal/types"
)

// CalculateHeatScore computes the aggregate heat score from all factors
func CalculateHeatScore(factors types.Factors) int {
	// Weights (must sum to 100 before test coverage subtraction)
	const (
		dependencyWeight  = 20
		incidentWeight    = 25
		changeWeight      = 10
		userImpactWeight  = 20
		sensitivityWeight = 15
		complexityWeight  = 10
		coverageWeight    = 10 // Subtracted
	)

	score := 0.0

	// Add weighted factors
	score += factors.DependencyCentrality.Score * dependencyWeight
	score += float64(factors.IncidentHistory.Score) * incidentWeight
	score += float64(factors.ChangeFrequency.Score) * changeWeight
	score += float64(factors.UserImpact.Score) * userImpactWeight
	score += float64(factors.DataSensitivity.Score) * sensitivityWeight
	score += float64(factors.Complexity.Score) * complexityWeight

	// Subtract test coverage (higher coverage = lower risk)
	score -= float64(factors.TestCoverage.Score) * coverageWeight

	// Clamp to [0, 100]
	if score < 0 {
		score = 0
	}
	if score > 100 {
		score = 100
	}

	return int(score)
}

// CalculateTier determines tier from heat score
func CalculateTier(score int, thresholds types.TierThresholds) types.Tier {
	switch {
	case score >= thresholds.Critical:
		return types.TierCritical
	case score >= thresholds.High:
		return types.TierHigh
	case score >= thresholds.Medium:
		return types.TierMedium
	default:
		return types.TierLow
	}
}

// DefaultTierThresholds returns the default tier boundaries
func DefaultTierThresholds() types.TierThresholds {
	return types.TierThresholds{
		Critical: 86,
		High:     61,
		Medium:   31,
		Low:      0,
	}
}

// CalculateReviewRequirements determines review depth based on tier
func CalculateReviewRequirements(tier types.Tier) types.ReviewRequirements {
	switch tier {
	case types.TierCritical:
		return types.ReviewRequirements{
			MinReviewers:               2,
			RequiresSenior:             true,
			RequiresSecurityScan:       true,
			RequiresIntegrationTests:   true,
			AutoMerge:                  false,
			EstimatedReviewTimeMinutes: 60,
		}
	case types.TierHigh:
		return types.ReviewRequirements{
			MinReviewers:               2,
			RequiresSenior:             false,
			RequiresSecurityScan:       false,
			RequiresIntegrationTests:   true,
			AutoMerge:                  false,
			EstimatedReviewTimeMinutes: 45,
		}
	case types.TierMedium:
		return types.ReviewRequirements{
			MinReviewers:               1,
			RequiresSenior:             false,
			RequiresSecurityScan:       false,
			RequiresIntegrationTests:   false,
			AutoMerge:                  false,
			EstimatedReviewTimeMinutes: 20,
		}
	default: // Low
		return types.ReviewRequirements{
			MinReviewers:               0,
			RequiresSenior:             false,
			RequiresSecurityScan:       false,
			RequiresIntegrationTests:   false,
			AutoMerge:                  true,
			EstimatedReviewTimeMinutes: 5,
		}
	}
}

// CalculateIncidentScore computes score from incident history
func CalculateIncidentScore(incidents []types.Incident, now time.Time) int {
	if len(incidents) == 0 {
		return 0
	}

	// Base score from count (cap at 80)
	base := len(incidents) * 20
	if base > 80 {
		base = 80
	}

	// Recency boost (20 points if incident within 90 days)
	recencyBoost := 0
	for _, inc := range incidents {
		if now.Sub(inc.Date).Hours() < 90*24 {
			recencyBoost = 20
			break
		}
	}

	// Severity boost
	severityBoost := 0
	for _, inc := range incidents {
		switch inc.Severity {
		case "critical":
			severityBoost += 10
		case "high":
			severityBoost += 5
		case "medium":
			severityBoost += 2
		}
	}

	score := base + recencyBoost + severityBoost
	if score > 100 {
		score = 100
	}

	return score
}

// CalculateChangeFrequencyScore computes score from git history
func CalculateChangeFrequencyScore(commitsLast90d int) int {
	if commitsLast90d == 0 {
		return 0 // Stable = low risk
	}

	// Commits per day
	churnRate := float64(commitsLast90d) / 90.0

	// Scale: 1 commit/day = 50 points
	score := int(churnRate * 50)

	if score > 100 {
		score = 100
	}

	return score
}

// CalculateUserImpactScore computes score from user-facing flags
func CalculateUserImpactScore(userFacing, affectsAuth, affectsData, affectsPayments bool) int {
	score := 0

	if userFacing {
		score += 40
	}
	if affectsAuth {
		score += 30
	}
	if affectsData {
		score += 20
	}
	if affectsPayments {
		score += 30
	}

	if score > 100 {
		score = 100
	}

	return score
}

// CalculateDataSensitivityScore computes score from data handling flags
func CalculateDataSensitivityScore(handlesPII, handlesSecrets, handlesFinancial bool) int {
	score := 0

	if handlesPII {
		score += 50
	}
	if handlesSecrets {
		score += 40
	}
	if handlesFinancial {
		score += 50
	}

	if score > 100 {
		score = 100
	}

	return score
}

// CalculateTestCoverageScore computes score from coverage
// Higher coverage = LOWER score (this gets subtracted from heat)
func CalculateTestCoverageScore(coveragePercent float64, hasIntegrationTests bool) int {
	score := int(100 - coveragePercent)

	if hasIntegrationTests {
		score -= 20
	}

	if score < 0 {
		score = 0
	}

	return score
}

// CalculateComplexityScore computes score from cyclomatic/cognitive complexity
func CalculateComplexityScore(cyclomatic, cognitive int) int {
	// Normalize cyclomatic (cap at 20)
	normCyclomatic := float64(cyclomatic) / 20.0
	if normCyclomatic > 1.0 {
		normCyclomatic = 1.0
	}

	// Normalize cognitive (cap at 30)
	normCognitive := float64(cognitive) / 30.0
	if normCognitive > 1.0 {
		normCognitive = 1.0
	}

	score := int((normCyclomatic * 50) + (normCognitive * 50))

	return score
}
