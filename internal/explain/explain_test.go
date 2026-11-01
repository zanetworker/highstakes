package explain

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/zanetworker/highstakes/internal/types"
)

// repo builds stats over 100 plain files with metric value 1, plus the file
// under test, so the file under test is "top 1%" when its values exceed 1.
func repo(f *types.FileHeat) RepoStats {
	hm := &types.Heatmap{
		Metadata: types.Metadata{LikelyCutoff: 33, ImpactThreshold: 61},
		Files:    map[string]*types.FileHeat{f.Path: f},
	}
	for i := 0; i < 99; i++ {
		p := fmt.Sprintf("plain%d.go", i)
		g := &types.FileHeat{Path: p, Size: types.FileSize{Lines: 1}}
		g.Factors.ChangeFrequency = types.ChangeFrequency{CommitsLast90d: 1, UniqueAuthors: 1}
		g.Factors.Complexity = types.Complexity{Cyclomatic: 1, FunctionCount: 1}
		g.Dependencies.ImportedBy = []types.Dependency{{Path: "x"}}
		hm.Files[p] = g
	}
	return BuildRepoStats(hm, func(p string) bool { return strings.HasSuffix(p, "_test.go") })
}

func hot() *types.FileHeat {
	f := &types.FileHeat{
		Path: "net/proxy.rs", Impact: 92, Likelihood: 50, ImpactAssessed: true,
		Quadrant: types.QuadrantFragileCritical, Size: types.FileSize{Lines: 9000},
	}
	f.Factors.BlastRadius = types.BlastRadius{
		Assessed: true, Score: 92, SecurityImpact: 92, AvailabilityImpact: 85, UserImpact: 85, DataImpact: 70,
		Summary: "Core egress proxy.", CriticalReason: "Bugs bypass network policy.",
	}
	f.Factors.ChangeFrequency = types.ChangeFrequency{CommitsLast90d: 55, UniqueAuthors: 28, CommitCount: 120}
	f.Factors.Complexity = types.Complexity{Cyclomatic: 1092, FunctionCount: 479}
	for i := 0; i < 11; i++ {
		f.Dependencies.ImportedBy = append(f.Dependencies.ImportedBy, types.Dependency{Path: fmt.Sprintf("i%d", i)})
	}
	f.RecentChanges = []types.Change{
		{Date: time.Date(2026, 10, 30, 0, 0, 0, 0, time.UTC), Message: "feat: add thing"},
		{Date: time.Date(2026, 10, 28, 0, 0, 0, 0, time.UTC), Message: "fix(network): align destination checks"},
	}
	return f
}

func contains(t *testing.T, got []string, want ...string) {
	t.Helper()
	joined := strings.Join(got, "\n")
	for _, w := range want {
		if !strings.Contains(joined, w) {
			t.Errorf("missing %q in:\n%s", w, joined)
		}
	}
}

func lacks(t *testing.T, got []string, not ...string) {
	t.Helper()
	joined := strings.Join(got, "\n")
	for _, n := range not {
		if strings.Contains(joined, n) {
			t.Errorf("unexpected %q in:\n%s", n, joined)
		}
	}
}

func TestExplain_Headline(t *testing.T) {
	tests := []struct {
		name string
		edit func(*types.FileHeat)
		want string
	}{
		{"harden now", func(*types.FileHeat) {}, "HARDEN NOW (will it break 50, cutoff 33; how bad 92, cutoff 61)"},
		{"protect", func(f *types.FileHeat) { f.Quadrant = types.QuadrantProtect }, "PROTECT (will it break 50"},
		{"stabilize", func(f *types.FileHeat) { f.Quadrant = types.QuadrantWatch }, "STABILIZE (will it break 50"},
		{"routine", func(f *types.FileHeat) { f.Quadrant = types.QuadrantLow }, "ROUTINE (will it break 50"},
		{"not assessed", func(f *types.FileHeat) { f.ImpactAssessed = false; f.Quadrant = ""; f.Impact = 20 },
			"NOT ASSESSED (will it break 50, cutoff 33; how bad ~20 estimate)"},
		{"only a likely cutoff", func(*types.FileHeat) {}, "HARDEN NOW (will it break 50, cutoff 33; how bad 92)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := hot()
			tt.edit(f)
			rs := repo(f)
			if tt.name == "only a likely cutoff" {
				rs.ImpactThreshold = 0 // heatmaps saved before the impact cutoff existed
			}
			got := Explain(f, rs, nil).Headline
			if !strings.HasPrefix(got, tt.want) {
				t.Errorf("Headline = %q, want prefix %q", got, tt.want)
			}
		})
	}
}

