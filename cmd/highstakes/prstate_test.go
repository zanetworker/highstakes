package main

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/zanetworker/highstakes/internal/prdata"
	"github.com/zanetworker/highstakes/internal/prstate"
)

// fakePR is a PR for the pr signoff/state/fresh commands.
type fakePR struct {
	head      string
	autoMerge bool
	hasLabel  bool
	events    []prstate.LabelEvent
	statuses  map[string]prstate.Status
	perms     map[string]string
	reviews   []prstate.Review
	created   []string
	err       error
}

func (f *fakePR) HasLabel(context.Context) (bool, error)                    { return f.hasLabel, f.err }
func (f *fakePR) LabelEvents(context.Context) ([]prstate.LabelEvent, error) { return f.events, f.err }
func (f *fakePR) LatestStatus(_ context.Context, sha, _ string) (*prstate.Status, error) {
	if s, ok := f.statuses[sha]; ok {
		return &s, f.err
	}
	return nil, f.err
}
func (f *fakePR) Permission(_ context.Context, login string) (string, error) {
	return f.perms[login], nil
}
func (f *fakePR) CreateStatus(_ context.Context, sha, state, _, desc string) error {
	f.created = append(f.created, sha+" "+state)
	if f.statuses == nil {
		f.statuses = map[string]prstate.Status{}
	}
	f.statuses[sha] = prstate.Status{State: state, Creator: prstate.BotLogin, Description: desc}
	return nil
}
func (f *fakePR) RemoveLabel(context.Context) error                 { return nil }
func (f *fakePR) PR(context.Context) (string, bool, error)          { return f.head, f.autoMerge, f.err }
func (f *fakePR) Reviews(context.Context) ([]prstate.Review, error) { return f.reviews, f.err }

func withFakePR(t *testing.T, f *fakePR) *[]string {
	t.Helper()
	var got []string
	orig := newPRAPI
	newPRAPI = func(repo prdata.Repo, pr int) prstate.API {
		got = append(got, repo.String(), strconv.Itoa(pr))
		return f
	}
	t.Cleanup(func() { newPRAPI = orig })
	return &got
}

var aliceAdd = prstate.LabelEvent{Event: "labeled", Actor: "alice", CreatedAt: "2026-11-01T10:00:00Z"}

func TestPRSignoff_LabelingRunSigns(t *testing.T) {
	f := &fakePR{hasLabel: true, events: []prstate.LabelEvent{aliceAdd}, perms: map[string]string{"alice": "write"}}
	who := withFakePR(t, f)
	out, err := runCLI(t, t.TempDir(), "pr", "signoff", "--repo", "o/r", "--pr", "7",
		"--action", "labeled", "--label", "highstakes:signed-off", "--head-sha", "abc")
	if err != nil {
		t.Fatal(err)
	}
	if out != "signed=true\nlabel_state=labeled alice 2026-11-01T10:00:00Z\n" {
		t.Errorf("output = %q, want GITHUB_OUTPUT lines", out)
	}
	if len(f.created) != 1 || f.created[0] != "abc success" {
		t.Errorf("statuses created = %v, want one success on abc", f.created)
	}
	if (*who)[0] != "o/r" || (*who)[1] != "7" {
		t.Errorf("API built for %v, want o/r #7", *who)
	}
}

func TestPRSignoff_Errors(t *testing.T) {
	withFakePR(t, &fakePR{err: errors.New("gh: HTTP 502")})
	tests := []struct {
		name string
		args []string
		code int
	}{
		{"no head sha", []string{"pr", "signoff", "--repo", "o/r", "--pr", "7"}, 2},
		{"no PR number", []string{"pr", "signoff", "--repo", "o/r", "--head-sha", "abc"}, 2},
		{"GitHub fails", []string{"pr", "signoff", "--repo", "o/r", "--pr", "7", "--head-sha", "abc"}, 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("GITHUB_REF", "")
			t.Setenv("GITHUB_REPOSITORY", "")
			_, err := runCLI(t, t.TempDir(), tt.args...)
			var ee *exitError
			if !errors.As(err, &ee) || ee.code != tt.code {
				t.Errorf("error = %v, want exit %d", err, tt.code)
			}
		})
	}
}

func TestPRState_PrintsLiveState(t *testing.T) {
	withFakePR(t, &fakePR{head: "abc", autoMerge: true, events: []prstate.LabelEvent{aliceAdd},
		perms:   map[string]string{"alice": "write", "carl": "read"},
		reviews: []prstate.Review{{Login: "alice", State: "APPROVED", CommitID: "abc"}, {Login: "carl", State: "APPROVED", CommitID: "abc"}}})
	out, err := runCLI(t, t.TempDir(), "pr", "state", "--repo", "o/r", "--pr", "7", "--head-sha", "abc")
	if err != nil {
		t.Fatal(err)
	}
	want := "head=abc\nauto_merge=true\nlabel_state=labeled alice 2026-11-01T10:00:00Z\napprovals=alice,carl\nmaintainers=alice\n"
	if out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
}

func TestPRFresh(t *testing.T) {
	f := &fakePR{head: "abc", events: []prstate.LabelEvent{aliceAdd}, perms: map[string]string{"alice": "write"},
		reviews: []prstate.Review{{Login: "alice", State: "APPROVED", CommitID: "abc"}}}
	withFakePR(t, f)
	assessed := []string{"pr", "fresh", "--repo", "o/r", "--pr", "7", "--head-sha", "abc", "--auto-merge", "false",
		"--label-state", aliceAdd.String(), "--approvals", "alice", "--maintainers", "alice"}
	if _, err := runCLI(t, t.TempDir(), assessed...); err != nil {
		t.Errorf("unchanged PR: error = %v, want fresh", err)
	}
	f.perms["alice"] = "read" // lost write access mid-run
	_, err := runCLI(t, t.TempDir(), assessed...)
	var lost *exitError
	if !errors.As(err, &lost) || lost.code != 5 || !strings.Contains(lost.msg, "write access changed") {
		t.Errorf("approver lost write access: error = %v, want exit 5", err)
	}
	f.perms["alice"] = "write"
	out, err := runCLI(t, t.TempDir(), append(assessed, "--json")...)
	if err != nil || !strings.Contains(out, `"fresh": true`) {
		t.Errorf("--json fresh: out = %q, err = %v", out, err)
	}
	f.autoMerge = true
	_, err = runCLI(t, t.TempDir(), assessed...)
	var ee *exitError
	if !errors.As(err, &ee) || ee.code != 5 || !strings.Contains(ee.msg, "auto-merge changed") {
		t.Errorf("auto-merge turned on: error = %v, want exit 5 naming the change", err)
	}
	_, err = runCLI(t, t.TempDir(), "pr", "fresh", "--repo", "o/r", "--pr", "7", "--head-sha", "abc", "--auto-merge", "maybe")
	if !errors.As(err, &ee) || ee.code != 2 {
		t.Errorf("--auto-merge maybe: error = %v, want exit 2", err)
	}
}

func TestPRFresh_NoApprovalsAndNoLabel(t *testing.T) {
	withFakePR(t, &fakePR{head: "abc"})
	_, err := runCLI(t, t.TempDir(), "pr", "fresh", "--repo", "o/r", "--pr", "7", "--head-sha", "abc",
		"--auto-merge", "false", "--label-state", "", "--approvals", "", "--maintainers", "")
	if err != nil {
		t.Errorf("empty approvals and label state as the workflow passes them: error = %v, want fresh", err)
	}
}
