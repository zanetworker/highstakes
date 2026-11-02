package calibration

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/zanetworker/highstakes/internal/prdata"
	"github.com/zanetworker/highstakes/internal/scorer"
	"github.com/zanetworker/highstakes/internal/types"
)

func labeled(n int, merged time.Time, files []string, set func(*prdata.LabeledPR)) prdata.LabeledPR {
	l := prdata.LabeledPR{PR: prdata.PR{Number: n, MergedAt: merged}, Files: files}
	if set != nil {
		set(&l)
	}
	return l
}

func TestFragilityEvents(t *testing.T) {
	in := base.Add(time.Hour)
	large := make([]string, MaxFilesPerEvent+1)
	for i := range large {
		large[i] = fmt.Sprintf("f%d.go", i)
	}
	prs := []prdata.LabeledPR{
		labeled(1, in, []string{"a.go", "b.go"}, func(l *prdata.LabeledPR) { l.BugFix = true }),
		labeled(2, in, []string{"a.go"}, func(l *prdata.LabeledPR) { l.Revert = true }),
		labeled(3, in, []string{"a.go"}, func(l *prdata.LabeledPR) { l.BugFix, l.Revert = true, true }), // counted once
		labeled(4, in, []string{"c.go"}, nil),                                                           // not fragility
		labeled(5, base, []string{"d.go"}, func(l *prdata.LabeledPR) { l.BugFix = true }),               // at From: outside (From, To]
		labeled(6, in, large, func(l *prdata.LabeledPR) { l.BugFix = true }),                            // too large
	}
	got := FragilityEvents(prs, base, base.AddDate(0, 0, 1))
	want := map[string]FileEvents{
		"a.go": {Total: 3, BugFix: 2, Revert: 2},
		"b.go": {Total: 1, BugFix: 1},
	}
	if len(got) != len(want) {
		t.Fatalf("FragilityEvents = %v files, want %v", len(got), len(want))
	}
	for p, w := range want {
		if got[p] == nil || *got[p] != w {
			t.Errorf("%s = %+v, want %+v", p, got[p], w)
		}
	}
}

func TestStakesCounts(t *testing.T) {
	prs := []prdata.LabeledPR{
		labeled(1, base, []string{"a.go", "b.go"}, func(l *prdata.LabeledPR) { l.Backported, l.DeepReview = true, true }),
		labeled(2, base, []string{"a.go"}, func(l *prdata.LabeledPR) { l.Severity, l.DeepReview = true, true }),
		labeled(3, base, []string{"c.go"}, func(l *prdata.LabeledPR) { l.FastRevert = true }),
		labeled(4, base, []string{"d.go"}, nil),
	}
	got := StakesCounts(prs)
	want := map[string]map[string]int{
		StakesBackported:  {"a.go": 1, "b.go": 1},
		StakesFastRevert:  {"c.go": 1},
		StakesSeverity:    {"a.go": 1},
		StakesReviewDepth: {"a.go": 2, "b.go": 1},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("StakesCounts =\n%v\nwant\n%v", got, want)
	}
}

func TestCombineStakes(t *testing.T) {
	got := CombineStakes([]StakesInput{
		{Name: "x", Available: true, Counts: map[string]int{"a.go": 3, "b.go": 1}},
		{Name: "y", Available: true, Counts: map[string]int{"a.go": 1, "c.go": 0}},
		{Name: "z", Available: false, Counts: map[string]int{"a.go": 1, "d.go": 1}}, // ignored
		{Name: StakesActivity, Available: true, Counts: map[string]int{"e.go": 4}},  // a reference, not a stakes signal
	})
	want := map[string]int{"a.go": 2, "b.go": 1}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("CombineStakes = %v, want %v", got, want)
	}
}

func stakesHeatmap() *types.Heatmap {
	hm := &types.Heatmap{Files: map[string]*types.FileHeat{}}
	for i, br := range []int{90, 80, 70, 60, 50, 40, 30, 20, 10, 5} {
		p := fmt.Sprintf("f%d.go", i)
		hm.Files[p] = &types.FileHeat{Path: p, Factors: types.Factors{BlastRadius: types.BlastRadius{Score: br, Assessed: true}}}
	}
	hm.Files["new.go"] = &types.FileHeat{Path: "new.go"} // not assessed: out of the population
	hm.Files["f_test.go"] = &types.FileHeat{Path: "f_test.go", Factors: types.Factors{BlastRadius: types.BlastRadius{Score: 99, Assessed: true}}}
	return hm
}

