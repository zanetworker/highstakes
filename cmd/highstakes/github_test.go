package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGithubInstall_WritesTriageWorkflow(t *testing.T) {
	dir := t.TempDir()
	if _, err := runCLI(t, dir, "github", "install"); err != nil {
		t.Fatalf("github install: %v", err)
	}
	b, err := os.ReadFile(filepath.Join(dir, ".github", "workflows", "heatmap-triage.yml"))
	if err != nil {
		t.Fatal(err)
	}
	wf := string(b)
	for _, want := range []string{
		"go install github.com/zanetworker/highstakes/cmd/highstakes@latest",
		"if [ ! -f .heatmap/config.yaml ]; then highstakes init; fi",
		`--format markdown --fail-on critical --approvals "$APPROVALS"`,
		"<!-- highstakes-triage -->",
		"pull_request_review:",
		"BASE: origin/${{ github.event.pull_request.base.ref }}",
		"types: [opened, synchronize, reopened, labeled, unlabeled, auto_merge_enabled, auto_merge_disabled]",
		"SIGNED_OFF: ${{ steps.signoff.outputs.signed }}",
		`--maintainer-approvals "$MAINTAINERS"`,
		`--auto-merge-enabled "$AUTO_MERGE"`,
		`if [ "$SIGNED_OFF" = "true" ]; then signed=--signed-off; fi`,
	} {
		if !strings.Contains(wf, want) {
			t.Errorf("workflow missing %q", want)
		}
	}
	// github.base_ref and head_ref are empty on pull_request_review events.
	for _, stale := range []string{"heat_score", "cmd/heatmap@", "estimated_review_time", "github.base_ref", "github.head_ref"} {
		if strings.Contains(wf, stale) {
			t.Errorf("workflow still contains %q", stale)
		}
	}
}

// TestTriageWorkflow_MatchesRepoWorkflow: the template differs from the
// repo's own workflow only in how highstakes is installed and the cache key.
func TestTriageWorkflow_MatchesRepoWorkflow(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", ".github", "workflows", "heatmap-triage.yml"))
	if err != nil {
		t.Fatal(err)
	}
	repo := strings.NewReplacer(
		"go install ./cmd/highstakes", "go install github.com/zanetworker/highstakes/cmd/highstakes@latest",
		"heatmap-${{ hashFiles('**/*.go') }}", "heatmap-${{ github.sha }}",
	).Replace(string(b))
	if repo != triageWorkflow {
		t.Error("cmd/highstakes/triage-workflow.yml drifted from .github/workflows/heatmap-triage.yml; copy the change over")
	}
}

// TestTriageWorkflow_SignOffIsTrusted pins the sign-off safeguards in both
// workflow copies. The logic lives in Go (pr signoff, pr state, pr fresh,
// tested in internal/prstate); the workflow only calls it in the right
// order with the event's values.
func TestTriageWorkflow_SignOffIsTrusted(t *testing.T) {
	repoCopy, err := os.ReadFile(filepath.Join("..", "..", ".github", "workflows", "heatmap-triage.yml"))
	if err != nil {
		t.Fatal(err)
	}
	for name, wf := range map[string]string{"template": triageWorkflow, "repo copy": string(repoCopy)} {
		for _, want := range []string{
			"auto_merge_enabled", "auto_merge_disabled", // re-run when auto-merge changes
			"issues: write",   // pr signoff removes a label added before a push
			"statuses: write", // ... and records the sign-off as a commit status
			`highstakes pr signoff --repo "$REPO" --pr "$PR" --head-sha "$HEAD_SHA" --action "$ACTION" --label "$LABEL" --pushed-at "$PUSHED_AT" >> "$GITHUB_OUTPUT"`,
			"LABEL: ${{ github.event.label.name }}",
			"PUSHED_AT: ${{ github.event.pull_request.updated_at }}",
			"HEAD_SHA: ${{ github.event.pull_request.head.sha }}",
			`highstakes pr state --repo "$REPO" --pr "$PR" --head-sha "$HEAD_SHA" >> "$GITHUB_OUTPUT"`,
			"AUTO_MERGE: ${{ steps.state.outputs.auto_merge }}", // live, not the event snapshot
			"APPROVALS: ${{ steps.state.outputs.approvals }}",   // approvals of the assessed head only
			"MAINTAINERS: ${{ steps.state.outputs.maintainers }}",
			"LABEL_STATE: ${{ steps.signoff.outputs.label_state }}",
			"steps.fresh.outputs.fresh == 'true'", // a stale run does not post the comment
		} {
			if !strings.Contains(wf, want) {
				t.Errorf("%s workflow lacks %q", name, want)
			}
		}
		fresh := `highstakes pr fresh --repo "$REPO" --pr "$PR" --head-sha "$HEAD_SHA" --auto-merge "$AUTO_MERGE" --label-state "$LABEL_STATE" --approvals "$APPROVALS" --maintainers "$MAINTAINERS"`
		if n := strings.Count(wf, fresh); n != 2 {
			t.Errorf("%s workflow checks freshness %d times, want 2 (before posting and before passing)", name, n)
		}
		// Order: sign-off before the analysis (a failed analysis cannot leave a
		// stale label in place), state before the assessment, gate last.
		order := []string{"name: Install highstakes", "name: Verify sign-off", "name: Read PR state", "name: Analyze repository",
			"name: Assess PR risk", "name: Check the PR did not change", "name: Post PR comment", "name: Gate on Harden now files"}
		last := -1
		for _, step := range order {
			i := strings.Index(wf, step)
			if i < 0 || i < last {
				t.Errorf("%s workflow: step %q missing or out of order", name, step)
			}
			last = i
		}
		for _, bad := range []string{"gh api", "contains(github.event.pull_request.labels", "concurrency:", "highstakes-signed-off-sha"} {
			if strings.Contains(wf, bad) {
				t.Errorf("%s workflow contains %q: sign-off logic belongs in highstakes pr signoff/state/fresh", name, bad)
			}
		}
	}
}
