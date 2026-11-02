package explain

import (
	"strings"
	"testing"
	"time"

	"github.com/zanetworker/highstakes/internal/types"
)

// briefFile is a Harden now file with fixes, importers and an LLM summary.
func briefFile() *types.FileHeat {
	f := hot()
	f.Language = "go"
	f.Path = "pkg/heatmap/heatmap.go"
	day := time.Date(2026, 10, 28, 0, 0, 0, 0, time.UTC)
	f.RecentChanges = []types.Change{
		{Message: "fix: keep cache keys stable", Date: day},
		{Message: "feat: add units", Date: day.AddDate(0, 0, -1)},
		{Message: "fix: nil heatmap on load", Date: day.AddDate(0, 0, -2)},
	}
	f.Dependencies.ImportedBy = []types.Dependency{
		{Path: "cmd/a.go", HeatScore: 10}, {Path: "cmd/b.go", HeatScore: 90}, {Path: "cmd/c.go", HeatScore: 50}, {Path: "cmd/d.go", HeatScore: 70},
	}
	f.Factors.DependencyCentrality.ExportedSymbols = 12
	f.Explanation = &types.Explanation{LikelihoodReasons: []string{
		"55 commits in the last 90 days, 28 authors overall (top 1% of this repo)",
		`2 recent fixes; latest: "fix: keep cache keys stable" (2026-10-28)`,
	}}
	return f
}

func hardenInput() BriefInput {
	return BriefInput{Posture: "Harden now", LikelyCutoff: 33, ImpactThreshold: 78, Owners: []string{"@ana", "@bo"},
		Autonomy: &types.Autonomy{Level: 0, Label: "assist only"},
		PR:       12, PRURL: "https://github.com/o/r/pull/12", CommitSHA: "6ef6978"}
}

func TestIssueBrief_Sections(t *testing.T) {
	b := IssueBrief(briefFile(), types.Action{ID: "regression-tests"}, hardenInput())
	tests := []struct {
		name    string
		section []string
		want    []string
	}{
		{"why it matters: consequence and top dimensions", b.WhyItMatters,
			[]string{"Core egress proxy.", "Bugs bypass network policy.", "Top impact: security 92 · availability 85 · user 85"}},
		{"why now: posture, scores, fixes with dates, other signals", b.WhyNow, []string{
			"Harden now: likely to break and costly if it does",
			"Will it break? 50 (high, cutoff 33); How bad if it breaks? 92 (high, cutoff 78)",
			`Recent fixes: "fix: keep cache keys stable" (2026-10-28); "fix: nil heatmap on load" (2026-10-26)`,
			"55 commits in the last 90 days, 28 authors overall (top 1% of this repo)",
		}},
		{"approach names the fixes", b.Approach, []string{
			`Write a failing test that reproduces each recent fix: "fix: keep cache keys stable" (2026-10-28); "fix: nil heatmap on load" (2026-10-26)`,
		}},
		{"done when: acceptance plus tests pass", b.DoneWhen,
			[]string{"a test next to pkg/heatmap/heatmap.go covers the recent fixes and passes", "tests pass"}},
		{"context: who, importers, autonomy, PR, commit", []string{b.Who, b.Importers, b.Autonomy, b.PR, b.Commit}, []string{
			"@ana, @bo",
			"4 files import it, including `cmd/b.go`, `cmd/d.go`, `cmd/c.go`",
			"L0 assist only: agents may write tests and explanations; changes need a named senior owner",
			"[PR #12](https://github.com/o/r/pull/12)",
			"`6ef6978`",
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := strings.Join(tt.section, "\n")
			for _, w := range tt.want {
				if !strings.Contains(got, w) {
					t.Errorf("missing %q in:\n%s", w, got)
				}
			}
		})
	}
	t.Run("the latest-fix reason is not repeated next to the fix list", func(t *testing.T) {
		if got := strings.Join(b.WhyNow, "\n"); strings.Contains(got, "latest:") {
			t.Errorf("fix reason duplicated:\n%s", got)
		}
	})
	if b.Title != "Add regression tests for the recent fixes to pkg/heatmap/heatmap.go" {
		t.Errorf("title = %q", b.Title)
	}
}

func TestIssueBrief_Fallbacks(t *testing.T) {
	f := briefFile()
	f.ImpactAssessed, f.Impact = false, 40
	f.Factors.BlastRadius = types.BlastRadius{}
	f.Dependencies.ImportedBy = nil
	f.RecentChanges = nil
	f.Explanation = nil
	b := IssueBrief(f, types.Action{ID: "split"}, BriefInput{Posture: "Stabilize", LikelyCutoff: 33, ImpactThreshold: 78})
	md := b.Markdown()
	for _, want := range []string{
		"Impact estimated (~40 from centrality and path keywords; not AI-assessed yet)",
		"How bad if it breaks? ~40 (estimate, cutoff 78)",
		"someone who knows the file",
		"No file imports it",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("missing %q in:\n%s", want, md)
		}
	}
	for _, absent := range []string{"PR #", "Commit", "AI autonomy"} {
		if strings.Contains(md, absent) {
			t.Errorf("unexpected %q without its input:\n%s", absent, md)
		}
	}
	if n := strings.Count(md, "tests pass"); n != 1 {
		t.Errorf("\"tests pass\" appears %d times, want once:\n%s", n, md)
	}
}

