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
			want: []string{"Impact      90", "Likelihood  10", "protect"},
		},
		{
			name: "unassessed marks impact as an estimate",
			heat: &types.FileHeat{Path: "lib/x.go", Tier: types.TierLow, Impact: 20, Likelihood: 45},
			want: []string{"Impact     ~20 (estimate)", "Likelihood  45"},
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
