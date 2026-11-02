package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zanetworker/highstakes/internal/types"
	"github.com/zanetworker/highstakes/pkg/heatmap"
)

func writeUnitsHeatmap(t *testing.T, units []types.Unit) string {
	t.Helper()
	dir := t.TempDir()
	hm := &types.Heatmap{Version: "1", Files: map[string]*types.FileHeat{}, Metadata: types.Metadata{Units: units}}
	if err := os.MkdirAll(filepath.Join(dir, ".heatmap"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := heatmap.Save(hm, filepath.Join(dir, ".heatmap", "heatmap.json")); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestListUnits(t *testing.T) {
	sup := types.Unit{
		Name: "openshell-supervisor", Kind: "rust-bin", Entry: "crates/sup/src/main.rs", Evidence: "crates/sup/Cargo.toml:13",
		Home: "crates/sup", Components: []string{"crates/sup", "crates/cli", "crates/core", "crates/sup-network"}, Shared: []string{"crates/core"},
		Files: 12, MaxImpact: 92, ImpactAssessed: true, FragileCritical: 3,
		InferredPlacement: []types.Placement{{Kind: "dockerfile", Detail: "ENTRYPOINT of deploy/Dockerfile.supervisor", Evidence: "deploy/Dockerfile.supervisor:18"}},
	}
	cli := types.Unit{Name: "cli", Kind: "rust-bin", Entry: "crates/cli/src/main.rs", Evidence: "crates/cli/src/main.rs:5",
		Home: "crates/cli", Components: []string{"crates/cli"}, Shared: []string{}, Files: 2, MaxImpact: 40}
	dir := writeUnitsHeatmap(t, []types.Unit{cli, sup})

	t.Run("json sorted by impact", func(t *testing.T) {
		out, err := runCLI(t, dir, "list", "units", "--json")
		if err != nil {
			t.Fatal(err)
		}
		var res struct {
			Units []types.Unit `json:"units"`
			Count int          `json:"count"`
		}
		if err := json.Unmarshal([]byte(out), &res); err != nil {
			t.Fatalf("invalid JSON %q: %v", out, err)
		}
		if res.Count != 2 || res.Units[0].Name != "openshell-supervisor" || res.Units[0].InferredPlacement[0].Evidence != "deploy/Dockerfile.supervisor:18" {
			t.Errorf("units = %+v, want supervisor first with its placement", res.Units)
		}
	})

	t.Run("text", func(t *testing.T) {
		out, err := runCLI(t, dir, "list", "units", "--json=false")
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{
			"openshell-supervisor", "crates/sup/src/main.rs (crates/sup/Cargo.toml:13)",
			"impact 92", "3 Harden now", "own: crates/sup, crates/sup-network", "shared: crates/core",
			"inferred placement: ENTRYPOINT of deploy/Dockerfile.supervisor [deploy/Dockerfile.supervisor:18]",
			"inferred placement: none found",
			"links units: cli (crates/cli)", // crates/cli is the cli unit's crate, not sup's own
		} {
			if !strings.Contains(out, want) {
				t.Errorf("output missing %q:\n%s", want, out)
			}
		}
		if strings.Index(out, "openshell-supervisor") > strings.Index(out, "\ncli") {
			t.Errorf("supervisor (impact 92) should come before cli:\n%s", out)
		}
	})

	t.Run("no units", func(t *testing.T) {
		out, err := runCLI(t, writeUnitsHeatmap(t, nil), "list", "units", "--json=false")
		if err != nil || !strings.Contains(out, "No runtime units") {
			t.Errorf("out = %q err = %v, want a no-units message", out, err)
		}
	})

	t.Run("no heatmap", func(t *testing.T) {
		_, err := runCLI(t, t.TempDir(), "list", "units")
		if ee, ok := err.(*exitError); !ok || ee.code != 4 {
			t.Errorf("err = %v, want exit 4", err)
		}
	})
}