func TestExplain_Verdict(t *testing.T) {
	tests := []struct {
		name string
		edit func(*types.FileHeat)
		want string
	}{
		{"harden now", func(*types.FileHeat) {}, "Harden now"},
		{"stabilize", func(f *types.FileHeat) { f.Quadrant = types.QuadrantWatch }, "Stabilize"},
		{"not assessed", func(f *types.FileHeat) { f.ImpactAssessed = false; f.Quadrant = "" }, "Not assessed"},
		{"test file is routine", func(f *types.FileHeat) { f.Path = "net/proxy_test.go" }, "Routine"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := hot()
			tt.edit(f)
			if got := Explain(f, repo(f), nil).Verdict; got != tt.want {
				t.Errorf("Verdict = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestExplain_AxisFlags(t *testing.T) {
	tests := []struct {
		name                 string
		impact, likelihood   int
		path                 string
		wantImpact, wantLike bool
	}{
		{"both at the boundary", 61, 33, "a.go", true, true},
		{"both just below", 60, 32, "a.go", false, false},
		{"test file is never likely", 90, 90, "a_test.go", true, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := &types.FileHeat{Path: tt.path, Impact: tt.impact, Likelihood: tt.likelihood}
			e := Explain(f, repo(f), nil)
			if e.ImpactHigh != tt.wantImpact || e.Likely != tt.wantLike {
				t.Errorf("ImpactHigh, Likely = %v, %v; want %v, %v", e.ImpactHigh, e.Likely, tt.wantImpact, tt.wantLike)
			}
		})
	}
}

func TestExplain_ImpactReasons(t *testing.T) {
	t.Run("dimensions at 60 or more, highest first, then the LLM reasoning", func(t *testing.T) {
		f := hot()
		got := Explain(f, repo(f), nil).ImpactReasons
		if len(got) < 1 || got[0] != "security 92 · availability 85 · user 85 · data 70" {
			t.Fatalf("first impact reason = %q", got)
		}
		contains(t, got, `"Core egress proxy."`, `"Bugs bypass network policy."`)
	})
	t.Run("dimension at 59 is left out, 60 kept", func(t *testing.T) {
		f := hot()
		f.Factors.BlastRadius.DataImpact = 59
		f.Factors.BlastRadius.UserImpact = 60
		got := Explain(f, repo(f), nil).ImpactReasons
		if got[0] != "security 92 · availability 85 · user 60" {
			t.Errorf("first impact reason = %q", got[0])
		}
	})
	t.Run("no dimension reaches 60", func(t *testing.T) {
		f := hot()
		f.Factors.BlastRadius = types.BlastRadius{Assessed: true, SecurityImpact: 40, DataImpact: 10}
		got := Explain(f, repo(f), nil).ImpactReasons
		contains(t, got, "no impact dimension reaches 60 (highest: security 40)")
	})
	t.Run("critical reason equal to summary is not repeated", func(t *testing.T) {
		f := hot()
		f.Factors.BlastRadius.CriticalReason = f.Factors.BlastRadius.Summary
		got := Explain(f, repo(f), nil).ImpactReasons
		if strings.Count(strings.Join(got, "\n"), "Core egress proxy.") != 1 {
			t.Errorf("summary repeated: %q", got)
		}
	})
	t.Run("not assessed says it is an estimate", func(t *testing.T) {
		f := hot()
		f.ImpactAssessed = false
		f.Impact = 20
		f.Factors.BlastRadius = types.BlastRadius{}
		got := Explain(f, repo(f), nil).ImpactReasons
		contains(t, got, "~20 is an estimate from centrality and path keywords; run analyze with an LLM to assess it")
		lacks(t, got, "security")
	})
}

func TestExplain_LikelihoodReasons(t *testing.T) {
	t.Run("hot file quotes numbers and percentiles", func(t *testing.T) {
		f := hot()
		got := Explain(f, repo(f), nil).LikelihoodReasons
		contains(t, got,
			"55 commits in the last 90 days, 28 authors overall (top 1% of this repo)",
			"479 functions, cyclomatic 1,092 in one file (top 1%)",
			"11 files import it (top 1%)",
			`recent fix: "fix(network): align destination checks" (2026-10-28)`,
		)
		lacks(t, got, "lines (top") // size is covered by complexity
	})
	t.Run("quiet file has no notable signal", func(t *testing.T) {
		f := &types.FileHeat{Path: "quiet.go"}
		got := Explain(f, repo(f), nil).LikelihoodReasons
		if len(got) != 1 || got[0] != "no notable churn, complexity, importers or fixes" {
			t.Errorf("got %q", got)
		}
	})
	t.Run("boundary: top 20% is notable, top 21% is not", func(t *testing.T) {
		// 100 files: the file and 19 others at 5 commits is top 20%.
		hm := &types.Heatmap{Files: map[string]*types.FileHeat{}}
		for i := 0; i < 100; i++ {
			g := &types.FileHeat{Path: fmt.Sprintf("f%d.go", i)}
			if i < 20 {
				g.Factors.ChangeFrequency.CommitsLast90d = 5
			} else if i == 20 {
				g.Factors.ChangeFrequency.CommitsLast90d = 4
			}
			hm.Files[g.Path] = g
		}
		s := BuildRepoStats(hm, nil)
		top20 := Explain(hm.Files["f0.go"], s, nil).LikelihoodReasons
		contains(t, top20, "5 commits in the last 90 days")
		top21 := Explain(hm.Files["f20.go"], s, nil).LikelihoodReasons
		lacks(t, top21, "commits in the last 90 days")
	})
	t.Run("several fixes, incidents and no tests", func(t *testing.T) {
		f := &types.FileHeat{Path: "a.go"}
		no := false
		f.Factors.TestCoverage.HasTestFile = &no
		last := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
		f.Factors.IncidentHistory = types.IncidentHistory{IncidentCount: 2, LastIncident: &last}
		f.RecentChanges = []types.Change{
			{Date: time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC), Message: "Fix crash on empty input"},
			{Date: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), Message: "fix: nil map"},
			{Date: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC), Message: "prefix the logger"}, // whole-word: not a fix
		}
		got := Explain(f, repo(f), nil).LikelihoodReasons
		contains(t, got,
			`2 recent fixes; latest: "Fix crash on empty input" (2026-10-02)`,
			"2 incidents recorded, last on 2026-09-01",
			"no tests found next to this file",
		)
	})
	t.Run("long fix message is truncated", func(t *testing.T) {
		f := &types.FileHeat{Path: "a.go", RecentChanges: []types.Change{{Message: "fix: " + strings.Repeat("x", 200)}}}
		got := Explain(f, repo(f), nil).LikelihoodReasons
		for _, r := range got {
			if strings.HasPrefix(r, "recent fix") && len(r) > 110 {
				t.Errorf("fix reason not truncated: %d chars", len(r))
			}
		}
	})
	t.Run("test file never reports missing tests", func(t *testing.T) {
		f := &types.FileHeat{Path: "a_test.go"}
		no := false
		f.Factors.TestCoverage.HasTestFile = &no
		lacks(t, Explain(f, repo(f), nil).LikelihoodReasons, "no tests found")
	})
	t.Run("counts of one are singular", func(t *testing.T) {
		hm := &types.Heatmap{Files: map[string]*types.FileHeat{}}
		for i := 0; i < 99; i++ {
			hm.Files[fmt.Sprintf("z%d.go", i)] = &types.FileHeat{Path: fmt.Sprintf("z%d.go", i)}
		}
		f := &types.FileHeat{Path: "one.go", ImpactAssessed: true, Quadrant: types.QuadrantWatch}
		f.Factors.ChangeFrequency = types.ChangeFrequency{CommitsLast90d: 1, UniqueAuthors: 1}
		f.Factors.Complexity = types.Complexity{Cyclomatic: 1, FunctionCount: 1}
		f.Factors.IncidentHistory.IncidentCount = 1
		f.Dependencies.ImportedBy = []types.Dependency{{Path: "x"}}
		hm.Files[f.Path] = f
		e := Explain(f, BuildRepoStats(hm, nil), nil)
		contains(t, e.LikelihoodReasons,
			"1 commit in the last 90 days, 1 author overall (top 1% of this repo)",
			"1 function, cyclomatic 1 in one file (top 1%)",
			"1 file imports it (top 1%)",
			"1 incident recorded",
		)
		var texts []string
		for _, a := range e.Actions {
			texts = append(texts, a.Text, a.Signal)
		}
		contains(t, texts, "cyclomatic 1 across 1 function")
		lacks(t, append(texts, e.LikelihoodReasons...), "1 authors", "1 commits", "1 functions", "1 files", "1 incidents")
	})
	t.Run("authors notable without churn; size without complexity", func(t *testing.T) {
		f := &types.FileHeat{Path: "a.go", Size: types.FileSize{Lines: 5000}}
		f.Factors.ChangeFrequency.UniqueAuthors = 9
		got := Explain(f, repo(f), nil).LikelihoodReasons
		contains(t, got, "9 authors overall (top 1% of this repo)", "5,000 lines in one file (top 1%)")
	})
}

