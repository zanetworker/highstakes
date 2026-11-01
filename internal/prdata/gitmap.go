package prdata

import (
	"bytes"
	"fmt"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Commit is one local git commit with the files it changed (against its first
// parent for merge commits).
type Commit struct {
	SHA     string
	Parents []string
	When    time.Time // committer date
	Message string
	Files   []string
}

func (c Commit) subject() string {
	return strings.TrimSpace(strings.SplitN(c.Message, "\n", 2)[0])
}

// ReadCommits runs git log over revs (e.g. "HEAD", or "origin/release-1" and
// "--not" "HEAD") for commits with a committer date at or after since. With
// firstParent it follows only the first parent, which on a default branch is
// the sequence of landed changes; merge commits list the files they brought
// in. Renames are listed as the new path only.
func ReadCommits(repoDir string, since time.Time, firstParent bool, revs ...string) ([]Commit, error) {
	args := []string{"-C", repoDir, "log", "--no-renames", "--name-only", "--diff-merges=first-parent",
		"--format=%x1e%H%x1f%P%x1f%ct%x1f%B%x1f"}
	if !since.IsZero() {
		args = append(args, fmt.Sprintf("--since=%d", since.Unix()))
	}
	if firstParent {
		args = append(args, "--first-parent")
	}
	args = append(args, revs...)
	args = append(args, "--")
	var stderr bytes.Buffer
	cmd := exec.Command("git", args...)
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git log %s in %s: %v: %s", strings.Join(revs, " "), repoDir, err, strings.TrimSpace(stderr.String()))
	}
	return parseLog(string(out))
}

// parseLog parses the output of the git log format used by ReadCommits.
func parseLog(raw string) ([]Commit, error) {
	var out []Commit
	for _, rec := range strings.Split(raw, "\x1e") {
		if strings.TrimSpace(rec) == "" {
			continue
		}
		parts := strings.SplitN(rec, "\x1f", 5)
		if len(parts) != 5 {
			return nil, fmt.Errorf("malformed git log record (%d fields, want 5): %.60q", len(parts), rec)
		}
		ts, err := strconv.ParseInt(parts[2], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("commit %s: bad timestamp %q: %w", parts[0], parts[2], err)
		}
		c := Commit{
			SHA:     parts[0],
			Parents: strings.Fields(parts[1]),
			When:    time.Unix(ts, 0),
			Message: strings.TrimSpace(parts[3]),
		}
		for _, line := range strings.Split(parts[4], "\n") {
			if line = strings.TrimSpace(line); line != "" {
				c.Files = append(c.Files, line)
			}
		}
		out = append(out, c)
	}
	return out, nil
}

var (
	squashSubject = regexp.MustCompile(`\(#(\d+)\)\s*$`)
	mergeSubject  = regexp.MustCompile(`^Merge pull request #(\d+)`)
	anyPRRef      = regexp.MustCompile(`\(#(\d+)\)`)
	cherryPick    = regexp.MustCompile(`\(cherry picked from commit ([0-9a-f]{7,40})\)`)
	revertsSHA    = regexp.MustCompile(`This reverts commit ([0-9a-f]{7,40})`)
	revertsPR     = regexp.MustCompile(`(?m)^Reverts (?:[\w.-]+/[\w.-]+)?#(\d+)`)
)

// subjectPRNumber returns the PR number a commit subject says landed it:
// "... (#123)" at the end (squash merge) or "Merge pull request #123".
func subjectPRNumber(subject string) (int, bool) {
	subject = strings.TrimSpace(strings.SplitN(subject, "\n", 2)[0])
	for _, re := range []*regexp.Regexp{squashSubject, mergeSubject} {
		if m := re.FindStringSubmatch(subject); m != nil {
			n, err := strconv.Atoi(m[1])
			return n, err == nil
		}
	}
	return 0, false
}

// MapPRs maps each PR to the commit that landed it on the default branch:
// the commit GitHub reports as its merge commit when that commit is in
// commits, otherwise the newest commit whose subject names the PR. PRs with
// no such commit are absent from the result. A rebase-merged PR (merge commit
// with one parent and no "(#N)" in its subject, more than one PR commit, and
// subjects matching the PR's commit headlines) is reported as its last commit
// with Files the union over the PR's commits.
func MapPRs(prs []PR, commits []Commit) map[int]Commit {
	bySHA := make(map[string]Commit, len(commits))
	byNumber := map[int]Commit{}
	for _, c := range commits { // newest first, so keep the first subject match
		bySHA[c.SHA] = c
		if n, ok := subjectPRNumber(c.subject()); ok {
			if _, seen := byNumber[n]; !seen {
				byNumber[n] = c
			}
		}
	}
	out := map[int]Commit{}
	for _, pr := range prs {
		if c, ok := bySHA[pr.MergeCommit]; ok && pr.MergeCommit != "" {
			out[pr.Number] = withRebasedFiles(c, pr, bySHA)
		} else if c, ok := byNumber[pr.Number]; ok {
			out[pr.Number] = c
		}
	}
	return out
}

// withRebasedFiles returns c with Files covering every commit of pr when pr
// was rebase-merged: GitHub reports only the last rebased commit as the merge
// commit, so the files of up to pr.Commits-1 first-parent ancestors are added
// (as far as commits reach). A commit counts as the PR's only while its
// subject is one of pr.CommitHeadlines, so a squash merge with a custom
// message (or the PR title as its subject) is not walked into unrelated
// earlier commits.
func withRebasedFiles(c Commit, pr PR, bySHA map[string]Commit) Commit {
	if pr.Commits <= 1 || len(c.Parents) != 1 {
		return c
	}
	if n, ok := subjectPRNumber(c.subject()); ok && n == pr.Number {
		return c
	}
	unmatched := map[string]int{}
	for _, h := range pr.CommitHeadlines {
		unmatched[strings.TrimSpace(h)]++
	}
	if unmatched[c.subject()] == 0 {
		return c
	}
	seen := map[string]bool{}
	var files []string
	cur, ok := c, true
	for i := 0; i < pr.Commits && ok && unmatched[cur.subject()] > 0; i++ {
		unmatched[cur.subject()]--
		for _, f := range cur.Files {
			if !seen[f] {
				seen[f] = true
				files = append(files, f)
			}
		}
		if len(cur.Parents) == 0 {
			break
		}
		cur, ok = bySHA[cur.Parents[0]]
	}
	sort.Strings(files)
	c.Files = files
	return c
}

var (
	backportPrefix = regexp.MustCompile(`^(release|backport|stable|v[0-9])`)
	versionLine    = regexp.MustCompile(`^[^/]*[0-9]\.x$`) // tokio-1.43.x, v0.2.x
)

// IsBackportBranch reports whether a remote-tracking ref ("origin/release-1")
// looks like a release or backport line: the branch name starts with release,
// backport, stable or v<digit>, or is a version line ending in "<digit>.x".
func IsBackportBranch(ref string) bool {
	_, name, ok := strings.Cut(ref, "/")
	if !ok {
		return false
	}
	return backportPrefix.MatchString(name) || versionLine.MatchString(name)
}

// BackportBranches lists the remote-tracking refs in repoDir that
// IsBackportBranch accepts.
func BackportBranches(repoDir string) ([]string, error) {
	out, err := exec.Command("git", "-C", repoDir, "for-each-ref", "--format=%(refname:short)", "refs/remotes").Output()
	if err != nil {
		return nil, fmt.Errorf("list remote branches in %s: %w", repoDir, err)
	}
	var refs []string
	for _, ref := range strings.Fields(string(out)) {
		if IsBackportBranch(ref) {
			refs = append(refs, ref)
		}
	}
	return refs, nil
}

// BranchOnly returns the branch commits that are not in head: with both read
// first-parent, these are the commits made on a release line itself, whether
// or not the line was later merged into the default branch.
func BranchOnly(branchCommits, head []Commit) []Commit {
	onHead := make(map[string]bool, len(head))
	for _, c := range head {
		onHead[c.SHA] = true
	}
	var out []Commit
	for _, c := range branchCommits {
		if !onHead[c.SHA] {
			out = append(out, c)
		}
	}
	return out
}

// BackportedPRs finds PRs whose change reached a release or backport branch:
// a branch commit carries "(cherry picked from commit <sha>)" naming the PR's
// commit, or a "(#N)" in a branch commit subject is the PR's number or a
// number in the subject of the PR's own commit (a fix landed on the release
// line first and merged forward as "fix (#N) (#M)").
func BackportedPRs(branchCommits []Commit, prCommit map[int]Commit) map[int]bool {
	out := map[int]bool{}
	branchRefs := map[int]bool{}
	for _, bc := range branchCommits {
		for _, m := range cherryPick.FindAllStringSubmatch(bc.Message, -1) {
			if n, ok := prBySHAPrefix(prCommit, m[1]); ok {
				out[n] = true
			}
		}
		for _, n := range subjectRefs(bc.subject()) {
			branchRefs[n] = true
		}
	}
	for n, c := range prCommit {
		if branchRefs[n] {
			out[n] = true
			continue
		}
		for _, ref := range subjectRefs(c.subject()) {
			if branchRefs[ref] {
				out[n] = true
				break
			}
		}
	}
	return out
}

func subjectRefs(subject string) []int {
	var out []int
	for _, m := range anyPRRef.FindAllStringSubmatch(subject, -1) {
		if n, err := strconv.Atoi(m[1]); err == nil {
			out = append(out, n)
		}
	}
	return out
}

// FastReverts finds PRs reverted within the given time of their merge: a
// commit says "This reverts commit <sha>" for the PR's commit, or
// "Reverts [owner/repo]#N" (GitHub's revert button), and is dated no later
// than within after the PR merged.
func FastReverts(prs []PR, prCommit map[int]Commit, commits []Commit, within time.Duration) map[int]bool {
	mergedAt := make(map[int]time.Time, len(prs))
	for _, pr := range prs {
		mergedAt[pr.Number] = pr.MergedAt
	}
	out := map[int]bool{}
	mark := func(n int, when time.Time) {
		m, ok := mergedAt[n]
		if !ok {
			return
		}
		if d := when.Sub(m); d >= 0 && d <= within {
			out[n] = true
		}
	}
	for _, c := range commits {
		for _, m := range revertsSHA.FindAllStringSubmatch(c.Message, -1) {
			if n, ok := prBySHAPrefix(prCommit, m[1]); ok {
				mark(n, c.When)
			}
		}
		for _, m := range revertsPR.FindAllStringSubmatch(c.Message, -1) {
			if n, err := strconv.Atoi(m[1]); err == nil {
				mark(n, c.When)
			}
		}
	}
	return out
}

func prBySHAPrefix(prCommit map[int]Commit, sha string) (int, bool) {
	for n, c := range prCommit {
		if strings.HasPrefix(c.SHA, sha) {
			return n, true
		}
	}
	return 0, false
}
