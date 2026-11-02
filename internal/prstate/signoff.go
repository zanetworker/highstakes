package prstate

import (
	"context"
	"fmt"
	"time"

	"github.com/zanetworker/highstakes/internal/explain"
)

// SignOffLabel is the label a maintainer adds after reviewing.
const SignOffLabel = explain.SignOffLabel

// BotLogin creates the sign-off statuses; a status from anyone else never counts.
const BotLogin = "github-actions[bot]"

// Status is a commit status.
type Status struct {
	State       string // success, failure, pending, error
	Creator     string
	Description string
}

// API is the GitHub access the sign-off and state checks need, for one PR.
type API interface {
	HasLabel(ctx context.Context) (bool, error)                             // the sign-off label is on the PR now
	LabelEvents(ctx context.Context) ([]LabelEvent, error)                  // sign-off label adds and removes, oldest first
	LatestStatus(ctx context.Context, sha, context string) (*Status, error) // nil when the commit has none
	Permission(ctx context.Context, login string) (string, error)           // admin, maintain, write, triage, read, none
	CreateStatus(ctx context.Context, sha, state, context, description string) error
	RemoveLabel(ctx context.Context) error
	PR(ctx context.Context) (head string, autoMerge bool, err error)
	Reviews(ctx context.Context) ([]Review, error)
}

// SignOffInput is the event the run was triggered by.
type SignOffInput struct {
	PR       int
	Action   string // github.event.action
	Label    string // github.event.label.name, for labeled/unlabeled
	HeadSHA  string // the head commit when the event happened
	PushedAt string // github.event.pull_request.updated_at, for synchronize
}

// SignOff is the verdict: whether the head is signed off, the label state it
// was decided on (for the freshness checks) and notes for the log.
type SignOff struct {
	Signed     bool
	LabelState string
	Notes      []string
}

// StatusContext is the per-PR commit status context, so another PR on the
// same commit cannot borrow a sign-off.
func StatusContext(pr int) string { return fmt.Sprintf("highstakes/signed-off/pr-%d", pr) }

// IsWriter reports permissions that may sign off.
func IsWriter(perm string) bool { return perm == "admin" || perm == "maintain" || perm == "write" }

// VerifySignOff decides whether the PR's head is signed off and records it:
//   - GitHub's label history decides whether the label stands (latest add or
//     remove wins), and only when whoever added it last has write access;
//   - only the run the labeling triggered signs, by creating a success status
//     on the head of that moment that names the label event, so a push leaves
//     the new head unsigned and an older sign-off never stands in for a newer
//     label;
//   - a push removes a label added before it;
//   - a sign-off that no longer stands gets a newer failing status, unless the
//     label changed meanwhile (the run that change triggered decides).
func VerifySignOff(ctx context.Context, api API, in SignOffInput) (SignOff, error) {
	var out SignOff
	has, err := api.HasLabel(ctx)
	if err != nil {
		return out, err
	}
	events, err := api.LabelEvents(ctx)
	if err != nil {
		return out, err
	}
	last := Last(events)
	if in.Action == "synchronize" && has && before(last.CreatedAt, in.PushedAt) {
		if err := api.RemoveLabel(ctx); err != nil {
			return out, err
		}
		has = false
		out.Notes = append(out.Notes, "New commits pushed: the sign-off no longer applies; review again and re-add the label.")
		if events, err = api.LabelEvents(ctx); err != nil {
			return out, err
		}
		last = Last(events)
	}
	out.LabelState = last.String()

	stands := has && last.Event == "labeled"
	if stands {
		perm, err := api.Permission(ctx, last.Actor)
		if err != nil {
			return out, err
		}
		if !IsWriter(perm) {
			stands = false
			out.Notes = append(out.Notes, fmt.Sprintf("Sign-off label added by %s (permission: %s) does not count: write access is required.", last.Actor, perm))
		}
	}

	statusCtx := StatusContext(in.PR)
	want := Status{State: "success", Creator: BotLogin, Description: fmt.Sprintf("Signed off by %s at %s", last.Actor, last.CreatedAt)}
	status, err := api.LatestStatus(ctx, in.HeadSHA, statusCtx)
	if err != nil {
		return out, err
	}
	if in.Action == "labeled" && in.Label == SignOffLabel && stands && !same(status, want) {
		if err := api.CreateStatus(ctx, in.HeadSHA, want.State, statusCtx, want.Description); err != nil {
			return out, err
		}
		if status, err = api.LatestStatus(ctx, in.HeadSHA, statusCtx); err != nil {
			return out, err
		}
	}
	out.Signed = stands && same(status, want)
	if stands && !out.Signed {
		out.Notes = append(out.Notes, "The sign-off label was not added on this commit: review again and re-add it.")
	}

	if !out.Signed && status != nil && status.State == "success" && status.Creator == BotLogin {
		now, err := api.LabelEvents(ctx)
		if err != nil {
			return out, err
		}
		if Last(now) == last {
			if err := api.CreateStatus(ctx, in.HeadSHA, "failure", statusCtx, "Sign-off withdrawn"); err != nil {
				return out, err
			}
		}
	}
	return out, nil
}

func same(s *Status, want Status) bool { return s != nil && *s == want }

// before reports a < b for RFC 3339 times; unparsable times compare as text.
func before(a, b string) bool {
	ta, errA := time.Parse(time.RFC3339, a)
	tb, errB := time.Parse(time.RFC3339, b)
	if errA != nil || errB != nil {
		return a < b
	}
	return ta.Before(tb)
}
