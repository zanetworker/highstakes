package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/zanetworker/highstakes/internal/architecture"
	"github.com/zanetworker/highstakes/internal/types"
)

// architecturePath is where humans declare runtime links and placements.
var architecturePath = filepath.Join(".heatmap", "architecture.yaml")

// loadDeclared reads .heatmap/architecture.yaml (absent: empty) and checks
// it against the heatmap's unit names. Problems exit 2.
func loadDeclared(hm *types.Heatmap) (types.DeclaredArchitecture, error) {
	d, found, err := architecture.Load(architecturePath)
	if err != nil {
		if errors.Is(err, architecture.ErrInvalid) {
			return d, cliError(2, "%v", err)
		}
		return d, cliError(1, "%v", err)
	}
	if !found {
		return d, nil
	}
	names := make([]string, 0, len(hm.Metadata.Units))
	for _, u := range hm.Metadata.Units {
		names = append(names, u.Name)
	}
	if err := architecture.Validate(d, names); err != nil {
		return d, cliError(2, "%s: %v", architecturePath, err)
	}
	return d, nil
}

// writeOutput writes JSON to path, or to stdout when path is "".
func writeOutput(v any, path string) error {
	if path == "" {
		return printJSON(v)
	}
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return cliError(1, "encode %s: %v", path, err)
	}
	if err := os.WriteFile(path, append(b, '\n'), 0o644); err != nil {
		return cliError(1, "write %s: %v", path, err)
	}
	fmt.Fprintf(os.Stderr, "Wrote %s\n", path)
	return nil
}

var architectureCmd = &cobra.Command{
	Use:   "architecture",
	Short: "Get a generated architecture diagram of the runtime units (SeeCode spec or JSON)",
	Long: `Build an architecture picture from the heatmap: the top product runtime
units by max impact, the library shared by the most units, and one "other
units" node for the rest. Arrows are imports (labelled with importing files)
and the runtime links declared in .heatmap/architecture.yaml (dashed).
Groups are declared placements, else the first inferred deployment hint.
Every node lists its top 3 files by impact as evidence.

--format seecode emits a SeeCode architecture spec to render with SeeCode;
--format json (or --json) prints the same diagram as plain data.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		format, _ := cmd.Flags().GetString("format")
		if j, _ := cmd.Flags().GetBool("json"); j {
			format = "json"
		}
		maxNodes, _ := cmd.Flags().GetInt("max-nodes")
		out, _ := cmd.Flags().GetString("out")
		includeSupporting, _ := cmd.Flags().GetBool("include-supporting")
		if format != "seecode" && format != "json" {
			return cliError(2, "--format %q is not supported (valid: seecode, json)", format)
		}

		hm, err := loadHeatmap()
		if err != nil {
			return err
		}
		decl, err := loadDeclared(hm)
		if err != nil {
			return err
		}
		d, err := architecture.Build(hm, decl, architecture.Options{
			MaxNodes: maxNodes, IncludeSupporting: includeSupporting, Source: os.ReadFile,
		})
		switch {
		case errors.Is(err, architecture.ErrInvalidMaxNodes):
			return cliError(2, "--max-nodes must be 2-24 (got: %d)", maxNodes)
		case errors.Is(err, architecture.ErrNoUnits):
			return cliError(4, "no runtime units in the heatmap (no entry points found, or it predates units: re-run highstakes analyze)")
		case errors.Is(err, architecture.ErrOnlySupporting):
			return cliError(4, "every runtime unit is supporting (examples, scripts, helper bins). Add --include-supporting")
		case err != nil:
			return cliError(1, "build architecture: %v", err)
		}
		if format == "seecode" {
			return writeOutput(architecture.SeeCode(d), out)
		}
		return writeOutput(d, out)
	},
}

func init() {
	architectureCmd.Flags().String("format", "seecode", "Output format: seecode (SeeCode architecture spec) or json (plain diagram)")
	architectureCmd.Flags().Bool("json", false, "Same as --format json")
	architectureCmd.Flags().Int("max-nodes", 9, "Most boxes to draw (2-24); the rest fold into one \"other units\" box")
	architectureCmd.Flags().String("out", "", "Write to this file instead of stdout")
	architectureCmd.Flags().Bool("include-supporting", false, "Include supporting units (examples, scripts, tests, helper bins)")
	scoreCmd.AddCommand(architectureCmd)
}
