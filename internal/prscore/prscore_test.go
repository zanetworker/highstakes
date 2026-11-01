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
		{"dependency manifest is not an unscored new file", "src/auth/requirements.txt", 0, types.TierLow, false, false},
		{"go.mod is not an unscored new file", "src/auth/go.mod", 0, types.TierLow, false, false},
		{"lock file is not scored", "src/auth/Cargo.lock", 0, types.TierLow, false, false},
		{"go.sum is not scored", "go.sum", 0, types.TierLow, false, false},
		{"image is not scored", "src/auth/logo.png", 0, types.TierLow, false, false},
		{"font is not scored", "src/auth/inter.woff2", 0, types.TierLow, false, false},
		{"video is not scored", "src/auth/demo.mp4", 0, types.TierLow, false, false},
		{"wasm module inherits directory risk", "src/auth/tool.wasm", 95, types.TierCritical, true, true},
		{"jar inherits directory risk", "src/auth/lib.jar", 95, types.TierCritical, true, true},
		{"shared library inherits directory risk", "src/auth/libx.so", 95, types.TierCritical, true, true},
		{"dylib inherits directory risk", "src/auth/libx.dylib", 95, types.TierCritical, true, true},
		{"windows binary inherits directory risk", "src/auth/tool.exe", 95, types.TierCritical, true, true},
		{"raw binary inherits directory risk", "src/auth/fw.bin", 95, types.TierCritical, true, true},
		{"archive inherits directory risk", "src/auth/bundle.tgz", 95, types.TierCritical, true, true},
		{"compiled python inherits directory risk", "src/auth/x.pyc", 95, types.TierCritical, true, true},
		{"java class inherits directory risk", "src/auth/X.class", 95, types.TierCritical, true, true},
		{"extensionless file under bin is unscored", "bin/tool", 0, types.TierLow, true, true},
		{"golden file is not scored", "src/auth/want.golden", 0, types.TierLow, false, false},
		{"testdata data file is not scored", "src/auth/testdata/input.json", 0, types.TierLow, false, false},
		{"testdata source file inherits risk", "src/auth/testdata/input.go", 95, types.TierCritical, true, true},
		{"tool config dot directory is not scored", ".impeccable/config.json", 0, types.TierLow, false, false},
		{"tool config dotfile is not scored", ".golangci.yml", 0, types.TierLow, false, false},
		{"ignore dotfile is not scored", ".gitignore", 0, types.TierLow, false, false},
		{"devcontainer json is tool config", ".devcontainer/devcontainer.json", 0, types.TierLow, false, false},
		{"git hook script in dot dir is flagged", ".husky/pre-commit", 0, types.TierLow, true, true},
		{"shell script in dot dir is flagged", ".devcontainer/setup.sh", 0, types.TierLow, true, true},
		{"js config in dot dir is flagged", ".storybook/main.js", 0, types.TierLow, true, true},
		{"python script in dot dir is flagged", ".tools/gen.py", 0, types.TierLow, true, true},
		{"executable dotfile is flagged", ".envrc", 0, types.TierLow, true, true},
		{"CI workflow is still flagged", ".github/workflows/ci.yml", 0, types.TierLow, true, true},
		{"new code file next to a dotfile dir still inherits", "src/auth/.hidden/x.go", 95, types.TierCritical, true, true},
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

