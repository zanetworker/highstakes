// Package prdata collects merged pull requests from GitHub, maps them to
// local git commits, and labels them as evidence for the two HighStakes axes:
// fragility (bug-fix and revert PRs, for "Will it break?") and stakes
// (backports, fast reverts, severity labels, review depth and CODEOWNERS
// coverage, for "How bad if it breaks?").
package prdata

import "time"

// PR is the metadata of one merged pull request.
type PR struct {
	Number             int       `json:"number"`
	Title              string    `json:"title"`
	MergedAt           time.Time `json:"merged_at"`
	UpdatedAt          time.Time `json:"updated_at"`
	MergeCommit        string    `json:"merge_commit"` // SHA GitHub reports for the merge
	Author             string    `json:"author"`
	Labels             []string  `json:"labels"`
	ClosingIssues      []int     `json:"closing_issues"`
	ClosingIssueLabels []string  `json:"closing_issue_labels"`
	Reviews            int       `json:"reviews"`   // total review count
	Approvers          int       `json:"approvers"` // distinct approving reviewers
}
