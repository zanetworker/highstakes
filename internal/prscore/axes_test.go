package prscore

import (
	"strings"
	"testing"

	"github.com/zanetworker/highstakes/internal/ownership"
	"github.com/zanetworker/highstakes/internal/scorer"
	"github.com/zanetworker/highstakes/internal/types"
)

func TestChangeRisk(t *testing.T) {
	tests := []struct {
		name       string
		changes    []FileChange
		wantScore  int
		wantFactor string // substring expected in some factor; "" = no factors
	}{
		{"docs only is zero", []FileChange{{Path: "README.md", LinesAdded: 900}}, 0, ""},
		{"no changes is zero", nil, 0, ""},
		{
			"small change with matching tests is near zero",
			[]FileChange{{Path: "src/api/handler.go", LinesAdded: 10}, {Path: "src/api/handler_test.go", LinesAdded: 10}},
			1, "",
		},
		{
			"size caps at 30",
			[]FileChange{{Path: "src/api/handler.go", LinesAdded: 900, LinesDeleted: 100}, {Path: "src/api/handler_test.go", LinesAdded: 500}},
			30, "lines changed",
		},
		{
			"new files add 5 each, capped at 15",
			[]FileChange{
				{Path: "a.go", IsNew: true}, {Path: "b.go", IsNew: true}, {Path: "c.go", IsNew: true}, {Path: "d.go", IsNew: true},
			},
			15, "new file",
		},
		{
			"dependency manifest is infra, not docs",
			[]FileChange{{Path: "requirements.txt", LinesAdded: 1}},
			20, "config/infra",
		},
		{
			"infra file adds 20",
			[]FileChange{{Path: ".github/workflows/ci.yml", LinesAdded: 1}},
			20, "config/infra",
		},
		{
			"deletions in hot file add up to 20",
			[]FileChange{{Path: "src/auth/jwt.go", LinesDeleted: 50}, {Path: "src/auth/jwt_test.go", LinesAdded: 1}},
			13, "deleted from high-stakes",
		},
		{
			"untested code adds 15",
			[]FileChange{{Path: "src/api/handler.go", LinesAdded: 50}},
			18, "test ratio",
		},
		{
			"everything at once caps at 100",
			[]FileChange{
				{Path: "src/auth/jwt.go", LinesAdded: 2000, LinesDeleted: 500},
				{Path: "x.go", IsNew: true}, {Path: "y.go", IsNew: true}, {Path: "z.go", IsNew: true},
				{Path: "Dockerfile", LinesAdded: 3},
			},
			100, "config/infra",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score, factors := ChangeRisk(tt.changes, makeHeatmap())
			if score != tt.wantScore {
				t.Errorf("score = %d, want %d (factors %v)", score, tt.wantScore, factors)
			}
			if tt.wantFactor == "" {
				if len(factors) != 0 {
					t.Errorf("want no factors, got %v", factors)
				}
				return
			}
			if !strings.Contains(strings.Join(factors, "|"), tt.wantFactor) {
				t.Errorf("factors %v missing %q", factors, tt.wantFactor)
			}
		})
	}
}

func TestChangeRiskLevel(t *testing.T) {
	tests := []struct {
		score int
		want  string
	}{{0, "low"}, {29, "low"}, {30, "medium"}, {59, "medium"}, {60, "high"}, {100, "high"}}
	for _, tt := range tests {
		if got := ChangeRiskLevel(tt.score); got != tt.want {
			t.Errorf("ChangeRiskLevel(%d) = %s, want %s", tt.score, got, tt.want)
		}
	}
}

