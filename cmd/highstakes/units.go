package main

import (
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"github.com/zanetworker/highstakes/internal/types"
	"github.com/zanetworker/highstakes/internal/units"
)

var listUnitsCmd = &cobra.Command{
	Use:   "units",
	Short: "List runtime units (binaries, scripts) and the components each links",
	Long: `List the deployable entry points found by analyze: Rust binaries, Go main
packages, package.json bin and start scripts, Python scripts and __main__.py.
Each unit lists the components it links through the import graph (own ones
first, then other units' code it links, then libraries shared with other
units), its max impact, its count of
fragile-critical files, and deployment hints from Dockerfiles, Helm values
and Kubernetes manifests. Placement is inferred, not observed.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		jsonOut, _ := cmd.Flags().GetBool("json")
		hm, err := loadHeatmap()
		if err != nil {
			return err
		}
		us := append([]types.Unit{}, hm.Metadata.Units...)
		sort.SliceStable(us, func(i, j int) bool {
			if us[i].MaxImpact != us[j].MaxImpact {
				return us[i].MaxImpact > us[j].MaxImpact
			}
			return us[i].Name < us[j].Name
		})
		if jsonOut {
			return printJSON(map[string]interface{}{"units": us, "count": len(us)})
		}
		if len(us) == 0 {
			fmt.Println("No runtime units in the heatmap (no entry points found, or it predates units: re-run highstakes analyze).")
			return nil
		}
		owner := units.Owners(us)
		for _, u := range us {
			fmt.Print(formatUnit(u, owner))
		}
		return nil
	},
}

// formatUnit renders one unit for `list units`. owner maps unit homes to
// their units (units.Owners): another unit's crate is listed as a linked
// unit, not as this unit's own code.
func formatUnit(u types.Unit, owner map[string]string) string {
	shared := map[string]bool{}
	for _, c := range u.Shared {
		shared[c] = true
	}
	var own, others []string
	for _, c := range u.Components {
		switch o := owner[c]; {
		case shared[c]:
		case o != "" && o != u.Name:
			others = append(others, o+" ("+c+")")
		default:
			own = append(own, c)
		}
	}
	estimate := ""
	if !u.ImpactAssessed {
		estimate = "~"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s  [%s]  %s (%s)\n", u.Name, u.Kind, u.Entry, u.Evidence)
	fmt.Fprintf(&b, "  impact %s%d  %d fragile-critical  %d files\n", estimate, u.MaxImpact, u.FragileCritical, u.Files)
	fmt.Fprintf(&b, "  own: %s\n", strings.Join(own, ", "))
	if len(others) > 0 {
		fmt.Fprintf(&b, "  links units: %s\n", strings.Join(others, ", "))
	}
	if len(u.Shared) > 0 {
		fmt.Fprintf(&b, "  shared: %s\n", strings.Join(u.Shared, ", "))
	}
	if len(u.InferredPlacement) == 0 {
		b.WriteString("  inferred placement: none found\n")
	}
	for _, p := range u.InferredPlacement {
		fmt.Fprintf(&b, "  inferred placement: %s [%s]\n", p.Detail, p.Evidence)
	}
	b.WriteString("\n")
	return b.String()
}
