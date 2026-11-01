package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/pflag"
	"github.com/zanetworker/highstakes/internal/tour"
	"github.com/zanetworker/highstakes/internal/types"
	"github.com/zanetworker/highstakes/pkg/heatmap"
)

func TestGetRouting(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"tour subcommand", []string{"get", "tour"}, "tour"},
		{"tour with flags", []string{"get", "tour", "--limit", "3"}, "tour"},
		{"file path still goes to get", []string{"get", "internal/scorer/scorer.go"}, "get"},
		{"file named like a prefix of tour", []string{"get", "tou"}, "get"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd, _, err := rootCmd.Find(tt.args)
			if err != nil {
				t.Fatalf("Find(%v) err = %v", tt.args, err)
			}
			if cmd.Name() != tt.want {
				t.Errorf("Find(%v) = %q, want %q", tt.args, cmd.Name(), tt.want)
			}
		})
	}
}

// runCLI executes the root command in dir and returns stdout and the error.
func runCLI(t *testing.T, dir string, args ...string) (string, error) {
	t.Helper()
	t.Chdir(dir)
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	orig := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = orig }()

	// Reset flags that persist across Execute calls in one process.
	for _, c := range []string{"json", "limit", "focus", "per-component", "min-impact", "components"} {
		if f := tourCmd.Flags().Lookup(c); f != nil {
			if sv, ok := f.Value.(pflag.SliceValue); ok {
				_ = sv.Replace(nil)
			} else {
				_ = f.Value.Set(f.DefValue)
			}
		}
	}
	if f := scoreCmd.Flags().Lookup("json"); f != nil {
		_ = f.Value.Set(f.DefValue)
	}

	rootCmd.SetArgs(args)
	runErr := rootCmd.Execute()
	w.Close()
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	return buf.String(), runErr
}

