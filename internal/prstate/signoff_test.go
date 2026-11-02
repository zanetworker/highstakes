package prstate

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

// fake is an in-memory PR. events returns successive snapshots on each call
// (the last one repeats), so a test can change the history mid-run.
type fake struct {
	hasLabel   bool
	events     [][]LabelEvent
	eventCalls int
	statuses   map[string][]Status // newest first, per head SHA
	perms      map[string]string
	created    []string // "sha state description"
	removed    bool
	err        error
}

func (f *fake) HasLabel(context.Context) (bool, error) { return f.hasLabel, f.err }
func (f *fake) LabelEvents(context.Context) ([]LabelEvent, error) {
	if f.err != nil {
		return nil, f.err
	}
	if len(f.events) == 0 {
		return nil, nil
	}
	i := min(f.eventCalls, len(f.events)-1)
	f.eventCalls++
	return f.events[i], nil
}
func (f *fake) LatestStatus(_ context.Context, sha, statusContext string) (*Status, error) {
	if statusContext != "highstakes/signed-off/pr-7" {
		return nil, errors.New("wrong context " + statusContext)
	}
	if s := f.statuses[sha]; len(s) > 0 {
		return &s[0], nil
	}
	return nil, nil
}
func (f *fake) Permission(_ context.Context, login string) (string, error) {
	if p, ok := f.perms[login]; ok {
		return p, nil
	}
	return "none", nil
}
func (f *fake) CreateStatus(_ context.Context, sha, state, _, description string) error {
	f.created = append(f.created, sha+" "+state+" "+description)
	if f.statuses == nil {
		f.statuses = map[string][]Status{}
	}
	f.statuses[sha] = append([]Status{{State: state, Creator: BotLogin, Description: description}}, f.statuses[sha]...)
	return nil
}
func (f *fake) RemoveLabel(context.Context) error         { f.removed = true; f.hasLabel = false; return nil }
func (f *fake) PR(context.Context) (string, bool, error)  { return "", false, nil }
func (f *fake) Reviews(context.Context) ([]Review, error) { return nil, nil }

var (
	addAlice   = LabelEvent{"labeled", "alice", "2026-11-01T10:00:00Z"}
	removeBob  = LabelEvent{"unlabeled", "bob", "2026-11-01T11:00:00Z"}
	addTriager = LabelEvent{"labeled", "tri", "2026-11-01T12:00:00Z"}
	aliceOK    = Status{State: "success", Creator: BotLogin, Description: "Signed off by alice at 2026-11-01T10:00:00Z"}
	writers    = map[string]string{"alice": "write", "bob": "admin", "tri": "triage"}
)