func TestRunStakes(t *testing.T) {
	inputs := []StakesInput{
		{Name: "perfect", Available: true, Counts: map[string]int{"f0.go": 3, "f1.go": 2, "f2.go": 1, "f_test.go": 9, "new.go": 9}},
		{Name: "none", Available: true, Counts: map[string]int{}},
		{Name: "missing", Available: false, Note: "no release branches"},
	}
	got := RunStakes(stakesHeatmap(), inputs, 3)
	if len(got) != 4 || got[3].Name != StakesCombined {
		t.Fatalf("RunStakes returned %+v, want 3 inputs plus combined", got)
	}

	p := got[0]
	if !p.Available || p.Files != 10 || p.FilesWithEvents != 3 || !p.TooFewEvents {
		t.Errorf("perfect header = %+v, want 10 assessed non-test files, 3 with events, too few", p)
	}
	if !near(p.PrecisionAtK, 1) || !near(p.Lift, 10.0/3) || p.Spearman == nil || *p.Spearman <= 0.7 {
		t.Errorf("perfect metrics = P@K %v lift %v rho %v", p.PrecisionAtK, p.Lift, p.Spearman)
	}

	if n := got[1]; !n.Available || n.FilesWithEvents != 0 || n.Spearman != nil || !n.TooFewEvents {
		t.Errorf("none = %+v, want available, no events, undefined Spearman, too few", n)
	}
	if m := got[2]; m.Available || m.Note != "no release branches" {
		t.Errorf("missing = %+v, want unavailable with its note", m)
	}
	if c := got[3]; !c.Available || c.FilesWithEvents != 3 || !strings.Contains(c.Note, "perfect") {
		t.Errorf("combined = %+v, want built from the available signals", c)
	}

	unassessed := &types.Heatmap{Files: map[string]*types.FileHeat{"a.go": {Path: "a.go"}}}
	for _, r := range RunStakes(unassessed, inputs, 3) {
		if r.Available {
			t.Errorf("%s available without an LLM assessment", r.Name)
		}
	}
}

func TestRunWithEvents(t *testing.T) {
	events := map[string]*FileEvents{"hot.go": {Total: 2, BugFix: 2}, "cold.go": {Total: 1, Revert: 1}}
	r := RunWithEvents(testHeatmap(), nil, nil, events, Options{
		K: 2, From: base, To: base.AddDate(0, 0, 30), Tiers: scorer.DefaultTierThresholds(), Source: SourcePRs,
	})
	if r.Source != SourcePRs || r.FilesWithEvents != 2 || r.Events.Total != 3 || r.Events.Revert != 1 {
		t.Errorf("report = source %q files with events %d events %+v", r.Source, r.FilesWithEvents, r.Events)
	}
	if !containsText(r.Caveats, "pull request") {
		t.Errorf("PR-sourced report should carry the PR caveat: %v", r.Caveats)
	}
	if def := Run(testHeatmap(), nil, nil, Options{K: 2, From: base, To: base.AddDate(0, 0, 30)}); def.Source != SourceCommits {
		t.Errorf("Run source = %q, want %q", def.Source, SourceCommits)
	}
}

func containsText(list []string, sub string) bool {
	for _, s := range list {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

func TestStakesInputs(t *testing.T) {
	hm := stakesHeatmap()
	prs := []prdata.LabeledPR{labeled(1, base, []string{"f0.go"}, func(l *prdata.LabeledPR) { l.Backported = true })}

	t.Run("no branches and no CODEOWNERS", func(t *testing.T) {
		got := StakesInputs(hm, prs, nil, nil, "")
		if len(got) != 6 || got[0].Name != StakesBackported || got[0].Available || got[4].Name != StakesCodeOwners || got[4].Available {
			t.Fatalf("got %+v, want backport and codeowners unavailable", got)
		}
		if got[1].Name != StakesFastRevert || !got[1].Available || !got[2].Available || !got[3].Available {
			t.Errorf("PR-based signals should be available: %+v", got[1:4])
		}
	})
	t.Run("activity reference counts every PR", func(t *testing.T) {
		all := append(append([]prdata.LabeledPR{}, prs...), labeled(2, base, []string{"f0.go", "f1.go"}, nil))
		got := StakesInputs(hm, all, nil, nil, "")
		ref := got[5]
		if ref.Name != StakesActivity || !ref.Available || !reflect.DeepEqual(ref.Counts, map[string]int{"f0.go": 2, "f1.go": 1}) {
			t.Errorf("activity = %+v, want every mapped PR counted", ref)
		}
	})
	t.Run("branches and CODEOWNERS", func(t *testing.T) {
		got := StakesInputs(hm, prs, []string{"origin/release-1"}, prdata.ParseCodeOwners("/f1.go @a\n"), ".github/CODEOWNERS")
		if !got[0].Available || got[0].Counts["f0.go"] != 1 {
			t.Errorf("backport = %+v", got[0])
		}
		if !got[4].Available || !reflect.DeepEqual(got[4].Counts, map[string]int{"f1.go": 1}) || got[4].Note != ".github/CODEOWNERS" {
			t.Errorf("codeowners = %+v", got[4])
		}
	})
}

func TestSummarize(t *testing.T) {
	fetch := prdata.FetchResult{PRs: make([]prdata.PR, 4)}
	prs := []prdata.LabeledPR{
		labeled(1, base, nil, func(l *prdata.LabeledPR) { l.BugFix, l.DeepReview = true, true }),
		labeled(2, base, nil, func(l *prdata.LabeledPR) { l.Revert, l.FastRevert, l.Severity, l.Backported = true, true, true, true }),
	}
	got := Summarize("o/n", fetch, prs, nil, "")
	want := PRSummary{Repo: "o/n", Fetch: fetch, InWindow: 4, Mapped: 2, BugFix: 1, Revert: 1, Backported: 1, FastRevert: 1, Severity: 1, DeepReview: 1, BackportBranches: []string{}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Summarize =\n%+v\nwant\n%+v", got, want)
	}
}
