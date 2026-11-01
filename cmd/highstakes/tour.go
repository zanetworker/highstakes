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
)

// tourCmd is a subcommand of `get`; cobra matches subcommands before
// positional args, so `get tour` and `get <file>` both work.
var tourCmd = &cobra.Command{
	Use:   "tour",
	Short: "Get a reading path for learning the code (dependencies first, most impactful first)",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		jsonOut, _ := cmd.Flags().GetBool("json")
		limit, _ := cmd.Flags().GetInt("limit")
		focus, _ := cmd.Flags().GetString("focus")
		perComponent, _ := cmd.Flags().GetInt("per-component")
		minImpact, _ := cmd.Flags().GetInt("min-impact")
		components, _ := cmd.Flags().GetStringSlice("components")

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
		case errors.Is(err, tour.ErrBelowMinImpact):
			return cliError(4, "no component under --focus %q reaches --min-impact %d. Lower --min-impact", focus, minImpact)
		case errors.Is(err, tour.ErrNoFiles):
			return cliError(4, "no heatmap files under --focus %q. Use a repo-relative directory or file path", focus)
		case err != nil:
			return cliError(1, "build tour: %v", err)
		}

		if jsonOut {
			return printJSON(tr)
		}
		printTour(os.Stdout, tr)
		return nil
	},
}

func printTour(w io.Writer, tr *tour.Tour) {
	header := fmt.Sprintf("Tour: %d stops, dependencies first", tr.TotalStops)
	if tr.Focus != "" {
		header += " (focus: " + tr.Focus + ")"
	}
	fmt.Fprintln(w, header)

	for _, s := range tr.Sections {
		fmt.Fprintf(w, "\n%d. %s  [level %d, max impact %d, %s]\n", s.Order, s.Title, s.Level, s.MaxImpact, strings.ToUpper(string(s.Tier)))
		for _, st := range s.Stops {
			fmt.Fprintf(w, "   %d. %s  %s %s  %s\n", st.Order, st.Anchor, tierEmoji(st.Tier), st.Tier, axesLabel(st.Impact, st.Likelihood, st.ImpactAssessed, st.Quadrant))
			fmt.Fprintf(w, "      why: %s\n", st.Why)
			if e := st.Explanation; e != nil {
				if len(e.LikelihoodReasons) > 0 {
					fmt.Fprintf(w, "      likely because: %s\n", e.LikelihoodReasons[0])
				}
				if n := min(2, len(e.Actions)); n > 0 {
					texts := make([]string, n)
					for i := range texts {
						texts[i] = e.Actions[i].Text
					}
					fmt.Fprintf(w, "      do: %s\n", strings.Join(texts, "; "))
				}
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

// axesLabel renders both axes, marking an impact estimate made without the LLM.
func axesLabel(impact, likelihood int, assessed bool, q types.Quadrant) string {
	if !assessed {
		return fmt.Sprintf("impact ~%d (estimate), likelihood %d", impact, likelihood)
	}
	return fmt.Sprintf("impact %d, likelihood %d, %s", impact, likelihood, q)
}

func more(total, shown int) string {
	if total > shown {
		return fmt.Sprintf(" (+%d more)", total-shown)
	}
	return ""
}

func init() {
	tourCmd.Flags().Bool("json", false, "Output as JSON")
	tourCmd.Flags().Int("limit", 0, "Keep the N most impactful files (0 = all); ignored with --per-component")
	tourCmd.Flags().Int("per-component", 0, "Keep the N most important files of each component, one section per component (0 = off)")
	tourCmd.Flags().Int("min-impact", 0, "Skip components whose highest file impact is below this (0-100)")
	tourCmd.Flags().StringSlice("components", nil, "Only these components, comma-separated (e.g. crates/openshell-core,internal/tour)")
	tourCmd.Flags().String("focus", "", "Only files under this repo-relative directory or file")
	scoreCmd.AddCommand(tourCmd)
}
