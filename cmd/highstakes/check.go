package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
	"github.com/zanetworker/highstakes/internal/calibration"
	"github.com/zanetworker/highstakes/internal/gitanalyzer"
	"github.com/zanetworker/highstakes/internal/incidents"
)

// --- check ---

var checkCmd = &cobra.Command{
	Use:   "check",
	Short: "Check how well the heatmap matches reality",
}

var checkCalibrationCmd = &cobra.Command{
	Use:   "calibration",
	Short: "Compare heat scores with reverts, bug fixes, security fixes and incidents in git history",
	Long: `Replays git history and labels files touched by reverts, bug-fix commits,
security fixes (CVE-, GHSA-, "security", "vuln") and recorded incidents. Reports
per-tier event rates, precision@K and recall@K of the K hottest files, lift over
the base rate, Spearman rank correlation, and the same metrics for blast radius,
churn, complexity and centrality alone.

By default the run is in-sample: churn comes from the same history as the
events. Use --holdout to score with history up to a cutoff and count events after it.

--events picks the event kinds: all (default), fixes, reverts, or security.
Fix history measures how likely a file is to break; security fixes are a rough
proxy for impact, the axis blast radius scores.

--labels FILE skips git history and compares blast radius and heat with
maintainer ratings from a sheet made by "highstakes create labels".`,
	RunE: func(cmd *cobra.Command, args []string) error {
		jsonOut, _ := cmd.Flags().GetBool("json")
		top, _ := cmd.Flags().GetInt("top")
		limit, _ := cmd.Flags().GetInt("limit")
		sinceStr, _ := cmd.Flags().GetString("since")
		holdoutStr, _ := cmd.Flags().GetString("holdout")
		eventsStr, _ := cmd.Flags().GetString("events")
		labelsFile, _ := cmd.Flags().GetString("labels")

		if top < 1 {
			return cliError(2, "invalid --top %d: must be 1 or more", top)
		}
		if limit < 0 {
			return cliError(2, "invalid --limit %d: must be 0 or more", limit)
		}
		if holdoutStr != "" && cmd.Flags().Changed("since") {
			return cliError(2, "use --since or --holdout, not both (holdout counts events from the cutoff to now)")
		}
		events, err := calibration.ParseEventFilter(eventsStr)
		if err != nil {
			return cliError(2, "invalid --events: %v", err)
		}
		if labelsFile != "" {
			if holdoutStr != "" || cmd.Flags().Changed("since") || cmd.Flags().Changed("events") {
				return cliError(2, "--labels compares with maintainer ratings, not git history; drop --since, --holdout and --events")
			}
			return checkLabels(labelsFile, top, jsonOut)
		}

		now := time.Now()
		opts := calibration.Options{K: top, To: now, SurpriseLimit: limit, Events: events}
		var walkFrom time.Time
		if holdoutStr != "" {
			cutoff, err := parseCutoff(holdoutStr, now)
			if err != nil {
				return cliError(2, "invalid --holdout %q: %v", holdoutStr, err)
			}
			opts.From, opts.Holdout = cutoff, true
			walkFrom = cutoff.AddDate(0, 0, -90) // churn window before the cutoff
		} else {
			days, err := parseDays(sinceStr)
			if err != nil {
				return cliError(2, "invalid --since %q: %v", sinceStr, err)
			}
			opts.From = now.AddDate(0, 0, -days)
			walkFrom = opts.From
		}

		hm, err := loadHeatmap()
		if err != nil {
			return err
		}

		opts.Tiers = configuredTiers()

		store, err := incidents.NewStore(filepath.Join(".heatmap", "incidents.json"))
		if err != nil {
			return cliError(1, "load incidents: %v", err)
		}

		ga, err := gitanalyzer.New(".")
		if err != nil {
			return cliError(3, "open git repository: %v", err)
		}
		commits, err := ga.CommitsSince(walkFrom)
		if err != nil {
			return cliError(3, "read git history: %v", err)
		}
		if opts.Holdout {
			if opts.ExistingAtCutoff, err = ga.FilesAt(opts.From); err != nil {
				return cliError(3, "read files at cutoff: %v", err)
			}
		}

		report := calibration.Run(hm, commits, store.All(), opts)
		if jsonOut {
			return printJSON(report)
		}
		printCalibration(os.Stdout, report)
		return nil
	},
}

var daysPattern = regexp.MustCompile(`^(\d+)d$`)

// parseDays parses a positive day count written as "<n>d", e.g. "365d".
func parseDays(s string) (int, error) {
	m := daysPattern.FindStringSubmatch(s)
	if m == nil {
		return 0, fmt.Errorf("want a day count like 365d")
	}
	n, err := strconv.Atoi(m[1])
	if err != nil || n < 1 {
		return 0, fmt.Errorf("want at least 1d")
	}
	return n, nil
}

// parseCutoff parses a holdout cutoff: "<n>d" (n days before now) or a
// YYYY-MM-DD date (UTC midnight). The cutoff must not be in the future.
func parseCutoff(s string, now time.Time) (time.Time, error) {
	if daysPattern.MatchString(s) {
		n, err := parseDays(s)
		if err != nil {
			return time.Time{}, err
		}
		return now.AddDate(0, 0, -n), nil
	}
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return time.Time{}, fmt.Errorf("want YYYY-MM-DD or a day count like 180d")
	}
	if t.After(now) {
		return time.Time{}, fmt.Errorf("cutoff is in the future")
	}
	return t, nil
}

