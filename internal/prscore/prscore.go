package prscore

import (
	"path/filepath"
	"strings"

	"github.com/zanetworker/code-heatmap/internal/scorer"
	"github.com/zanetworker/code-heatmap/internal/types"
)

// CalculatePRRisk assesses the risk of a set of changed files
func CalculatePRRisk(prNumber int, changes []FileChange, hm *types.Heatmap, thresholds types.TierThresholds) types.PRRisk {
	risk := types.PRRisk{
		PRNumber:     prNumber,
		FilesChanged: make([]types.FileChange, 0, len(changes)),
	}

	maxHeat := 0

	for _, change := range changes {
		heat, ok := hm.Files[change.Path]

		fc := types.FileChange{
			Path:         change.Path,
			LinesAdded:   change.LinesAdded,
			LinesDeleted: change.LinesDeleted,
			IsNew:        change.IsNew,
		}

		if ok {
			fc.HeatScore = heat.HeatScore
			fc.Tier = heat.Tier
		} else {
			fc.HeatScore = 0
			fc.Tier = types.TierLow
		}

		risk.FilesChanged = append(risk.FilesChanged, fc)

		if fc.HeatScore > maxHeat {
			maxHeat = fc.HeatScore
		}
	}

	risk.HeatScore = maxHeat
	risk.Tier = scorer.CalculateTier(maxHeat, thresholds)
	risk.ReviewRequirements = scorer.CalculateReviewRequirements(risk.Tier)
	risk.CircuitBreakerSignals = checkCircuitBreakers(changes, hm)

	return risk
}

// FileChange represents a file diff in a PR
type FileChange struct {
	Path         string
	LinesAdded   int
	LinesDeleted int
	IsNew        bool
}

// checkCircuitBreakers returns warning signals for the PR
func checkCircuitBreakers(changes []FileChange, hm *types.Heatmap) []string {
	var signals []string

	// Large diff
	totalLines := 0
	for _, c := range changes {
		totalLines += c.LinesAdded + c.LinesDeleted
	}
	if totalLines > 500 {
		signals = append(signals, "Large diff (>500 lines)")
	}

	// Many languages
	langs := map[string]bool{}
	for _, c := range changes {
		if heat, ok := hm.Files[c.Path]; ok && heat.Language != "" {
			langs[heat.Language] = true
		}
	}
	if len(langs) > 3 {
		signals = append(signals, "Many languages (>3)")
	}

	// Low test ratio
	testLines := 0
	codeLines := 0
	for _, c := range changes {
		if isTestFile(c.Path) {
			testLines += c.LinesAdded
		} else {
			codeLines += c.LinesAdded
		}
	}
	if codeLines > 0 {
		ratio := float64(testLines) / float64(codeLines)
		if ratio < 0.3 {
			signals = append(signals, "Low test ratio (<0.3)")
		}
	}

	return signals
}

func isTestFile(path string) bool {
	base := filepath.Base(path)

	suffixes := []string{"_test.go", ".test.ts", ".test.js", ".spec.ts", ".spec.js", "_test.py"}
	for _, s := range suffixes {
		if strings.HasSuffix(base, s) {
			return true
		}
	}

	prefixes := []string{"test_"}
	for _, p := range prefixes {
		if strings.HasPrefix(base, p) {
			return true
		}
	}

	return false
}
