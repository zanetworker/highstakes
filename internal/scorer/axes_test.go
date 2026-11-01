package scorer

import (
	"fmt"
	"strings"
	"testing"

	"github.com/zanetworker/highstakes/internal/types"
)

func TestCalculateLikelihood(t *testing.T) {
	hot := types.Factors{
		ChangeFrequency: types.ChangeFrequency{Score: 80, CommitCount: 10},
		Complexity:      types.Complexity{Score: 60},
	}
	withLLM := hot
	withLLM.BlastRadius = types.BlastRadius{Score: 95, Assessed: true}

	tests := []struct {
		name string
		f    types.Factors
		want int
	}{
		{"all zero", types.Factors{}, 0},
		{"churn and complexity", hot, 58},                        // (80*40 + 60*30) / (40+30+10+5)
		{"blast radius does not change likelihood", withLLM, 58}, // same as without the LLM
		{"churn alone", types.Factors{ChangeFrequency: types.ChangeFrequency{Score: 100, CommitCount: 5}}, 47},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := CalculateLikelihood(tt.f); got != tt.want {
				t.Errorf("CalculateLikelihood = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestCalculateImpact(t *testing.T) {
	tests := []struct {
		name         string
		f            types.Factors
		want         int
		wantAssessed bool
	}{
		{"assessed uses blast radius", types.Factors{BlastRadius: types.BlastRadius{Score: 72, Assessed: true}, DependencyCentrality: types.DependencyCentrality{Score: 1}}, 72, true},
		{"assessed zero stays zero", types.Factors{BlastRadius: types.BlastRadius{Assessed: true}, DependencyCentrality: types.DependencyCentrality{Score: 1}}, 0, true},
		{"estimate from centrality", types.Factors{DependencyCentrality: types.DependencyCentrality{Score: 0.9}}, 60, false}, // 90*10/15
		{"estimate from keywords takes the larger", types.Factors{UserImpact: types.UserImpact{Score: 30}, DataSensitivity: types.DataSensitivity{Score: 90}}, 30, false},
		{"estimate all zero", types.Factors{}, 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, assessed := CalculateImpact(tt.f)
			if got != tt.want || assessed != tt.wantAssessed {
				t.Errorf("CalculateImpact = (%d, %v), want (%d, %v)", got, assessed, tt.want, tt.wantAssessed)
			}
		})
	}
}

func TestCalculateHeatScore_AssessedIsMaxOfAxes(t *testing.T) {
	churny := types.Factors{
		ChangeFrequency: types.ChangeFrequency{Score: 80, CommitCount: 10},
		Complexity:      types.Complexity{Score: 60},
	}
	tests := []struct {
		name  string
		blast int
		want  int
	}{
		{"impact above likelihood", 90, 90},
		{"likelihood above impact", 20, 58},
		{"equal", 58, 58},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := churny
			f.BlastRadius = types.BlastRadius{Score: tt.blast, Assessed: true}
			if got := CalculateHeatScore(f); got != tt.want {
				t.Errorf("heat = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestLikelihoodCutoff(t *testing.T) {
	tests := []struct {
		name   string
		scores []int
		want   int
	}{
		{"empty: nothing is likely", nil, 101},
		{"top 20 percent of ten", []int{10, 20, 30, 40, 50, 60, 70, 80, 90, 100}, 90},
		{"tiny repo keeps at least one", []int{40, 10}, 40},
		{"single file", []int{55}, 55},
		{"ties at the cutoff are inclusive", []int{90, 70, 70, 70, 10, 10, 10, 10, 10, 10}, 70},
		{"zero likelihood is never likely", []int{0, 0, 0}, 1},
		{"rounds up: 6 files keep 2", []int{60, 50, 40, 30, 20, 10}, 50},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := LikelihoodCutoff(tt.scores); got != tt.want {
				t.Errorf("LikelihoodCutoff(%v) = %d, want %d", tt.scores, got, tt.want)
			}
		})
	}
}

func TestImpactCutoff(t *testing.T) {
	tests := []struct {
		name    string
		impacts []int
		floor   int
		want    int
	}{
		{"empty uses the floor", nil, 61, 61},
		{"generous scorer raises the cutoff to the top 20 percent", []int{95, 90, 85, 80, 75, 72, 70, 70, 68, 65}, 61, 90},
		{"modest scorer keeps the floor", []int{80, 70, 50, 40, 30, 20, 10, 10, 10, 10}, 61, 70},
		{"small repo below the floor keeps the floor", []int{40, 30}, 61, 61},
		{"ties at the percentile are inclusive", []int{90, 80, 80, 80, 10, 10, 10, 10, 10, 10}, 61, 80},
		{"percentile exactly at the floor", []int{61, 61, 10, 10, 10}, 61, 61},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ImpactCutoff(tt.impacts, tt.floor); got != tt.want {
				t.Errorf("ImpactCutoff(%v, %d) = %d, want %d", tt.impacts, tt.floor, got, tt.want)
			}
		})
	}
}

func TestQuadrantOf(t *testing.T) {
	tests := []struct {
		likely, impactful bool
		wantQ             types.Quadrant
		wantTier          types.Tier
	}{
		{true, true, types.QuadrantFragileCritical, types.TierCritical},
		{false, true, types.QuadrantProtect, types.TierHigh},
		{true, false, types.QuadrantWatch, types.TierMedium},
		{false, false, types.QuadrantLow, types.TierLow},
	}
	for _, tt := range tests {
		t.Run(string(tt.wantQ), func(t *testing.T) {
			q, tier := QuadrantOf(tt.likely, tt.impactful)
			if q != tt.wantQ || tier != tt.wantTier {
				t.Errorf("QuadrantOf(%v, %v) = (%s, %s), want (%s, %s)", tt.likely, tt.impactful, q, tier, tt.wantQ, tt.wantTier)
			}
		})
	}
}

func TestApplyQuadrants(t *testing.T) {
	th := DefaultTierThresholds()
	file := func(path string, likelihood, impact int, assessed bool, heatTier types.Tier) *types.FileHeat {
		return &types.FileHeat{Path: path, Likelihood: likelihood, Impact: impact, ImpactAssessed: assessed, Tier: heatTier, HeatScore: 50}
	}
	isTest := func(p string) bool { return strings.HasSuffix(p, "_test.go") }

	t.Run("assessed files get quadrant tiers", func(t *testing.T) {
		files := map[string]*types.FileHeat{
			"a.go":      file("a.go", 90, 80, true, types.TierLow),       // likely, impactful
			"b.go":      file("b.go", 10, 61, true, types.TierLow),       // impact at the threshold
			"c.go":      file("c.go", 85, 60, true, types.TierLow),       // likely, just below threshold
			"d.go":      file("d.go", 5, 10, true, types.TierHigh),       // neither
			"e.go":      file("e.go", 20, 0, true, types.TierLow),        // neither
			"f.go":      file("f.go", 30, 20, true, types.TierLow),       // neither
			"g.go":      file("g.go", 40, 20, true, types.TierLow),       // neither
			"h.go":      file("h.go", 50, 20, true, types.TierLow),       // neither
			"i.go":      file("i.go", 60, 20, true, types.TierLow),       // neither
			"j.go":      file("j.go", 70, 20, true, types.TierLow),       // neither
			"x_test.go": file("x_test.go", 100, 30, true, types.TierLow), // test: excluded from the percentile and never likely
		}
		cutoff, impactCutoff := ApplyQuadrants(files, th, isTest)
		if cutoff != 85 {
			t.Fatalf("cutoff = %d, want 85 (top 2 of 10 non-test files)", cutoff)
		}
		if impactCutoff != 61 {
			t.Fatalf("impact cutoff = %d, want the 61 floor (top 20%% of impact is 61)", impactCutoff)
		}
		want := map[string]struct {
			q    types.Quadrant
			tier types.Tier
		}{
			"a.go":      {types.QuadrantFragileCritical, types.TierCritical},
			"b.go":      {types.QuadrantProtect, types.TierHigh},
			"c.go":      {types.QuadrantWatch, types.TierMedium},
			"d.go":      {types.QuadrantLow, types.TierLow},
			"x_test.go": {types.QuadrantLow, types.TierLow},
		}
		for p, w := range want {
			f := files[p]
			if f.Quadrant != w.q || f.Tier != w.tier {
				t.Errorf("%s: quadrant=%s tier=%s, want %s %s", p, f.Quadrant, f.Tier, w.q, w.tier)
			}
			if f.ReviewRequirements != CalculateReviewRequirements(w.tier) {
				t.Errorf("%s: review requirements not recomputed for tier %s", p, w.tier)
			}
		}
	})

	t.Run("generous impact scores raise the impactful cutoff", func(t *testing.T) {
		files := map[string]*types.FileHeat{}
		for i, impact := range []int{95, 90, 85, 80, 75, 72, 70, 70, 68, 65} {
			p := fmt.Sprintf("f%d.go", i)
			files[p] = file(p, 0, impact, true, types.TierLow)
		}
		files["u.go"] = file("u.go", 0, 99, false, types.TierLow)          // unassessed: not in the population
		files["z_test.go"] = file("z_test.go", 0, 99, true, types.TierLow) // test: not in the population
		_, impactCutoff := ApplyQuadrants(files, th, isTest)
		if impactCutoff != 90 {
			t.Fatalf("impact cutoff = %d, want 90", impactCutoff)
		}
		if f := files["f1.go"]; f.Quadrant != types.QuadrantProtect {
			t.Errorf("impact 90: quadrant = %s, want protect", f.Quadrant)
		}
		if f := files["f2.go"]; f.Quadrant != types.QuadrantLow {
			t.Errorf("impact 85 (above 61, below the repo cutoff): quadrant = %s, want low", f.Quadrant)
		}
	})

	t.Run("unassessed files keep the heat tier and no quadrant", func(t *testing.T) {
		files := map[string]*types.FileHeat{
			"a.go": file("a.go", 90, 70, false, types.TierHigh),
			"b.go": file("b.go", 10, 10, true, types.TierHigh),
		}
		ApplyQuadrants(files, th, isTest)
		if f := files["a.go"]; f.Tier != types.TierHigh || f.Quadrant != "" {
			t.Errorf("unassessed: tier=%s quadrant=%q, want high and empty", f.Tier, f.Quadrant)
		}
		if f := files["b.go"]; f.Tier != types.TierLow || f.Quadrant != types.QuadrantLow {
			t.Errorf("assessed: tier=%s quadrant=%q, want low/low", f.Tier, f.Quadrant)
		}
	})

	t.Run("empty map", func(t *testing.T) {
		if got, impact := ApplyQuadrants(map[string]*types.FileHeat{}, th, isTest); got != 101 || impact != th.High {
			t.Errorf("cutoffs = %d/%d, want 101/%d", got, impact, th.High)
		}
	})
}
