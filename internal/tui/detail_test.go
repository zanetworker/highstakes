package tui

import (
	"strings"
	"testing"

	"github.com/zanetworker/highstakes/internal/types"
)

func TestRenderFileDetail_ShowsAxes(t *testing.T) {
	tests := []struct {
		name string
		heat *types.FileHeat
		want []string
		not  []string
	}{
		{
			name: "assessed shows impact, likelihood and quadrant",
			heat: &types.FileHeat{Path: "core/auth.go", Tier: types.TierHigh, Impact: 90, Likelihood: 10, ImpactAssessed: true, Quadrant: types.QuadrantProtect},
			want: []string{"How bad if it breaks?  90", "Will it break?         10", "PROTECT"},
			not:  []string{"Likelihood", "Impact "},
		},
		{
			name: "unassessed marks impact as an estimate",
			heat: &types.FileHeat{Path: "lib/x.go", Tier: types.TierLow, Impact: 20, Likelihood: 45},
			want: []string{"How bad if it breaks? ~20 (estimate)", "Will it break?         45"},
			not:  []string{"QUADRANT"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := Model{}.renderFileDetail(tt.heat, 60)
			for _, w := range tt.want {
				if !strings.Contains(out, w) {
					t.Errorf("missing %q in:\n%s", w, out)
				}
			}
			for _, n := range tt.not {
				if strings.Contains(out, n) {
					t.Errorf("unexpected %q in:\n%s", n, out)
				}
			}
		})
	}
}

func TestRenderFileDetail_ShowsExplanation(t *testing.T) {
	heat := &types.FileHeat{Path: "a.go", ImpactAssessed: true, Explanation: &types.Explanation{
		ImpactHigh: true, Likely: true,
		ImpactReasons:     []string{"security 92"},
		LikelihoodReasons: []string{"11 files import it (top 1% of this repo)"},
		Actions: []types.Action{
			{ID: "senior-review", Text: "Get sign-off from a senior owner", Scope: types.ScopeBeforeMerge},
			{ID: "codeowners", Text: "Add a CODEOWNERS line", Scope: types.ScopeBacklog, Command: "/a.go @ana"},
		},
	}}
	out := Model{}.renderFileDetail(heat, 80)
	for _, w := range []string{"BREAK COST (HIGH)", "security 92", "BREAK SIGNALS (HIGH)", "11 files import it",
		"DO BEFORE THE NEXT MERGE", "[ ] Get sign-off from a senior owner", "PUT ON THE BACKLOG", "[ ] Add a CODEOWNERS line", "/a.go @ana"} {
		if !strings.Contains(out, w) {
			t.Errorf("missing %q in:\n%s", w, out)
		}
	}
	plain := Model{}.renderFileDetail(&types.FileHeat{Path: "b.go"}, 80)
	if strings.Contains(plain, "BEFORE THE NEXT MERGE") {
		t.Error("file without explanation shows an actions section")
	}
}
