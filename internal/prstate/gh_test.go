package prstate

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/zanetworker/highstakes/internal/prdata"
)

// recorder answers gh calls from a table keyed by the API path and records them.
type recorder struct {
	answers map[string]string // path -> stdout
	errs    map[string]error
	calls   []string
}

func (r *recorder) run(_ context.Context, args ...string) ([]byte, error) {
	r.calls = append(r.calls, strings.Join(args, " "))
	for _, a := range args {
		if err, ok := r.errs[a]; ok {
			return nil, err
		}
		if out, ok := r.answers[a]; ok {
			return []byte(out), nil
		}
	}
	return nil, nil
}

func newGH(r *recorder) *GH {
	return &GH{Repo: prdata.Repo{Owner: "o", Name: "r"}, Number: 7, Run: r.run}
}

func TestGH_Reads(t *testing.T) {
	r := &recorder{answers: map[string]string{
		"repos/o/r/issues/7/labels":                "bug\nhighstakes:signed-off\n",
		"repos/o/r/issues/7/events":                "labeled\talice\t2026-11-01T10:00:00Z\nunlabeled\tbob\t2026-11-01T11:00:00Z\n",
		"repos/o/r/commits/abc/statuses":           "success\tgithub-actions[bot]\tSigned off by alice at t\nfailure\tgithub-actions[bot]\tSign-off withdrawn\n",
		"repos/o/r/collaborators/alice/permission": "write\n",
		"repos/o/r/pulls/7":                        "def\ttrue\n",
		"repos/o/r/pulls/7/reviews":                "alice\tAPPROVED\tabc\n",
	}}
	gh, ctx := newGH(r), context.Background()

	if has, err := gh.HasLabel(ctx); err != nil || !has {
		t.Errorf("HasLabel() = %t, %v; want true", has, err)
	}
	events, err := gh.LabelEvents(ctx)
	wantEvents := []LabelEvent{{"labeled", "alice", "2026-11-01T10:00:00Z"}, {"unlabeled", "bob", "2026-11-01T11:00:00Z"}}
	if err != nil || !reflect.DeepEqual(events, wantEvents) {
		t.Errorf("LabelEvents() = %v, %v; want %v", events, err, wantEvents)
	}
	st, err := gh.LatestStatus(ctx, "abc", "highstakes/signed-off/pr-7")
	if err != nil || st == nil || *st != (Status{"success", BotLogin, "Signed off by alice at t"}) {
		t.Errorf("LatestStatus() = %+v, %v; want the newest status", st, err)
	}
	if perm, err := gh.Permission(ctx, "alice"); err != nil || perm != "write" {
		t.Errorf("Permission() = %q, %v; want write", perm, err)
	}
	if head, auto, err := gh.PR(ctx); err != nil || head != "def" || !auto {
		t.Errorf("PR() = %q, %t, %v; want def, true", head, auto, err)
	}
	if revs, err := gh.Reviews(ctx); err != nil || !reflect.DeepEqual(revs, []Review{{"alice", "APPROVED", "abc"}}) {
		t.Errorf("Reviews() = %v, %v", revs, err)
	}
	for _, c := range r.calls {
		if strings.Contains(c, "/labels") || strings.Contains(c, "/events") || strings.Contains(c, "/statuses") || strings.Contains(c, "/reviews") {
			if !strings.Contains(c, "--paginate") {
				t.Errorf("list call is not paginated: %s", c)
			}
		}
	}
	if !strings.Contains(strings.Join(r.calls, "\n"), `.context == "highstakes/signed-off/pr-7"`) {
		t.Error("LatestStatus does not filter on the status context")
	}
}

func TestGH_EmptyAndMissing(t *testing.T) {
	r := &recorder{
		answers: map[string]string{"repos/o/r/issues/7/labels": "bug\n"},
		errs:    map[string]error{"repos/o/r/collaborators/stranger/permission": errors.New("gh: HTTP 404")},
	}
	gh, ctx := newGH(r), context.Background()
	if has, _ := gh.HasLabel(ctx); has {
		t.Error("HasLabel() = true for a PR without the label")
	}
	if st, err := gh.LatestStatus(ctx, "abc", "highstakes/signed-off/pr-7"); err != nil || st != nil {
		t.Errorf("LatestStatus() = %+v, %v; want nil, nil for a commit without statuses", st, err)
	}
	if perm, err := gh.Permission(ctx, "stranger"); err != nil || perm != "none" {
		t.Errorf("Permission() = %q, %v; want none for a non-collaborator", perm, err)
	}
}

func TestGH_Writes(t *testing.T) {
	r := &recorder{errs: map[string]error{"repos/o/r/issues/7/labels/highstakes:signed-off": errors.New("gh: Label does not exist (HTTP 404)")}}
	gh, ctx := newGH(r), context.Background()
	if err := gh.CreateStatus(ctx, "abc", "success", "highstakes/signed-off/pr-7", "Signed off by alice at t"); err != nil {
		t.Fatal(err)
	}
	want := "api -X POST repos/o/r/statuses/abc -f state=success -f context=highstakes/signed-off/pr-7 -f description=Signed off by alice at t"
	if r.calls[0] != want {
		t.Errorf("CreateStatus call = %q, want %q", r.calls[0], want)
	}
	if err := gh.RemoveLabel(ctx); err != nil {
		t.Errorf("RemoveLabel() = %v; a label already gone is not an error", err)
	}
}

func TestGH_ErrorsPropagate(t *testing.T) {
	r := &recorder{errs: map[string]error{"repos/o/r/pulls/7": errors.New("gh: HTTP 502")}}
	if _, _, err := newGH(r).PR(context.Background()); err == nil {
		t.Error("PR() error = nil, want the gh error")
	}
}
