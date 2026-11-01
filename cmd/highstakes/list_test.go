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
