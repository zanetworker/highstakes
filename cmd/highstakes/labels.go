package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"github.com/zanetworker/highstakes/internal/calibration"
)

var createLabelsCmd = &cobra.Command{
	Use:   "labels",
	Short: "Write a blind CSV sheet for maintainers to rate how bad a bug in each file would be",
	Long: `Writes a CSV labelling sheet (path, maintainer_rating, notes) that maintainers
fill in with a rating from 1 to 5, where 5 means "a bug here would be a serious
incident". The sheet mixes the --top files HighStakes rates highest on blast
radius (heat when the heatmap has no LLM assessment) with --random other
non-test files, shuffled, and shows no HighStakes scores, so the ratings stay
blind. Score the filled sheet with "highstakes check calibration --labels FILE".`,
	RunE: func(cmd *cobra.Command, args []string) error {
		jsonOut, _ := cmd.Flags().GetBool("json")
		top, _ := cmd.Flags().GetInt("top")
		random, _ := cmd.Flags().GetInt("random")
		seed, _ := cmd.Flags().GetInt64("seed")
		out, _ := cmd.Flags().GetString("out")

		if top < 0 || random < 0 || top+random < 1 {
			return cliError(2, "invalid --top %d / --random %d: both must be 0 or more and at least one above 0", top, random)
		}
		hm, err := loadHeatmap()
		if err != nil {
			return err
		}
		paths, rankedBy := calibration.SampleForLabels(hm, top, random, seed)
		if len(paths) == 0 {
			return cliError(4, "no non-test source files in the heatmap; run analyze first")
		}

		var sheet bytes.Buffer
		if err := calibration.WriteLabelSheet(&sheet, paths); err != nil {
			return cliError(1, "encode labelling sheet: %v", err)
		}
		if out != "" {
			if err := os.WriteFile(out, sheet.Bytes(), 0o644); err != nil {
				return cliError(1, "write %s: %v", out, err)
			}
		}
		if jsonOut {
			// paths only, in sheet order: which file came from which group stays hidden.
			return printJSON(map[string]interface{}{
				"ranked_by": rankedBy, "top": top, "random": random, "seed": seed,
				"files": len(paths), "out": out, "paths": paths,
			})
		}
		if out == "" {
			fmt.Print(sheet.String())
		} else {
			fmt.Fprintf(os.Stderr, "Wrote %s (%d files; top by %s plus random, shuffled).\n", out, len(paths), rankedBy)
		}
		fmt.Fprintln(os.Stderr, `Ask maintainers to fill maintainer_rating 1-5 (5 = "a bug here would be a serious incident"), leave unsure rows blank, then run: highstakes check calibration --labels FILE`)
		return nil
	},
}

// checkLabels scores the heatmap against a filled labelling sheet.
func checkLabels(file string, k int, jsonOut bool) error {
	f, err := os.Open(file)
	if err != nil {
		if os.IsNotExist(err) {
			return cliError(4, "labels file %s not found", file)
		}
		return cliError(1, "open %s: %v", file, err)
	}
	defer f.Close()
	labels, err := calibration.ReadLabels(f)
	if err != nil {
		return cliError(2, "invalid labels file %s: %v", file, err)
	}
	hm, err := loadHeatmap()
	if err != nil {
		return err
	}
	r := calibration.ScoreLabels(hm, labels, k)
	if jsonOut {
		return printJSON(r)
	}
	printLabelReport(os.Stdout, r)
	return nil
}

func printLabelReport(out io.Writer, r *calibration.LabelReport) {
	fmt.Fprintln(out, "Calibration against maintainer ratings")
	fmt.Fprintf(out, "Rated files: %d   in heatmap: %d   rated >= %d: %d (base rate %.1f%%)\n",
		r.Rated, r.Matched, calibration.RatingPositive, r.Positives, 100*r.BaseRate)
	if r.TooFew {
		fmt.Fprintf(out, "WARNING: only %d rated file(s) are in the heatmap (under %d). Too few for these numbers to mean anything.\n", r.Matched, calibration.MinFilesWithEvents)
	}
	fmt.Fprintf(out, "\nSignals (K=%d; positive = rating >= %d; spearman vs rating)\n", r.K, calibration.RatingPositive)
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', tabwriter.AlignRight)
	fmt.Fprintln(tw, "signal\tfiles\tprecision@K\tlift\tspearman\t")
	var notes []string
	for _, s := range r.Signals {
		if !s.Available {
			fmt.Fprintf(tw, "%s\t%d\tn/a\tn/a\tn/a\t\n", s.Name, s.Files)
		} else {
			rho := "n/a"
			if s.Spearman != nil {
				rho = fmt.Sprintf("%.2f", *s.Spearman)
			}
			fmt.Fprintf(tw, "%s\t%d\t%.2f\t%.2f\t%s\t\n", s.Name, s.Files, s.PrecisionAtK, s.Lift, rho)
		}
		if s.Note != "" {
			notes = append(notes, fmt.Sprintf("  %s: %s", s.Name, s.Note))
		}
	}
	tw.Flush()
	for _, n := range notes {
		fmt.Fprintln(out, n)
	}
	if len(r.Unmatched) > 0 {
		fmt.Fprintf(out, "\nRated but not in the heatmap: %s\n", strings.Join(r.Unmatched, ", "))
	}
}

func init() {
	createLabelsCmd.Flags().Bool("json", false, "Output the sampled paths and settings as JSON")
	createLabelsCmd.Flags().Int("top", 20, "Files HighStakes rates highest on blast radius (heat without LLM)")
	createLabelsCmd.Flags().Int("random", 20, "Randomly sampled other non-test files")
	createLabelsCmd.Flags().Int64("seed", 1, "Random seed; the same seed gives the same sheet")
	createLabelsCmd.Flags().String("out", "", "Write the CSV to this file (default stdout)")
	createLabelsCmd.SetFlagErrorFunc(func(_ *cobra.Command, err error) error {
		return cliError(2, "%v", err)
	})
	createCmd.AddCommand(createLabelsCmd)
}
