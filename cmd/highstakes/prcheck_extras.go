package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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

// addedFiles returns paths added between base and HEAD. Errors yield an empty set:
// the caller already validated the base with git diff --numstat.
func addedFiles(base string) map[string]bool {
	added := map[string]bool{}
	out, err := exec.Command("git", "diff", "--diff-filter=A", "--name-only", base+"...HEAD").Output()
	if err != nil {
		return added
	}
	for _, p := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if p != "" {
			added[p] = true
		}
	}
	return added
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

// printPRExtras prints the two-axis summary, reviewers and gate decision.
func printPRExtras(risk *types.PRRisk) {
	fmt.Printf("Importance: %d (%s)   Change risk: %d (%s)\n",
		risk.Importance, risk.Tier, risk.ChangeRisk, risk.ChangeRiskLevel)
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
	fmt.Println()
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