func printCalibration(out io.Writer, r *calibration.Report) {
	fmt.Fprintf(out, "Calibration (%s): events from %s to %s\n", r.Mode, r.From.Format("2006-01-02"), r.To.Format("2006-01-02"))
	fmt.Fprintf(out, "Events counted: %s\n", r.EventFilter)
	if r.TooFewEvents {
		fmt.Fprintf(out, "WARNING: only %d file(s) had %s events (under %d). Too few for these numbers to mean anything.\n", r.FilesWithEvents, r.EventFilter, calibration.MinFilesWithEvents)
	}
	fmt.Fprintf(out, "Files scored: %d   with events: %d   base rate: %.1f%%\n", r.Files, r.FilesWithEvents, 100*r.BaseRate)
	fmt.Fprintf(out, "Events: %d (revert %d, bug fix %d, security %d, incident %d)",
		r.Events.Total, r.Events.Revert, r.Events.BugFix, r.Events.Security, r.Events.Incident)
	if r.UnscoredEventFiles > 0 {
		fmt.Fprintf(out, "; %d more file(s) with events are not in the scored set", r.UnscoredEventFiles)
	}
	fmt.Fprintln(out)

	fmt.Fprintln(out, "\nBy tier")
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', tabwriter.AlignRight)
	fmt.Fprintln(tw, "tier\tfiles\twith events\thit rate\tevents/file\t")
	for _, t := range r.Tiers {
		fmt.Fprintf(tw, "%s\t%d\t%d\t%.1f%%\t%.2f\t\n", strings.ToUpper(string(t.Tier)), t.Files, t.FilesWithEvents, 100*t.HitRate, t.EventsPerFile)
	}
	tw.Flush()

	fmt.Fprintf(out, "\nSignals (K=%d; lift 1.0 = random, spearman vs event count)\n", r.K)
	tw = tabwriter.NewWriter(out, 0, 0, 2, ' ', tabwriter.AlignRight)
	fmt.Fprintln(tw, "signal\tprecision@K\trecall@K\tlift\tspearman\t")
	var notes []string
	for _, s := range r.Signals {
		if !s.Available {
			fmt.Fprintf(tw, "%s\tn/a\tn/a\tn/a\tn/a\t\n", s.Name)
		} else {
			rho := "n/a"
			if s.Spearman != nil {
				rho = fmt.Sprintf("%.2f", *s.Spearman)
			}
			fmt.Fprintf(tw, "%s\t%.2f\t%.2f\t%.2f\t%s\t\n", s.Name, s.PrecisionAtK, s.RecallAtK, s.Lift, rho)
		}
		if s.Note != "" {
			notes = append(notes, fmt.Sprintf("  %s: %s", s.Name, s.Note))
		}
	}
	tw.Flush()
	for _, n := range notes {
		fmt.Fprintln(out, n)
	}

	fmt.Fprintf(out, "\nTop %d hottest files\n", len(r.TopK))
	printFileResults(out, r.TopK)

	fmt.Fprintln(out, "\nSurprises: LOW tier with events")
	printFileResults(out, r.Surprises.LowTierWithEvents)
	fmt.Fprintln(out, "\nSurprises: CRITICAL/HIGH with no events")
	printFileResults(out, r.Surprises.HotWithoutEvents)

	fmt.Fprintln(out, "\nCaveats")
	for _, c := range r.Caveats {
		fmt.Fprintf(out, "  - %s\n", c)
	}
}

func printFileResults(out io.Writer, files []calibration.FileResult) {
	if len(files) == 0 {
		fmt.Fprintln(out, "  (none)")
		return
	}
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "  heat\ttier\tevents\t(rev/fix/sec/inc)\tpath")
	for _, f := range files {
		e := f.Events
		fmt.Fprintf(tw, "  %d\t%s\t%d\t(%d/%d/%d/%d)\t%s\n", f.HeatScore, strings.ToUpper(string(f.Tier)), e.Total, e.Revert, e.BugFix, e.Security, e.Incident, f.Path)
	}
	tw.Flush()
}

func init() {
	checkCalibrationCmd.Flags().Bool("json", false, "Output as JSON")
	checkCalibrationCmd.Flags().Int("top", 10, "K: how many of the hottest files to evaluate for precision@K and recall@K")
	checkCalibrationCmd.Flags().String("since", "365d", "Event window: count events in the last N days (e.g. 365d)")
	checkCalibrationCmd.Flags().String("holdout", "", "Cutoff (YYYY-MM-DD or Nd ago): score with history up to it, count events after it")
	checkCalibrationCmd.Flags().Int("limit", 10, "Max files per surprise list")
	checkCalibrationCmd.Flags().String("events", "all", "Event kinds to count: all, fixes, reverts, security")
	checkCalibrationCmd.Flags().String("labels", "", "Compare with maintainer ratings in this CSV (from create labels) instead of git history")
	checkCalibrationCmd.SetFlagErrorFunc(func(_ *cobra.Command, err error) error {
		return cliError(2, "%v", err)
	})
	checkCmd.AddCommand(checkCalibrationCmd)
	rootCmd.AddCommand(checkCmd)
}
