package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/zanetworker/highstakes/internal/prdata"
	"github.com/zanetworker/highstakes/internal/prstate"
)

// newPRAPI builds the GitHub access for one PR; tests swap in a fake.
var newPRAPI = func(repo prdata.Repo, pr int) prstate.API {
	return &prstate.GH{Repo: repo, Number: pr}
}

// prTarget resolves --repo, --pr and --head-sha, which every PR state
// command needs (repo and PR default from Actions' environment).
func prTarget(cmd *cobra.Command) (prstate.API, int, string, error) {
	repoFlag, _ := cmd.Flags().GetString("repo")
	prFlag, _ := cmd.Flags().GetInt("pr")
	head, _ := cmd.Flags().GetString("head-sha")
	repoStr, err := commentRepo(repoFlag, os.Getenv("GITHUB_REPOSITORY"), func() (prdata.Repo, error) { return prdata.OriginRepo(".") })
	if err != nil {
		return nil, 0, "", cliError(2, "%v", err)
	}
	if repoStr == "" {
		return nil, 0, "", cliError(2, "--repo is required (owner/name) outside GitHub Actions and without an origin remote")
	}
	pr, err := prNumber(prFlag, os.Getenv("GITHUB_REF"))
	if err != nil {
		return nil, 0, "", cliError(2, "%v", err)
	}
	if pr == 0 {
		return nil, 0, "", cliError(2, "--pr is required outside a GitHub Actions pull_request run")
	}
	if head == "" {
		return nil, 0, "", cliError(2, "--head-sha is required: the PR head commit the run assesses")
	}
	repo, _ := prdata.ParseRepo(repoStr) // commentRepo returns a valid owner/name
	return newPRAPI(repo, pr), pr, head, nil
}

// printOutputs writes key=value lines (the $GITHUB_OUTPUT format), or JSON.
func printOutputs(cmd *cobra.Command, keys []string, values map[string]any) error {
	if j, _ := cmd.Flags().GetBool("json"); j {
		return printJSON(values)
	}
	for _, k := range keys {
		v := values[k]
		if xs, ok := v.([]string); ok {
			v = strings.Join(xs, ",")
		}
		fmt.Printf("%s=%v\n", k, v)
	}
	return nil
}

var prSignoffCmd = &cobra.Command{
	Use:   "signoff",
	Short: "Verify and record the highstakes:signed-off label for the PR head (CI); prints signed= and label_state=",
	RunE: func(cmd *cobra.Command, args []string) error {
		api, pr, head, err := prTarget(cmd)
		if err != nil {
			return err
		}
		action, _ := cmd.Flags().GetString("action")
		label, _ := cmd.Flags().GetString("label")
		pushedAt, _ := cmd.Flags().GetString("pushed-at")
		res, err := prstate.VerifySignOff(context.Background(), api, prstate.SignOffInput{
			PR: pr, Action: action, Label: label, HeadSHA: head, PushedAt: pushedAt,
		})
		if err != nil {
			return cliError(3, "verify sign-off: %v", err)
		}
		for _, n := range res.Notes {
			fmt.Fprintln(os.Stderr, n)
		}
		return printOutputs(cmd, []string{"signed", "label_state"}, map[string]any{
			"signed": res.Signed, "label_state": res.LabelState, "notes": res.Notes,
		})
	},
}

var prStateCmd = &cobra.Command{
	Use:   "state",
	Short: "Print the PR's live state (CI): head, auto_merge, label_state, approvals and maintainers of --head-sha",
	RunE: func(cmd *cobra.Command, args []string) error {
		api, _, head, err := prTarget(cmd)
		if err != nil {
			return err
		}
		s, err := prstate.ReadState(context.Background(), api, head)
		if err != nil {
			return cliError(3, "read PR state: %v", err)
		}
		return printOutputs(cmd, []string{"head", "auto_merge", "label_state", "approvals", "maintainers"}, stateValues(s))
	},
}

func stateValues(s prstate.State) map[string]any {
	nz := func(xs []string) []string {
		if xs == nil {
			return []string{}
		}
		return xs
	}
	return map[string]any{"head": s.Head, "auto_merge": s.AutoMerge, "label_state": s.LabelState,
		"approvals": nz(s.Approvals), "maintainers": nz(s.Maintainers)}
}

var prFreshCmd = &cobra.Command{
	Use:   "fresh",
	Short: "Exit 5 when the PR changed since the run assessed it (CI): head, auto-merge, sign-off label or approvals",
	RunE: func(cmd *cobra.Command, args []string) error {
		autoFlag, _ := cmd.Flags().GetString("auto-merge")
		auto, err := parseAutoMerge(autoFlag)
		if err != nil || auto == "unknown" {
			return cliError(2, "--auto-merge must be true or false (got: %q)", autoFlag)
		}
		api, _, head, err := prTarget(cmd)
		if err != nil {
			return err
		}
		labelState, _ := cmd.Flags().GetString("label-state")
		approvals, _ := cmd.Flags().GetStringSlice("approvals")
		maintainers, _ := cmd.Flags().GetStringSlice("maintainers")
		assessed := prstate.State{Head: head, AutoMerge: auto == "true", LabelState: labelState, Approvals: approvals, Maintainers: maintainers}
		now, err := prstate.ReadState(context.Background(), api, head)
		if err != nil {
			return cliError(3, "read PR state: %v", err)
		}
		stale := prstate.Stale(assessed, now)
		if j, _ := cmd.Flags().GetBool("json"); j {
			if stale == nil {
				stale = []string{}
			}
			if err := printJSON(map[string]any{"fresh": len(stale) == 0, "changed": stale}); err != nil {
				return err
			}
		}
		if len(stale) > 0 {
			return cliError(5, "PR changed during this run (%s); the run that change triggered decides", strings.Join(stale, "; "))
		}
		fmt.Fprintln(os.Stderr, "fresh: the PR is as assessed")
		return nil
	},
}

func init() {
	for _, c := range []*cobra.Command{prSignoffCmd, prStateCmd, prFreshCmd} {
		c.Flags().String("repo", "", "GitHub owner/name (default: $GITHUB_REPOSITORY, then the origin remote)")
		c.Flags().Int("pr", 0, "PR number (default: from $GITHUB_REF refs/pull/N/merge)")
		c.Flags().String("head-sha", "", "The PR head commit the run assesses (github.event.pull_request.head.sha)")
		prCmd.AddCommand(c)
	}
	for _, c := range []*cobra.Command{prSignoffCmd, prStateCmd, prFreshCmd} {
		c.Flags().Bool("json", false, "Output as JSON instead of key=value lines")
	}
	prSignoffCmd.Flags().String("action", "", "The event action (github.event.action): labeled, unlabeled, synchronize, ...")
	prSignoffCmd.Flags().String("label", "", "The label of a labeled/unlabeled event (github.event.label.name)")
	prSignoffCmd.Flags().String("pushed-at", "", "When the PR was last updated, RFC 3339 (github.event.pull_request.updated_at); a push removes a label added before it")
	prFreshCmd.Flags().String("auto-merge", "", "Auto-merge as assessed: true or false")
	prFreshCmd.Flags().String("label-state", "", "label_state as assessed (from pr signoff)")
	prFreshCmd.Flags().StringSlice("approvals", nil, "Approvals as assessed (from pr state)")
	prFreshCmd.Flags().StringSlice("maintainers", nil, "Approvers with write access as assessed (from pr state)")
}
