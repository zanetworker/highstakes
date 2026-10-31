package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
