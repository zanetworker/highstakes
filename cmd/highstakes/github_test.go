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
		"AUTO_MERGE: ${{ github.event.pull_request.auto_merge != null }}",
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
// workflow copies: a new push clears the label, only write access counts,
// and auto-merge changes re-run the gate.
func TestTriageWorkflow_SignOffIsTrusted(t *testing.T) {
	repoCopy, err := os.ReadFile(filepath.Join("..", "..", ".github", "workflows", "heatmap-triage.yml"))
	if err != nil {
		t.Fatal(err)
	}
	for name, wf := range map[string]string{"template": triageWorkflow, "repo copy": string(repoCopy)} {
		for _, want := range []string{
			"auto_merge_enabled", "auto_merge_disabled", // re-run when auto-merge changes
			"issues: write",                              // to remove the label
			`"$ACTION" = "synchronize"`,                  // a new push clears the sign-off
			"labels/highstakes:signed-off",               // ... by deleting the label
			"collaborators/$SENDER/permission",           // only write access signs off
			"admin|maintain|write",
			"statuses: write",                              // sign-off recorded as a commit status
			"statuses/$HEAD_SHA",                           // ... on the reviewed head commit
			"commits/$HEAD_SHA/statuses",                   // ... read back for that commit only
			`"success github-actions[bot]"`,                 // ... only the workflow's own, latest status counts
			"PUSHED_AT",                                    // keep a label added after the push
			`"$ACTION" = "unlabeled"`,                      // removing the label withdraws the sign-off
			"state=failure",                                // ... as a newer, failing status
			"first(",                                       // only the latest status for the context counts
			"concurrency:",                                 // one run per PR at a time
			"highstakes/signed-off/pr-$PR",                 // sign-off scoped to this PR
			"repos/$REPO/issues/$PR/labels",                // live labels, not the event snapshot
		} {
			if !strings.Contains(wf, want) {
				t.Errorf("%s workflow lacks %q", name, want)
			}
		}
		if strings.Contains(wf, "contains(github.event.pull_request.labels") {
			t.Errorf("%s workflow trusts the label's presence alone", name)
		}
		if strings.Contains(wf, "highstakes-signed-off-sha") {
			t.Errorf("%s workflow trusts a comment marker that any commenter can write", name)
		}
	}
}
