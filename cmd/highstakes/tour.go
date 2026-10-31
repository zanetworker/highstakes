package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/zanetworker/highstakes/internal/tour"
)

// tourCmd is a subcommand of `get`; cobra matches subcommands before
// positional args, so `get tour` and `get <file>` both work.
var tourCmd = &cobra.Command{
	Use:   "tour",
	Short: "Get a risk-ordered reading path (dependencies first, hottest first)",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		jsonOut, _ := cmd.Flags().GetBool("json")
		limit, _ := cmd.Flags().GetInt("limit")
		focus, _ := cmd.Flags().GetString("focus")

		hm, err := loadHeatmap()
		if err != nil {
			return err
		}

		tr, err := tour.Build(hm, tour.Options{Limit: limit, Focus: focus, Source: os.ReadFile})
		switch {
		case errors.Is(err, tour.ErrInvalidLimit):
			return cliError(2, "--limit must be 0 (all) or a positive number (got: %d)", limit)
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
		fmt.Fprintf(w, "\n%d. %s  [level %d, max heat %d %s]\n", s.Order, s.Title, s.Level, s.MaxHeat, strings.ToUpper(string(s.Tier)))
		for _, st := range s.Stops {
			fmt.Fprintf(w, "   %d. %s  %s %d %s\n", st.Order, st.Anchor, tierEmoji(st.Tier), st.Heat, st.Tier)
			fmt.Fprintf(w, "      why: %s\n", st.Why)
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
	tourCmd.Flags().Bool("json", false, "Output as JSON")
	tourCmd.Flags().Int("limit", 0, "Keep the N hottest files (0 = all)")
	tourCmd.Flags().String("focus", "", "Only files under this repo-relative directory or file")
	scoreCmd.AddCommand(tourCmd)
}