func TestIssueBrief_ApproachPerKind(t *testing.T) {
	tests := []struct {
		id, path string
		want     []string
	}{
		{"regression-tests", "a/x.go", []string{"reproduces each recent fix"}},
		{"split", "a/x.go", []string{"cohesive groups of the 479 functions", "one group per PR", "Keep the public API unchanged"}},
		{"complexity-issue", "a/x.go", []string{"tests that pin the current behavior first", "cyclomatic 1,092", "extract"}},
		{"test-gap", "a/x.go", []string{"`x_test.go`", "12 exported symbols"}},
		{"test-gap", "a/x.py", []string{"`test_x.py`"}},
		{"test-gap", "a/x.ts", []string{"`x.test.ts`"}},
		{"test-gap", "a/x.rs", []string{"#[cfg(test)]"}},
		{"test-gap", "a/x.zz", []string{"the project's test convention"}},
		{"document-invariants", "a/x.go", []string{"next to the code", "`cmd/b.go`, `cmd/d.go`, `cmd/c.go`"}},
		{"unknown-kind", "a/x.go", []string{"tests pass"}},
	}
	for _, tt := range tests {
		t.Run(tt.id+" "+tt.path, func(t *testing.T) {
			f := briefFile()
			f.Path = tt.path
			b := IssueBrief(f, types.Action{ID: tt.id, Text: "Do the thing"}, hardenInput())
			if len(b.Approach) < 2 || len(b.Approach) > 4 {
				t.Errorf("%d steps, want 2-4: %q", len(b.Approach), b.Approach)
			}
			got := strings.Join(b.Approach, "\n")
			for _, w := range tt.want {
				if !strings.Contains(got, w) {
					t.Errorf("missing %q in:\n%s", w, got)
				}
			}
		})
	}
}

func TestBrief_Markdown(t *testing.T) {
	md := IssueBrief(briefFile(), types.Action{ID: "regression-tests"}, hardenInput()).Markdown()
	order := []string{"## Why it matters\n", "## Why now\n", "## Suggested approach\n1. ", "## Done when\n- [ ] ", "## Context\n",
		"- Review by: @ana, @bo\n", "- From: [PR #12]", "_Opened from the HighStakes PR comment_"}
	at := 0
	for _, w := range order {
		i := strings.Index(md[at:], w)
		if i < 0 {
			t.Fatalf("missing or out of order %q in:\n%s", w, md)
		}
		at += i
	}
	if !strings.HasSuffix(md, "_Opened from the HighStakes PR comment_\n") {
		t.Errorf("footer not last:\n%s", md)
	}
}

func TestIssueBrief_NoZeroNoise(t *testing.T) {
	f := &types.FileHeat{Path: "a/x.go", Impact: 90, ImpactAssessed: true,
		Factors: types.Factors{BlastRadius: types.BlastRadius{Assessed: true, Summary: "Core proxy."}}}
	md := IssueBrief(f, types.Action{ID: "split"}, BriefInput{Posture: "Harden now"}).Markdown()
	for _, bad := range []string{"Top impact", "the 0 functions"} {
		if strings.Contains(md, bad) {
			t.Errorf("unexpected %q for a file without dimension scores or function counts:\n%s", bad, md)
		}
	}
	if !strings.Contains(md, "- Core proxy.\n") || !strings.Contains(md, "1. Identify cohesive groups of functions") {
		t.Errorf("summary or first step missing:\n%s", md)
	}
}

func TestTopImporters_SkipsTests(t *testing.T) {
	f := &types.FileHeat{Path: "core/a.go", Dependencies: types.Dependencies{ImportedBy: []types.Dependency{
		{Path: "core/a_test.go", HeatScore: 90}, {Path: "cmd/main.go", HeatScore: 50},
		{Path: "web/app.test.ts", HeatScore: 80}, {Path: "api/h.go", HeatScore: 40},
	}}}
	got := strings.Join(topImporters(f), ", ")
	if got != "`cmd/main.go`, `api/h.go`" {
		t.Errorf("topImporters = %s, want only non-test importers", got)
	}
	if line := importersLine(f); line != "2 files import it: `cmd/main.go`, `api/h.go` (plus 2 tests)" {
		t.Errorf("importersLine = %q", line)
	}
}