func writeHeatmap(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	hm := &types.Heatmap{
		Version: "1",
		Files: map[string]*types.FileHeat{
			"core/a.go": {Path: "core/a.go", HeatScore: 80, Impact: 80, Tier: types.TierHigh,
				Dependencies: types.Dependencies{ImportedBy: []types.Dependency{{Path: "cmd/main.go"}}}},
			"cmd/main.go": {Path: "cmd/main.go", HeatScore: 30, Impact: 30, Tier: types.TierLow},
		},
	}
	if err := os.MkdirAll(filepath.Join(dir, ".heatmap"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := heatmap.Save(hm, filepath.Join(dir, ".heatmap", "heatmap.json")); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "core"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "core", "a.go"), []byte("package core\n\nfunc A() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestGetTour_JSON(t *testing.T) {
	dir := writeHeatmap(t)
	out, err := runCLI(t, dir, "get", "tour", "--json")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	var tr tour.Tour
	if err := json.Unmarshal([]byte(out), &tr); err != nil {
		t.Fatalf("invalid JSON %q: %v", out, err)
	}
	if tr.TotalStops != 2 || tr.Sections[0].Dir != "core" {
		t.Fatalf("tour = %+v", tr)
	}
	if got := tr.Sections[0].Stops[0].Anchor; got != "core/a.go:3" {
		t.Errorf("anchor = %q, want core/a.go:3 (line hint from source)", got)
	}
}

func TestGetTour_Errors(t *testing.T) {
	dir := writeHeatmap(t)
	tests := []struct {
		name     string
		args     []string
		wantCode int
		wantMsg  string
	}{
		{"negative limit", []string{"get", "tour", "--limit", "-1"}, 2, "limit"},
		{"unknown focus", []string{"get", "tour", "--focus", "nope"}, 4, "nope"},
		{"negative per-component", []string{"get", "tour", "--per-component", "-1"}, 2, "per-component"},
		{"min impact above 100", []string{"get", "tour", "--min-impact", "101"}, 2, "min-impact"},
		{"unknown component lists valid ones", []string{"get", "tour", "--per-component", "1", "--components", "nope"}, 2, "valid: cmd, core"},
		{"min impact leaves nothing", []string{"get", "tour", "--per-component", "1", "--min-impact", "100"}, 4, "min-impact"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := runCLI(t, dir, tt.args...)
			ee, ok := err.(*exitError)
			if !ok {
				t.Fatalf("err = %v (%T), want *exitError", err, err)
			}
			if ee.code != tt.wantCode || !strings.Contains(ee.msg, tt.wantMsg) {
				t.Errorf("code = %d msg = %q", ee.code, ee.msg)
			}
		})
	}
}

func TestGetFile_StillWorks(t *testing.T) {
	dir := writeHeatmap(t)
	out, err := runCLI(t, dir, "get", "core/a.go", "--json")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	var f types.FileHeat
	if err := json.Unmarshal([]byte(out), &f); err != nil || f.Path != "core/a.go" {
		t.Fatalf("out = %q err = %v", out, err)
	}
}

func TestPrintTour(t *testing.T) {
	tr, err := tour.Build(&types.Heatmap{Files: map[string]*types.FileHeat{
		"core/a.go": {Path: "core/a.go", HeatScore: 80, Tier: types.TierHigh,
			Impact: 80, Likelihood: 20, ImpactAssessed: true, Quadrant: types.QuadrantProtect,
			Dependencies: types.Dependencies{ImportedBy: []types.Dependency{{Path: "cmd/main.go"}}}},
		"cmd/main.go": {Path: "cmd/main.go", HeatScore: 30, Tier: types.TierLow},
	}}, tour.Options{})
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	printTour(&buf, tr)
	out := buf.String()
	for _, want := range []string{"2 stops", "1. core", "1. core/a.go:1", "used by: cmd/main.go", "2. cmd", "2. cmd/main.go:1", "depends on: core/a.go",
		"impact 80, likelihood 20, protect", "max impact 80", "impact ~0 (estimate), likelihood 0"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
	if strings.Index(out, "core/a.go:1") > strings.Index(out, "cmd/main.go:1") {
		t.Errorf("dependency should be listed first:\n%s", out)
	}
}

func TestGetTour_PerComponent(t *testing.T) {
	dir := writeHeatmap(t)
	tests := []struct {
		name string
		args []string
		want []string // section components in order
	}{
		{"one section per component", []string{"get", "tour", "--per-component", "1", "--json"}, []string{"core", "cmd"}},
		{"components restricts", []string{"get", "tour", "--per-component", "1", "--components", "cmd", "--json"}, []string{"cmd"}},
		{"min impact filters", []string{"get", "tour", "--per-component", "1", "--min-impact", "50", "--json"}, []string{"core"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := runCLI(t, dir, tt.args...)
			if err != nil {
				t.Fatalf("err = %v", err)
			}
			var tr tour.Tour
			if err := json.Unmarshal([]byte(out), &tr); err != nil {
				t.Fatalf("invalid JSON %q: %v", out, err)
			}
			var got []string
			for _, s := range tr.Sections {
				got = append(got, s.Component)
			}
			if strings.Join(got, ",") != strings.Join(tt.want, ",") {
				t.Errorf("components = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPrintTour_ShowsExplanation(t *testing.T) {
	exp := &types.Explanation{
		LikelihoodReasons: []string{"40 commits in the last 90 days (top 3% of this repo)", "2 recent fixes"},
		Actions: []types.Action{
			{ID: "owner-review", Text: "Every PR: owner review (@alice)"},
			{ID: "codeowners", Text: "Add to CODEOWNERS; never auto-merge"},
			{ID: "stable-interface", Text: "Keep its public interface stable"},
		},
	}
	tr, err := tour.Build(&types.Heatmap{Files: map[string]*types.FileHeat{
		"core/a.go": {Path: "core/a.go", Tier: types.TierHigh, Impact: 80, ImpactAssessed: true, Explanation: exp},
		"core/b.go": {Path: "core/b.go", Tier: types.TierLow},
	}}, tour.Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range tr.Sections {
		for _, st := range s.Stops {
			if st.Path == "core/a.go" && st.Explanation != exp {
				t.Errorf("stop explanation = %v, want the file's explanation", st.Explanation)
			}
		}
	}
	var buf bytes.Buffer
	printTour(&buf, tr)
	out := buf.String()
	for _, want := range []string{
		"likely because: 40 commits in the last 90 days (top 3% of this repo)",
		"do: Every PR: owner review (@alice); Add to CODEOWNERS; never auto-merge",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "Keep its public interface stable") {
		t.Errorf("tour should show only the top two actions:\n%s", out)
	}
	if strings.Count(out, "do:") != 1 {
		t.Errorf("files without an explanation should print no actions:\n%s", out)
	}
}
