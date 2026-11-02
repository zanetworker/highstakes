// Package prstate reads and verifies the PR state the triage workflow gates
// on: the sign-off label (who added it, on which commit), approvals of the
// head commit, auto-merge, and whether any of it changed during a run. The
// decisions are pure functions; GitHub access goes through the API
// interface (gh CLI in production, a fake in tests).
package prstate

import (
	"context"
	"fmt"
	"slices"
	"strings"
)

// LabelEvent is one add or remove of the sign-off label, from GitHub's
// durable issue event history.
type LabelEvent struct {
	Event     string // "labeled" or "unlabeled"
	Actor     string
	CreatedAt string // RFC 3339, as GitHub returns it
}

// String is "<event> <actor> <time>", "" for no event.
func (e LabelEvent) String() string {
	if e.Event == "" {
		return ""
	}
	return e.Event + " " + e.Actor + " " + e.CreatedAt
}

// Last is the latest event, the zero value when there is none.
func Last(events []LabelEvent) LabelEvent {
	if len(events) == 0 {
		return LabelEvent{}
	}
	return events[len(events)-1]
}

// Review is one PR review.
type Review struct {
	Login    string
	State    string // APPROVED, CHANGES_REQUESTED, COMMENTED, DISMISSED
	CommitID string // the commit reviewed
}

// HeadApprovals are the logins whose latest review (comments aside) is an
// approval of head, sorted. An approval of an earlier commit does not cover
// new pushes.
func HeadApprovals(reviews []Review, head string) []string {
	last := map[string]Review{}
	for _, r := range reviews {
		if r.State != "COMMENTED" {
			last[r.Login] = r
		}
	}
	var out []string
	for login, r := range last {
		if r.State == "APPROVED" && r.CommitID == head {
			out = append(out, login)
		}
	}
	slices.Sort(out)
	return out
}

// State is what a triage run assessed, or what the PR looks like now.
type State struct {
	Head        string
	AutoMerge   bool
	LabelState  string   // LabelEvent.String() of the latest sign-off label event
	Approvals   []string // HeadApprovals
	Maintainers []string // the approvals from logins with write access
}

// Stale lists what changed between the assessed state and now; nil means
// the assessment still holds.
func Stale(assessed, now State) []string {
	var out []string
	if assessed.Head != now.Head {
		out = append(out, fmt.Sprintf("head moved from %s to %s", assessed.Head, now.Head))
	}
	if assessed.AutoMerge != now.AutoMerge {
		out = append(out, fmt.Sprintf("auto-merge changed from %t to %t", assessed.AutoMerge, now.AutoMerge))
	}
	if assessed.LabelState != now.LabelState {
		out = append(out, "the sign-off label changed")
	}
	switch {
	case strings.Join(assessed.Approvals, ",") != strings.Join(now.Approvals, ","):
		out = append(out, "approvals changed")
	case strings.Join(assessed.Maintainers, ",") != strings.Join(now.Maintainers, ","):
		out = append(out, "approvers with write access changed")
	}
	return out
}

// ReadState reads the PR as it is now: its head, auto-merge, the latest
// sign-off label event, the approvals of the assessed head commit (assessed,
// not the live head, so a push shows up as a moved head, not as lost
// approvals) and which of those approvers have write access.
func ReadState(ctx context.Context, api API, assessedHead string) (State, error) {
	var s State
	var err error
	if s.Head, s.AutoMerge, err = api.PR(ctx); err != nil {
		return s, err
	}
	events, err := api.LabelEvents(ctx)
	if err != nil {
		return s, err
	}
	s.LabelState = Last(events).String()
	reviews, err := api.Reviews(ctx)
	if err != nil {
		return s, err
	}
	s.Approvals = HeadApprovals(reviews, assessedHead)
	for _, login := range s.Approvals {
		perm, err := api.Permission(ctx, login)
		if err != nil {
			return s, err
		}
		if IsWriter(perm) {
			s.Maintainers = append(s.Maintainers, login)
		}
	}
	return s, nil
}
