// Package ownership derives per-file owners from git history.
//
// It shells out to the git CLI rather than go-git because go-git has no
// .mailmap support and computing per-commit line stats with it diffs every
// commit in full. One `git log --numstat` pass is mailmap-aware and cheap.
package ownership

import (
	"fmt"
	"math"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// DefaultHalfLifeDays is how fast an author's contribution loses weight.
const DefaultHalfLifeDays = 180

// maxPathspecs caps paths passed to git; above it we read all history and filter.
const maxPathspecs = 200

// Commit is one parsed commit with lines changed per file.
type Commit struct {
	Name  string
	Email string
	When  time.Time
	Files map[string]int // path -> lines added+deleted (binary counts as 1)
}

// Owner is one author's share of a file.
type Owner struct {
	Handle  string  `json:"handle"` // @github-handle from aliases/noreply, else email
	Email   string  `json:"email"`
	Name    string  `json:"name"`
	Share   float64 `json:"share"` // 0-1, recency- and line-weighted
	Commits int     `json:"commits"`
}

// Options tunes ownership scoring.
type Options struct {
	Aliases      map[string]string // email -> @handle (config `owners`)
	HalfLifeDays int               // default DefaultHalfLifeDays
	Now          time.Time         // default time.Now()
}

// FileOwners returns owners per file, ranked by share, for the repo at repoPath.
// paths limits results; nil means every file in history.
func FileOwners(repoPath string, paths []string, opts Options) (map[string][]Owner, error) {
	args := []string{"-C", repoPath, "log", "--no-merges", "--no-renames", "--numstat",
		"--format=%x1e%aN%x1f%aE%x1f%at"}
	if len(paths) > 0 && len(paths) <= maxPathspecs {
		args = append(args, "--")
		args = append(args, paths...)
	}
	out, err := exec.Command("git", args...).Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("git log in %s: %s", repoPath, strings.TrimSpace(string(ee.Stderr)))
		}
		return nil, fmt.Errorf("git log in %s: %w", repoPath, err)
	}

	all := Compute(parseLog(string(out)), opts)
	if len(paths) == 0 {
		return all, nil
	}
	filtered := make(map[string][]Owner, len(paths))
	for _, p := range paths {
		if o, ok := all[p]; ok {
			filtered[p] = o
		}
	}
	return filtered, nil
}

// parseLog parses output of `git log --numstat --format=%x1e%aN%x1f%aE%x1f%at`.
func parseLog(out string) []Commit {
	var commits []Commit
	for _, rec := range strings.Split(out, "\x1e") {
		if strings.TrimSpace(rec) == "" {
			continue
		}
		lines := strings.Split(rec, "\n")
		hdr := strings.Split(lines[0], "\x1f")
		if len(hdr) != 3 {
			continue
		}
		ts, err := strconv.ParseInt(strings.TrimSpace(hdr[2]), 10, 64)
		if err != nil {
			continue
		}
		c := Commit{Name: hdr[0], Email: hdr[1], When: time.Unix(ts, 0), Files: map[string]int{}}
		for _, l := range lines[1:] {
			parts := strings.SplitN(l, "\t", 3)
			if len(parts) != 3 {
				continue
			}
			added, errA := strconv.Atoi(parts[0])
			deleted, errD := strconv.Atoi(parts[1])
			n := added + deleted
			if errA != nil || errD != nil || n == 0 {
				n = 1 // binary ("-") or mode-only change still counts as a touch
			}
			c.Files[parts[2]] += n
		}
		commits = append(commits, c)
	}
	return commits
}

// Compute scores authors per file: sum of lines * 0.5^(age/halfLife), normalized to shares.
// Bot authors (name or email containing "[bot]") are ignored.
func Compute(commits []Commit, opts Options) map[string][]Owner {
	if opts.Now.IsZero() {
		opts.Now = time.Now()
	}
	if opts.HalfLifeDays <= 0 {
		opts.HalfLifeDays = DefaultHalfLifeDays
	}

	type acc struct {
		owner Owner
		score float64
	}
	byFile := map[string]map[string]*acc{}
	for _, c := range commits {
		if strings.Contains(c.Name, "[bot]") || strings.Contains(c.Email, "[bot]") {
			continue
		}
		email := strings.ToLower(c.Email)
		ageDays := opts.Now.Sub(c.When).Hours() / 24
		if ageDays < 0 {
			ageDays = 0
		}
		weight := math.Pow(0.5, ageDays/float64(opts.HalfLifeDays))
		for path, n := range c.Files {
			if byFile[path] == nil {
				byFile[path] = map[string]*acc{}
			}
			a := byFile[path][email]
			if a == nil {
				a = &acc{owner: Owner{Email: email, Name: c.Name, Handle: HandleFor(email, opts.Aliases)}}
				byFile[path][email] = a
			}
			a.owner.Commits++
			a.score += float64(n) * weight
		}
	}

	result := make(map[string][]Owner, len(byFile))
	for path, authors := range byFile {
		total := 0.0
		for _, a := range authors {
			total += a.score
		}
		owners := make([]Owner, 0, len(authors))
		for _, a := range authors {
			o := a.owner
			if total > 0 {
				o.Share = a.score / total
			}
			owners = append(owners, o)
		}
		sort.Slice(owners, func(i, j int) bool {
			if owners[i].Share != owners[j].Share {
				return owners[i].Share > owners[j].Share
			}
			return owners[i].Email < owners[j].Email
		})
		result[path] = owners
	}
	return result
}

var noreplyRe = regexp.MustCompile(`^(?:\d+\+)?([^@+]+)@users\.noreply\.github\.com$`)

// HandleFor maps an email to a reviewer handle: the alias if configured,
// @user for GitHub noreply addresses, otherwise the email itself.
func HandleFor(email string, aliases map[string]string) string {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" {
		return ""
	}
	for k, v := range aliases {
		if strings.ToLower(strings.TrimSpace(k)) == email && strings.TrimSpace(v) != "" {
			v = strings.TrimSpace(v)
			if !strings.HasPrefix(v, "@") {
				v = "@" + v
			}
			return v
		}
	}
	if m := noreplyRe.FindStringSubmatch(email); m != nil {
		return "@" + m[1]
	}
	return email
}
