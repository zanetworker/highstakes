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

// TestRender_OverflowGolden: a sixth Harden now file gets its own
// "needs action" row and keeps its details; only routine files are Routine.
func TestRender_OverflowGolden(t *testing.T) {
	risk := &types.PRRisk{Tier: types.TierCritical, Verdict: "Harden now", LikelyCutoff: 33, ImpactThreshold: 78}
	for i := 0; i < 6; i++ {
		risk.FilesChanged = append(risk.FilesChanged, types.FileChange{Path: fmt.Sprintf("core/h%d.go", i), Tier: types.TierCritical,
			Likelihood: 60 - i, Impact: 90, ImpactAssessed: true, Quadrant: types.QuadrantFragileCritical})
	}
	for _, p := range []string{"ui/a.go", "ui/b.go"} {
		risk.FilesChanged = append(risk.FilesChanged, types.FileChange{Path: p, Tier: types.TierLow,
			Likelihood: 3, Impact: 10, ImpactAssessed: true, Quadrant: types.QuadrantLow})
	}
	risk.Checklist = []types.Action{{ID: "senior-review", Text: "Get sign-off from a senior owner", Scope: types.ScopeBeforeMerge,
		Signal: "Harden now", Files: []string{"core/h0.go", "core/h5.go"}}}
	golden(t, "overflow", Render(risk, &types.Heatmap{}))
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
	t.Run("huge PRs stay under GitHub's comment limit", func(t *testing.T) {
		risk := &types.PRRisk{Tier: types.TierCritical, Verdict: "Harden now", LikelyCutoff: 33, ImpactThreshold: 78}
		hm := &types.Heatmap{Files: map[string]*types.FileHeat{}}
		long := strings.Repeat("signal ", 40)
		for i := 0; i < 3000; i++ {
			p := fmt.Sprintf("pkg/dir%d/file_with_a_long_name_%d.go", i%50, i)
			risk.FilesChanged = append(risk.FilesChanged, types.FileChange{Path: p, Tier: types.TierCritical,
				Likelihood: 60, Impact: 90, ImpactAssessed: true, Quadrant: types.QuadrantFragileCritical})
			hm.Files[p] = &types.FileHeat{Path: p, Explanation: &types.Explanation{LikelihoodReasons: []string{long}, ImpactReasons: []string{long}}}
			risk.Backlog = append(risk.Backlog, types.Action{ID: "split", Text: "Split it", Files: []string{p}, Signal: long, Command: "gh issue create --title " + p})
		}
		out := Render(risk, hm)
		if len(out) > MaxCommentChars {
			t.Fatalf("comment is %d chars, over the %d budget", len(out), MaxCommentChars)
		}
		if !strings.Contains(out, "omitted to fit GitHub's comment size limit") {
			t.Errorf("no omitted-content notice")
		}
		if !strings.HasSuffix(out, "</details>\n") {
			t.Errorf("details block not closed")
		}
	})
	t.Run("a giant circuit-breaker warning is budgeted too", func(t *testing.T) {
		var paths []string
		for i := 0; i < 4000; i++ {
			paths = append(paths, fmt.Sprintf("pkg/dir%d/new_unscored_file_%d.go", i%50, i))
		}
		risk := &types.PRRisk{Tier: types.TierLow, Verdict: "Routine", LikelyCutoff: 33, ImpactThreshold: 78,
			CircuitBreakerSignals: []string{"Unscored new file(s), heat inherited from directory: " + strings.Join(paths, ", ")}}
		out := Render(risk, &types.Heatmap{})
		if len(out) > MaxCommentChars {
			t.Fatalf("comment is %d chars, over the %d budget", len(out), MaxCommentChars)
		}
		if !strings.Contains(out, "Unscored new file(s)") || !strings.Contains(out, "omitted to fit GitHub's comment size limit") {
			t.Errorf("warning should be kept but truncated with a notice")
		}
	})
	t.Run("small PRs are not truncated", func(t *testing.T) {
		risk, hm := hardenPR()
		if out := Render(risk, hm); strings.Contains(out, "omitted") {
			t.Errorf("unexpected truncation notice:\n%s", out)
		}
	})
	t.Run("circuit breaker warnings are shown", func(t *testing.T) {
		risk := &types.PRRisk{Tier: types.TierLow, Verdict: "Routine", LikelyCutoff: 33, ImpactThreshold: 78,
			CircuitBreakerSignals: []string{"Unscored new file(s), heat inherited from directory: a/new.go"},
			FilesChanged:          []types.FileChange{{Path: "a/new.go", Tier: types.TierLow, Unscored: true}}}
		out := Render(risk, &types.Heatmap{})
		if !strings.Contains(out, "Unscored new file(s), heat inherited from directory: a/new.go") {
			t.Errorf("circuit breaker warning missing:\n%s", out)
		}
	})
	t.Run("no warnings section without signals", func(t *testing.T) {
		risk, hm := hardenPR()
		risk.CircuitBreakerSignals = nil
		if out := Render(risk, hm); strings.Contains(out, "Warnings") {
			t.Errorf("unexpected warnings section:\n%s", out)
		}
	})
	t.Run("at most five non-routine rows, overflow kept apart from routine", func(t *testing.T) {
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
		if !strings.Contains(out, "| 2 more files need action |  |  | see details |") {
			t.Errorf("overflow files not shown as their own row:\n%s", out)
		}
		if !strings.Contains(out, "| 1 other file | low | low | Routine |") {
			t.Errorf("routine row should hold only routine files:\n%s", out)
		}
		if !strings.Contains(out, "**`s0.go`** (Stabilize)") {
			t.Errorf("overflow files dropped from the details:\n%s", out)
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