func TestIsInfraFile(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{".github/workflows/ci.yml", true},
		{"Dockerfile", true},
		{"deploy/Dockerfile.prod", true},
		{"Containerfile", true},
		{"charts/app/values.yaml", true},
		{"infra/main.tf", true},
		{"pyproject.toml", true},
		{"go.mod", true},
		{"package.json", true},
		{"requirements.txt", true},
		{"requirements-dev.txt", true},
		{"rust/Cargo.toml", true},
		{"Cargo.lock", true},
		{"Gemfile", true},
		{"Gemfile.lock", true},
		{"pom.xml", true},
		{"build.gradle", true},
		{"build.gradle.kts", true},
		{"package-lock.json", true},
		{"yarn.lock", true},
		{"poetry.lock", true},
		{"go.sum", false},
		{"notes.txt", false},
		{"testdata/fixture.yaml", false},
		{"src/main.go", false},
		{"data/sample.json", false},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			if got := isInfraFile(tt.path); got != tt.want {
				t.Errorf("isInfraFile(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

func TestCalculatePRRisk_TwoAxes(t *testing.T) {
	risk := CalculatePRRisk(1, []FileChange{{Path: "src/auth/jwt.go", LinesAdded: 1}, {Path: "src/auth/jwt_test.go", LinesAdded: 1}},
		makeHeatmap(), scorer.DefaultTierThresholds())
	if risk.Importance != 95 || risk.HeatScore != 95 {
		t.Errorf("importance/heat = %d/%d, want 95/95", risk.Importance, risk.HeatScore)
	}
	if risk.ChangeRiskLevel != "low" {
		t.Errorf("tiny tested change to critical file: change risk level = %s (%d), want low", risk.ChangeRiskLevel, risk.ChangeRisk)
	}
}

func TestSuggestReviewers(t *testing.T) {
	risk := types.PRRisk{FilesChanged: []types.FileChange{
		{Path: "src/auth/jwt.go", HeatScore: 95},
		{Path: "src/utils/format.go", HeatScore: 5},
		{Path: "README.md", HeatScore: 0},
	}}
	owners := map[string][]ownership.Owner{
		"src/auth/jwt.go":     {{Handle: "@alice", Email: "alice@x.com", Share: 0.7}, {Handle: "@bob", Email: "bob@x.com", Share: 0.3}},
		"src/utils/format.go": {{Handle: "@carol", Email: "carol@x.com", Share: 1}},
		"README.md":           {{Handle: "@dave", Email: "dave@x.com", Share: 1}},
	}

	tests := []struct {
		name   string
		author string
		n      int
		want   []string
	}{
		{"ranked by share times heat, docs ignored", "", 5, []string{"@alice", "@bob", "@carol"}},
		{"author excluded by email", "Alice@x.com", 5, []string{"@bob", "@carol"}},
		{"author excluded by handle without @", "alice", 5, []string{"@bob", "@carol"}},
		{"limit applies", "", 1, []string{"@alice"}},
		{"zero limit gives none", "", 0, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SuggestReviewers(risk, owners, tt.author, tt.n)
			if len(got) != len(tt.want) {
				t.Fatalf("got %+v, want %v", got, tt.want)
			}
			for i, r := range got {
				if r.Handle != tt.want[i] {
					t.Errorf("reviewer %d = %s, want %s", i, r.Handle, tt.want[i])
				}
			}
		})
	}

	t.Run("reviewer lists the files they own", func(t *testing.T) {
		got := SuggestReviewers(risk, owners, "", 1)
		if len(got[0].Files) != 1 || got[0].Files[0] != "src/auth/jwt.go" {
			t.Errorf("files = %v", got[0].Files)
		}
	})

	t.Run("no owners gives empty", func(t *testing.T) {
		if got := SuggestReviewers(risk, nil, "", 3); len(got) != 0 {
			t.Errorf("got %v", got)
		}
	})
}

func TestDecide(t *testing.T) {
	tests := []struct {
		tier   types.Tier
		failOn types.Tier
		want   string
	}{
		{types.TierCritical, types.TierCritical, DecisionBlock},
		{types.TierHigh, types.TierCritical, DecisionPass},
		{types.TierHigh, types.TierHigh, DecisionBlock},
		{types.TierLow, types.TierLow, DecisionBlock},
		{types.TierCritical, "", DecisionPass},
	}
	for _, tt := range tests {
		t.Run(string(tt.tier)+"/"+string(tt.failOn), func(t *testing.T) {
			if got := Decide(tt.tier, tt.failOn); got != tt.want {
				t.Errorf("Decide(%s, %s) = %s, want %s", tt.tier, tt.failOn, got, tt.want)
			}
		})
	}
}

func TestCalculatePRRisk_ImportanceFromImpact(t *testing.T) {
	hm := &types.Heatmap{Files: map[string]*types.FileHeat{
		// stable but impactful: protect quadrant
		"core/auth.go": {Path: "core/auth.go", HeatScore: 70, Likelihood: 10, Impact: 70, ImpactAssessed: true, Tier: types.TierHigh},
		// churny, low impact: watch quadrant, heat above the other file's impact
		"ui/view.go": {Path: "ui/view.go", HeatScore: 80, Likelihood: 80, Impact: 20, ImpactAssessed: true, Tier: types.TierMedium},
		// fragile-critical: quadrant tier is above its heat tier
		"pay/charge.go": {Path: "pay/charge.go", HeatScore: 65, Likelihood: 50, Impact: 65, ImpactAssessed: true, Tier: types.TierCritical},
		// no LLM: impact is an estimate, importance falls back to heat
		"lib/util.go": {Path: "lib/util.go", HeatScore: 40, Likelihood: 40, Impact: 90, Tier: types.TierMedium},
	}}
	tests := []struct {
		name           string
		paths          []string
		wantImportance int
		wantTier       types.Tier
	}{
		{"impact, not heat, of assessed files", []string{"core/auth.go", "ui/view.go"}, 70, types.TierHigh},
		{"churny low-impact file alone", []string{"ui/view.go"}, 20, types.TierMedium},
		{"PR tier is the most severe file tier", []string{"pay/charge.go"}, 65, types.TierCritical},
		{"unassessed falls back to heat", []string{"lib/util.go"}, 40, types.TierMedium},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var changes []FileChange
			for _, p := range tt.paths {
				changes = append(changes, FileChange{Path: p, LinesAdded: 1})
			}
			risk := CalculatePRRisk(0, changes, hm, scorer.DefaultTierThresholds())
			if risk.Importance != tt.wantImportance || risk.Tier != tt.wantTier {
				t.Errorf("importance=%d tier=%s, want %d %s", risk.Importance, risk.Tier, tt.wantImportance, tt.wantTier)
			}
		})
	}
}