func TestCalculatePRRisk_ManifestStillCountsAsConfig(t *testing.T) {
	risk := CalculatePRRisk(1, []FileChange{{Path: "go.mod", LinesAdded: 2, IsNew: true}}, makeHeatmap(), scorer.DefaultTierThresholds())
	if !strings.Contains(strings.Join(risk.ChangeRiskFactors, ";"), "config/infra file(s): go.mod") {
		t.Errorf("go.mod should still be a config change, factors: %v", risk.ChangeRiskFactors)
	}
	if risk.FilesChanged[0].Unscored {
		t.Error("go.mod should not be an unscored new file")
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

func TestApplyAutonomy(t *testing.T) {
	hm := &types.Heatmap{Files: map[string]*types.FileHeat{
		"ui/a.go":   {Path: "ui/a.go", Tier: types.TierLow, ImpactAssessed: true, Quadrant: types.QuadrantLow},
		"core/p.go": {Path: "core/p.go", Tier: types.TierHigh, ImpactAssessed: true, Quadrant: types.QuadrantProtect},
		"est/e.go":  {Path: "est/e.go", Tier: types.TierLow},
	}}
	th := scorer.DefaultTierThresholds()
	tests := []struct {
		name    string
		paths   []string
		max     int
		level   int
		allowed bool
		reason  string
	}{
		{"every file routine", []string{"ui/a.go"}, 3, 3, true, "every touched file is L3"},
		{"markdown docs ride along", []string{"ui/a.go", "README.md", "docs/guide.rst"}, 3, 3, true, "every touched file is L3"},
		{"an image is not assessed", []string{"ui/a.go", "README.md", "ui/logo.png"}, 3, 1, false, "touches files HighStakes has not assessed"},
		{"new binary artifact is unknown code", []string{"ui/a.go", "ui/plugin.wasm"}, 3, 1, false, "touches files whose impact is not AI-assessed"},
		{"new hook script is unknown code", []string{"ui/a.go", ".husky/pre-commit"}, 3, 1, false, "touches files whose impact is not AI-assessed"},
		{"protect file", []string{"ui/a.go", "core/p.go"}, 3, 1, false, "touches Protect files"},
		{"unassessed file", []string{"ui/a.go", "est/e.go"}, 3, 1, false, "touches files whose impact is not AI-assessed"},
		{"new unscored file", []string{"ui/a.go", "ui/new.go"}, 3, 1, false, "touches files whose impact is not AI-assessed"},
		{"dependency manifest", []string{"ui/a.go", "go.mod"}, 3, 1, false, "touches dependency or tool config files"},
		{"config cap", []string{"ui/a.go"}, 2, 2, false, "autonomy.max_level is 2"},
		{"empty PR", nil, 3, 3, true, "every touched file is L3"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var changes []FileChange
			for _, p := range tt.paths {
				changes = append(changes, FileChange{Path: p, LinesAdded: 1, IsNew: hm.Files[p] == nil})
			}
			risk := CalculatePRRisk(0, changes, hm, th)
			ApplyAutonomy(&risk, hm, tt.max)
			if risk.AutonomyLevel != tt.level || risk.AutoMergeAllowed != tt.allowed || risk.AutonomyReason != tt.reason {
				t.Errorf("got L%d allowed=%v %q, want L%d allowed=%v %q",
					risk.AutonomyLevel, risk.AutoMergeAllowed, risk.AutonomyReason, tt.level, tt.allowed, tt.reason)
			}
			for _, fc := range risk.FilesChanged {
				if fc.Autonomy == nil || fc.Autonomy.Level < tt.level || fc.Autonomy.Label == "" {
					t.Errorf("%s autonomy = %+v, want a labelled level >= L%d", fc.Path, fc.Autonomy, tt.level)
				}
			}
		})
	}
}

func TestApplyAutonomy_UnscoredNonCodeCapsAtL1(t *testing.T) {
	tests := []struct {
		path string
		want int
	}{
		{"web/static/logo.svg", 1},           // SVG can carry scripts
		{"pkg/x/testdata/run.sh", 1},         // test harnesses execute fixtures
		{"pkg/x/testdata/case.golden", 1},    // unknown content is not proven inert
		{"assets/banner.png", 1},             // never assessed: propose only
		{"docs/guide.mdx", 1},                // MDX can contain code
		{"docs/guide.md", types.MaxAutonomy}, // plain-text docs cannot execute
		{"CHANGELOG.rst", types.MaxAutonomy},
		{"notes/todo.txt", 1},                // .txt can be a build input
		{"CMakeLists.txt", 1},                // executes commands at configure time

	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			risk := &types.PRRisk{FilesChanged: []types.FileChange{{Path: tt.path, Tier: types.TierLow}}}
			ApplyAutonomy(risk, &types.Heatmap{Files: map[string]*types.FileHeat{}}, types.MaxAutonomy)
			if got := risk.FilesChanged[0].Autonomy.Level; got != tt.want {
				t.Errorf("autonomy L%d, want L%d (%s)", got, tt.want, risk.FilesChanged[0].Autonomy.Reason)
			}
			if tt.want < types.MaxAutonomy && risk.AutoMergeAllowed {
				t.Errorf("auto-merge allowed for unscored %s", tt.path)
			}
		})
	}
}

func TestIsInert_ActiveContent(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"ui/logo.png", true},
		{"ui/logo.svg", false}, // SVG can carry scripts
		{"pkg/x/testdata/case.golden", true},
		{"pkg/x/testdata/input.json", true},
		{"pkg/x/testdata/run.sh", false},   // harnesses execute fixtures
		{"pkg/x/testdata/main.go", false},  // compiled and run by tests
		{"testdata/fixture.py", false},
		{"pkg/x/testdata/plugin.so", false},    // unrecognised: not proven inert
		{"pkg/x/testdata/app.jar", false},
		{"pkg/x/testdata/component.tsx", false},
		{"pkg/x/testdata/table.csv", true},
		{"pkg/x/testdata/expected.yaml", true},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			if got := isInert(tt.path); got != tt.want {
				t.Errorf("isInert(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}
