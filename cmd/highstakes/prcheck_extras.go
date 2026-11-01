package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/zanetworker/highstakes/internal/ownership"
	"github.com/zanetworker/highstakes/internal/policy"
	"github.com/zanetworker/highstakes/internal/prscore"
	"github.com/zanetworker/highstakes/internal/types"
)

// exitBlocked is returned by `pr check --fail-on` when the PR tier meets the gate.
const exitBlocked = 5

// shadowLogPath collects `pr check --shadow` decisions for later calibration.
var shadowLogPath = filepath.Join(".heatmap", "shadow.jsonl")

// diffPaths returns paths between base and HEAD with the given
// --diff-filter status (A added, D deleted). Renames count as a delete plus
// an add, matching pr check's --no-renames numstat. Errors yield an empty
// set: the caller already validated the base with git diff --numstat.
func diffPaths(base, filter string) map[string]bool {
	paths := map[string]bool{}
	out, err := exec.Command("git", "diff", "--no-renames", "--diff-filter="+filter, "--name-only", base+"...HEAD").Output()
	if err != nil {
		return paths
	}
	for _, p := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if p != "" {
			paths[p] = true
		}
	}
	return paths
}

// prAuthor returns the --author flag, else the HEAD commit's (mailmap-aware) author email.
func prAuthor(flag string) string {
	if flag != "" {
		return flag
	}
	out, err := exec.Command("git", "log", "-1", "--format=%aE", "HEAD").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// applyAutonomy sets the PR's agent autonomy with autonomy.max_level from
// the config; an unreadable config caps at L1 rather than allow more.
func applyAutonomy(risk *types.PRRisk, hm *types.Heatmap) {
	maxLevel := 1
	if cfg, err := loadConfigOrDefault(); err != nil {
		fmt.Fprintf(os.Stderr, "warning: %v; agent autonomy capped at L1\n", err)
	} else {
		maxLevel, _ = cfg.Autonomy.Max() // validated when the config was loaded
	}
	prscore.ApplyAutonomy(risk, hm, maxLevel)
}

// addReviewers fills SuggestedReviewers; ownership is best effort, so git
// failures are reported on stderr and leave the list empty.
func addReviewers(risk *types.PRRisk, author string, n int) {
	if n <= 0 {
		return
	}
	cfg, err := loadConfigOrDefault()
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: %v; reviewer aliases ignored\n", err)
	}
	paths := make([]string, 0, len(risk.FilesChanged))
	for _, fc := range risk.FilesChanged {
		paths = append(paths, fc.Path)
	}
	owners, err := ownership.FileOwners(".", paths, ownership.Options{Aliases: cfg.Owners})
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: no suggested reviewers: %v\n", err)
		return
	}
	risk.SuggestedReviewers = prscore.SuggestReviewers(*risk, owners, author, n)
}

// applyGate sets FailOn/Decision/Shadow. Shadow mode defaults the gate to
// critical (matching the shipped workflow) and never returns a block error.
func applyGate(risk *types.PRRisk, failOn string, shadow bool) error {
	if failOn == "" && shadow {
		failOn = string(types.TierCritical)
	}
	risk.Shadow = shadow
	if failOn == "" {
		return nil
	}
	tier, err := policy.ParseTier(failOn)
	if err != nil {
		return cliError(2, "--fail-on: %v", err)
	}
	risk.FailOn = tier
	risk.Decision = prscore.Decide(risk.Tier, tier)
	if risk.Decision == prscore.DecisionBlock && !shadow {
		return cliError(exitBlocked, "PR blocked: tier %s is at or above --fail-on %s", risk.Tier, tier)
	}
	return nil
}

// appendShadowLog appends one JSON line per shadow run.
func appendShadowLog(path, base string, risk *types.PRRisk) error {
	head, _ := exec.Command("git", "rev-parse", "HEAD").Output() // best effort; empty outside a repo
	rec := map[string]interface{}{
		"time":              time.Now().UTC().Format(time.RFC3339),
		"base":              base,
		"head":              strings.TrimSpace(string(head)),
		"tier":              risk.Tier,
		"heat_score":        risk.HeatScore,
		"change_risk":       risk.ChangeRisk,
		"change_risk_level": risk.ChangeRiskLevel,
		"fail_on":           risk.FailOn,
		"decision":          risk.Decision,
		"files":             len(risk.FilesChanged),
	}
	line, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Dir(path), err)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open shadow log %s: %w", path, err)
	}
	defer f.Close()
	if _, err := f.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("write shadow log %s: %w", path, err)
	}
	return nil
}

