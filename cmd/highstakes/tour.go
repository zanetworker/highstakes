package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/zanetworker/highstakes/internal/tour"
	"github.com/zanetworker/highstakes/internal/types"
	"github.com/zanetworker/highstakes/internal/units"
)

// tourCmd is a subcommand of `get`; cobra matches subcommands before
// positional args, so `get tour` and `get <file>` both work.
var tourCmd = &cobra.Command{
	Use:   "tour",
	Short: "Get a reading path for learning the code (most important first, dependencies before them)",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		format, _ := cmd.Flags().GetString("format")
		if j, _ := cmd.Flags().GetBool("json"); j {
			format = "json"
		}
		out, _ := cmd.Flags().GetString("out")
		if format != "text" && format != "json" && format != "walkthrough" {
			return cliError(2, "--format %q is not supported (valid: text, json, walkthrough)", format)
		}
		limit, _ := cmd.Flags().GetInt("limit")
		focus, _ := cmd.Flags().GetString("focus")
		perComponent, _ := cmd.Flags().GetInt("per-component")
		minImpact, _ := cmd.Flags().GetInt("min-impact")
		components, _ := cmd.Flags().GetStringSlice("components")
		includeSupporting, _ := cmd.Flags().GetBool("include-supporting")

		cfg, err := loadConfigOrDefault()
		if err != nil {
			return err
		}

		hm, err := loadHeatmap()
		if err != nil {
			return err
		}

		paths := make([]string, 0, len(hm.Files))
		for p := range hm.Files {
			paths = append(paths, p)
		}
		markers := tour.Markers(paths, func(p string) bool {
			_, err := os.Stat(p)
			return err == nil
		})

		tr, err := tour.Build(hm, tour.Options{
			Limit: limit, Focus: focus, Source: os.ReadFile,
			PerComponent: perComponent, MinImpact: minImpact, Components: components, Markers: markers,
			Supporting: cfg.Tour.Supporting, IncludeSupporting: includeSupporting,
		})
		switch {
		case errors.Is(err, tour.ErrInvalidLimit):
			return cliError(2, "--limit must be 0 (all) or a positive number (got: %d)", limit)
		case errors.Is(err, tour.ErrInvalidPerComponent):
			return cliError(2, "--per-component must be 0 (off) or a positive number (got: %d)", perComponent)
		case errors.Is(err, tour.ErrInvalidMinImpact):
			return cliError(2, "--min-impact must be 0-100 (got: %d)", minImpact)
		case errors.Is(err, tour.ErrUnknownComponent):
			return cliError(2, "--components: %v", err)
		case errors.Is(err, tour.ErrInvalidSupporting):
			return cliError(2, "tour.supporting in .heatmap/config.yaml: %v", err)
		case errors.Is(err, tour.ErrOnlySupporting):
			return cliError(4, "only supporting components (CI, deployment, scripts, examples) match --focus %q and --min-impact %d. Add --include-supporting", focus, minImpact)
		case errors.Is(err, tour.ErrBelowMinImpact):
			return cliError(4, "no component under --focus %q reaches --min-impact %d. Lower --min-impact", focus, minImpact)
		case errors.Is(err, tour.ErrNoFiles):
			return cliError(4, "no heatmap files under --focus %q. Use a repo-relative directory or file path", focus)
		case err != nil:
			return cliError(1, "build tour: %v", err)
		}

		switch format {
		case "json":
			return writeOutput(tr, out)
		case "walkthrough":
			page, err := tour.Walkthrough(tr, componentRoles(hm.Metadata.Units), tour.ComponentLinks(hm, markers))
			if err != nil {
				return cliError(4, "%v", err)
			}
			return writeOutput(page, out)
		}
		if out == "" {
			printTour(os.Stdout, tr)
			return nil
		}
		f, err := os.Create(out)
		if err != nil {
			return cliError(1, "write %s: %v", out, err)
		}
		printTour(f, tr)
		if err := f.Close(); err != nil {
			return cliError(1, "write %s: %v", out, err)
		}
		fmt.Fprintf(os.Stderr, "Wrote %s\n", out)
		return nil
	},
}

// componentRoles labels components for walkthrough boxes: the unit owning
// a unit home or linking a component alone, or "shared by N units" for a
// library several units link (the own/shared split of list units).
func componentRoles(us []types.Unit) map[string]string {
	roles := map[string]string{}
	owner := units.Owners(us)
	for home, name := range owner {
		roles[home] = name
	}
	for _, u := range us {
		for _, c := range u.Components {
			if roles[c] == "" && owner[c] == "" {
				roles[c] = u.Name
			}
		}
	}
	linkers := map[string]int{}
	for _, u := range us {
		for _, c := range u.Shared {
			linkers[c]++
		}
	}
	for c, n := range linkers {
		roles[c] = fmt.Sprintf("shared by %d units", n)
	}
	return roles
}

