package prcomment

import (
	"fmt"
	"math"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/zanetworker/highstakes/internal/types"
)

func TestPostureCounts(t *testing.T) {
	fc := func(q types.Quadrant) types.FileChange {
		return types.FileChange{Path: "x", ImpactAssessed: true, Quadrant: q, Tier: types.TierLow}
	}
	tests := []struct {
		name  string
		files []types.FileChange
		want  string
	}{
		{"every posture, most severe first", []types.FileChange{fc(types.QuadrantLow), fc(types.QuadrantWatch), fc(types.QuadrantProtect),
			fc(types.QuadrantFragileCritical), fc(types.QuadrantLow)}, "⛔ 1 harden · 🛡 1 protect · 🔧 1 stabilize · ✅ 2 routine"},
		{"zero counts are left out", []types.FileChange{fc(types.QuadrantProtect), fc(types.QuadrantLow)}, "🛡 1 protect · ✅ 1 routine"},
		{"no files", nil, ""},
		{"thousands separators", slices.Repeat([]types.FileChange{fc(types.QuadrantLow)}, 1200), "✅ 1,200 routine"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := postureCounts(tt.files); got != tt.want {
				t.Errorf("postureCounts = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestReviewText(t *testing.T) {
	tests := []struct {
		name string
		a    types.Action
		want string
	}{
		{"handles name the reviewers", types.Action{ID: "owner-review", Owners: []string{"@a", "@b"}, Files: []string{"x.go"}},
			"Request a review from @a, @b on `x.go`"},
		{"noreply addresses become handles", types.Action{ID: "owner-review", Owners: []string{"7+bo@users.noreply.github.com"}, Files: []string{"x.go", "y.go"}},
			"Request a review from @bo on `x.go`, `y.go`"},
		{"private emails are never named", types.Action{ID: "owner-review", Owners: []string{"ana@example.com"}, Files: []string{"x.go"}},
			"Request a review from someone who knows `x.go`"},
		{"no owners known", types.Action{ID: "owner-review", Files: []string{"x.go"}}, "Request a review from someone who knows `x.go`"},
		{"senior sign-off", types.Action{ID: "senior-review", Owners: []string{"ana@example.com", "@carl"}, Files: []string{"x.go"}},
			"Get sign-off from @carl on `x.go`"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := reviewText(tt.a); got != tt.want {
				t.Errorf("reviewText = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBreaksOften(t *testing.T) {
	reasons := func(r ...string) *types.Heatmap {
		return &types.Heatmap{Files: map[string]*types.FileHeat{"x.go": {Path: "x.go", Explanation: &types.Explanation{LikelihoodReasons: r}}}}
	}
	tests := []struct {
		name string
		fc   types.FileChange
		hm   *types.Heatmap
		want string
	}{
		{"below the cutoff", types.FileChange{Path: "x.go", Likelihood: 10}, reasons("13 commits"), "🟢 rarely"},
		{"churn in a few words", types.FileChange{Path: "x.go", Likelihood: 50},
			reasons("13 commits in the last 90 days, 4 authors overall (top 2% of this repo)", "11 files import it (top 1%)"), "🔴 13 commits in the last 90 days"},
		{"fixes are the strongest signal", types.FileChange{Path: "x.go", Likelihood: 50},
			reasons("13 commits in the last 90 days, 4 authors overall (top 2% of this repo)", `5 recent fixes; latest: "fix: x" (2026-10-01)`), "🔴 5 recent fixes"},
		{"one fix", types.FileChange{Path: "x.go", Likelihood: 50}, reasons("40 functions, cyclomatic 90 in one file (top 3%)", `recent fix: "fix: y" (2026-10-01)`), "🔴 1 recent fix"},
		{"complexity keeps its words", types.FileChange{Path: "x.go", Likelihood: 50}, reasons("40 functions, cyclomatic 90 in one file (top 3% of this repo)"), "🔴 40 functions, cyclomatic 90 in one file"},
		{"no reasons", types.FileChange{Path: "x.go", Likelihood: 50}, &types.Heatmap{}, "🔴 yes"},
		{"new file", types.FileChange{Path: "x.go", Unscored: true}, &types.Heatmap{}, "🆕 new file"},
		{"at the cutoff counts", types.FileChange{Path: "x.go", Likelihood: 33}, reasons("2 incidents recorded, last on 2026-01-02"), "🔴 2 incidents recorded"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := breaksOften(tt.fc, tt.hm, 33); got != tt.want {
				t.Errorf("breaksOften = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNextStep(t *testing.T) {
	risk := &types.PRRisk{
		Checklist: []types.Action{
			{ID: "no-auto-merge", Text: "Keep auto-merge off", Files: []string{"h.go", "p.go", "only.go"}},
			{ID: "smaller-pr", Text: "Ask for a smaller PR: 900 lines across 4 components", Files: []string{"h.go", "only.go"}},
			{ID: "senior-review", Text: "Get sign-off", Files: []string{"h.go"}, Done: true},
			{ID: "regression-tests", Text: "Add regression tests", Files: []string{"h.go"}},
			{ID: "owner-review", Text: "Get an owner's review", Files: []string{"p.go"}},
		},
		WorthDoing: []types.Action{{ID: "refactor", Text: "Refactor", Files: []string{"s.go"}}},
		Backlog: []types.Action{
			{ID: "split", Text: "Split it", Files: []string{"h.go"}},
			{ID: "complexity-issue", Text: "Cut it", Files: []string{"c.go"}},
		},
	}
	hm := &types.Heatmap{Files: map[string]*types.FileHeat{
		"fixed.go": {Path: "fixed.go", Explanation: &types.Explanation{LikelihoodReasons: []string{`2 recent fixes; latest: "fix: x" (2026-10-01)`}}},
	}}
	tests := []struct {
		name, path string
		q          types.Quadrant
		want       string
	}{
		{"done items and PR-level items are skipped", "h.go", types.QuadrantFragileCritical, "🧪 add regression tests"},
		{"before-merge item", "p.go", types.QuadrantProtect, "👀 owner review"},
		{"worth-doing item", "s.go", types.QuadrantWatch, "✂️ refactor"},
		{"backlog item", "c.go", types.QuadrantWatch, "✂️ cut complexity"},
		{"only PR-level items: posture default", "only.go", types.QuadrantProtect, "👀 owner review"},
		{"no actions, Protect", "none.go", types.QuadrantProtect, "👀 owner review"},
		{"no actions, Harden now", "none.go", types.QuadrantFragileCritical, "👀 senior sign-off"},
		{"no actions, Stabilize with fixes", "fixed.go", types.QuadrantWatch, "🧪 regression tests"},
		{"no actions, Stabilize without fixes", "none.go", types.QuadrantWatch, "🔍 review the change"},
		{"routine file without actions", "r.go", types.QuadrantLow, "normal review"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fc := types.FileChange{Path: tt.path, ImpactAssessed: true, Quadrant: tt.q}
			if got := nextStep(fc, risk, hm).label; got != tt.want {
				t.Errorf("nextStep(%s) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
	t.Run("never the vague careful review", func(t *testing.T) {
		for _, tier := range []types.Tier{types.TierCritical, types.TierHigh, types.TierMedium} {
			fc := types.FileChange{Path: "new.go", Tier: tier, Unscored: true}
			if got := nextStep(fc, &types.PRRisk{}, &types.Heatmap{}).label; got == "careful review" {
				t.Errorf("tier %s: careful review", tier)
			}
		}
	})
}

func TestIssueURL(t *testing.T) {
	t.Run("title and body are encoded and decode back", func(t *testing.T) {
		u, ok := issueURL("o/r", "Split a b&c.go", "File: `a b&c.go`\nWhy: x")
		if !ok {
			t.Fatal("short URL rejected")
		}
		if !strings.HasPrefix(u, "https://github.com/o/r/issues/new?") {
			t.Errorf("url = %s", u)
		}
		q, err := url.Parse(u)
		if err != nil {
			t.Fatal(err)
		}
		if q.Query().Get("title") != "Split a b&c.go" || q.Query().Get("body") != "File: `a b&c.go`\nWhy: x" {
			t.Errorf("round trip failed: %v", q.Query())
		}
	})
	t.Run("too long for a link", func(t *testing.T) {
		if u, ok := issueURL("o/r", "t", strings.Repeat("x", maxURL)); ok {
			t.Errorf("%d-char URL accepted", len(u))
		}
	})
	t.Run("no repo, no link", func(t *testing.T) {
		if _, ok := issueURL("", "t", "b"); ok {
			t.Error("link without a repo")
		}
	})
}

func issuePR() (*types.PRRisk, *types.Heatmap) {
	hm := &types.Heatmap{Files: map[string]*types.FileHeat{}}
	risk := &types.PRRisk{Verdict: "Stabilize", LikelyCutoff: 33, ImpactThreshold: 78}
	for i := 0; i < 7; i++ {
		p := fmt.Sprintf("pkg/s%d.go", i)
		hm.Files[p] = &types.FileHeat{Path: p}
		risk.FilesChanged = append(risk.FilesChanged, types.FileChange{Path: p, Tier: types.TierMedium,
			Likelihood: 60 - i, Impact: 10, ImpactAssessed: true, Quadrant: types.QuadrantWatch})
		risk.Backlog = append(risk.Backlog, types.Action{ID: "complexity-issue", Text: "Open an issue to cut its complexity",
			Signal: "cyclomatic 90 (top 1%)", Files: []string{p}})
	}
	risk.Backlog = append(risk.Backlog, types.Action{ID: "ownership-issue", Text: "Spread ownership", Signal: "ana@example.com wrote 90% of it", Files: []string{"pkg/s0.go"}})
	risk.WorthDoing = []types.Action{{ID: "regression-tests", Text: "Add regression tests", Signal: "2 recent fixes", Files: []string{"pkg/s1.go"}}}
	return risk, hm
}

func TestIssueLinks(t *testing.T) {
	t.Run("at most five, riskiest files first, only agent-shaped items", func(t *testing.T) {
		risk, hm := issuePR()
		links := issueLinks(risk, ranked(risk.FilesChanged), hm, Links{Repo: "o/r"})
		if len(links) != maxLinks {
			t.Fatalf("%d links, want %d", len(links), maxLinks)
		}
		if links[0].file != "pkg/s0.go" || links[0].title != "Cut the cyclomatic complexity of pkg/s0.go" {
			t.Errorf("first link = %+v", links[0])
		}
		if links[1].file != "pkg/s1.go" || links[1].title != "Add regression tests for the recent fixes to pkg/s1.go" {
			t.Errorf("worth-doing regression tests should come first for s1: %+v", links[1])
		}
		for _, l := range links {
			if strings.Contains(l.title, "ownership") {
				t.Errorf("ownership needs a person, not an issue: %+v", l)
			}
			q, err := url.Parse(l.url)
			if err != nil || len(l.url) > maxURL {
				t.Fatalf("bad url %q (%v)", l.url, err)
			}
			body := q.Query().Get("body")
			for _, want := range []string{"## Why it matters\n", "## Why now\n", "## Suggested approach\n1. ", "## Done when\n- [ ] ",
				"- [ ] tests pass\n", "## Context\n", "_Opened from the HighStakes PR comment_"} {
				if !strings.Contains(body, want) {
					t.Errorf("body missing %q:\n%s", want, body)
				}
			}
		}
	})
	t.Run("one link per file before a second one for any file", func(t *testing.T) {
		risk, hm := issuePR()
		risk.FilesChanged = risk.FilesChanged[:2]
		risk.Backlog = []types.Action{
			{ID: "split", Signal: "x", Files: []string{"pkg/s0.go"}},
			{ID: "test-gap", Signal: "y", Files: []string{"pkg/s0.go"}},
			{ID: "split", Signal: "z", Files: []string{"pkg/s1.go"}},
		}
		risk.WorthDoing = nil
		var got []string
		for _, l := range issueLinks(risk, ranked(risk.FilesChanged), hm, Links{Repo: "o/r"}) {
			got = append(got, l.title)
		}
		want := "Split pkg/s0.go into smaller files|Split pkg/s1.go into smaller files|Add tests next to pkg/s0.go"
		if strings.Join(got, "|") != want {
			t.Errorf("links = %v, want %s", got, want)
		}
	})
	t.Run("emails are scrubbed before encoding", func(t *testing.T) {
		risk, hm := issuePR()
		risk.Backlog = []types.Action{{ID: "document-invariants", Text: "Document", Signal: "ana@example.com wrote 90% of it", Files: []string{"pkg/s0.go"}}}
		risk.WorthDoing = nil
		links := issueLinks(risk, ranked(risk.FilesChanged), hm, Links{Repo: "o/r"})
		if len(links) != 1 {
			t.Fatalf("links = %+v", links)
		}
		u, _ := url.QueryUnescape(links[0].url)
		if strings.Contains(u, "example.com") {
			t.Errorf("email leaked through URL encoding: %s", u)
		}
	})
	t.Run("the brief links back to the PR and commit and names handles only", func(t *testing.T) {
		risk, hm := issuePR()
		risk.PRNumber = 12
		risk.Checklist = []types.Action{{ID: "owner-review", Owners: []string{"ana@example.com", "7+bo@users.noreply.github.com"}, Files: []string{"pkg/s0.go"}}}
		risk.SuggestedReviewers = []types.Reviewer{{Handle: "carl@example.com", Files: []string{"pkg/s2.go"}}}
		fc := risk.FilesChanged[0]
		fc.Autonomy = &types.Autonomy{Level: 2, Label: "auto-fix, human merges"}
		risk.FilesChanged[0] = fc
		links := issueLinks(risk, ranked(risk.FilesChanged), hm, Links{Repo: "o/r", SHA: "6ef6978"})
		body := func(i int) string {
			q, err := url.Parse(links[i].url)
			if err != nil {
				t.Fatal(err)
			}
			return q.Query().Get("body")
		}
		for _, want := range []string{"- Review by: @bo\n", "- From: [PR #12](https://github.com/o/r/pull/12)\n", "- Commit: `6ef6978`\n",
			"- AI autonomy: L2 auto-fix, human merges: agents may fix and open a PR; a human merges\n"} {
			if !strings.Contains(body(0), want) {
				t.Errorf("missing %q in:\n%s", want, body(0))
			}
		}
		if b := body(2); !strings.Contains(b, "- Review by: someone who knows the file\n") || strings.Contains(b, "example.com") {
			t.Errorf("private email reviewer should read as someone who knows the file:\n%s", b)
		}
		noPR := issueLinks(&types.PRRisk{FilesChanged: risk.FilesChanged, Backlog: risk.Backlog}, ranked(risk.FilesChanged), hm, Links{Repo: "o/r"})
		if u, _ := url.QueryUnescape(noPR[0].url); strings.Contains(u, "PR #") || strings.Contains(u, "Commit:") {
			t.Errorf("PR link or commit without a PR number or SHA:\n%s", u)
		}
	})
	t.Run("a huge brief is trimmed to fit the URL, never dropped or broken", func(t *testing.T) {
		risk, hm := issuePR()
		f := hm.Files["pkg/s0.go"]
		f.ImpactAssessed = true
		f.Factors.BlastRadius = types.BlastRadius{Assessed: true, Summary: strings.Repeat("Huge consequence. ", 600), CriticalReason: strings.Repeat("ünïcode reason ", 400)}
		var reasons []string
		for i := 0; i < 200; i++ {
			reasons = append(reasons, fmt.Sprintf("signal %d by ana@example.com", i))
		}
		f.Explanation = &types.Explanation{LikelihoodReasons: reasons}
		l := issueLinks(risk, ranked(risk.FilesChanged), hm, Links{Repo: "o/r"})[0]
		if l.url == "" || len(l.url) > maxURL {
			t.Fatalf("url length %d, want 1..%d", len(l.url), maxURL)
		}
		q, err := url.Parse(l.url)
		if err != nil {
			t.Fatal(err)
		}
		body := q.Query().Get("body")
		if !utf8.ValidString(body) {
			t.Error("trimming broke a UTF-8 character")
		}
		for _, want := range []string{"## Why it matters\n- Huge consequence.", "## Done when\n", "_Opened from the HighStakes PR comment_"} {
			if !strings.Contains(body, want) {
				t.Errorf("trimmed body lost %q:\n%s", want, body)
			}
		}
		if strings.Contains(body, "example.com") {
			t.Errorf("email in the issue body:\n%s", body)
		}
	})
	t.Run("without a repo there are plain-text items, no URLs", func(t *testing.T) {
		risk, hm := issuePR()
		for _, l := range issueLinks(risk, ranked(risk.FilesChanged), hm, Links{}) {
			if l.url != "" {
				t.Errorf("url without a repo: %+v", l)
			}
		}
	})
}

func TestCodeownersAction(t *testing.T) {
	tests := []struct {
		name, cmd, repo, base, codeowners, want string
	}{
		{"handles link to the edit page", "/core/p.go @ana @bo", "o/r", "main", ".github/CODEOWNERS",
			"- [👥 Add `/core/p.go @ana @bo` to CODEOWNERS](https://github.com/o/r/edit/main/.github/CODEOWNERS)"},
		{"noreply becomes a handle", "/core/p.go 1+bo@users.noreply.github.com", "o/r", "main", ".github/CODEOWNERS",
			"- [👥 Add `/core/p.go @bo` to CODEOWNERS](https://github.com/o/r/edit/main/.github/CODEOWNERS)"},
		{"private email: plain text", "/core/p.go ana@example.com", "o/r", "main", ".github/CODEOWNERS",
			"- 👥 Add an `owners` alias in .heatmap/config.yaml, then a CODEOWNERS line for `core/p.go`"},
		{"no repo: plain text", "/core/p.go @ana", "", "main", ".github/CODEOWNERS", "- 👥 Add `/core/p.go @ana` to CODEOWNERS"},
		{"no base branch: plain text", "/core/p.go @ana", "o/r", "", ".github/CODEOWNERS", "- 👥 Add `/core/p.go @ana` to CODEOWNERS"},
		{"CODEOWNERS at the repo root", "/core/p.go @ana", "o/r", "main", "CODEOWNERS",
			"- [👥 Add `/core/p.go @ana` to CODEOWNERS](https://github.com/o/r/edit/main/CODEOWNERS)"},
		{"CODEOWNERS under docs", "/core/p.go @ana", "o/r", "main", "docs/CODEOWNERS",
			"- [👥 Add `/core/p.go @ana` to CODEOWNERS](https://github.com/o/r/edit/main/docs/CODEOWNERS)"},
		{"no CODEOWNERS yet: create it", "/core/p.go @ana", "o/r", "main", "",
			"- [👥 Create CODEOWNERS with `/core/p.go @ana`](https://github.com/o/r/new/main?filename=.github%2FCODEOWNERS&value=%2Fcore%2Fp.go+%40ana%0A)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := types.Action{ID: "codeowners", Command: tt.cmd, Files: []string{"core/p.go"}}
			if got := codeownersAction(a, Links{Repo: tt.repo, Base: tt.base, Codeowners: tt.codeowners}); got != tt.want {
				t.Errorf("got  %q\nwant %q", got, tt.want)
			}
		})
	}
}

func TestBreakerLine(t *testing.T) {
	risk := &types.PRRisk{FilesChanged: []types.FileChange{{Path: "a.go", LinesAdded: 1000, LinesDeleted: 234}}}
	tests := []struct {
		signal, want string
	}{
		{"Large diff (>500 lines)", "✂️ Large diff (1,234 lines): consider splitting this PR"},
		{"Unscored new file(s), heat inherited from directory: a/new.go, b/n.go",
			"🆕 Unscored new file(s), treated like their directory until the next analyze scores them: a/new.go, b/n.go"},
		{"Low test ratio (<0.3)", "🧪 Low test ratio (<0.3 test lines per code line added): add tests"},
		{"Many languages (>3)", "⚠️ Many languages (>3)"},
	}
	for _, tt := range tests {
		t.Run(tt.signal, func(t *testing.T) {
			if got := breakerLine(tt.signal, risk); got != tt.want {
				t.Errorf("got  %q\nwant %q", got, tt.want)
			}
		})
	}
}

func TestQuadrantChart(t *testing.T) {
	risk := &types.PRRisk{LikelyCutoff: 33, ImpactThreshold: 78,
		Checklist:  []types.Action{{ID: "senior-review", Files: []string{"net/proxy.rs"}}},
		WorthDoing: []types.Action{{ID: "refactor", Files: []string{"a/policy.rs"}}}}
	files := []types.FileChange{
		{Path: "net/proxy.rs", Likelihood: 50, Impact: 92, ImpactAssessed: true, Quadrant: types.QuadrantFragileCritical},
		// on the impact cutoff: plotted inside Harden now, not on the border
		{Path: "pkg/heatmap.go", Likelihood: 34, Impact: 78, ImpactAssessed: true, Quadrant: types.QuadrantFragileCritical},
		{Path: "net/policy.rs", Likelihood: 5, Impact: 80, ImpactAssessed: true, Quadrant: types.QuadrantProtect},
		{Path: "a/policy.rs", Likelihood: 100, Impact: 0, ImpactAssessed: true, Quadrant: types.QuadrantWatch},
		{Path: "x/we[ir]d:name;.go", Likelihood: 40, Impact: 10, ImpactAssessed: true, Quadrant: types.QuadrantWatch},
		{Path: "net/new.rs", Tier: types.TierHigh, Unscored: true}, // no scores to plot
		{Path: "ui/r.go", Likelihood: 3, Impact: 3, ImpactAssessed: true, Quadrant: types.QuadrantLow},
	}
	golden(t, "chart", quadrantChart(ranked(files), risk, &types.Heatmap{}))

	t.Run("no non-routine files, no chart", func(t *testing.T) {
		if got := quadrantChart(files[6:], risk, &types.Heatmap{}); got != "" {
			t.Errorf("chart for routine-only PR:\n%s", got)
		}
	})
	t.Run("every point sits inside its posture's quadrant, off the borders", func(t *testing.T) {
		in := []types.FileChange{
			// unassessed: posture from the tier, so its scores can sit below the cutoffs
			{Path: "u.go", Tier: types.TierMedium, Likelihood: 20, Impact: 40},
			{Path: "at.go", Likelihood: 33, Impact: 78, ImpactAssessed: true, Quadrant: types.QuadrantFragileCritical},
			{Path: "p.go", Tier: types.TierHigh, Likelihood: 90, Impact: 10},
		}
		got := points(t, quadrantChart(in, risk, &types.Heatmap{}))
		want := map[string][2]bool{"1": {true, false}, "2": {true, true}, "3": {false, true}} // label: high x, high y
		for label, hi := range want {
			p, ok := got[label]
			if !ok {
				t.Fatalf("no point %s in %v", label, got)
			}
			for axis, v := range p {
				lo, up := 0.1, 0.4
				if hi[axis] {
					lo, up = 0.6, 0.9
				}
				if v < lo || v > up {
					t.Errorf("point %s axis %d = %.2f, want within [%.1f, %.1f]", label, axis, v, lo, up)
				}
			}
		}
	})
	t.Run("points never crowd, even when scores tie or correlate", func(t *testing.T) {
		for name, score := range map[string]func(i int) (int, int){
			"ties":       func(int) (int, int) { return 50, 90 },
			"correlated": func(i int) (int, int) { return 40 + i, 20 + i },
		} {
			var fs []types.FileChange
			for i := 0; i < maxChartPoints; i++ {
				l, im := score(i)
				fs = append(fs, types.FileChange{Path: fmt.Sprintf("s%02d.go", i), Tier: types.TierMedium, Likelihood: l, Impact: im})
			}
			var ps [][2]float64
			for _, p := range points(t, quadrantChart(fs, risk, &types.Heatmap{})) {
				ps = append(ps, p)
			}
			for i := range ps {
				for j := i + 1; j < len(ps); j++ {
					if d := math.Hypot(ps[i][0]-ps[j][0], ps[i][1]-ps[j][1]); d < 0.05 {
						t.Errorf("%s: points %v and %v are %.3f apart, want at least 0.05", name, ps[i], ps[j], d)
					}
				}
			}
		}
	})
	t.Run("at most fifteen points, each with a legend line", func(t *testing.T) {
		var many []types.FileChange
		for i := 0; i < 20; i++ {
			many = append(many, types.FileChange{Path: fmt.Sprintf("p/f%d.go", i), Likelihood: 50, Impact: 90, ImpactAssessed: true, Quadrant: types.QuadrantFragileCritical})
		}
		out := quadrantChart(many, risk, &types.Heatmap{})
		if n := len(points(t, out)); n != maxChartPoints {
			t.Errorf("%d points, want %d", n, maxChartPoints)
		}
		if !strings.Contains(out, "15. ⛔ `p/f14.go`: senior sign-off\n") || strings.Contains(out, "16. ") {
			t.Errorf("legend should list exactly the fifteen points:\n%s", out)
		}
	})
}

var pointRe = regexp.MustCompile(`(?m)^  (\S+): \[([0-9.]+), ([0-9.]+)\]$`)

// points parses a chart's points by label.
func points(t *testing.T, chart string) map[string][2]float64 {
	t.Helper()
	out := map[string][2]float64{}
	for _, m := range pointRe.FindAllStringSubmatch(chart, -1) {
		x, _ := strconv.ParseFloat(m[2], 64)
		y, _ := strconv.ParseFloat(m[3], 64)
		out[m[1]] = [2]float64{x, y}
	}
	return out
}

func TestOneClick_SkipsDoneActions(t *testing.T) {
	risk := &types.PRRisk{Tier: types.TierMedium, Verdict: "Stabilize", LikelyCutoff: 33, ImpactThreshold: 78,
		FilesChanged: []types.FileChange{{Path: "a/x.go", Tier: types.TierMedium, Likelihood: 50, Impact: 10, ImpactAssessed: true, Quadrant: types.QuadrantWatch}},
		WorthDoing: []types.Action{{ID: "regression-tests", Text: "Add regression tests", Files: []string{"a/x.go"}, Signal: "2 recent fixes", Done: true}},
		Backlog:    []types.Action{{ID: "codeowners", Text: "Add a CODEOWNERS line", Files: []string{"a/x.go"}, Command: "/a/x.go @ana", Done: true}},
	}
	out := Render(risk, &types.Heatmap{Files: map[string]*types.FileHeat{}}, Links{Repo: "o/r", Base: "main", Codeowners: ".github/CODEOWNERS"})
	if strings.Contains(out, "Open issue: Add regression tests") || strings.Contains(out, "to CODEOWNERS](") {
		t.Errorf("one-click offers work the PR already did:\n%s", out)
	}
}
