package prstate

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/zanetworker/highstakes/internal/prdata"
)

// GH implements API for one PR through the gh CLI, which supplies auth
// (GH_TOKEN in Actions).
type GH struct {
	Repo   prdata.Repo
	Number int
	// Run executes gh with args; nil runs the gh binary.
	Run func(ctx context.Context, args ...string) ([]byte, error)
}

func (g *GH) gh(ctx context.Context, args ...string) ([]byte, error) {
	if g.Run != nil {
		return g.Run(ctx, args...)
	}
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, "gh", args...)
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("gh %s: %v: %s", strings.Join(args[:min(len(args), 3)], " "), err, strings.TrimSpace(stderr.String()))
	}
	return out, nil
}

// rows runs a gh api call and splits its tab-separated output.
func (g *GH) rows(ctx context.Context, args ...string) ([][]string, error) {
	out, err := g.gh(ctx, args...)
	if err != nil {
		return nil, err
	}
	var rows [][]string
	for _, line := range strings.Split(strings.TrimRight(string(out), "\n"), "\n") {
		if line != "" {
			rows = append(rows, strings.Split(line, "\t"))
		}
	}
	return rows, nil
}

func (g *GH) issue() string { return fmt.Sprintf("repos/%s/issues/%d", g.Repo, g.Number) }
func (g *GH) pull() string  { return fmt.Sprintf("repos/%s/pulls/%d", g.Repo, g.Number) }

// HasLabel implements API.
func (g *GH) HasLabel(ctx context.Context) (bool, error) {
	rows, err := g.rows(ctx, "api", g.issue()+"/labels", "--paginate", "--jq", ".[].name")
	if err != nil {
		return false, err
	}
	for _, r := range rows {
		if r[0] == SignOffLabel {
			return true, nil
		}
	}
	return false, nil
}

// LabelEvents implements API.
func (g *GH) LabelEvents(ctx context.Context) ([]LabelEvent, error) {
	jq := `.[] | select((.event == "labeled" or .event == "unlabeled") and .label.name == "` + SignOffLabel + `") | [.event, .actor.login, .created_at] | @tsv`
	rows, err := g.rows(ctx, "api", g.issue()+"/events", "--paginate", "--jq", jq)
	if err != nil {
		return nil, err
	}
	var out []LabelEvent
	for _, r := range rows {
		if len(r) == 3 {
			out = append(out, LabelEvent{Event: r[0], Actor: r[1], CreatedAt: r[2]})
		}
	}
	return out, nil
}

// LatestStatus implements API; GitHub lists a commit's statuses newest first.
func (g *GH) LatestStatus(ctx context.Context, sha, statusContext string) (*Status, error) {
	jq := `.[] | select(.context == "` + statusContext + `") | [.state, .creator.login, .description] | @tsv`
	rows, err := g.rows(ctx, "api", fmt.Sprintf("repos/%s/commits/%s/statuses", g.Repo, sha), "--paginate", "--jq", jq)
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	r := rows[0]
	field := func(i int) string {
		if i < len(r) {
			return r[i]
		}
		return ""
	}
	return &Status{State: field(0), Creator: field(1), Description: field(2)}, nil
}

// Permission implements API. A login that is not a collaborator (or any
// failure to look it up) is "none": it can never sign off.
func (g *GH) Permission(ctx context.Context, login string) (string, error) {
	out, err := g.gh(ctx, "api", fmt.Sprintf("repos/%s/collaborators/%s/permission", g.Repo, login), "--jq", ".permission")
	if err != nil || strings.TrimSpace(string(out)) == "" {
		return "none", nil
	}
	return strings.TrimSpace(string(out)), nil
}

// CreateStatus implements API.
func (g *GH) CreateStatus(ctx context.Context, sha, state, statusContext, description string) error {
	_, err := g.gh(ctx, "api", "-X", "POST", fmt.Sprintf("repos/%s/statuses/%s", g.Repo, sha),
		"-f", "state="+state, "-f", "context="+statusContext, "-f", "description="+description)
	return err
}

// RemoveLabel implements API; a label already gone is not an error.
func (g *GH) RemoveLabel(ctx context.Context) error {
	_, err := g.gh(ctx, "api", "-X", "DELETE", g.issue()+"/labels/"+SignOffLabel)
	if err != nil && strings.Contains(err.Error(), "404") {
		return nil
	}
	return err
}

// PR implements API.
func (g *GH) PR(ctx context.Context) (string, bool, error) {
	rows, err := g.rows(ctx, "api", g.pull(), "--jq", `[.head.sha, (.auto_merge != null)] | @tsv`)
	if err != nil {
		return "", false, err
	}
	if len(rows) == 0 || len(rows[0]) != 2 {
		return "", false, fmt.Errorf("unexpected PR response for %s#%d", g.Repo, g.Number)
	}
	return rows[0][0], rows[0][1] == "true", nil
}

// Reviews implements API.
func (g *GH) Reviews(ctx context.Context) ([]Review, error) {
	rows, err := g.rows(ctx, "api", g.pull()+"/reviews", "--paginate", "--jq", `.[] | [.user.login, .state, .commit_id] | @tsv`)
	if err != nil {
		return nil, err
	}
	var out []Review
	for _, r := range rows {
		if len(r) == 3 {
			out = append(out, Review{Login: r[0], State: r[1], CommitID: r[2]})
		}
	}
	return out, nil
}
