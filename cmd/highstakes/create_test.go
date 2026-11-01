package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zanetworker/highstakes/internal/policy"
)

func TestNoFilesHint(t *testing.T) {
	tests := []struct {
		minTier string
		want    string
	}{
		{"critical", "No files at or above tier critical. Try --min-tier high, or run analyze with an LLM."},
		{"high", "No files at or above tier high. Try --min-tier medium, or run analyze with an LLM."},
		{"medium", "No files at or above tier medium. Try --min-tier low, or run analyze with an LLM."},
		{"low", "No files at or above tier low. Run analyze first, with an LLM for meaningful tiers."},
	}
	for _, tt := range tests {
		t.Run(tt.minTier, func(t *testing.T) {
			if got := noFilesHint(tt.minTier); got != tt.want {
				t.Errorf("noFilesHint(%q) =\n %q\nwant\n %q", tt.minTier, got, tt.want)
			}
		})
	}
}

func TestCreatePolicy_Agents(t *testing.T) {
	dir := writeAxesHeatmap(t) // core/auth.go Protect (L1), ui/view.go Stabilize (L2)
	t.Run("yaml with header to stdout", func(t *testing.T) {
		out, err := runCLI(t, dir, "create", "policy", "--agents")
		if err != nil {
			t.Fatalf("err = %v", err)
		}
		for _, want := range []string{"# Agent autonomy policy", "how_to_read:", "cap_rule:", "label: auto-fix, human merges", "- ui/view.go", "- core/auth.go"} {
			if !strings.Contains(out, want) {
				t.Errorf("output missing %q:\n%s", want, out)
			}
		}
	})
	t.Run("json to a file", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "autonomy.json")
		out, err := runCLI(t, dir, "create", "policy", "--agents", "--format", "json", "--out", path)
		if err != nil {
			t.Fatalf("err = %v", err)
		}
		if out != "" {
			t.Errorf("--out should keep stdout empty, got %q", out)
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var p policy.AgentPolicy
		if err := json.Unmarshal(raw, &p); err != nil {
			t.Fatalf("invalid JSON: %v", err)
		}
		if len(p.Levels) != 4 || strings.Join(p.Levels[1].Paths, ",") != "ui/view.go" || p.Levels[1].Level != 2 {
			t.Errorf("levels = %+v", p.Levels)
		}
	})
	t.Run("max_level from config caps", func(t *testing.T) {
		capped := writeAxesHeatmap(t)
		if err := os.WriteFile(filepath.Join(capped, ".heatmap", "config.yaml"), []byte("autonomy:\n  max_level: 1\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		out, err := runCLI(t, capped, "create", "policy", "--agents", "--json")
		if err != nil {
			t.Fatalf("err = %v", err)
		}
		var p policy.AgentPolicy
		if err := json.Unmarshal([]byte(out), &p); err != nil {
			t.Fatalf("invalid JSON %q: %v", out, err)
		}
		if p.MaxLevel != 1 || len(p.Levels[1].Paths) != 0 {
			t.Errorf("max_level 1 should leave L2 empty: %+v", p)
		}
	})
	t.Run("invalid format lists valid values", func(t *testing.T) {
		_, err := runCLI(t, dir, "create", "policy", "--agents", "--format", "toml")
		if ee, ok := err.(*exitError); !ok || ee.code != 2 || !strings.Contains(ee.msg, "yaml, json") {
			t.Errorf("err = %v, want exit 2 listing yaml, json", err)
		}
	})
	t.Run("without --agents the review policy is unchanged", func(t *testing.T) {
		out, err := runCLI(t, dir, "create", "policy", "--min-tier", "low")
		if err != nil || !strings.Contains(out, "min_tier: low") {
			t.Errorf("err = %v, out:\n%s", err, out)
		}
	})
}
