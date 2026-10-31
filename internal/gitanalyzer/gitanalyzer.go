package gitanalyzer

import (
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/zanetworker/highstakes/internal/types"
)

// GitAnalyzer analyzes git history for change frequency and patterns
type GitAnalyzer struct {
	repo     *git.Repository
	repoPath string
}

// FileHistory holds git history for a file
type FileHistory struct {
	Path           string
	CommitCount    int
	CommitsLast90d int
	UniqueAuthors  map[string]bool
	RecentChanges  []types.Change
}

// New creates a new git analyzer for the given repository
func New(repoPath string) (*GitAnalyzer, error) {
	// EnableDotGitCommonDir lets go-git read linked worktrees (`git worktree add`).
	repo, err := git.PlainOpenWithOptions(repoPath, &git.PlainOpenOptions{EnableDotGitCommonDir: true})
	if err != nil {
		return nil, fmt.Errorf("open repository: %w", err)
	}

	return &GitAnalyzer{repo: repo, repoPath: repoPath}, nil
}

// AnalyzeAll reads history for the given files in one `git log` pass over the
// whole repository. Earlier versions walked the log once per file with a 2s
// per-file and 30s overall timeout, which silently dropped all history on
// large repos. Files with no commits are absent from the result. An error
// means history could not be read at all (no git binary, no commits).
func (g *GitAnalyzer) AnalyzeAll(files []string, lookbackDays int) (map[string]*FileHistory, error) {
	// Merge commits list no files with --name-only, so they are ignored.
	out, err := exec.Command("git", "-C", g.repoPath, "-c", "core.quotePath=false",
		"log", "--relative", "--name-only", "--format=%x1e%h%x1f%aE%x1f%at%x1f%s", "HEAD").Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("git log in %s: %s", g.repoPath, strings.TrimSpace(string(ee.Stderr)))
		}
		return nil, fmt.Errorf("git log in %s: %w", g.repoPath, err)
	}
	return historyFromLog(string(out), files, lookbackDays, time.Now()), nil
}

// historyFromLog builds per-file history from newest-first `git log
// --name-only --format=%x1e%h%x1f%aE%x1f%at%x1f%s` output.
func historyFromLog(out string, files []string, lookbackDays int, now time.Time) map[string]*FileHistory {
	wanted := make(map[string]bool, len(files))
	for _, f := range files {
		wanted[f] = true
	}
	cutoff := now.AddDate(0, 0, -lookbackDays)
	recentCutoff := now.AddDate(0, 0, -90)

	results := make(map[string]*FileHistory)
	for _, rec := range strings.Split(out, "\x1e") {
		lines := strings.Split(rec, "\n")
		hdr := strings.SplitN(lines[0], "\x1f", 4)
		if len(hdr) != 4 {
			continue
		}
		ts, err := strconv.ParseInt(hdr[2], 10, 64)
		if err != nil {
			continue
		}
		when := time.Unix(ts, 0)
		for _, path := range lines[1:] {
			if !wanted[path] {
				continue
			}
			h := results[path]
			if h == nil {
				h = &FileHistory{Path: path, UniqueAuthors: map[string]bool{}, RecentChanges: []types.Change{}}
				results[path] = h
			}
			h.CommitCount++
			h.UniqueAuthors[hdr[1]] = true
			if when.After(recentCutoff) {
				h.CommitsLast90d++
			}
			if len(h.RecentChanges) < 10 && when.After(cutoff) {
				h.RecentChanges = append(h.RecentChanges, types.Change{
					Date:        when,
					Message:     hdr[3],
					Author:      hdr[1],
					SHA:         shortSHA(hdr[0]),
					PRNumber:    extractPRNumber(hdr[3]),
					HadIncident: detectBugFix(hdr[3]),
				})
			}
		}
	}
	return results
}

func shortSHA(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}

// GetCurrentBranch returns the current git branch name
func (g *GitAnalyzer) GetCurrentBranch() (string, error) {
	head, err := g.repo.Head()
	if err != nil {
		return "", err
	}

	return head.Name().Short(), nil
}

// GetHeadCommit returns the current HEAD commit SHA
func (g *GitAnalyzer) GetHeadCommit() (string, error) {
	head, err := g.repo.Head()
	if err != nil {
		return "", err
	}

	return head.Hash().String(), nil
}

// extractPRNumber extracts PR number from commit message
// Looks for patterns like "Merge pull request #1234" or "(#1234)"
func extractPRNumber(message string) *int {
	// Pattern 1: Merge pull request #1234
	if strings.Contains(message, "Merge pull request #") {
		parts := strings.Split(message, "#")
		if len(parts) >= 2 {
			numStr := strings.Fields(parts[1])[0]
			var num int
			if _, err := fmt.Sscanf(numStr, "%d", &num); err == nil {
				return &num
			}
		}
	}

	// Pattern 2: (#1234)
	if strings.Contains(message, "(#") {
		start := strings.Index(message, "(#")
		end := strings.Index(message[start:], ")")
		if end > 0 {
			numStr := message[start+2 : start+end]
			var num int
			if _, err := fmt.Sscanf(numStr, "%d", &num); err == nil {
				return &num
			}
		}
	}

	return nil
}

