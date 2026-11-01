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
and Kubernetes manifests. Placement is inferred, not observed. Links and
placements declared in .heatmap/architecture.yaml print as "declared".
Supporting units (entry points in examples, scripts, tests or benchmarks,
and helper bins under src/bin/ of a crate with its own main) are hidden
unless --include-supporting is set.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		jsonOut, _ := cmd.Flags().GetBool("json")
		includeSupporting, _ := cmd.Flags().GetBool("include-supporting")
		hm, err := loadHeatmap()
		if err != nil {
			return err
		}
		decl, err := loadDeclared(hm)
		if err != nil {
			return err
		}
		var us []types.Unit
		hidden := 0
		for _, u := range hm.Metadata.Units {
			if u.Supporting && !includeSupporting {
				hidden++
				continue
			}
			us = append(us, u)
		}
		sort.SliceStable(us, func(i, j int) bool {
			if us[i].MaxImpact != us[j].MaxImpact {
				return us[i].MaxImpact > us[j].MaxImpact
			}
			return us[i].Name < us[j].Name
		})
		if jsonOut {
			if us == nil {
				us = []types.Unit{}
			}
			return printJSON(map[string]interface{}{"units": us, "count": len(us), "supporting_hidden": hidden, "declared": decl})
		}
		if len(hm.Metadata.Units) == 0 {
			fmt.Println("No runtime units in the heatmap (no entry points found, or it predates units: re-run highstakes analyze).")
			return nil
		}
		owner := units.Owners(hm.Metadata.Units)
		for _, u := range us {
			fmt.Print(formatUnit(u, owner, decl))
		}
		if hidden > 0 {
			them := "them"
			if hidden == 1 {
				them = "it"
			}
			fmt.Printf("%d supporting %s hidden (--include-supporting shows %s)\n", hidden, plural(hidden, "unit"), them)
		}
		return nil
	},
}

// formatUnit renders one unit for `list units`. owner maps unit homes to
// their units (units.Owners): another unit's crate is listed as a linked
// unit, not as this unit's own code. Declared links and placement (from
// .heatmap/architecture.yaml) print apart from inferred ones.
func formatUnit(u types.Unit, owner map[string]string, decl types.DeclaredArchitecture) string {
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
	for _, l := range decl.Links {
		arrow, other := "", ""
		switch u.Name {
		case l.From:
			arrow, other = "->", l.To
		case l.To:
			arrow, other = "<-", l.From
		default:
			continue
		}
		if l.Both {
			arrow = "<->"
		}
		fmt.Fprintf(&b, "  declared link: %s %s%s%s\n", arrow, other, prefixed(": ", l.Label), bracketed(l.Evidence))
	}
	for _, p := range decl.Placement {
		if p.Unit == u.Name {
			fmt.Fprintf(&b, "  declared placement: %s%s\n", p.RunsIn, bracketed(p.Evidence))
		}
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

func prefixed(prefix, s string) string {
	if s == "" {
		return ""
	}
	return prefix + s
}

func bracketed(s string) string {
	if s == "" {
		return ""
	}
	return " [" + s + "]"
}
