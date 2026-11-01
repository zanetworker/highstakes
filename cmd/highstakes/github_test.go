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
	} {
		if !strings.Contains(wf, want) {
			t.Errorf("workflow missing %q", want)
		}
	}
	for _, stale := range []string{"heat_score", "cmd/heatmap@", "estimated_review_time"} {
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
