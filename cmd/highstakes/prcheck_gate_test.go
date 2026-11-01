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
	tests := []struct {
		name         string
		tier         types.Tier
		failOn       string
		shadow       bool
		wantDecision string
		wantFailOn   types.Tier
		wantCode     int // 0 = no error
	}{
		{"no gate leaves decision empty", types.TierCritical, "", false, "", "", 0},
		{"gate passes below threshold", types.TierHigh, "critical", false, "pass", types.TierCritical, 0},
		{"gate blocks at threshold with exit 5", types.TierCritical, "critical", false, "block", types.TierCritical, 5},
		{"shadow defaults gate to critical and never fails", types.TierCritical, "", true, "block", types.TierCritical, 0},
		{"shadow honours explicit gate", types.TierHigh, "high", true, "block", types.TierHigh, 0},
		{"invalid tier is exit 2 with valid values", types.TierLow, "severe", false, "", "", 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			risk := &types.PRRisk{Tier: tt.tier}
			err := applyGate(risk, tt.failOn, tt.shadow)
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