// printPRText prints pr check for a terminal: verdict, the before-merge
// checklist, files with both axes and their repo cutoffs, backlog, gate.
func printPRText(risk *types.PRRisk) {
	fmt.Printf("%s\n", strings.ToUpper(risk.Verdict))
	if risk.AutonomyReason != "" {
		fmt.Printf("AI may auto-merge: %s (%s)\n", map[bool]string{true: "yes", false: "no"}[risk.AutoMergeAllowed], risk.AutonomyReason)
	}
	fmt.Printf("Change risk: %d (%s)\n", risk.ChangeRisk, risk.ChangeRiskLevel)
	for _, f := range risk.ChangeRiskFactors {
		fmt.Printf("  - %s\n", f)
	}
	if len(risk.SuggestedReviewers) > 0 {
		var names []string
		for _, r := range risk.SuggestedReviewers {
			names = append(names, r.Handle)
		}
		fmt.Printf("Suggested reviewers: %s\n", strings.Join(names, ", "))
	}
	if len(risk.Checklist) > 0 {
		fmt.Println("\nDo before merge")
		for _, a := range risk.Checklist {
			fmt.Printf("  %s %s (%s)\n", checkbox(a.Done), a.Text, strings.Join(a.Files, ", "))
		}
	}
	if len(risk.WorthDoing) > 0 {
		fmt.Println("\nWorth doing (does not block)")
		for _, a := range risk.WorthDoing {
			fmt.Printf("  - %s (%s)\n", a.Text, strings.Join(a.Files, ", "))
		}
	}

	fmt.Printf("\nFiles (repo cutoffs: will it break %d, how bad %d; %s)\n",
		risk.LikelyCutoff, risk.ImpactThreshold, strings.ToLower(strings.TrimSuffix(types.AxisNote, ".")))
	files := append([]types.FileChange(nil), risk.FilesChanged...)
	sort.SliceStable(files, func(i, j int) bool {
		if a, b := tierOrder[files[i].Tier], tierOrder[files[j].Tier]; a != b {
			return a > b
		}
		return files[i].Impact > files[j].Impact
	})
	for _, fc := range files {
		posture := fc.QuadrantLabel
		if posture == "" {
			posture = types.TierPosture(fc.Tier)
		}
		axes := fmt.Sprintf("break %3d  bad %3d", fc.Likelihood, fc.Impact)
		switch {
		case fc.Unscored:
			axes = "new file, inherited"
		case !fc.ImpactAssessed:
			axes = fmt.Sprintf("break %3d  bad ~%2d", fc.Likelihood, fc.Impact)
		}
		fmt.Printf("  %-10s  %-19s  %s  +%d/-%d\n", posture, axes, fc.Path, fc.LinesAdded, fc.LinesDeleted)
	}

	if len(risk.Backlog) > 0 {
		fmt.Println("\nBacklog (the file over time, not this PR)")
		for _, a := range risk.Backlog {
			fmt.Printf("  [ ] %s (%s)\n", a.Text, strings.Join(a.Files, ", "))
			if a.Command != "" {
				fmt.Printf("      %s\n", a.Command)
			}
		}
	}
	if len(risk.CircuitBreakerSignals) > 0 {
		fmt.Println("\nCircuit breakers")
		for _, s := range risk.CircuitBreakerSignals {
			fmt.Printf("  - %s\n", s)
		}
	}
	printDecision(risk)
}

// printDecision prints the gate outcome, if a gate was set.
func printDecision(risk *types.PRRisk) {
	if risk.Decision == "" {
		return
	}
	mode := ""
	if risk.Shadow {
		mode = " (shadow: not enforced)"
	}
	fmt.Printf("Decision: %s (fail-on %s)%s\n", strings.ToUpper(risk.Decision), risk.FailOn, mode)
}
