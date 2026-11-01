package types

import "testing"

func TestQuadrant_Label(t *testing.T) {
	tests := []struct {
		q    Quadrant
		want string
	}{
		{QuadrantFragileCritical, "Harden now"},
		{QuadrantProtect, "Protect"},
		{QuadrantWatch, "Stabilize"},
		{QuadrantLow, "Routine"},
		{"", ""},
		{"bogus", ""},
	}
	for _, tt := range tests {
		t.Run(string(tt.q), func(t *testing.T) {
			if got := tt.q.Label(); got != tt.want {
				t.Errorf("Label() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestAxisLevel(t *testing.T) {
	tests := []struct {
		name          string
		score, cutoff int
		want          string
	}{
		{"above cutoff", 50, 33, "HIGH"},
		{"at cutoff counts", 33, 33, "HIGH"},
		{"below cutoff", 32, 33, "LOW"},
		{"no cutoff known", 90, 0, "LOW"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := AxisLevel(tt.score, tt.cutoff); got != tt.want {
				t.Errorf("AxisLevel(%d, %d) = %q, want %q", tt.score, tt.cutoff, got, tt.want)
			}
		})
	}
}

func TestTierPosture(t *testing.T) {
	tests := []struct {
		tier Tier
		want string
	}{
		{TierCritical, "Harden now"},
		{TierHigh, "Protect"},
		{TierMedium, "Stabilize"},
		{TierLow, "Routine"},
		{"", "Routine"},
	}
	for _, tt := range tests {
		t.Run(string(tt.tier), func(t *testing.T) {
			if got := TierPosture(tt.tier); got != tt.want {
				t.Errorf("TierPosture(%q) = %q, want %q", tt.tier, got, tt.want)
			}
		})
	}
}

func TestExplanation_Top(t *testing.T) {
	e := &Explanation{Actions: []Action{
		{ID: "a", Scope: ScopeBeforeMerge}, {ID: "b", Scope: ScopeBeforeMerge}, {ID: "c", Scope: ScopeBacklog},
	}}
	tests := []struct {
		name  string
		e     *Explanation
		scope string
		want  string
	}{
		{"first before-merge", e, ScopeBeforeMerge, "a"},
		{"first backlog", e, ScopeBacklog, "c"},
		{"none in scope", &Explanation{Actions: e.Actions[:1]}, ScopeBacklog, ""},
		{"nil explanation", nil, ScopeBacklog, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.e.Top(tt.scope)
			if (got == nil && tt.want != "") || (got != nil && got.ID != tt.want) {
				t.Errorf("Top(%q) = %+v, want %q", tt.scope, got, tt.want)
			}
		})
	}
}
