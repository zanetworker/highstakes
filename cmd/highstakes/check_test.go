package main

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestParseDays(t *testing.T) {
	tests := []struct {
		in      string
		want    int
		wantErr bool
	}{
		{"365d", 365, false},
		{"1d", 1, false},
		{"0d", 0, true},
		{"-5d", 0, true},
		{"30", 0, true},
		{"2w", 0, true},
		{"", 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := parseDays(tt.in)
			if (err != nil) != tt.wantErr || got != tt.want {
				t.Errorf("parseDays(%q) = (%d, %v), want (%d, err=%v)", tt.in, got, err, tt.want, tt.wantErr)
			}
		})
	}
}

func TestParseCutoff(t *testing.T) {
	now := time.Date(2026, 10, 30, 15, 0, 0, 0, time.UTC)
	tests := []struct {
		in      string
		want    time.Time
		wantErr bool
	}{
		{"180d", now.AddDate(0, 0, -180), false},
		{"2026-04-01", time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC), false},
		{"2026-10-30", time.Date(2026, 10, 30, 0, 0, 0, 0, time.UTC), false},
		{"2026-10-31", time.Time{}, true}, // in the future
		{"01/04/2026", time.Time{}, true},
		{"0d", time.Time{}, true},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := parseCutoff(tt.in, now)
			if (err != nil) != tt.wantErr || !got.Equal(tt.want) {
				t.Errorf("parseCutoff(%q) = (%v, %v), want (%v, err=%v)", tt.in, got, err, tt.want, tt.wantErr)
			}
		})
	}
}

func TestExitCode(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
	}{
		{"cli error keeps its code", cliError(4, "no heatmap"), 4},
		{"invalid input", cliError(2, "bad flag"), 2},
		{"plain error is internal", fmt.Errorf("boom"), 1},
		{"wrapped cli error", fmt.Errorf("ctx: %w", cliError(3, "git")), 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := exitCode(tt.err); got != tt.want {
				t.Errorf("exitCode = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestParseSource(t *testing.T) {
	tests := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{"commits", "commits", false},
		{"prs", "prs", false},
		{"PRs", "prs", false},
		{"issues", "", true},
		{"", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := parseSource(tt.in)
			if (err != nil) != tt.wantErr || got != tt.want {
				t.Errorf("parseSource(%q) = (%q, %v), want (%q, err=%v)", tt.in, got, err, tt.want, tt.wantErr)
			}
			if err != nil && !strings.Contains(err.Error(), "commits, prs") {
				t.Errorf("error %q should list the valid values", err)
			}
		})
	}
}
