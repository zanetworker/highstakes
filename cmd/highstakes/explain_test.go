package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zanetworker/highstakes/internal/prscore"
	"github.com/zanetworker/highstakes/internal/types"
)

func TestExplanationBlock(t *testing.T) {
	e := &types.Explanation{
		Verdict:           "Harden now",
		ImpactHigh:        true,
		Likely:            true,
		ImpactReasons:     []string{"security 92 · data 70", `"Core proxy."`},
		LikelihoodReasons: []string{"55 commits in the last 90 days (top 2% of this repo)", "11 files import it (top 1%)"},
		Actions: []types.Action{
			{ID: "senior-review", Text: "Get sign-off from a senior owner (@ana)", Scope: types.ScopeBeforeMerge, Signal: "Harden now: likely to break and costly when it does"},
			{ID: "codeowners", Text: "Add a CODEOWNERS line", Scope: types.ScopeBacklog, Signal: "not covered by CODEOWNERS", Command: "/net/proxy.rs @ana"},
		},
	}
	fix := types.Change{Date: time.Date(2026, 10, 28, 0, 0, 0, 0, time.UTC), Message: "fix(network): align destination checks"}
	tests := []struct {
		name string
		f    *types.FileHeat
		want string
	}{
		{"full block", &types.FileHeat{Path: "net/proxy.rs", Likelihood: 50, Impact: 92, ImpactAssessed: true, Explanation: e,
			RecentChanges: []types.Change{{Message: "feat: x"}, fix}},
			"net/proxy.rs\n" +
				"HARDEN NOW\n" +
				"Will it break?         50  HIGH  repo cutoff 33\n" +
				"How bad if it breaks?  92  HIGH  repo cutoff 78\n" +
				"Scores are a relative signal within this repo, not a probability.\n" +
				"Will it break? is validated on bug-fix history; How bad if it breaks? is AI-assessed, not yet checked against maintainer ratings.\n" +
				"\n" +
				"Do before the next merge\n" +
				"  [ ] Get sign-off from a senior owner (@ana)\n" +
				"      why: Harden now: likely to break and costly when it does\n" +
				"Put on the backlog\n" +
				"  [ ] Add a CODEOWNERS line\n" +
				"      why: not covered by CODEOWNERS\n" +
				"      /net/proxy.rs @ana\n" +
				"\n" +
				"Why\n" +
				"  Break signals: 55 commits in the last 90 days (top 2% of this repo); 11 files import it (top 1%)\n" +
				"  Break cost: security 92 · data 70; \"Core proxy.\"\n" +
				"Latest fix  2026-10-28  fix(network): align destination checks\n"},
		{"unassessed impact is an estimate; no actions, no fix",
			&types.FileHeat{Path: "a.go", Likelihood: 10, Impact: 20, Explanation: &types.Explanation{Verdict: "Not assessed"}},
			"a.go\n" +
				"NOT ASSESSED\n" +
				"Will it break?         10  LOW   repo cutoff 33\n" +
				"How bad if it breaks?  ~20 estimate (run analyze with an LLM to assess)\n" +
				"Scores are a relative signal within this repo, not a probability.\n" +
				"Will it break? is validated on bug-fix history; How bad if it breaks? is AI-assessed, not yet checked against maintainer ratings.\n"},
		{"old heatmap without explanation", &types.FileHeat{}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := explanationBlock(tt.f, 33, 78); got != tt.want {
				t.Errorf("got:\n%s\nwant:\n%s", got, tt.want)
			}
		})
	}
}

