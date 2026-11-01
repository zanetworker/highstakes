package prcomment

import (
	"fmt"
	"net/url"
	"strings"
	"testing"

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
			{ID: "no-auto-merge", Text: "Keep auto-merge off", Files: []string{"h.go", "p.go"}},
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
	tests := []struct {
		path, want string
	}{
		{"h.go", "🧪 add regression tests"}, // done items and the PR-wide no-auto-merge are skipped
		{"p.go", "👀 owner review"},
		{"s.go", "✂️ refactor"},
		{"c.go", "✂️ cut complexity"},
		{"none.go", "careful review"},
	}
	t.Run("routine file without actions", func(t *testing.T) {
		fc := types.FileChange{Path: "r.go", ImpactAssessed: true, Quadrant: types.QuadrantLow}
		if got := nextStep(fc, risk).label; got != "normal review" {
			t.Errorf("got %q", got)
		}
	})
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			fc := types.FileChange{Path: tt.path, ImpactAssessed: true, Quadrant: types.QuadrantProtect}
			if got := nextStep(fc, risk).label; got != tt.want {
				t.Errorf("nextStep(%s) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
	t.Run("only no-auto-merge left", func(t *testing.T) {
		r := &types.PRRisk{Checklist: []types.Action{{ID: "no-auto-merge", Files: []string{"p.go"}}}}
		if got := nextStep(types.FileChange{Path: "p.go"}, r).label; got != "🚫 no auto-merge" {
			t.Errorf("got %q", got)
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
		if u, ok := issueURL("o/r", "t", strings.Repeat("x", 2000)); ok {
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
		links := issueLinks(risk, ranked(risk.FilesChanged), hm, "o/r")
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
			for _, want := range []string{"File: `" + l.file + "`", "Why: ", "Done when: ", "Opened from the HighStakes PR comment"} {
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
		for _, l := range issueLinks(risk, ranked(risk.FilesChanged), hm, "o/r") {
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
		links := issueLinks(risk, ranked(risk.FilesChanged), hm, "o/r")
		if len(links) != 1 {
			t.Fatalf("links = %+v", links)
		}
		u, _ := url.QueryUnescape(links[0].url)
		if strings.Contains(u, "example.com") {
			t.Errorf("email leaked through URL encoding: %s", u)
		}
	})
	t.Run("without a repo there are plain-text items, no URLs", func(t *testing.T) {
		risk, hm := issuePR()
		for _, l := range issueLinks(risk, ranked(risk.FilesChanged), hm, "") {
			if l.url != "" {
				t.Errorf("url without a repo: %+v", l)
			}
		}
	})
}

func TestCodeownersAction(t *testing.T) {
	tests := []struct {
		name, cmd, repo, base, want string
	}{
		{"handles link to the edit page", "/core/p.go @ana @bo", "o/r", "main",
			"- [👥 Add `/core/p.go @ana @bo` to CODEOWNERS](https://github.com/o/r/edit/main/.github/CODEOWNERS)"},
		{"noreply becomes a handle", "/core/p.go 1+bo@users.noreply.github.com", "o/r", "main",
			"- [👥 Add `/core/p.go @bo` to CODEOWNERS](https://github.com/o/r/edit/main/.github/CODEOWNERS)"},
		{"private email: plain text", "/core/p.go ana@example.com", "o/r", "main",
			"- 👥 Add an `owners` alias in .heatmap/config.yaml, then a CODEOWNERS line for `core/p.go`"},
		{"no repo: plain text", "/core/p.go @ana", "", "main", "- 👥 Add `/core/p.go @ana` to CODEOWNERS"},
		{"no base branch: plain text", "/core/p.go @ana", "o/r", "", "- 👥 Add `/core/p.go @ana` to CODEOWNERS"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := types.Action{ID: "codeowners", Command: tt.cmd, Files: []string{"core/p.go"}}
			if got := codeownersAction(a, Links{Repo: tt.repo, Base: tt.base}); got != tt.want {
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
	risk := &types.PRRisk{LikelyCutoff: 33, ImpactThreshold: 78}
	files := []types.FileChange{
		{Path: "net/proxy.rs", Likelihood: 50, Impact: 92, ImpactAssessed: true, Quadrant: types.QuadrantFragileCritical},
		{Path: "net/policy.rs", Likelihood: 5, Impact: 80, ImpactAssessed: true, Quadrant: types.QuadrantProtect},
		{Path: "a/policy.rs", Likelihood: 100, Impact: 0, ImpactAssessed: true, Quadrant: types.QuadrantWatch},
		{Path: "x/we[ir]d:name;.go", Likelihood: 40, Impact: 10, ImpactAssessed: true, Quadrant: types.QuadrantWatch},
		{Path: "net/new.rs", Tier: types.TierHigh, Unscored: true}, // no scores to plot
		{Path: "ui/r.go", Likelihood: 3, Impact: 3, ImpactAssessed: true, Quadrant: types.QuadrantLow},
	}
	golden(t, "chart", quadrantChart(files, risk))

	t.Run("no non-routine files, no chart", func(t *testing.T) {
		if got := quadrantChart(files[5:], risk); got != "" {
			t.Errorf("chart for routine-only PR:\n%s", got)
		}
	})
	t.Run("every point lands in its posture's quadrant", func(t *testing.T) {
		got := quadrantChart([]types.FileChange{
			// unassessed: posture from the tier, so its scores can sit below the cutoffs
			{Path: "u.go", Tier: types.TierMedium, Likelihood: 20, Impact: 40},
			{Path: "at.go", Likelihood: 33, Impact: 78, ImpactAssessed: true, Quadrant: types.QuadrantFragileCritical},
			{Path: "p.go", Tier: types.TierHigh, Likelihood: 90, Impact: 10},
		}, risk)
		for _, want := range []string{"  u.go: [0.52, 0.26]\n", "  at.go: [0.52, 0.52]\n", "  p.go: [0.48, 0.52]\n"} {
			if !strings.Contains(got, want) {
				t.Errorf("missing %q in:\n%s", want, got)
			}
		}
	})
	t.Run("at most fifteen points", func(t *testing.T) {
		var many []types.FileChange
		for i := 0; i < 20; i++ {
			many = append(many, types.FileChange{Path: fmt.Sprintf("p/f%d.go", i), Likelihood: 50, Impact: 90, ImpactAssessed: true, Quadrant: types.QuadrantFragileCritical})
		}
		if n := strings.Count(quadrantChart(many, risk), ": ["); n != maxChartPoints {
			t.Errorf("%d points, want %d", n, maxChartPoints)
		}
	})
}
