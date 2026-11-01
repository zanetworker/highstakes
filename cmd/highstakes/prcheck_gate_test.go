package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zanetworker/highstakes/internal/prdata"
	"github.com/zanetworker/highstakes/internal/types"
)

func TestApplyGate(t *testing.T) {
	open := []types.Action{{ID: "senior-review"}, {ID: "no-tests", Done: true}}
	allDone := []types.Action{{ID: "senior-review", Done: true}, {ID: "no-tests", Done: true}}
	tests := []struct {
		name         string
		tier         types.Tier
		checklist    []types.Action
		failOn       string
		shadow       bool
		strict       bool
		wantDecision string
		wantReason   string
		wantOpen     int
		wantFailOn   types.Tier
		wantCode     int // 0 = no error
	}{
		{"no gate leaves decision empty", types.TierCritical, open, "", false, false, "", "", 1, "", 0},
		{"gate passes below threshold", types.TierHigh, open, "critical", false, false, "pass", "tier high is below --fail-on critical", 1, types.TierCritical, 0},
		{"gate blocks at threshold with one item open, exit 5", types.TierCritical, open, "critical", false, false, "block", "1 before-merge item open", 1, types.TierCritical, 5},
		{"gate passes at threshold once every item is done", types.TierCritical, allDone, "critical", false, false, "pass", "all before-merge items done", 0, types.TierCritical, 0},
		{"strict blocks on tier alone even when all done", types.TierCritical, allDone, "critical", false, true, "block", "tier critical is at or above --fail-on critical (--strict)", 0, types.TierCritical, 5},
		{"shadow defaults gate to critical and never fails", types.TierCritical, open, "", true, false, "block", "1 before-merge item open", 1, types.TierCritical, 0},
		{"shadow honours explicit gate", types.TierHigh, open, "high", true, false, "block", "1 before-merge item open", 1, types.TierHigh, 0},
		{"invalid tier is exit 2 with valid values", types.TierLow, nil, "severe", false, false, "", "", 0, "", 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			risk := &types.PRRisk{Tier: tt.tier, Checklist: tt.checklist}
			err := applyGate(risk, tt.failOn, tt.shadow, tt.strict)
			code := 0
			var ee *exitError
			if errors.As(err, &ee) {
				code = ee.code
			} else if err != nil {
				t.Fatalf("unexpected error type %T: %v", err, err)
			}
			if code != tt.wantCode {
				t.Errorf("exit code = %d (%v), want %d", code, err, tt.wantCode)
			}
			if code == 2 && !strings.Contains(err.Error(), "critical, high, medium, low") {
				t.Errorf("error should list valid values: %v", err)
			}
			if code == 2 {
				return
			}
			if risk.Decision != tt.wantDecision || risk.FailOn != tt.wantFailOn || risk.Shadow != tt.shadow {
				t.Errorf("risk = decision %q failOn %q shadow %v, want %q %q %v",
					risk.Decision, risk.FailOn, risk.Shadow, tt.wantDecision, tt.wantFailOn, tt.shadow)
			}
			if risk.DecisionReason != tt.wantReason || risk.OpenItems != tt.wantOpen {
				t.Errorf("reason %q open %d, want %q %d", risk.DecisionReason, risk.OpenItems, tt.wantReason, tt.wantOpen)
			}
			if code == 5 && !strings.Contains(err.Error(), tt.wantReason) {
				t.Errorf("block error should give the reason %q: %v", tt.wantReason, err)
			}
		})
	}
}

func TestAppendShadowLog(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "shadow.jsonl")
	risk := &types.PRRisk{Tier: types.TierHigh, HeatScore: 70, Decision: "pass", Shadow: true}
	for i := 0; i < 2; i++ {
		if err := appendShadowLog(path, "main", risk); err != nil {
			t.Fatal(err)
		}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2 appended records:\n%s", len(lines), data)
	}
	for _, want := range []string{`"tier":"high"`, `"decision":"pass"`, `"base":"main"`, `"heat_score":70`} {
		if !strings.Contains(lines[0], want) {
			t.Errorf("record missing %s: %s", want, lines[0])
		}
	}
}