func TestAttachPRActions(t *testing.T) {
	hard := []types.Action{
		{ID: "senior-review", Text: "Get sign-off from a senior owner (@ana)", Scope: types.ScopeBeforeMerge, Owners: []string{"@ana"}},
		{ID: "regression-tests", Text: "Add regression tests around the recent fix", Scope: types.ScopeBeforeMerge},
		{ID: "no-auto-merge", Text: "Keep auto-merge off", Scope: types.ScopeBeforeMerge},
		{ID: "codeowners", Text: "Add a CODEOWNERS line", Scope: types.ScopeBacklog, Command: "/net/proxy.rs @ana"},
	}
	hm := &types.Heatmap{Files: map[string]*types.FileHeat{
		"net/proxy.rs": {Path: "net/proxy.rs", Impact: 92, ImpactAssessed: true, Quadrant: types.QuadrantFragileCritical,
			Explanation: &types.Explanation{Actions: hard}},
		"ui/view.go": {Path: "ui/view.go", Quadrant: types.QuadrantLow, ImpactAssessed: true,
			Explanation: &types.Explanation{Actions: []types.Action{{ID: "light-review", Text: "Normal review", Scope: types.ScopeBeforeMerge}}}},
		"core/busy.go": {Path: "core/busy.go", Quadrant: types.QuadrantWatch, ImpactAssessed: true,
			Explanation: &types.Explanation{Actions: []types.Action{{ID: "refactor", Text: "Refactor the function you touch", Scope: types.ScopeBeforeMerge}}}},
	}}
	risk := &types.PRRisk{FilesChanged: []types.FileChange{
		{Path: "ui/view.go", Tier: types.TierLow},
		{Path: "net/proxy.rs", Tier: types.TierCritical},
		{Path: "net/proxy_tests.rs", Tier: types.TierLow},
		{Path: "net/new.rs", Tier: types.TierHigh, Unscored: true},
		{Path: "core/busy.go", Tier: types.TierMedium, Quadrant: types.QuadrantWatch},
	}}
	c := buildChange([]prscore.FileChange{
		{Path: "ui/view.go", LinesAdded: 3}, {Path: "net/proxy.rs", LinesAdded: 10, LinesDeleted: 2},
		{Path: "net/proxy_tests.rs", LinesAdded: 20}, {Path: "net/new.rs", LinesAdded: 5, IsNew: true},
	}, hm, []string{"ana"}, nil)
	if c.Lines != 40 {
		t.Errorf("lines = %d, want 40", c.Lines)
	}
	attachPRActions(risk, hm, c)

	var got []string
	for _, a := range risk.Checklist {
		got = append(got, a.ID+"="+map[bool]string{true: "done", false: "open"}[a.Done])
	}
	if strings.Join(got, ",") != "senior-review=done,regression-tests=done,no-auto-merge=open,owner-review=done" {
		t.Errorf("checklist = %v", got)
	}
	if len(risk.WorthDoing) != 1 || risk.WorthDoing[0].ID != "refactor" {
		t.Errorf("worth doing = %+v, want the Stabilize file's refactor only", risk.WorthDoing)
	}
	if len(risk.Backlog) != 1 || risk.Backlog[0].ID != "codeowners" {
		t.Errorf("backlog = %+v", risk.Backlog)
	}
	for _, fc := range risk.FilesChanged {
		switch fc.Path {
		case "net/proxy.rs":
			if len(fc.Actions) != 3 {
				t.Errorf("proxy.rs carries %d before-merge actions, want 3", len(fc.Actions))
			}
		case "core/busy.go":
			if len(fc.Actions) != 1 {
				t.Errorf("busy.go carries %d before-merge actions, want 1", len(fc.Actions))
			}
		case "net/new.rs": // unscored, inherits Protect: needs a review
			if len(fc.Actions) != 2 || fc.Actions[0].ID != "owner-review" || fc.Actions[1].ID != "no-auto-merge" {
				t.Errorf("new.rs actions = %+v, want an owner review and no auto-merge", fc.Actions)
			}
		default:
			if len(fc.Actions) != 0 {
				t.Errorf("%s: routine files and tests carry no actions: %+v", fc.Path, fc.Actions)
			}
		}
	}
}

func TestBuildChange_Components(t *testing.T) {
	hm := &types.Heatmap{Files: map[string]*types.FileHeat{
		"a/x.go": {Path: "a/x.go", Component: "a"},
		"b/y.go": {Path: "b/y.go", Component: "b"},
	}}
	tests := []struct {
		name  string
		paths []string
		want  int
	}{
		{"one component", []string{"a/x.go"}, 1},
		{"tests do not count", []string{"a/x.go", "b/y_test.go"}, 1},
		{"unscored files count by directory", []string{"a/x.go", "b/y.go", "c/new.go"}, 3},
		{"empty", nil, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var fcs []prscore.FileChange
			for _, p := range tt.paths {
				fcs = append(fcs, prscore.FileChange{Path: p})
			}
			if got := buildChange(fcs, hm, nil, nil).Components; got != tt.want {
				t.Errorf("components = %d, want %d", got, tt.want)
			}
		})
	}
}

