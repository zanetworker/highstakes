package scorer

import (
	"time"

	"github.com/zanetworker/highstakes/internal/types"
)

// CalculateHeatScore computes the aggregate heat score from all factors.
// When LLM blast radius is available, it is the primary signal (40% weight).
// Without LLM, it normalizes against available factors.
func CalculateHeatScore(factors types.Factors) int {
	type weightedFactor struct {
		score  float64
		weight float64
	}

	var active []weightedFactor

	if factors.BlastRadius.Assessed {
		// LLM-assisted mode: blast radius is primary signal
		active = append(active, weightedFactor{float64(factors.BlastRadius.Score), 40})
		active = append(active, weightedFactor{factors.DependencyCentrality.Score * 100, 15})
		active = append(active, weightedFactor{float64(factors.Complexity.Score), 15})
		active = append(active, weightedFactor{float64(factors.TestCoverage.Score), 10})

		if factors.IncidentHistory.IncidentCount > 0 || factors.IncidentHistory.Score > 0 {
			active = append(active, weightedFactor{float64(factors.IncidentHistory.Score), 10})
		}
		if factors.ChangeFrequency.CommitCount > 0 || factors.ChangeFrequency.Score > 0 {
			active = append(active, weightedFactor{float64(factors.ChangeFrequency.Score), 10})
		}
	} else {
		// Static-only mode: normalize across available heuristic factors.
		// Weights chosen on the bench/ calibration benchmark (holdout, 7 repos
		// in 5 languages): churn and complexity predict later fixes best,
		// centrality adds a little, test coverage is a placeholder (weight 0).
		active = append(active, weightedFactor{factors.DependencyCentrality.Score * 100, 10})
		// Path keywords (user impact, data sensitivity) are a weak signal that
		// matches on file names alone, so they count once, as the larger of
		// the two, at a low weight. They used to weigh 35 and dominated.
		keywords := factors.UserImpact.Score
		if factors.DataSensitivity.Score > keywords {
			keywords = factors.DataSensitivity.Score
		}
		active = append(active, weightedFactor{float64(keywords), 5})
		active = append(active, weightedFactor{float64(factors.Complexity.Score), 30})

		if factors.IncidentHistory.IncidentCount > 0 || factors.IncidentHistory.Score > 0 {
			active = append(active, weightedFactor{float64(factors.IncidentHistory.Score), 25})
		}
		if factors.ChangeFrequency.CommitCount > 0 || factors.ChangeFrequency.Score > 0 {
			active = append(active, weightedFactor{float64(factors.ChangeFrequency.Score), 40})
		}
	}

	if len(active) == 0 {
		return 0
	}

	totalWeight := 0.0
	for _, f := range active {
		totalWeight += f.weight
	}

	score := 0.0
	for _, f := range active {
		normalizedWeight := f.weight / totalWeight * 100
		score += f.score * normalizedWeight / 100
	}

	if score < 0 {
		score = 0
	}
	if score > 100 {
		score = 100
	}

	return int(score)
}

// CapTestFile keeps test files below the MEDIUM tier: they import everything
// and change often, but a broken test does not reach users. On the bench/
// benchmark, ranking them alongside source made the heat ranking worse.
func CapTestFile(score int, isTest bool, thresholds types.TierThresholds) int {
	if isTest && score >= thresholds.Medium {
		return thresholds.Medium - 1
	}
	return score
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
