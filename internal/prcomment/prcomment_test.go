package prcomment

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zanetworker/highstakes/internal/types"
)

var update = flag.Bool("update", false, "rewrite golden files")

func golden(t *testing.T, name, got string) {
	t.Helper()
	p := filepath.Join("testdata", name+".golden")
	if *update {
		if err := os.WriteFile(p, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("%v (run go test -update to create it)", err)
	}
	if got != string(want) {
		t.Errorf("%s mismatch (run go test ./internal/prcomment -update after checking)\n--- got\n%s\n--- want\n%s", name, got, want)
	}
}

func hardenPR() (*types.PRRisk, *types.Heatmap) {
	hm := &types.Heatmap{Files: map[string]*types.FileHeat{
		"net/proxy.rs": {Path: "net/proxy.rs", Explanation: &types.Explanation{
			ImpactReasons:     []string{"security 92 · availability 85", `"Core egress proxy."`},
			LikelihoodReasons: []string{"55 commits in the last 90 days (top 1% of this repo)", "11 files import it (top 1%)"},
		}, Factors: types.Factors{BlastRadius: types.BlastRadius{Assessed: true, Summary: "Core egress proxy that enforces network policy."}}},
		"net/policy.rs": {Path: "net/policy.rs", Explanation: &types.Explanation{
			ImpactReasons: []string{"security 80"}, LikelihoodReasons: []string{"no notable churn, complexity, importers or fixes"},
		}},
	}}
	risk := &types.PRRisk{
		Tier: types.TierCritical, Verdict: "Harden now", LikelyCutoff: 33, ImpactThreshold: 78,
		FailOn: types.TierCritical, Decision: "block",
		FilesChanged: []types.FileChange{
			{Path: "docs/readme.md", Tier: types.TierLow},
			{Path: "net/proxy.rs", Tier: types.TierCritical, Likelihood: 50, Impact: 92, ImpactAssessed: true,
				Quadrant: types.QuadrantFragileCritical, QuadrantLabel: "Harden now"},
			{Path: "net/policy.rs", Tier: types.TierHigh, Likelihood: 5, Impact: 80, ImpactAssessed: true,
				Quadrant: types.QuadrantProtect, QuadrantLabel: "Protect"},
			{Path: "net/new|file.rs", Tier: types.TierHigh, IsNew: true, Unscored: true},
			{Path: "ui/view.go", Tier: types.TierLow, Likelihood: 10, Impact: 12, ImpactAssessed: true,
				Quadrant: types.QuadrantLow, QuadrantLabel: "Routine"},
		},
		Checklist: []types.Action{
			{ID: "senior-review", Text: "Get sign-off from a senior owner (@ana, @bo)", Scope: types.ScopeBeforeMerge,
				Signal: "Harden now: likely to break and costly when it does", Files: []string{"net/proxy.rs"}},
			{ID: "regression-tests", Text: "Add regression tests around the recent fix", Scope: types.ScopeBeforeMerge,
				Signal: `recent fix: "fix: align checks" (2026-10-28)`, Done: true, Files: []string{"net/proxy.rs"}},
			{ID: "no-auto-merge", Text: "Keep auto-merge off", Scope: types.ScopeBeforeMerge,
				Signal: "Harden now files never auto-merge", Files: []string{"net/proxy.rs", "net/policy.rs"}},
		},
		Backlog: []types.Action{
			{ID: "split", Text: "Split it into smaller files", Scope: types.ScopeBacklog, Signal: "479 functions in one file (top 1%)",
				Command: "gh issue create --title 'Split net/proxy.rs' --body 'x'", Files: []string{"net/proxy.rs"}},
		},
		SuggestedReviewers: []types.Reviewer{{Handle: "@ana"}, {Handle: "@bo"}},
	}
	return risk, hm
}

func TestRender_HardenGolden(t *testing.T) {
	risk, hm := hardenPR()
	golden(t, "harden", Render(risk, hm))
}

func TestRender_RoutineGolden(t *testing.T) {
	risk := &types.PRRisk{Tier: types.TierLow, Verdict: "Routine", LikelyCutoff: 33, ImpactThreshold: 78,
		FilesChanged: []types.FileChange{
			{Path: "ui/a.go", Tier: types.TierLow, Likelihood: 3, Impact: 10, ImpactAssessed: true, Quadrant: types.QuadrantLow},
			{Path: "ui/b.go", Tier: types.TierLow, Likelihood: 4, Impact: 9, ImpactAssessed: true, Quadrant: types.QuadrantLow},
		}}
	golden(t, "routine", Render(risk, &types.Heatmap{}))
}

func TestRender_Rules(t *testing.T) {
	t.Run("at most five non-routine rows, the rest collapsed", func(t *testing.T) {
		risk := &types.PRRisk{Tier: types.TierMedium, Verdict: "Stabilize", LikelyCutoff: 33, ImpactThreshold: 78}
		for i := 0; i < 7; i++ {
			risk.FilesChanged = append(risk.FilesChanged, types.FileChange{Path: fmt.Sprintf("s%d.go", i), Tier: types.TierMedium,
				Likelihood: 40 + i, Impact: 10, ImpactAssessed: true, Quadrant: types.QuadrantWatch})
		}
		risk.FilesChanged = append(risk.FilesChanged, types.FileChange{Path: "r.go", Tier: types.TierLow, Quadrant: types.QuadrantLow, ImpactAssessed: true})
		out := Render(risk, &types.Heatmap{})
		if n := strings.Count(out, "| Stabilize |"); n != 5 {
			t.Errorf("%d Stabilize rows, want 5:\n%s", n, out)
		}
		if !strings.Contains(out, "| 3 other files | low | low | Routine |") {
			t.Errorf("overflow and routine files not collapsed into one row:\n%s", out)
		}
		if !strings.Contains(out, "`s6.go`") || strings.Contains(out, "`s0.go` |") {
			t.Errorf("rows not ordered by score:\n%s", out)
		}
	})
	t.Run("no heat score, review time or tier names", func(t *testing.T) {
		risk, hm := hardenPR()
		out := Render(risk, hm)
		for _, bad := range []string{"heat", "Heat", "min", "CRITICAL", "🔥"} {
			if strings.Contains(out, bad) {
				t.Errorf("comment contains %q", bad)
			}
		}
	})
	t.Run("every score shows its repo cutoff once", func(t *testing.T) {
		risk, hm := hardenPR()
		out := Render(risk, hm)
		if !strings.Contains(out, "Repo cutoffs: will it break 33, how bad 78") {
			t.Errorf("cutoffs missing:\n%s", out)
		}
		if strings.Count(out, "not a probability") != 1 {
			t.Error("the relative-signal note should appear exactly once")
		}
		if strings.Count(out, "How bad? is AI-assessed, not yet checked against maintainer ratings") != 1 {
			t.Error("the evidence note for the two questions should appear exactly once")
		}
	})
	t.Run("marker lets the workflow update its own comment", func(t *testing.T) {
		out := Render(&types.PRRisk{Verdict: "Routine"}, &types.Heatmap{})
		if !strings.HasPrefix(out, Marker+"\n") {
			t.Errorf("comment does not start with the marker:\n%s", out)
		}
	})
}
