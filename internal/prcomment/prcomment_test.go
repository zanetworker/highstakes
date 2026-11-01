package prcomment

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
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
		FailOn: types.TierCritical, Decision: "block", AutonomyReason: "touches Harden now files",
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
			{ID: "senior-review", Text: "Get sign-off from a senior owner (@ana, @bo)", Scope: types.ScopeBeforeMerge, Owners: []string{"@ana", "@bo"},
				Signal: "Harden now: likely to break and costly when it does", Files: []string{"net/proxy.rs"}},
			{ID: "regression-tests", Text: "Add regression tests around the recent fix", Scope: types.ScopeBeforeMerge,
				Signal: `recent fix: "fix: align checks" (2026-10-28)`, Done: true, Files: []string{"net/proxy.rs"}},
			{ID: "no-auto-merge", Text: "Keep auto-merge off", Scope: types.ScopeBeforeMerge,
				Signal: "Harden now files never auto-merge", Files: []string{"net/proxy.rs", "net/policy.rs"}},
		},
		WorthDoing: []types.Action{
			{ID: "refactor", Text: "Refactor the function you touch: cyclomatic 40 across 12 functions", Scope: types.ScopeBeforeMerge,
				Signal: "complexity in the top 5% of this repo", Files: []string{"ui/busy.go"}},
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
	golden(t, "harden", Render(risk, hm, Links{Repo: "acme/net", Base: "main"}))
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
	golden(t, "overflow", Render(risk, &types.Heatmap{}, Links{}))
}

func TestRender_RoutineGolden(t *testing.T) {
	risk := &types.PRRisk{Tier: types.TierLow, Verdict: "Routine", LikelyCutoff: 33, ImpactThreshold: 78,
		FilesChanged: []types.FileChange{
			{Path: "ui/a.go", Tier: types.TierLow, Likelihood: 3, Impact: 10, ImpactAssessed: true, Quadrant: types.QuadrantLow},
			{Path: "ui/b.go", Tier: types.TierLow, Likelihood: 4, Impact: 9, ImpactAssessed: true, Quadrant: types.QuadrantLow},
		}}
	golden(t, "routine", Render(risk, &types.Heatmap{}, Links{}))
}

