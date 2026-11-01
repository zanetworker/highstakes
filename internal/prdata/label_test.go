package prdata

import (
	"reflect"
	"testing"
)

func TestLabel(t *testing.T) {
	prs := []PR{
		{Number: 1, Title: "fix: crash", Labels: []string{"security"}, Approvers: 2},
		{Number: 2, Title: `Revert "feat: x"`},
		{Number: 3, Title: "feat: y"},
		{Number: 4, Title: "fix: unmapped"}, // no local commit
	}
	prCommit := map[int]Commit{
		1: {SHA: "a", Files: []string{"a.go"}},
		2: {SHA: "b", Files: []string{"b.go", "c.go"}},
		3: {SHA: "c", Files: []string{"c.go"}},
	}
	got := Label(prs, prCommit, map[int]bool{3: true}, map[int]bool{3: true})
	want := []LabeledPR{
		{PR: prs[0], Files: []string{"a.go"}, BugFix: true, Severity: true, DeepReview: true},
		{PR: prs[1], Files: []string{"b.go", "c.go"}, Revert: true},
		{PR: prs[2], Files: []string{"c.go"}, Backported: true, FastRevert: true},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Label =\n%+v\nwant\n%+v", got, want)
	}
	if len(Label(prs, nil, nil, nil)) != 0 {
		t.Error("no mapped commits should label nothing")
	}
}