func TestCommentRepo(t *testing.T) {
	origin := func(r prdata.Repo, err error) func() (prdata.Repo, error) {
		return func() (prdata.Repo, error) { return r, err }
	}
	noOrigin := origin(prdata.Repo{}, errors.New("no origin"))
	tests := []struct {
		name, flag, env string
		origin          func() (prdata.Repo, error)
		want            string
		wantErr         bool
	}{
		{"flag wins", "acme/net", "other/repo", origin(prdata.Repo{Owner: "o", Name: "r"}, nil), "acme/net", false},
		{"GITHUB_REPOSITORY next", "", "other/repo", origin(prdata.Repo{Owner: "o", Name: "r"}, nil), "other/repo", false},
		{"origin remote last", "", "", origin(prdata.Repo{Owner: "o", Name: "r"}, nil), "o/r", false},
		{"nothing known: plain text, no error", "", "", noOrigin, "", false},
		{"bad env value is skipped", "", "not-a-repo", noOrigin, "", false},
		{"bad flag is an error", "acme", "", noOrigin, "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := commentRepo(tt.flag, tt.env, tt.origin)
			if (err != nil) != tt.wantErr || got != tt.want {
				t.Errorf("commentRepo = %q, %v; want %q, err=%v", got, err, tt.want, tt.wantErr)
			}
		})
	}
}

func TestLinkBase(t *testing.T) {
	tests := []struct{ base, want string }{
		{"main", "main"},
		{"origin/main", "main"},
		{"origin/release/1.2", "release/1.2"},
		{"refs/heads/dev", "dev"},
		{"a4715ad", ""},                                  // a commit, not a branch
		{"a4715ad0c1e2f3a4b5c6d7e8f9a0b1c2d3e4f5a6", ""}, // full SHA
		{"HEAD~3", ""},
		{"HEAD", ""},
		{"", ""},
	}
	for _, tt := range tests {
		t.Run(tt.base, func(t *testing.T) {
			if got := linkBase(tt.base); got != tt.want {
				t.Errorf("linkBase(%q) = %q, want %q", tt.base, got, tt.want)
			}
		})
	}
}

func TestPRNumber(t *testing.T) {
	tests := []struct {
		name    string
		flag    int
		ref     string
		want    int
		wantErr bool
	}{
		{"flag wins", 7, "refs/pull/12/merge", 7, false},
		{"GitHub Actions pull request ref", 0, "refs/pull/12/merge", 12, false},
		{"head ref form", 0, "refs/pull/345/head", 345, false},
		{"branch ref: no PR", 0, "refs/heads/main", 0, false},
		{"malformed ref", 0, "refs/pull/abc/merge", 0, false},
		{"nothing known", 0, "", 0, false},
		{"negative flag is invalid", -1, "", 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := prNumber(tt.flag, tt.ref)
			if (err != nil) != tt.wantErr || got != tt.want {
				t.Errorf("prNumber(%d, %q) = %d, %v; want %d, err=%v", tt.flag, tt.ref, got, err, tt.want, tt.wantErr)
			}
		})
	}
}

func TestParseAutoMerge(t *testing.T) {
	tests := []struct {
		in, want string
		wantErr  bool
	}{
		{"true", "true", false},
		{"false", "false", false},
		{"unknown", "unknown", false},
		{"FALSE", "false", false},
		{"", "", true},
		{"yes", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := parseAutoMerge(tt.in)
			if (err != nil) != tt.wantErr || got != tt.want {
				t.Errorf("parseAutoMerge(%q) = %q, %v; want %q, err=%v", tt.in, got, err, tt.want, tt.wantErr)
			}
			if err != nil && !strings.Contains(err.Error(), "true, false, unknown") {
				t.Errorf("error should list valid values: %v", err)
			}
		})
	}
}
