package heatmap

import (
	"os"

	"github.com/zanetworker/highstakes/internal/scorer"
	"github.com/zanetworker/highstakes/internal/types"
	"gopkg.in/yaml.v3"
)

// DefaultConfig returns the default configuration
func DefaultConfig() types.Config {
	return types.Config{
		Version: "1.0.0",
		Tiers:   scorer.DefaultTierThresholds(),
		Requirements: map[types.Tier]types.ReviewRequirements{
			types.TierCritical: scorer.CalculateReviewRequirements(types.TierCritical),
			types.TierHigh:     scorer.CalculateReviewRequirements(types.TierHigh),
			types.TierMedium:   scorer.CalculateReviewRequirements(types.TierMedium),
			types.TierLow:      scorer.CalculateReviewRequirements(types.TierLow),
		},
		CircuitBreakers: types.CircuitBreakerConfig{
			MaxDiffLines:        500,
			MaxLanguages:        3,
			MinTestRatio:        0.3,
			RequirePRDescription: true,
		},
		Notifications: types.NotificationConfig{
			Slack: types.SlackConfig{
				Enabled:      false,
				NotifyOnTier: []types.Tier{types.TierCritical, types.TierHigh},
			},
			Email: types.EmailConfig{
				Enabled:     false,
				DailyDigest: true,
			},
		},
		GitHub: types.GitHubConfig{
			PostPRComments:       true,
			UpdateOnPush:         true,
			BlockAutoMergeOnTier: []types.Tier{types.TierCritical, types.TierHigh},
		},
		Exclude: types.ExcludeConfig{
			Dirs: []string{
				"node_modules", "vendor", "__pycache__",
				".venv", "venv", ".cache", "dist", "build",
				".tox", ".mypy_cache", ".pytest_cache",
				"target", ".eggs", "site-packages",
				"third_party", "third-party", "external", "deps",
			},
			Patterns: []string{
				"distribution-*", "env*", "*.egg-info",
			},
		},
	}
}

// LoadConfig loads configuration from a YAML file
func LoadConfig(path string) (types.Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return types.Config{}, err
	}

	var config types.Config
	if err := yaml.Unmarshal(data, &config); err != nil {
		return types.Config{}, err
	}

	return config, nil
}

// SaveConfig saves configuration to a YAML file
func SaveConfig(config types.Config, path string) error {
	data, err := yaml.Marshal(config)
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0644)
}

// LoadIncidents loads incidents from a JSON file
func LoadIncidents(path string) ([]types.Incident, error) {
	// TODO: Implement
	return []types.Incident{}, nil
}

// LoadAnnotations loads annotations from a JSON file
func LoadAnnotations(path string) (map[string]types.Annotation, error) {
	// TODO: Implement
	return map[string]types.Annotation{}, nil
}
