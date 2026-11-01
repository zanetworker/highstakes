package types

import "fmt"

// Autonomy is how far AI agents may act on a file without a human: Level 3
// auto-merge, 2 auto-fix with a human merging, 1 propose only, 0 assist
// only. "How bad?" is AI-assessed and not human-validated, so levels are
// deliberately conservative and teams can cap them (autonomy.max_level).
type Autonomy struct {
	Level  int    `json:"level"`
	Label  string `json:"label"`
	Reason string `json:"reason"`
}

// MaxAutonomy is the highest autonomy level.
const MaxAutonomy = 3

// AutonomyLevel describes one level for agents reading a policy.
type AutonomyLevel struct {
	Level   int    `json:"level" yaml:"level"`
	Label   string `json:"label" yaml:"label"`
	Meaning string `json:"meaning" yaml:"meaning"`
}

// AutonomyLevels lists the levels, highest first.
var AutonomyLevels = []AutonomyLevel{
	{3, "auto-merge", "Agents may fix, open a PR and merge once CI and AI review pass."},
	{2, "auto-fix, human merges", "Agents may fix and open a PR; a human merges."},
	{1, "propose only", "Agents may open draft PRs or suggestions; an owner merges."},
	{0, "assist only", "Agents may write tests and explanations; changes need a named senior owner."},
}

// AutonomyLabel is the label of a level, or "" outside 0-3.
func AutonomyLabel(level int) string {
	for _, l := range AutonomyLevels {
		if l.Level == level {
			return l.Label
		}
	}
	return ""
}

var quadrantAutonomy = map[Quadrant]struct {
	level  int
	reason string
}{
	QuadrantLow:             {3, "Routine: neither likely to break nor costly"},
	QuadrantWatch:           {2, "Stabilize: likely to break, with limited damage"},
	QuadrantProtect:         {1, "Protect: costly if it breaks"},
	QuadrantFragileCritical: {0, "Harden now: likely to break and costly if it does"},
}

// Autonomy is the quadrant's autonomy before caps. No quadrant means the
// impact is unknown: propose only.
func (q Quadrant) Autonomy() Autonomy {
	a, ok := quadrantAutonomy[q]
	if !ok {
		a.level, a.reason = 1, "impact unknown"
	}
	return Autonomy{Level: a.level, Label: AutonomyLabel(a.level), Reason: a.reason}
}

// AutonomyConfig is the autonomy section of .heatmap/config.yaml.
type AutonomyConfig struct {
	// MaxLevel caps every file's level (0-3); unset means 3.
	MaxLevel *int `yaml:"max_level,omitempty"`
}

// Max is the configured cap, 3 when unset.
func (c AutonomyConfig) Max() (int, error) {
	if c.MaxLevel == nil {
		return MaxAutonomy, nil
	}
	if *c.MaxLevel < 0 || *c.MaxLevel > MaxAutonomy {
		return 0, fmt.Errorf("autonomy.max_level must be 0-3, got %d. Valid values: 0, 1, 2, 3", *c.MaxLevel)
	}
	return *c.MaxLevel, nil
}
