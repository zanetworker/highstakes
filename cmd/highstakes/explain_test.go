package main

import (
	"strings"
	"testing"

	"github.com/zanetworker/highstakes/internal/types"
)

func TestExplanationBlock(t *testing.T) {
	e := &types.Explanation{
		Headline:          "FRAGILE AND CRITICAL (impact 92, likelihood 50; likely starts at 33 in this repo)",
		ImpactHigh:        true,
		Likely:            true,
		ImpactReasons:     []string{"security 92 · data 70", `"Core proxy."`},
		LikelihoodReasons: []string{"55 commits in the last 90 days, 28 authors overall (top 2% of this repo)"},
		Actions:           []types.Action{{ID: "codeowners", Text: "Add to CODEOWNERS; never auto-merge"}},
	}
	tests := []struct {
		name string
		f    *types.FileHeat
		want string
	}{
		{"full block", &types.FileHeat{ImpactAssessed: true, Explanation: e},
			"FRAGILE AND CRITICAL (impact 92, likelihood 50; likely starts at 33 in this repo)\n" +
				"Why impact is high: security 92 · data 70\n" +
				"  \"Core proxy.\"\n" +
				"Why it's likely to break:\n" +
				"  55 commits in the last 90 days, 28 authors overall (top 2% of this repo)\n" +
				"What to do:\n" +
				"  • Add to CODEOWNERS; never auto-merge\n"},
		{"no actions and no reasons", &types.FileHeat{Explanation: &types.Explanation{Headline: "LOW STAKES (impact 1, likelihood 1)"}},
			"LOW STAKES (impact 1, likelihood 1)\n"},
		{"old heatmap without explanation", &types.FileHeat{}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := explanationBlock(tt.f); got != tt.want {
				t.Errorf("got:\n%s\nwant:\n%s", got, tt.want)
			}
		})
	}
}

func TestTopActions(t *testing.T) {
	as := []types.Action{{Text: "a"}, {Text: "b"}, {Text: "c"}, {Text: "d"}}
	tests := []struct {
		name string
		f    *types.FileHeat
		n    int
		want string
	}{
		{"first three", &types.FileHeat{Explanation: &types.Explanation{Actions: as}}, 3, "a|b|c"},
		{"fewer than n", &types.FileHeat{Explanation: &types.Explanation{Actions: as[:1]}}, 3, "a"},
		{"no explanation", &types.FileHeat{}, 3, ""},
		{"zero", &types.FileHeat{Explanation: &types.Explanation{Actions: as}}, 0, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got []string
			for _, a := range topActions(tt.f, tt.n) {
				got = append(got, a.Text)
			}
			if strings.Join(got, "|") != tt.want {
				t.Errorf("got %v, want %s", got, tt.want)
			}
		})
	}
}

func TestAttachActions(t *testing.T) {
	acts := []types.Action{{ID: "a"}, {ID: "b"}, {ID: "c"}, {ID: "d"}}
	hm := &types.Heatmap{Files: map[string]*types.FileHeat{
		"crit.go": {Explanation: &types.Explanation{Actions: acts}},
		"high.go": {Explanation: &types.Explanation{Actions: acts[:2]}},
		"med.go":  {Explanation: &types.Explanation{Actions: acts}},
	}}
	risk := &types.PRRisk{FilesChanged: []types.FileChange{
		{Path: "crit.go", Tier: types.TierCritical},
		{Path: "high.go", Tier: types.TierHigh},
		{Path: "med.go", Tier: types.TierMedium},
		{Path: "new.go", Tier: types.TierHigh}, // not in heatmap
	}}
	attachActions(risk, hm)
	want := map[string]int{"crit.go": 3, "high.go": 2, "med.go": 0, "new.go": 0}
	for _, fc := range risk.FilesChanged {
		if len(fc.Actions) != want[fc.Path] {
			t.Errorf("%s: %d actions, want %d", fc.Path, len(fc.Actions), want[fc.Path])
		}
	}
}
