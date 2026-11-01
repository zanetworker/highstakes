package main

import (
	"context"
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
	"github.com/zanetworker/highstakes/internal/prdata"
	"github.com/zanetworker/highstakes/internal/types"
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

--source prs takes events from merged GitHub pull requests instead of commit
messages (needs the gh CLI, logged in). Bug-fix and revert PRs test likelihood
("Will it break?"); backports, fast reverts, severity labels, review depth and
CODEOWNERS coverage test blast radius ("How bad if it breaks?"). File lists come
from local git; PR metadata is cached in .heatmap/cache/github/. The
commit-keyword source is reported side by side. --repo owner/name overrides the
repository read from the origin remote.

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
		sourceStr, _ := cmd.Flags().GetString("source")
		repoStr, _ := cmd.Flags().GetString("repo")

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
		source, err := parseSource(sourceStr)
		if err != nil {
			return cliError(2, "invalid --source: %v", err)
		}
		if source != calibration.SourcePRs {
			if repoStr != "" {
				return cliError(2, "--repo only applies with --source prs")
			}
		} else if cmd.Flags().Changed("events") {
			return cliError(2, "--events picks commit-message event kinds; with --source prs the events are bug-fix and revert PRs, so drop --events")
		}
		var repo prdata.Repo
		if repoStr != "" {
			if repo, err = prdata.ParseRepo(repoStr); err != nil {
				return cliError(2, "invalid --repo: %v", err)
			}
		}
		if labelsFile != "" {
			if holdoutStr != "" || cmd.Flags().Changed("since") || cmd.Flags().Changed("events") || cmd.Flags().Changed("source") {
				return cliError(2, "--labels compares with maintainer ratings, not git history; drop --since, --holdout, --events and --source")
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

		if source == calibration.SourcePRs {
			if repo.Owner == "" {
				if repo, err = prdata.OriginRepo("."); err != nil {
					return cliError(2, "cannot infer the GitHub repository (%v); pass --repo owner/name", err)
				}
			}
			pr, err := runPRCalibration(cmd.Context(), prdata.GHClient{}, repo, hm, commits, store.All(), opts)
			if err != nil {
				return err
			}
			if jsonOut {
				return printJSON(pr)
			}
			printPRCalibration(os.Stdout, pr)
			return nil
		}

		report := calibration.Run(hm, commits, store.All(), opts)
		if jsonOut {
			return printJSON(report)
		}
		printCalibration(os.Stdout, report)
		return nil
	},
}

// parseSource parses a --source value (case-insensitive).
func parseSource(s string) (string, error) {
	switch v := strings.ToLower(s); v {
	case calibration.SourceCommits, calibration.SourcePRs:
		return v, nil
	}
	return "", fmt.Errorf("unknown source %q (valid: commits, prs)", s)
}

// PR fetch limits: at most maxPRs PRs per run, and stop while this many
// GraphQL points are left so other tools keep some quota.
const (
	maxPRs          = 1500
	minGraphQLQuota = 200
	fastRevertDays  = 7
)

// runPRCalibration fetches merged PRs for the window, maps them to local
// commits on HEAD, labels them, and compares the scores with the PR events.
func runPRCalibration(ctx context.Context, client prdata.Client, repo prdata.Repo, hm *types.Heatmap, commits []gitanalyzer.CommitInfo, incs []types.Incident, opts calibration.Options) (*calibration.PRReport, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	fetch, err := prdata.Fetch(ctx, client, repo, prdata.FetchOptions{
		Since: opts.From, Now: opts.To, Max: maxPRs, MinRemaining: minGraphQLQuota,
		CacheDir: filepath.Join(".heatmap", "cache", "github"),
	})
	if err != nil {
		return nil, cliError(3, "fetch merged PRs for %s: %v", repo, err)
	}
	// A day of slack: the landed commit's date can trail the merge time.
	head, err := prdata.ReadCommits(".", opts.From.AddDate(0, 0, -1), true, "HEAD")
	if err != nil {
		return nil, cliError(3, "read git history: %v", err)
	}
	prCommit := prdata.MapPRs(fetch.PRs, head)

	branches, err := prdata.BackportBranches(".")
	if err != nil {
		return nil, cliError(3, "%v", err)
	}
	var onBranches []prdata.Commit
	for _, b := range branches {
		bc, err := prdata.ReadCommits(".", opts.From, true, b)
		if err != nil {
			return nil, cliError(3, "read release branch %s: %v", b, err)
		}
		onBranches = append(onBranches, prdata.BranchOnly(bc, head)...)
	}
	backported := prdata.BackportedPRs(onBranches, prCommit)
	fastReverted := prdata.FastReverts(fetch.PRs, prCommit, head, fastRevertDays*24*time.Hour)
	labeled := prdata.Label(fetch.PRs, prCommit, backported, fastReverted)

	co, coFile, err := prdata.LoadCodeOwners(".")
	if err != nil {
		return nil, cliError(3, "%v", err)
	}

	prOpts := opts
	prOpts.Source = calibration.SourcePRs
	report := calibration.RunWithEvents(hm, commits, incs, calibration.FragilityEvents(labeled, opts.From, opts.To), prOpts)
	out := &calibration.PRReport{
		Report:        report,
		GitHub:        calibration.Summarize(repo.String(), fetch, labeled, branches, coFile),
		CommitKeyword: calibration.Run(hm, commits, incs, opts),
		Stakes:        calibration.RunStakes(hm, calibration.StakesInputs(hm, labeled, branches, co, coFile), opts.K),
	}
	report.Caveats = append(report.Caveats, calibration.CaveatStakes)
	if fetch.Capped {
		report.Caveats = append(report.Caveats, fmt.Sprintf("PR fetch stopped at the cap of %d PRs read; older PRs in the window are missing.", fetch.Cap))
	}
	if fetch.Partial {
		report.Caveats = append(report.Caveats, "PR fetch stopped early ("+fetch.PartialReason+"); results are partial. Rerun later: cached PRs are not fetched again.")
	}
	return out, nil
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

func printPRCalibration(out io.Writer, r *calibration.PRReport) {
	g := r.GitHub
	fmt.Fprintf(out, "Pull requests (%s): %d merged in the window, %d mapped to local commits", g.Repo, g.InWindow, g.Mapped)
	fmt.Fprintf(out, " (%d read from GitHub, %d from cache, %d API calls)\n", g.Fetch.Fetched, g.Fetch.FromCache, g.Fetch.APICalls)
	if g.Fetch.Capped {
		fmt.Fprintf(out, "WARNING: stopped at the cap of %d PRs.\n", g.Fetch.Cap)
	}
	if g.Fetch.Partial {
		fmt.Fprintf(out, "WARNING: partial results: %s\n", g.Fetch.PartialReason)
	}
	fmt.Fprintf(out, "Labels: bug fix %d, revert %d, backported %d, fast revert %d, severity %d, deep review %d\n\n",
		g.BugFix, g.Revert, g.Backported, g.FastRevert, g.Severity, g.DeepReview)

	fmt.Fprintln(out, "Will it break? Fragility events from PRs (bug fixes and reverts)")
	printCalibration(out, r.Report)

	fmt.Fprintf(out, "\nSame window, commit-keyword events (%d files with events, PR source %d)\n", r.CommitKeyword.FilesWithEvents, r.FilesWithEvents)
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', tabwriter.AlignRight)
	fmt.Fprintln(tw, "signal\tspearman prs\tspearman commits\tlift prs\tlift commits\t")
	for i, s := range r.Signals {
		c := r.CommitKeyword.Signals[i]
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t\n", s.Name, rhoText(s.Available, s.Spearman), rhoText(c.Available, c.Spearman), liftText(s.Available, s.Lift), liftText(c.Available, c.Lift))
	}
	tw.Flush()

	fmt.Fprintf(out, "\nHow bad if it breaks? Blast radius vs stakes evidence (K=%d)\n", r.K)
	tw = tabwriter.NewWriter(out, 0, 0, 2, ' ', tabwriter.AlignRight)
	fmt.Fprintln(tw, "signal\tfiles\twith events\tprecision@K\tlift\tspearman\t")
	var notes []string
	for _, s := range r.Stakes {
		if !s.Available {
			fmt.Fprintf(tw, "%s\tn/a\tn/a\tn/a\tn/a\tn/a\t\n", s.Name)
		} else {
			flag := ""
			if s.TooFewEvents {
				flag = " (too few)"
			}
			fmt.Fprintf(tw, "%s\t%d\t%d%s\t%.2f\t%.2f\t%s\t\n", s.Name, s.Files, s.FilesWithEvents, flag, s.PrecisionAtK, s.Lift, rhoText(true, s.Spearman))
		}
		if s.Note != "" {
			notes = append(notes, fmt.Sprintf("  %s: %s", s.Name, s.Note))
		}
	}
	tw.Flush()
	for _, n := range notes {
		fmt.Fprintln(out, n)
	}
}

func rhoText(available bool, rho *float64) string {
	if !available || rho == nil {
		return "n/a"
	}
	return fmt.Sprintf("%.2f", *rho)
}

func liftText(available bool, lift float64) string {
	if !available {
		return "n/a"
	}
	return fmt.Sprintf("%.2f", lift)
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
	checkCalibrationCmd.Flags().String("source", "commits", "Where events come from: commits (commit-message keywords) or prs (merged GitHub pull requests, via gh)")
	checkCalibrationCmd.Flags().String("repo", "", "GitHub repository owner/name for --source prs (default: from the origin remote)")
	checkCalibrationCmd.SetFlagErrorFunc(func(_ *cobra.Command, err error) error {
		return cliError(2, "%v", err)
	})
	checkCmd.AddCommand(checkCalibrationCmd)
	rootCmd.AddCommand(checkCmd)
}
