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
		"types: [opened, synchronize, reopened, labeled, unlabeled]",
		"SIGNED_OFF: ${{ contains(github.event.pull_request.labels.*.name, 'highstakes:signed-off') }}",
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
