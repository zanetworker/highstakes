package main

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zanetworker/highstakes/internal/types"
)

func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=T", "-c", "user.email=t@x.com"}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// TestPRCheck_UnscoredFileUsesConfiguredTiers: a new file inherits heat 80 from
// core/; the default tiers call that HIGH, a repo config with critical: 70 must call it CRITICAL.
func TestPRCheck_UnscoredFileUsesConfiguredTiers(t *testing.T) {
	tests := []struct {
		name   string
		config string
		want   types.Tier
	}{
		{"no config uses default tiers", "", types.TierHigh},
		{"custom critical threshold applies", "tiers:\n  critical: 70\n  high: 50\n  medium: 20\n  low: 0\n", types.TierCritical},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := writeHeatmap(t)
			if tt.config != "" {
				if err := os.WriteFile(filepath.Join(dir, ".heatmap", "config.yaml"), []byte(tt.config), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			gitIn(t, dir, "init", "-q", "-b", "main")
			gitIn(t, dir, "add", "core/a.go")
			gitIn(t, dir, "commit", "-q", "-m", "base")
			gitIn(t, dir, "checkout", "-q", "-b", "feature")
			if err := os.WriteFile(filepath.Join(dir, "core", "new.go"), []byte("package core\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			gitIn(t, dir, "add", "core/new.go")
			gitIn(t, dir, "commit", "-q", "-m", "add new")

			out, err := runCLI(t, dir, "pr", "check", "--base", "main", "--json", "--shadow=false", "--fail-on=", "--author=t@x.com")
			if err != nil {
				t.Fatalf("pr check: %v\n%s", err, out)
			}
			var risk types.PRRisk
			if err := json.Unmarshal([]byte(out), &risk); err != nil {
				t.Fatalf("decode %q: %v", out, err)
			}
			if len(risk.FilesChanged) != 1 || !risk.FilesChanged[0].Unscored {
				t.Fatalf("files = %+v, want one unscored file", risk.FilesChanged)
			}
			if got := risk.FilesChanged[0].Tier; got != tt.want {
				t.Errorf("unscored file tier = %s, want %s", got, tt.want)
			}
		})
	}
}

// TestPRCheck_EmptyDiffStillGates: an empty diff reports LOW, and the gate and
// shadow log still apply to that LOW result.
func TestPRCheck_EmptyDiffStillGates(t *testing.T) {
	tests := []struct {
		name      string
		args      []string
		wantCode  int    // 0 = no error
		wantErr   string // substring of the error
		wantOut   string // substring of stdout
		wantShade bool   // shadow log written
	}{
		{"invalid --fail-on lists valid values", []string{"--fail-on=invalid", "--shadow=false"}, 2, "low", "", false},
		{"--fail-on low blocks a LOW result", []string{"--fail-on=low", "--shadow=false"}, exitBlocked, "tier low", "", false},
		{"--fail-on critical passes", []string{"--fail-on=critical", "--shadow=false"}, 0, "", `"decision": "pass"`, false},
		{"--shadow writes its decision record", []string{"--fail-on=", "--shadow=true"}, 0, "", `"tier": "low"`, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := writeHeatmap(t)
			gitIn(t, dir, "init", "-q", "-b", "main")
			gitIn(t, dir, "add", "core/a.go")
			gitIn(t, dir, "commit", "-q", "-m", "base")

			args := append([]string{"pr", "check", "--base", "main", "--json", "--author=t@x.com"}, tt.args...)
			out, err := runCLI(t, dir, args...)
			if tt.wantCode == 0 {
				if err != nil {
					t.Fatalf("pr check: %v\n%s", err, out)
				}
			} else {
				var ee *exitError
				if !errors.As(err, &ee) || ee.code != tt.wantCode || !strings.Contains(ee.msg, tt.wantErr) {
					t.Fatalf("err = %v, want exit %d containing %q", err, tt.wantCode, tt.wantErr)
				}
			}
			if !strings.Contains(out, tt.wantOut) {
				t.Errorf("stdout = %q, want it to contain %q", out, tt.wantOut)
			}
			log, readErr := os.ReadFile(filepath.Join(dir, shadowLogPath))
			if tt.wantShade && (readErr != nil || !strings.Contains(string(log), `"tier":"low"`) || !strings.Contains(string(log), `"files":0`)) {
				t.Errorf("shadow log = %q (%v), want a LOW record with 0 files", log, readErr)
			}
			if !tt.wantShade && readErr == nil {
				t.Errorf("shadow log written without --shadow: %q", log)
			}
		})
	}
}
