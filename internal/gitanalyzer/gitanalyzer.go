package gitanalyzer

import (
	"fmt"
	"strings"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/zanetworker/code-heatmap/internal/types"
)

// GitAnalyzer analyzes git history for change frequency and patterns
type GitAnalyzer struct {
	repo *git.Repository
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
	repo, err := git.PlainOpen(repoPath)
	if err != nil {
		return nil, fmt.Errorf("open repository: %w", err)
	}

	return &GitAnalyzer{repo: repo}, nil
}

// AnalyzeFile analyzes git history for a specific file
func (g *GitAnalyzer) AnalyzeFile(filePath string, lookbackDays int) (*FileHistory, error) {
	history := &FileHistory{
		Path:          filePath,
		UniqueAuthors: make(map[string]bool),
		RecentChanges: []types.Change{},
	}

	// Get HEAD commit
	head, err := g.repo.Head()
	if err != nil {
		return nil, fmt.Errorf("get HEAD: %w", err)
	}

	// Get commit history
	commits, err := g.repo.Log(&git.LogOptions{
		From:     head.Hash(),
		FileName: &filePath,
	})
	if err != nil {
		return nil, fmt.Errorf("get log: %w", err)
	}

	now := time.Now()
	cutoff := now.AddDate(0, 0, -lookbackDays)
	recentCutoff := now.AddDate(0, 0, -90)

	// Iterate commits
	err = commits.ForEach(func(c *object.Commit) error {
		history.CommitCount++

		// Count unique authors
		history.UniqueAuthors[c.Author.Email] = true

		// Count commits in last 90 days
		if c.Author.When.After(recentCutoff) {
			history.CommitsLast90d++
		}

		// Store recent changes (last 10 commits)
		if len(history.RecentChanges) < 10 && c.Author.When.After(cutoff) {
			prNumber := extractPRNumber(c.Message)
			change := types.Change{
				Date:        c.Author.When,
				Message:     firstLine(c.Message),
				Author:      c.Author.Email,
				SHA:         c.Hash.String()[:7],
				PRNumber:    prNumber,
				HadIncident: detectBugFix(c.Message),
			}
			history.RecentChanges = append(history.RecentChanges, change)
		}

		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("iterate commits: %w", err)
	}

	return history, nil
}

// AnalyzeAll analyzes git history for all files in the repository
func (g *GitAnalyzer) AnalyzeAll(files []string, lookbackDays int) (map[string]*FileHistory, error) {
	results := make(map[string]*FileHistory)

	for _, file := range files {
		history, err := g.AnalyzeFile(file, lookbackDays)
		if err != nil {
			// Log error but continue
			fmt.Printf("Warning: failed to analyze %s: %v\n", file, err)
			continue
		}
		results[file] = history
	}

	return results, nil
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
func detectBugFix(message string) bool {
	lower := strings.ToLower(message)
	keywords := []string{"fix", "bug", "patch", "hotfix", "repair", "correct", "resolve"}

	for _, keyword := range keywords {
		if strings.Contains(lower, keyword) {
			return true
		}
	}

	return false
}

// firstLine returns the first line of a multi-line string
func firstLine(s string) string {
	lines := strings.Split(s, "\n")
	if len(lines) == 0 {
		return ""
	}
	return strings.TrimSpace(lines[0])
}