// detectBugFix returns true if the commit message indicates a bug fix
// bugFixWords matches fix vocabulary as whole words, so "prefix", "debug",
// "dispatch", "incorrect" and "resolver" do not count as fixes.
var bugFixWords = regexp.MustCompile(`(?i)\b(fix(es|ed|ing)?|bug(s|fix|fixes)?|hotfix(es)?|patch(es|ed)?|repair(s|ed)?|correct(s|ed)?|resolve[sd]?)\b`)

func detectBugFix(message string) bool {
	return bugFixWords.MatchString(message)
}

// firstLine returns the first line of a multi-line string
func firstLine(s string) string {
	lines := strings.Split(s, "\n")
	if len(lines) == 0 {
		return ""
	}
	return strings.TrimSpace(lines[0])
}

// IsBugFix reports whether a commit message looks like a bug fix.
func IsBugFix(message string) bool {
	return detectBugFix(message)
}

// CommitInfo is one non-merge commit with the paths it touched.
type CommitInfo struct {
	SHA     string
	Message string
	When    time.Time // committer date
	Files   []string  // paths added, modified or deleted (repo-relative, forward slashes)
}

// CommitsSince walks history from HEAD once and returns every non-merge commit
// whose committer date is at or after since, with the files it touched. Merge
// commits are skipped because their diff repeats the merged branch's commits.
func (g *GitAnalyzer) CommitsSince(since time.Time) ([]CommitInfo, error) {
	head, err := g.repo.Head()
	if err != nil {
		return nil, fmt.Errorf("get HEAD: %w", err)
	}
	iter, err := g.repo.Log(&git.LogOptions{From: head.Hash()})
	if err != nil {
		return nil, fmt.Errorf("get log: %w", err)
	}

	var out []CommitInfo
	err = iter.ForEach(func(c *object.Commit) error {
		if c.NumParents() > 1 || c.Committer.When.Before(since) {
			return nil
		}
		files, err := changedFiles(c)
		if err != nil {
			return fmt.Errorf("diff %s: %w", c.Hash.String()[:7], err)
		}
		out = append(out, CommitInfo{
			SHA:     c.Hash.String(),
			Message: strings.TrimSpace(c.Message),
			When:    c.Committer.When,
			Files:   files,
		})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("iterate commits: %w", err)
	}
	return out, nil
}

// FilesAt returns the files present in the newest commit (reachable from HEAD)
// whose committer date is at or before t. It returns an empty set when no
// commit is that old.
func (g *GitAnalyzer) FilesAt(t time.Time) (map[string]bool, error) {
	head, err := g.repo.Head()
	if err != nil {
		return nil, fmt.Errorf("get HEAD: %w", err)
	}
	iter, err := g.repo.Log(&git.LogOptions{From: head.Hash()})
	if err != nil {
		return nil, fmt.Errorf("get log: %w", err)
	}
	var best *object.Commit
	err = iter.ForEach(func(c *object.Commit) error {
		if c.Committer.When.After(t) {
			return nil
		}
		if best == nil || c.Committer.When.After(best.Committer.When) {
			best = c
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("iterate commits: %w", err)
	}
	files := map[string]bool{}
	if best == nil {
		return files, nil
	}
	tree, err := best.Tree()
	if err != nil {
		return nil, fmt.Errorf("tree of %s: %w", best.Hash.String()[:7], err)
	}
	err = tree.Files().ForEach(func(f *object.File) error {
		files[f.Name] = true
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("list files: %w", err)
	}
	return files, nil
}

// changedFiles lists the paths a commit changed relative to its first parent
// (or every file, for a root commit). It compares trees only, without diffing
// file contents.
func changedFiles(c *object.Commit) ([]string, error) {
	tree, err := c.Tree()
	if err != nil {
		return nil, err
	}
	var parentTree *object.Tree
	if c.NumParents() > 0 {
		parent, err := c.Parent(0)
		if err != nil {
			return nil, err
		}
		if parentTree, err = parent.Tree(); err != nil {
			return nil, err
		}
	}
	changes, err := object.DiffTree(parentTree, tree)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var files []string
	for _, ch := range changes {
		for _, name := range []string{ch.From.Name, ch.To.Name} {
			if name != "" && !seen[name] {
				seen[name] = true
				files = append(files, name)
			}
		}
	}
	return files, nil
}
