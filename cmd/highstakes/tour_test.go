package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
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
	for _, c := range []*cobra.Command{tourCmd, listUnitsCmd, architectureCmd, prCheckCmd, createPolicyCmd, listTasksCmd} {
		c.Flags().VisitAll(func(f *pflag.Flag) {
			if sv, ok := f.Value.(pflag.SliceValue); ok {
				_ = sv.Replace(nil)
			} else {
				_ = f.Value.Set(f.DefValue)
			}
			f.Changed = false
		})
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
	tr, err := tour.Build(&types.Heatmap{Metadata: types.Metadata{LikelyCutoff: 33, ImpactThreshold: 61}, Files: map[string]*types.FileHeat{
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
		"1. core/a.go:1  Protect", "Will it break? 20 LOW, cutoff 33 · How bad if it breaks? 80 HIGH, cutoff 61",
		"max how bad 80, Protect", "How bad if it breaks? ~0 (estimate)"} {
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
			{ID: "owner-review", Text: "Get an owner's review (@alice)", Scope: types.ScopeBeforeMerge},
			{ID: "no-auto-merge", Text: "Block auto-merge", Scope: types.ScopeBeforeMerge},
			{ID: "codeowners", Text: "Add a CODEOWNERS line", Scope: types.ScopeBacklog},
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
		"break signals: 40 commits in the last 90 days (top 3% of this repo)",
		"before merge: Get an owner's review (@alice)",
		"backlog: Add a CODEOWNERS line",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "Block auto-merge") {
		t.Errorf("tour should show only the top action of each scope:\n%s", out)
	}
	if strings.Count(out, "before merge:") != 1 {
		t.Errorf("files without an explanation should print no actions:\n%s", out)
	}
	for _, s := range tr.Sections {
		for _, st := range s.Stops {
			if st.Path != "core/a.go" {
				continue
			}
			if st.TopBeforeMerge == nil || st.TopBeforeMerge.ID != "owner-review" || st.TopBacklog == nil || st.TopBacklog.ID != "codeowners" {
				t.Errorf("stop JSON top actions = %+v / %+v", st.TopBeforeMerge, st.TopBacklog)
			}
		}
	}
}

func TestGetTour_Supporting(t *testing.T) {
	dir := writeHeatmap(t)
	hm, err := heatmap.Load(filepath.Join(dir, ".heatmap", "heatmap.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"examples/demo.py", "tools/gen.py"} {
		hm.Files[p] = &types.FileHeat{Path: p, HeatScore: 95, Impact: 95, Tier: types.TierCritical}
	}
	if err := heatmap.Save(hm, filepath.Join(dir, ".heatmap", "heatmap.json")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".heatmap", "config.yaml"), []byte("tour:\n  supporting: [tools]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name      string
		args      []string
		wantComps []string
		wantKinds []string
	}{
		{"hidden by default, config glob honoured", []string{"get", "tour", "--per-component", "1", "--json"},
			[]string{"core", "cmd"}, []string{"product", "product"}},
		{"include-supporting appends them", []string{"get", "tour", "--per-component", "1", "--include-supporting", "--json"},
			[]string{"core", "cmd", "examples", "tools"}, []string{"product", "product", "supporting", "supporting"}},
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
			var comps, kinds []string
			for _, s := range tr.Sections {
				comps, kinds = append(comps, s.Component), append(kinds, s.Kind)
			}
			if strings.Join(comps, ",") != strings.Join(tt.wantComps, ",") || strings.Join(kinds, ",") != strings.Join(tt.wantKinds, ",") {
				t.Errorf("sections = %v %v, want %v %v", comps, kinds, tt.wantComps, tt.wantKinds)
			}
		})
	}

	t.Run("naming a supporting component shows it", func(t *testing.T) {
		_, err := runCLI(t, dir, "get", "tour", "--min-impact", "90", "--components", "core,examples")
		if err != nil {
			t.Fatalf("named supporting component should show: %v", err)
		}
	})
	t.Run("bad config glob is exit 2", func(t *testing.T) {
		if err := os.WriteFile(filepath.Join(dir, ".heatmap", "config.yaml"), []byte("tour:\n  supporting: [\"[\"]\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		_, err := runCLI(t, dir, "get", "tour")
		if ee, ok := err.(*exitError); !ok || ee.code != 2 || !strings.Contains(ee.msg, "tour.supporting") {
			t.Errorf("err = %v, want exit 2 naming tour.supporting", err)
		}
	})
}

func TestGetTour_OnlySupportingHint(t *testing.T) {
	dir := writeHeatmap(t)
	hm, err := heatmap.Load(filepath.Join(dir, ".heatmap", "heatmap.json"))
	if err != nil {
		t.Fatal(err)
	}
	hm.Files["examples/demo.py"] = &types.FileHeat{Path: "examples/demo.py", Impact: 95, Tier: types.TierCritical}
	if err := heatmap.Save(hm, filepath.Join(dir, ".heatmap", "heatmap.json")); err != nil {
		t.Fatal(err)
	}
	_, err = runCLI(t, dir, "get", "tour", "--min-impact", "90")
	if ee, ok := err.(*exitError); !ok || ee.code != 4 || !strings.Contains(ee.msg, "--include-supporting") {
		t.Errorf("err = %v, want exit 4 suggesting --include-supporting", err)
	}
}

func TestPrintTour_Parts(t *testing.T) {
	files := map[string]*types.FileHeat{
		"core/a.go":       {Path: "core/a.go", Impact: 80, Tier: types.TierHigh},
		"cmd/main.go":     {Path: "cmd/main.go", Impact: 30, Tier: types.TierLow},
		"examples/x.py":   {Path: "examples/x.py", Impact: 90, Tier: types.TierHigh},
		"scripts/a.sh":    {Path: "scripts/a.sh", Impact: 20, Tier: types.TierLow},
		".github/ci.yaml": {Path: ".github/ci.yaml", Impact: 20, Tier: types.TierLow},
	}
	tests := []struct {
		name    string
		opts    tour.Options
		want    []string
		notWant []string
	}{
		{"hidden", tour.Options{PerComponent: 1},
			[]string{"Tour: 2 stops across 2 product components, dependencies first", "3 supporting components hidden (--include-supporting shows them)"},
			[]string{"Supporting: CI, deployment, scripts and examples"}},
		{"included", tour.Options{PerComponent: 1, IncludeSupporting: true},
			[]string{"Tour: 5 stops across 2 product and 3 supporting components, dependencies first", "Supporting: CI, deployment, scripts and examples"},
			[]string{"hidden"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tr, err := tour.Build(&types.Heatmap{Files: files}, tt.opts)
			if err != nil {
				t.Fatal(err)
			}
			var buf bytes.Buffer
			printTour(&buf, tr)
			out := buf.String()
			for _, w := range tt.want {
				if !strings.Contains(out, w) {
					t.Errorf("output missing %q:\n%s", w, out)
				}
			}
			for _, w := range tt.notWant {
				if strings.Contains(out, w) {
					t.Errorf("output has %q:\n%s", w, out)
				}
			}
			if i := strings.Index(out, "Supporting:"); i >= 0 && strings.Index(out, "examples/x.py") < i {
				t.Errorf("supporting stops must follow the part title:\n%s", out)
			}
		})
	}
}