// TestGet_LegacyHeatmapShowsActions: a heatmap.json written before actions
// had a scope still lists them under both headings after an upgrade.
func TestGet_LegacyHeatmapShowsActions(t *testing.T) {
	dir := t.TempDir()
	b, err := os.ReadFile(filepath.Join("..", "..", "pkg", "heatmap", "testdata", "legacy-heatmap.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, ".heatmap"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".heatmap", "heatmap.json"), b, 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := runCLI(t, dir, "get", "core/a.go")
	if err != nil {
		t.Fatalf("get: %v\n%s", err, out)
	}
	for _, want := range []string{"Do before the next merge", "Get sign-off from a senior owner", "An action from an older release",
		"Put on the backlog", "Add a CODEOWNERS line"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

func TestAttachPRActions_UnscoredCriticalFileBlocks(t *testing.T) {
	hm := &types.Heatmap{Files: map[string]*types.FileHeat{}}
	for _, tt := range []struct {
		name      string
		tier      types.Tier
		signedOff bool
		want      string
	}{
		{"new file inheriting Harden now needs a senior review", types.TierCritical, false, "senior-review=open,no-auto-merge=open"},
		{"new file inheriting Protect needs an owner review", types.TierHigh, false, "owner-review=open,no-auto-merge=open"},
		{"the sign-off label satisfies the review", types.TierCritical, true, "senior-review=done,no-auto-merge=open"},
		{"a routine new file adds nothing", types.TierLow, false, ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			risk := &types.PRRisk{FilesChanged: []types.FileChange{{Path: "core/new.go", Tier: tt.tier, Unscored: true, IsNew: true}}}
			c := buildChange([]prscore.FileChange{{Path: "core/new.go", LinesAdded: 5, IsNew: true}}, hm, nil, nil)
			c.SignedOff = tt.signedOff
			attachPRActions(risk, hm, c)
			var got []string
			for _, a := range risk.Checklist {
				got = append(got, a.ID+"="+map[bool]string{true: "done", false: "open"}[a.Done])
			}
			if strings.Join(got, ",") != tt.want {
				t.Errorf("checklist = %v, want %q", got, tt.want)
			}
		})
	}
}

func TestAttachPRActions_UnscoredReviewUsesDirectoryOwners(t *testing.T) {
	hm := &types.Heatmap{Files: map[string]*types.FileHeat{
		"core/old.go": {Path: "core/old.go", ImpactAssessed: true, Quadrant: types.QuadrantProtect,
			Explanation: &types.Explanation{Actions: []types.Action{{ID: "owner-review", Scope: types.ScopeBeforeMerge, Owners: []string{"@ana"}}}}},
	}}
	for _, tt := range []struct {
		name      string
		approvals []string
		want      string
	}{
		{"a directory owner's approval ticks it", []string{"ana"}, "owner-review=done,no-auto-merge=open"},
		{"someone else's approval does not", []string{"carl"}, "owner-review=open,no-auto-merge=open"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			risk := &types.PRRisk{FilesChanged: []types.FileChange{{Path: "core/new.go", Tier: types.TierHigh, Unscored: true, IsNew: true}}}
			c := buildChange([]prscore.FileChange{{Path: "core/new.go", LinesAdded: 5, IsNew: true}}, hm, tt.approvals, nil)
			attachPRActions(risk, hm, c)
			var got []string
			for _, a := range risk.Checklist {
				got = append(got, a.ID+"="+map[bool]string{true: "done", false: "open"}[a.Done])
			}
			if strings.Join(got, ",") != tt.want {
				t.Errorf("checklist = %v, want %q", got, tt.want)
			}
		})
	}
}

func TestUnscoredReview_KeepsAutoMergeOff(t *testing.T) {
	hm := &types.Heatmap{Files: map[string]*types.FileHeat{}}
	risk := &types.PRRisk{FilesChanged: []types.FileChange{{Path: "core/new.go", Tier: types.TierCritical, Unscored: true, IsNew: true}}}
	c := buildChange([]prscore.FileChange{{Path: "core/new.go", LinesAdded: 5, IsNew: true}}, hm, nil, nil)
	c.SignedOff, c.AutoMerge = true, "true" // explain.AutoMergeOn
	attachPRActions(risk, hm, c)
	var got []string
	for _, a := range risk.Checklist {
		got = append(got, a.ID+"="+map[bool]string{true: "done", false: "open"}[a.Done])
	}
	if strings.Join(got, ",") != "senior-review=done,no-auto-merge=open" {
		t.Errorf("checklist = %v: auto-merge on must keep a new Harden now file blocked", got)
	}
}
