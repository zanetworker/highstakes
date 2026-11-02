package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/zanetworker/highstakes/internal/types"
	"github.com/zanetworker/highstakes/pkg/heatmap"
)

func TestSortListEntries(t *testing.T) {
	entries := func() []listEntry {
		return []listEntry{
			{Path: "a.go", Score: 50, Likelihood: 90, Impact: 10},
			{Path: "b.go", Score: 70, Likelihood: 20, Impact: 70},
			{Path: "c.go", Score: 70, Likelihood: 60, Impact: 40},
		}
	}
	tests := []struct {
		by      string
		want    []string
		wantErr bool
	}{
		{"heat", []string{"b.go", "c.go", "a.go"}, false}, // ties broken by path
		{"likelihood", []string{"a.go", "c.go", "b.go"}, false},
		{"impact", []string{"b.go", "c.go", "a.go"}, false},
		{"", nil, true},
		{"blast", nil, true},
	}
	for _, tt := range tests {
		t.Run(tt.by, func(t *testing.T) {
			got := entries()
			err := sortListEntries(got, tt.by)
			if tt.wantErr {
				if err == nil || !strings.Contains(err.Error(), "heat, likelihood, impact") {
					t.Fatalf("err = %v, want one listing valid values", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			var paths []string
			for _, e := range got {
				paths = append(paths, e.Path)
			}
			if !reflect.DeepEqual(paths, tt.want) {
				t.Errorf("order = %v, want %v", paths, tt.want)
			}
		})
	}
}

func writeAxesHeatmap(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	hm := &types.Heatmap{Version: "1", Files: map[string]*types.FileHeat{
		"core/auth.go": {Path: "core/auth.go", HeatScore: 90, Tier: types.TierHigh, Likelihood: 10, Impact: 90, ImpactAssessed: true, Quadrant: types.QuadrantProtect},
		"ui/view.go":   {Path: "ui/view.go", HeatScore: 80, Tier: types.TierMedium, Likelihood: 80, Impact: 20, ImpactAssessed: true, Quadrant: types.QuadrantWatch},
	}}
	if err := os.MkdirAll(filepath.Join(dir, ".heatmap"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := heatmap.Save(hm, filepath.Join(dir, ".heatmap", "heatmap.json")); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestList_SortFlag(t *testing.T) {
	dir := writeAxesHeatmap(t)
	out, err := runCLI(t, dir, "list", "--sort", "likelihood", "--json")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	var res struct {
		Files []listEntry `json:"files"`
	}
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("invalid JSON %q: %v", out, err)
	}
	if len(res.Files) != 2 || res.Files[0].Path != "ui/view.go" || res.Files[0].Likelihood != 80 || res.Files[0].Quadrant != types.QuadrantWatch {
		t.Errorf("files = %+v, want ui/view.go first with likelihood and quadrant", res.Files)
	}

	out, err = runCLI(t, dir, "list", "--sort", "impact", "--json=false")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(out, "I  90") || !strings.Contains(out, "L  10") || strings.Index(out, "core/auth.go") > strings.Index(out, "ui/view.go") {
		t.Errorf("text output should show both axes, impact first:\n%s", out)
	}

	_, err = runCLI(t, dir, "list", "--sort", "nope")
	if ee, ok := err.(*exitError); !ok || ee.code != 2 || !strings.Contains(ee.msg, "heat, likelihood, impact") {
		t.Errorf("invalid --sort: err = %v, want exit 2 listing valid values", err)
	}
	_, _ = runCLI(t, dir, "list", "--sort", "heat") // reset the persisted flag
}

func TestGetFile_ShowsAxes(t *testing.T) {
	dir := writeAxesHeatmap(t)
	out, err := runCLI(t, dir, "get", "core/auth.go")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	for _, want := range []string{"How bad if it breaks?  90", "Will it break?         10", "Protect"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestListTasks(t *testing.T) {
	dir := t.TempDir()
	busy := &types.FileHeat{Path: "ui/busy.go", ImpactAssessed: true, Quadrant: types.QuadrantWatch, Tier: types.TierMedium,
		Explanation: &types.Explanation{Actions: []types.Action{{ID: "complexity-issue", Text: "Open an issue to cut its complexity",
			Scope: types.ScopeBacklog, Signal: "cyclomatic 40", Command: "gh issue create --title x"}}}}
	busy.Factors.Complexity.Cyclomatic = 40
	prot := &types.FileHeat{Path: "core/p.go", ImpactAssessed: true, Quadrant: types.QuadrantProtect, Tier: types.TierHigh,
		Explanation: &types.Explanation{Actions: []types.Action{{ID: "codeowners", Text: "Add a CODEOWNERS line",
			Scope: types.ScopeBacklog, Signal: "not covered", Command: "/core/p.go @ana"}}}}
	hm := &types.Heatmap{Version: "1", Files: map[string]*types.FileHeat{busy.Path: busy, prot.Path: prot}}
	if err := os.MkdirAll(filepath.Join(dir, ".heatmap"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := heatmap.Save(hm, filepath.Join(dir, ".heatmap", "heatmap.json")); err != nil {
		t.Fatal(err)
	}

	out, err := runCLI(t, dir, "list", "tasks", "--json")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	var res struct {
		Tasks    []map[string]interface{} `json:"tasks"`
		Count    int                      `json:"count"`
		MinLevel int                      `json:"min_level"`
	}
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("invalid JSON %q: %v", out, err)
	}
	if res.Count != 1 || res.MinLevel != 2 || res.Tasks[0]["file"] != "ui/busy.go" || res.Tasks[0]["acceptance"] != "tests pass and cyclomatic complexity drops below 40" {
		t.Errorf("tasks = %+v", res)
	}

	out, err = runCLI(t, dir, "list", "tasks", "--level", "1")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	for _, want := range []string{"L2 auto-fix, human merges  Cut the cyclomatic complexity of ui/busy.go", "done when: CODEOWNERS covers core/p.go", "$ /core/p.go @ana"} {
		if !strings.Contains(out, want) {
			t.Errorf("text output missing %q:\n%s", want, out)
		}
	}

	_, err = runCLI(t, dir, "list", "tasks", "--level", "4")
	if ee, ok := err.(*exitError); !ok || ee.code != 2 || !strings.Contains(ee.msg, "0, 1, 2, 3") {
		t.Errorf("--level 4: err = %v, want exit 2 listing valid values", err)
	}
}