func TestRender_Rules(t *testing.T) {
	t.Run("details show each file's scores against the cutoffs", func(t *testing.T) {
		risk, hm := hardenPR()
		out := Render(risk, hm, Links{})
		if !strings.Contains(out, "- Scores: Will it break? 50 (high, cutoff 33); How bad if it breaks? 92 (high, cutoff 78)") {
			t.Errorf("per-file scores missing from details:\n%s", out)
		}
	})
	t.Run("backlog commands with an unmapped email are replaced, not broken", func(t *testing.T) {
		risk, hm := hardenPR()
		risk.Backlog = []types.Action{
			{ID: "codeowners", Text: "Add a CODEOWNERS line", Files: []string{"core/p.go"}, Signal: "not covered", Command: "/core/p.go ana@example.com"},
			{ID: "codeowners", Text: "Add a CODEOWNERS line", Files: []string{"core/q.go"}, Signal: "not covered", Command: "/core/q.go 1+bo@users.noreply.github.com"},
		}
		out := Render(risk, hm, Links{})
		if strings.Contains(out, "/core/p.go a top owner") {
			t.Errorf("redaction produced an invalid CODEOWNERS rule:\n%s", out)
		}
		if !strings.Contains(out, "add an `owners` alias in .heatmap/config.yaml to get a copyable CODEOWNERS line") {
			t.Errorf("no instruction for the unmapped owner:\n%s", out)
		}
		if !strings.Contains(out, "`/core/q.go @bo`") {
			t.Errorf("noreply owner should become a valid @handle rule:\n%s", out)
		}
	})
	t.Run("table cells stay on one line and never understate risk", func(t *testing.T) {
		hm := &types.Heatmap{Files: map[string]*types.FileHeat{
			"a/multi.go": {Path: "a/multi.go", Factors: types.Factors{BlastRadius: types.BlastRadius{Assessed: true, Summary: "line one\nline two"}}},
		}}
		risk := &types.PRRisk{Tier: types.TierHigh, Verdict: "Protect", LikelyCutoff: 33, ImpactThreshold: 78,
			FilesChanged: []types.FileChange{
				{Path: "a/multi.go", Tier: types.TierHigh, Impact: 85, ImpactAssessed: true, Quadrant: types.QuadrantProtect},
				{Path: "a/bare.go", Tier: types.TierHigh, Impact: 90, ImpactAssessed: true, Quadrant: types.QuadrantProtect},
			}}
		out := Render(risk, hm, Links{})
		if !strings.Contains(out, "| `a/multi.go` | line one line two |") {
			t.Errorf("multi-line summary not flattened:\n%s", out)
		}
		if strings.Contains(out, "| `a/bare.go` | limited damage") {
			t.Errorf("a Protect file without a description reads as limited damage:\n%s", out)
		}
		if !strings.Contains(out, "| `a/bare.go` | costly (no description; run analyze again) |") {
			t.Errorf("missing fallback for an undescribed costly file:\n%s", out)
		}
	})
	t.Run("table says what breaks and how often, not scores", func(t *testing.T) {
		risk, hm := hardenPR()
		out := Render(risk, hm, Links{})
		if !strings.Contains(out, "|    | File | If it breaks | Breaks often? | Next step |") {
			t.Errorf("table header not plain:\n%s", out)
		}
		// consequence from the LLM summary; frequency from the first likelihood reason
		if !strings.Contains(out, "| ⛔ | `net/proxy.rs` | Core egress proxy that enforces network policy. | 🔴 55 commits in the last 90 days | 👀 senior sign-off |") {
			t.Errorf("proxy row not in plain words:\n%s", out)
		}
		if !strings.Contains(out, "| 🛡 | `net/policy.rs` | security 80 | 🟢 rarely | 🚫 no auto-merge |") {
			t.Errorf("policy row not in plain words:\n%s", out)
		}
		table := out[strings.Index(out, "| File |"):strings.Index(out, "<details>")]
		for _, bad := range []string{"50 high", "92 high", " low |"} {
			if strings.Contains(table, bad) {
				t.Errorf("table still shows scores (%q):\n%s", bad, table)
			}
		}
	})
	t.Run("no email address appears anywhere", func(t *testing.T) {
		risk, hm := hardenPR()
		risk.Checklist = append(risk.Checklist, types.Action{ID: "owner-review", Scope: types.ScopeBeforeMerge,
			Text: "Get an owner's review (ana@example.com, 12345+bo@users.noreply.github.com)", Files: []string{"net/policy.rs"},
			Owners: []string{"ana@example.com", "12345+bo@users.noreply.github.com"}})
		risk.SuggestedReviewers = []types.Reviewer{{Handle: "ana@example.com"}, {Handle: "@carl"}}
		out := Render(risk, hm, Links{})
		if m := regexp.MustCompile(`[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}`).FindString(out); m != "" {
			t.Errorf("email %q in comment:\n%s", m, out)
		}
		if !strings.Contains(out, "@bo") || !strings.Contains(out, "@carl") {
			t.Errorf("GitHub handles should stay:\n%s", out)
		}
		if top, _, _ := strings.Cut(out, "<details><summary>Why"); strings.Contains(top, "a top owner") {
			t.Errorf("checklist names an unmapped owner as \"a top owner\":\n%s", out)
		}
		if !strings.Contains(out, "- [ ] 👀 Request a review from @bo on `net/policy.rs`\n") {
			t.Errorf("review request should name the GitHub handle only:\n%s", out)
		}
	})
	t.Run("routine summary leaves auto-merge to the autonomy line", func(t *testing.T) {
		risk := &types.PRRisk{Tier: types.TierLow, Verdict: "Routine", LikelyCutoff: 33, ImpactThreshold: 78,
			AutoMergeAllowed: false, AutonomyLevel: 1, AutonomyReason: "touches files whose impact is not AI-assessed",
			FilesChanged: []types.FileChange{{Path: "a.go", Tier: types.TierLow}}}
		out := Render(risk, &types.Heatmap{}, Links{})
		if strings.Contains(out, "auto-merge allowed") {
			t.Errorf("summary claims auto-merge while the autonomy line forbids it:\n%s", out)
		}
		if !strings.Contains(out, "🤖 AI may auto-merge: ❌ no") {
			t.Errorf("autonomy line missing:\n%s", out)
		}
	})
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
		out := Render(risk, hm, Links{})
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
		out := Render(risk, &types.Heatmap{}, Links{})
		if len(out) > MaxCommentChars {
			t.Fatalf("comment is %d chars, over the %d budget", len(out), MaxCommentChars)
		}
		if !strings.Contains(out, "Unscored new file(s)") || !strings.Contains(out, "omitted to fit GitHub's comment size limit") {
			t.Errorf("warning should be kept but truncated with a notice")
		}
	})
	t.Run("small PRs are not truncated", func(t *testing.T) {
		risk, hm := hardenPR()
		if out := Render(risk, hm, Links{}); strings.Contains(out, "omitted") {
			t.Errorf("unexpected truncation notice:\n%s", out)
		}
	})
	t.Run("circuit breaker warnings are shown", func(t *testing.T) {
		risk := &types.PRRisk{Tier: types.TierLow, Verdict: "Routine", LikelyCutoff: 33, ImpactThreshold: 78,
			CircuitBreakerSignals: []string{"Unscored new file(s), heat inherited from directory: a/new.go"},
			FilesChanged:          []types.FileChange{{Path: "a/new.go", Tier: types.TierLow, Unscored: true}}}
		out := Render(risk, &types.Heatmap{}, Links{})
		if !strings.Contains(out, "### ⚡ One-click actions\n\n- 🆕 Unscored new file(s), treated like their directory until the next analyze scores them: a/new.go\n") {
			t.Errorf("circuit breaker warning missing:\n%s", out)
		}
	})
	t.Run("no warnings section without signals", func(t *testing.T) {
		risk, hm := hardenPR()
		risk.CircuitBreakerSignals = nil
		if out := Render(risk, hm, Links{}); strings.Contains(out, "Warnings") {
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
		out := Render(risk, &types.Heatmap{}, Links{})
		if n := strings.Count(out, "| 🔧 |"); n != 5 {
			t.Errorf("%d Stabilize rows, want 5:\n%s", n, out)
		}
		if !strings.Contains(out, "<details><summary>📋 2 more files need action</summary>\n\n- 🔧 `s1.go`: careful review\n- 🔧 `s0.go`: careful review\n") {
			t.Errorf("overflow files not folded one line each:\n%s", out)
		}
		if !strings.Contains(out, "| ✅ | 1 other file | limited damage | 🟢 rarely | normal review |") {
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
		out := Render(risk, hm, Links{})
		for _, bad := range []string{"heat", "Heat", "min", "CRITICAL", "🔥"} {
			if strings.Contains(out, bad) {
				t.Errorf("comment contains %q", bad)
			}
		}
	})
	t.Run("every score shows its repo cutoff once", func(t *testing.T) {
		risk, hm := hardenPR()
		out := Render(risk, hm, Links{})
		if !strings.Contains(out, "Scores and repo cutoffs: Will it break? 33, How bad if it breaks? 78") {
			t.Errorf("cutoffs missing:\n%s", out)
		}
		if strings.Count(out, "not a probability") != 1 {
			t.Error("the relative-signal note should appear exactly once")
		}
		if strings.Count(out, "How bad if it breaks? is AI-assessed, not yet checked against maintainer ratings") != 1 {
			t.Error("the evidence note for the two questions should appear exactly once")
		}
	})
	t.Run("stabilize items are worth doing, not before merge", func(t *testing.T) {
		risk := &types.PRRisk{Tier: types.TierMedium, Verdict: "Stabilize", LikelyCutoff: 33, ImpactThreshold: 78,
			FilesChanged: []types.FileChange{{Path: "s.go", Tier: types.TierMedium, Likelihood: 50, Impact: 10, ImpactAssessed: true, Quadrant: types.QuadrantWatch}},
			WorthDoing:   []types.Action{{ID: "refactor", Text: "Refactor the function you touch", Signal: "complexity in the top 5%", Files: []string{"s.go"}}}}
		out := Render(risk, &types.Heatmap{}, Links{})
		if strings.Contains(out, "Do before merge") {
			t.Errorf("no blocking items, so no before-merge checklist:\n%s", out)
		}
		_, details, _ := strings.Cut(out, "<details>")
		if !strings.Contains(details, "**Worth doing (does not block)**\n- Refactor the function you touch (`s.go`): complexity in the top 5%\n") {
			t.Errorf("worth-doing items missing from the folded details:\n%s", out)
		}
		if strings.Contains(out, "- [ ] Refactor") {
			t.Errorf("worth-doing items must not be checkboxes:\n%s", out)
		}
	})
	t.Run("auto-merge line under the verdict", func(t *testing.T) {
		risk := &types.PRRisk{Tier: types.TierLow, Verdict: "Routine", AutonomyLevel: 3, AutoMergeAllowed: true,
			AutonomyReason: "every touched file is L3"}
		out := Render(risk, &types.Heatmap{}, Links{})
		want := "## ✅ Routine\n\n🤖 AI may auto-merge: ✅ yes (every touched file is L3)\n\n"
		if !strings.Contains(out, want) {
			t.Errorf("missing %q right after the verdict:\n%s", want, out)
		}
		risk.AutonomyReason = ""
		if strings.Contains(Render(risk, &types.Heatmap{}, Links{}), "AI may auto-merge") {
			t.Error("no autonomy computed, so no line")
		}
	})
	t.Run("with a repo, issue and CODEOWNERS actions are links", func(t *testing.T) {
		risk, hm := hardenPR()
		risk.Backlog = append(risk.Backlog, types.Action{ID: "codeowners", Text: "Add a CODEOWNERS line", Files: []string{"net/proxy.rs"},
			Signal: "not covered", Command: "/net/proxy.rs @ana"})
		out := Render(risk, hm, Links{Repo: "acme/net", Base: "main", Codeowners: ".github/CODEOWNERS"})
		for _, want := range []string{
			"| 👀 senior sign-off · [📝 open issue](https://github.com/acme/net/issues/new?title=Split+net%2Fproxy.rs+into+smaller+files&body=",
			"- [📝 Open issue: Split net/proxy.rs into smaller files](https://github.com/acme/net/issues/new?",
			"- [👥 Add `/net/proxy.rs @ana` to CODEOWNERS](https://github.com/acme/net/edit/main/.github/CODEOWNERS)",
		} {
			if !strings.Contains(out, want) {
				t.Errorf("missing %q:\n%s", want, out)
			}
		}
	})
	t.Run("without a repo, actions are plain text", func(t *testing.T) {
		risk, hm := hardenPR()
		out := Render(risk, hm, Links{})
		if strings.Contains(out, "https://") {
			t.Errorf("link without a repo:\n%s", out)
		}
		if !strings.Contains(out, "- 📝 Open an issue: Split net/proxy.rs into smaller files\n") {
			t.Errorf("plain-text issue action missing:\n%s", out)
		}
	})
	t.Run("a large diff becomes a split action with its size", func(t *testing.T) {
		risk := &types.PRRisk{Verdict: "Routine", CircuitBreakerSignals: []string{"Large diff (>500 lines)"},
			FilesChanged: []types.FileChange{{Path: "a.go", LinesAdded: 1200, LinesDeleted: 34, ImpactAssessed: true, Quadrant: types.QuadrantLow}}}
		if out := Render(risk, &types.Heatmap{}, Links{}); !strings.Contains(out, "- ✂️ Large diff (1,234 lines): consider splitting this PR\n") {
			t.Errorf("large diff action missing:\n%s", out)
		}
	})
	t.Run("marker lets the workflow update its own comment", func(t *testing.T) {
		out := Render(&types.PRRisk{Verdict: "Routine"}, &types.Heatmap{}, Links{})
		if !strings.HasPrefix(out, Marker+"\n") {
			t.Errorf("comment does not start with the marker:\n%s", out)
		}
	})
}
