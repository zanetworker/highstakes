package prdata

import (
	"regexp"
	"strings"
)

var (
	bugLabel      = regexp.MustCompile(`(?i)bug|fix|defect|regression`)
	fixTitle      = regexp.MustCompile(`(?i)^fix(\([^)]*\))?!?:`)
	severityLabel = regexp.MustCompile(`(?i)security|critical|\bp[01]\b|\bsev(erity)?([^a-z]|$)|incident|cve|vulnerab`)
	// A severity label that says the issue is minor is not stakes evidence.
	lowSeverity = regexp.MustCompile(`(?i)\b(low|minor|trivial)\b`)
)

// IsBugFix reports fragility evidence: a label matching bug, fix, defect or
// regression (case-insensitive) on the PR or on an issue it closes, or a
// conventional-commit title "fix:" / "fix(scope):".
func IsBugFix(pr PR) bool {
	return fixTitle.MatchString(strings.TrimSpace(pr.Title)) ||
		anyMatch(pr.Labels, bugLabel) || anyMatch(pr.ClosingIssueLabels, bugLabel)
}

// IsRevert reports a revert PR: the title starts with "Revert".
func IsRevert(pr PR) bool {
	return strings.HasPrefix(strings.TrimSpace(pr.Title), "Revert")
}

// HasSeverityLabel reports stakes evidence: a label on the PR or on an issue
// it closes matching security, critical, p0, p1, sev, incident, cve or
// vulnerab, unless the label also says low, minor or trivial.
func HasSeverityLabel(pr PR) bool {
	for _, labels := range [][]string{pr.Labels, pr.ClosingIssueLabels} {
		for _, l := range labels {
			if severityLabel.MatchString(l) && !lowSeverity.MatchString(l) {
				return true
			}
		}
	}
	return false
}

// DeepReview reports stakes evidence from review depth: two or more distinct
// approvers, or three or more reviews.
func DeepReview(pr PR) bool {
	return pr.Approvers >= 2 || pr.Reviews >= 3
}

func anyMatch(labels []string, re *regexp.Regexp) bool {
	for _, l := range labels {
		if re.MatchString(l) {
			return true
		}
	}
	return false
}

// LabeledPR is a merged PR mapped to its local commit, with its evidence.
type LabeledPR struct {
	PR
	Files []string // files changed by the PR's commit, from local git

	// Fragility evidence ("Will it break?").
	BugFix bool
	Revert bool

	// Stakes evidence ("How bad if it breaks?"), independent of the LLM.
	Backported bool
	FastRevert bool
	Severity   bool
	DeepReview bool
}

// Label applies the event rules to every PR that has a local commit in
// prCommit; unmapped PRs are left out. backported and fastReverted come from
// BackportedPRs and FastReverts.
func Label(prs []PR, prCommit map[int]Commit, backported, fastReverted map[int]bool) []LabeledPR {
	var out []LabeledPR
	for _, pr := range prs {
		c, ok := prCommit[pr.Number]
		if !ok {
			continue
		}
		out = append(out, LabeledPR{
			PR:         pr,
			Files:      c.Files,
			BugFix:     IsBugFix(pr),
			Revert:     IsRevert(pr),
			Backported: backported[pr.Number],
			FastRevert: fastReverted[pr.Number],
			Severity:   HasSeverityLabel(pr),
			DeepReview: DeepReview(pr),
		})
	}
	return out
}