func TestVerifySignOff(t *testing.T) {
	tests := []struct {
		name        string
		in          SignOffInput
		f           *fake
		wantSigned  bool
		wantState   string
		wantCreated []string
		wantRemoved bool
	}{
		{
			name:       "labeling run by a writer signs the labeled commit",
			in:         SignOffInput{Action: "labeled", Label: SignOffLabel, HeadSHA: "abc"},
			f:          &fake{hasLabel: true, events: [][]LabelEvent{{addAlice}}, perms: writers},
			wantSigned: true, wantState: addAlice.String(),
			wantCreated: []string{"abc success Signed off by alice at 2026-11-01T10:00:00Z"},
		},
		{
			name:      "labeling run by a triage user does not sign",
			in:        SignOffInput{Action: "labeled", Label: SignOffLabel, HeadSHA: "abc"},
			f:         &fake{hasLabel: true, events: [][]LabelEvent{{addTriager}}, perms: writers},
			wantState: addTriager.String(),
		},
		{
			name:      "another label's labeling run does not sign",
			in:        SignOffInput{Action: "labeled", Label: "bug", HeadSHA: "abc"},
			f:         &fake{hasLabel: true, events: [][]LabelEvent{{addAlice}}, perms: writers},
			wantState: addAlice.String(),
		},
		{
			name:       "a review run trusts the status minted for this label event",
			in:         SignOffInput{Action: "submitted", HeadSHA: "abc"},
			f:          &fake{hasLabel: true, events: [][]LabelEvent{{addAlice}}, perms: writers, statuses: map[string][]Status{"abc": {aliceOK}}},
			wantSigned: true, wantState: addAlice.String(),
		},
		{
			name:      "a review run after a push: the new head has no status, so unsigned",
			in:        SignOffInput{Action: "submitted", HeadSHA: "new"},
			f:         &fake{hasLabel: true, events: [][]LabelEvent{{addAlice}}, perms: writers, statuses: map[string][]Status{"abc": {aliceOK}}},
			wantState: addAlice.String(),
		},
		{
			name: "an older sign-off never stands in for a newer label added by a non-writer",
			in:   SignOffInput{Action: "labeled", Label: SignOffLabel, HeadSHA: "abc"},
			f: &fake{hasLabel: true, events: [][]LabelEvent{{addAlice, removeBob, addTriager}}, perms: writers,
				statuses: map[string][]Status{"abc": {aliceOK}}},
			wantState:   addTriager.String(),
			wantCreated: []string{"abc failure Sign-off withdrawn"},
		},
		{
			name: "a push removes a label added before it",
			in:   SignOffInput{Action: "synchronize", HeadSHA: "new", PushedAt: "2026-11-01T10:30:00Z"},
			f: &fake{hasLabel: true, perms: writers, events: [][]LabelEvent{
				{addAlice}, {addAlice, {"unlabeled", "github-actions[bot]", "2026-11-01T10:31:00Z"}},
			}},
			wantRemoved: true, wantState: "unlabeled github-actions[bot] 2026-11-01T10:31:00Z",
		},
		{
			name:      "a label added after the push is kept",
			in:        SignOffInput{Action: "synchronize", HeadSHA: "new", PushedAt: "2026-11-01T09:00:00Z"},
			f:         &fake{hasLabel: true, events: [][]LabelEvent{{addAlice}}, perms: writers},
			wantState: addAlice.String(),
		},
		{
			name: "removing the label withdraws the sign-off",
			in:   SignOffInput{Action: "unlabeled", Label: SignOffLabel, HeadSHA: "abc"},
			f: &fake{hasLabel: false, events: [][]LabelEvent{{addAlice, removeBob}}, perms: writers,
				statuses: map[string][]Status{"abc": {aliceOK}}},
			wantState:   removeBob.String(),
			wantCreated: []string{"abc failure Sign-off withdrawn"},
		},
		{
			name: "a stale unlabeled run never withdraws a sign-off re-added meanwhile",
			in:   SignOffInput{Action: "unlabeled", Label: SignOffLabel, HeadSHA: "abc"},
			f: &fake{hasLabel: false, perms: writers, statuses: map[string][]Status{"abc": {aliceOK}},
				events: [][]LabelEvent{{addAlice, removeBob}, {addAlice, removeBob, {"labeled", "alice", "2026-11-01T12:30:00Z"}}}},
			wantState: removeBob.String(),
		},
		{
			name: "no label ever: nothing to do",
			in:   SignOffInput{Action: "opened", HeadSHA: "abc"},
			f:    &fake{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.in.PR = 7
			got, err := VerifySignOff(context.Background(), tt.f, tt.in)
			if err != nil {
				t.Fatalf("VerifySignOff() error = %v", err)
			}
			if got.Signed != tt.wantSigned || got.LabelState != tt.wantState {
				t.Errorf("VerifySignOff() = signed %t, state %q; want %t, %q (notes %v)", got.Signed, got.LabelState, tt.wantSigned, tt.wantState, got.Notes)
			}
			if !reflect.DeepEqual(tt.f.created, tt.wantCreated) {
				t.Errorf("statuses created = %v, want %v", tt.f.created, tt.wantCreated)
			}
			if tt.f.removed != tt.wantRemoved {
				t.Errorf("label removed = %t, want %t", tt.f.removed, tt.wantRemoved)
			}
		})
	}
}

func TestVerifySignOff_APIErrorIsReturned(t *testing.T) {
	_, err := VerifySignOff(context.Background(), &fake{err: errors.New("gh: HTTP 502")}, SignOffInput{PR: 7, HeadSHA: "abc"})
	if err == nil {
		t.Fatal("VerifySignOff() error = nil, want the API error")
	}
}