func printTour(w io.Writer, tr *tour.Tour) {
	comps := fmt.Sprintf("%d product %s", tr.ProductComponents, plural(tr.ProductComponents, "component"))
	if tr.SupportingComponents > 0 {
		comps = fmt.Sprintf("%d product and %d supporting components", tr.ProductComponents, tr.SupportingComponents)
	}
	header := fmt.Sprintf("Tour: %d stops across %s, dependencies first", tr.TotalStops, comps)
	if tr.Focus != "" {
		header += " (focus: " + tr.Focus + ")"
	}
	fmt.Fprintln(w, header)
	if tr.SupportingHidden > 0 {
		fmt.Fprintf(w, "%d supporting %s hidden (--include-supporting shows them)\n", tr.SupportingHidden, plural(tr.SupportingHidden, "component"))
	}

	inSupporting := false
	for _, s := range tr.Sections {
		if s.Kind == tour.KindSupporting && !inSupporting {
			inSupporting = true
			fmt.Fprintln(w, "\nSupporting: CI, deployment, scripts and examples")
		}
		fmt.Fprintf(w, "\n%d. %s  [level %d, max how bad %d, %s]\n", s.Order, s.Title, s.Level, s.MaxImpact, types.TierPosture(s.Tier))
		for _, st := range s.Stops {
			fmt.Fprintf(w, "   %d. %s  %s\n", st.Order, st.Anchor, st.Verdict)
			fmt.Fprintf(w, "      %s\n", tour.StopAxes(st, tr.LikelyCutoff, tr.ImpactThreshold))
			fmt.Fprintf(w, "      why: %s\n", st.Why)
			if e := st.Explanation; e != nil && len(e.LikelihoodReasons) > 0 {
				fmt.Fprintf(w, "      break signals: %s\n", e.LikelihoodReasons[0])
			}
			if a := st.TopBeforeMerge; a != nil {
				fmt.Fprintf(w, "      before merge: %s\n", a.Text)
			}
			if a := st.TopBacklog; a != nil {
				fmt.Fprintf(w, "      backlog: %s\n", a.Text)
			}
			if len(st.DependsOn) > 0 {
				fmt.Fprintf(w, "      depends on: %s%s\n", strings.Join(st.DependsOn, ", "), more(st.DependsOnCount, len(st.DependsOn)))
			}
			if len(st.UsedBy) > 0 {
				fmt.Fprintf(w, "      used by: %s%s\n", strings.Join(st.UsedBy, ", "), more(st.UsedByCount, len(st.UsedBy)))
			}
			if len(st.Owners) > 0 {
				fmt.Fprintf(w, "      owners: %s\n", strings.Join(st.Owners, ", "))
			}
			if st.Notes != "" {
				fmt.Fprintf(w, "      notes: %s\n", st.Notes)
			}
		}
	}
}

func more(total, shown int) string {
	if total > shown {
		return fmt.Sprintf(" (+%d more)", total-shown)
	}
	return ""
}

func init() {
	tourCmd.Flags().Bool("json", false, "Output as JSON (same as --format json)")
	tourCmd.Flags().String("format", "text", "Output format: text, json, or walkthrough (data for the show-me rich step-through)")
	tourCmd.Flags().String("out", "", "Write to this file instead of stdout")
	tourCmd.Flags().Int("limit", 0, "Keep the N most impactful files (0 = all); ignored with --per-component")
	tourCmd.Flags().Int("per-component", 0, "Keep the N most important files of each component, one section per component (0 = off)")
	tourCmd.Flags().Int("min-impact", 0, "Skip files whose impact is below this, and components left without files (0-100)")
	tourCmd.Flags().Bool("include-supporting", false, "Add supporting components (examples, scripts, .github, docs, deploy, tests, benchmarks, vendor; tour.supporting in config) in a final part")
	tourCmd.Flags().StringSlice("components", nil, "Only these components, comma-separated (e.g. crates/core,internal/api)")
	tourCmd.Flags().String("focus", "", "Only files under this repo-relative directory or file")
	scoreCmd.AddCommand(tourCmd)
}

func plural(n int, word string) string {
	if n == 1 {
		return word
	}
	return word + "s"
}