func TestTitles(t *testing.T) {
	tests := []struct {
		name               string
		e                  types.Explanation
		assessed           bool
		wantImpact, wantLk string
	}{
		{"high and likely", types.Explanation{ImpactHigh: true, Likely: true}, true, "Break cost (HIGH)", "Break signals (HIGH)"},
		{"low and unlikely", types.Explanation{}, true, "Break cost (LOW)", "Break signals (LOW)"},
		{"not assessed", types.Explanation{ImpactHigh: true}, false, "Break cost (estimate)", "Break signals (LOW)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			i, l := Titles(tt.e, tt.assessed)
			if i != tt.wantImpact || l != tt.wantLk {
				t.Errorf("Titles = %q, %q; want %q, %q", i, l, tt.wantImpact, tt.wantLk)
			}
		})
	}
}

func TestThousands(t *testing.T) {
	tests := []struct {
		n    int
		want string
	}{
		{0, "0"}, {999, "999"}, {1000, "1,000"}, {43274, "43,274"}, {1234567, "1,234,567"}, {-1234, "-1,234"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := Thousands(tt.n); got != tt.want {
				t.Errorf("Thousands(%d) = %q, want %q", tt.n, got, tt.want)
			}
		})
	}
}

func TestCountAndImporters_UseThousands(t *testing.T) {
	tests := []struct{ got, want string }{
		{count(43274, "line"), "43,274 lines"},
		{count(1, "line"), "1 line"},
		{importedBy(1200), "1,200 files import it"},
	}
	for _, tt := range tests {
		if tt.got != tt.want {
			t.Errorf("got %q, want %q", tt.got, tt.want)
		}
	}
}
