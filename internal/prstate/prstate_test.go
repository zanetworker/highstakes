package prstate

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestLastLabelEvent(t *testing.T) {
	tests := []struct {
		name   string
		events []LabelEvent
		want   string
	}{
		{"none", nil, ""},
		{"one add", []LabelEvent{{"labeled", "alice", "2026-11-01T10:00:00Z"}}, "labeled alice 2026-11-01T10:00:00Z"},
		{"add then remove", []LabelEvent{
			{"labeled", "alice", "2026-11-01T10:00:00Z"}, {"unlabeled", "bob", "2026-11-01T11:00:00Z"},
		}, "unlabeled bob 2026-11-01T11:00:00Z"},
		{"remove then add again", []LabelEvent{
			{"labeled", "alice", "2026-11-01T10:00:00Z"}, {"unlabeled", "bob", "2026-11-01T11:00:00Z"},
			{"labeled", "carol", "2026-11-01T12:00:00Z"},
		}, "labeled carol 2026-11-01T12:00:00Z"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Last(tt.events).String(); got != tt.want {
				t.Errorf("Last().String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestHeadApprovals(t *testing.T) {
	tests := []struct {
		name    string
		reviews []Review
		want    []string
	}{
		{"none", nil, nil},
		{"approval of the head", []Review{{"alice", "APPROVED", "abc"}}, []string{"alice"}},
		{"approval of an older commit does not count", []Review{{"bob", "APPROVED", "old"}}, nil},
		{"changes requested after approval wins", []Review{
			{"carol", "APPROVED", "abc"}, {"carol", "CHANGES_REQUESTED", "abc"},
		}, nil},
		{"a comment after an approval keeps it", []Review{
			{"dan", "APPROVED", "abc"}, {"dan", "COMMENTED", "abc"},
		}, []string{"dan"}},
		{"dismissed approval does not count", []Review{
			{"erin", "APPROVED", "abc"}, {"erin", "DISMISSED", "abc"},
		}, nil},
		{"re-approval of the head after a push", []Review{
			{"fay", "APPROVED", "old"}, {"fay", "APPROVED", "abc"},
		}, []string{"fay"}},
		{"sorted", []Review{{"zed", "APPROVED", "abc"}, {"amy", "APPROVED", "abc"}}, []string{"amy", "zed"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := HeadApprovals(tt.reviews, "abc"); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("HeadApprovals() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestStale(t *testing.T) {
	base := State{Head: "abc", AutoMerge: false, LabelState: "labeled alice t1", Approvals: []string{"alice"}, Maintainers: []string{"alice"}}
	tests := []struct {
		name string
		now  func(State) State
		want []string
	}{
		{"unchanged", func(s State) State { return s }, nil},
		{"new push", func(s State) State { s.Head = "def"; return s }, []string{"head moved from abc to def"}},
		{"auto-merge turned on", func(s State) State { s.AutoMerge = true; return s }, []string{"auto-merge changed from false to true"}},
		{"label removed", func(s State) State { s.LabelState = "unlabeled bob t2"; return s }, []string{"the sign-off label changed"}},
		{"approval withdrawn", func(s State) State { s.Approvals, s.Maintainers = nil, nil; return s }, []string{"approvals changed"}},
		{"approver lost write access", func(s State) State { s.Maintainers = nil; return s }, []string{"approvers with write access changed"}},
		{"several at once", func(s State) State {
			s.Head = "def"
			s.Approvals, s.Maintainers = []string{"bob"}, []string{"bob"}
			return s
		},
			[]string{"head moved from abc to def", "approvals changed"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Stale(base, tt.now(base)); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Stale() = %v, want %v", got, tt.want)
			}
		})
	}
}

// stateFake extends fake with a PR head, auto-merge and reviews.
type stateFake struct {
	fake
	head      string
	autoMerge bool
	reviews   []Review
}

func (s *stateFake) PR(context.Context) (string, bool, error)  { return s.head, s.autoMerge, s.err }
func (s *stateFake) Reviews(context.Context) ([]Review, error) { return s.reviews, s.err }

func TestReadState(t *testing.T) {
	f := &stateFake{
		fake:      fake{events: [][]LabelEvent{{addAlice}}, perms: map[string]string{"alice": "write", "carl": "read"}},
		head:      "def",
		autoMerge: true,
		reviews:   []Review{{"alice", "APPROVED", "abc"}, {"carl", "APPROVED", "abc"}, {"dora", "APPROVED", "old"}},
	}
	got, err := ReadState(context.Background(), f, "abc")
	if err != nil {
		t.Fatal(err)
	}
	want := State{Head: "def", AutoMerge: true, LabelState: addAlice.String(),
		Approvals: []string{"alice", "carl"}, Maintainers: []string{"alice"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ReadState() = %+v, want %+v", got, want)
	}

	if _, err := ReadState(context.Background(), &stateFake{fake: fake{err: errors.New("boom")}}, "abc"); err == nil {
		t.Error("ReadState() error = nil, want the API error")
	}
}
