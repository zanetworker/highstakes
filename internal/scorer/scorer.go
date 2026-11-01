package scorer

import (
	"math"
	"sort"
	"time"

	"github.com/zanetworker/highstakes/internal/types"
)

type weightedFactor struct {
	score  float64
	weight float64
}

// CalculateHeatScore is the single score kept for backward compatibility.
// With an LLM assessment it is max(likelihood, impact); without one it is
// the likelihood. Tiers for assessed files come from ApplyQuadrants, not
// from this score.
func CalculateHeatScore(factors types.Factors) int {
	likelihood := CalculateLikelihood(factors)
	if factors.BlastRadius.Assessed && factors.BlastRadius.Score > likelihood {
		return factors.BlastRadius.Score
	}
	return likelihood
}

// CalculateLikelihood scores how likely a file is to need fixes, from
// static and history signals only (the LLM does not change it). Weights
// were chosen on the bench/ calibration benchmark (holdout, 7 repos in 5
// languages): churn and complexity predict later fixes best, centrality
// adds a little, test coverage is a placeholder (weight 0). Churn and
// incidents count only when there is history for them.
func CalculateLikelihood(factors types.Factors) int {
	active := []weightedFactor{
		{factors.DependencyCentrality.Score * 100, 10},
		// Path keywords match on file names alone, so they count once, as
		// the larger of the two, at a low weight. They used to weigh 35.
		{float64(pathKeywords(factors)), 5},
		{float64(factors.Complexity.Score), 30},
	}
	if factors.IncidentHistory.IncidentCount > 0 || factors.IncidentHistory.Score > 0 {
		active = append(active, weightedFactor{float64(factors.IncidentHistory.Score), 25})
	}
	if factors.ChangeFrequency.CommitCount > 0 || factors.ChangeFrequency.Score > 0 {
		active = append(active, weightedFactor{float64(factors.ChangeFrequency.Score), 40})
	}
	return weightedScore(active)
}

// CalculateImpact scores the damage if a file breaks. It is the LLM blast
// radius (max of its four dimensions) when assessed. Otherwise it is an
// estimate from centrality and path keywords, weighted as in likelihood,
// and assessed is false.
func CalculateImpact(factors types.Factors) (score int, assessed bool) {
	if factors.BlastRadius.Assessed {
		return factors.BlastRadius.Score, true
	}
	return weightedScore([]weightedFactor{
		{factors.DependencyCentrality.Score * 100, 10},
		{float64(pathKeywords(factors)), 5},
	}), false
}

func pathKeywords(factors types.Factors) int {
	return max(factors.UserImpact.Score, factors.DataSensitivity.Score)
}

// weightedScore normalizes the weights over the given factors and clamps
// the result to 0-100.
func weightedScore(active []weightedFactor) int {
	totalWeight := 0.0
	for _, f := range active {
		totalWeight += f.weight
	}
	if totalWeight == 0 {
		return 0
	}
	score := 0.0
	for _, f := range active {
		score += f.score * f.weight / totalWeight
	}
	return int(min(max(score, 0), 100))
}

// likelyShare is the share of non-test files counted as "likely".
const likelyShare = 0.2

// LikelihoodCutoff returns the lowest likelihood counted as likely: the
// score of the file at the top-20% rank (at least one file; ties at the
// cutoff count as likely). It is never below 1, so zero-likelihood files
// are never likely, and it is 101 (nothing likely) for an empty input.
func LikelihoodCutoff(likelihoods []int) int {
	if len(likelihoods) == 0 {
		return 101
	}
	sorted := append([]int(nil), likelihoods...)
	sort.Sort(sort.Reverse(sort.IntSlice(sorted)))
	k := max(int(math.Ceil(float64(len(sorted))*likelyShare)), 1)
	return max(sorted[k-1], 1)
}

// QuadrantOf maps the two axes to a quadrant and its tier.
func QuadrantOf(likely, impactful bool) (types.Quadrant, types.Tier) {
	switch {
	case likely && impactful:
		return types.QuadrantFragileCritical, types.TierCritical
	case impactful:
		return types.QuadrantProtect, types.TierHigh
	case likely:
		return types.QuadrantWatch, types.TierMedium
	default:
		return types.QuadrantLow, types.TierLow
	}
}

// ImpactCutoff returns the lowest impact counted as impactful: the impact at
// the repo's top-20% rank (ties inclusive), but never below floor (the High
// tier threshold). The percentile keeps the quadrant meaningful when an LLM
// scores generously and most files clear a fixed bar; the floor keeps small
// or low-stakes repos from calling modest impact "impactful". Empty input
// returns floor.
func ImpactCutoff(impacts []int, floor int) int {
	if len(impacts) == 0 {
		return floor
	}
	return max(LikelihoodCutoff(impacts), floor)
}

// ApplyQuadrants sets quadrant, tier and review requirements for every file
// whose impact was assessed. A file is likely when its likelihood is at or
// above the cutoff computed over non-test files (test files are never
// likely) and impactful when impact is at or above ImpactCutoff over assessed
// non-test files. Unassessed files keep their heat tier. It returns the
// likelihood cutoff and the impact cutoff.
func ApplyQuadrants(files map[string]*types.FileHeat, thresholds types.TierThresholds, isTest func(string) bool) (int, int) {
	var pop, impacts []int
	for p, f := range files {
		if isTest(p) {
			continue
		}
		pop = append(pop, f.Likelihood)
		if f.ImpactAssessed {
			impacts = append(impacts, f.Impact)
		}
	}
	cutoff := LikelihoodCutoff(pop)
	impactCutoff := ImpactCutoff(impacts, thresholds.High)
	for p, f := range files {
		if !f.ImpactAssessed {
			continue
		}
		likely := !isTest(p) && f.Likelihood >= cutoff
		f.Quadrant, f.Tier = QuadrantOf(likely, f.Impact >= impactCutoff)
		f.ReviewRequirements = CalculateReviewRequirements(f.Tier)
	}
	return cutoff, impactCutoff
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
