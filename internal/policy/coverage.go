package policy

import (
	"os"
	"path"
	"path/filepath"
	"strings"
)

// codeownersLocations are where GitHub looks for CODEOWNERS, in its order.
var codeownersLocations = []string{
	filepath.Join(".github", "CODEOWNERS"),
	"CODEOWNERS",
	filepath.Join("docs", "CODEOWNERS"),
}

// CodeownersFile is a parsed CODEOWNERS file.
type CodeownersFile struct{ rules []ownerRule }

type ownerRule struct {
	segs     []string // pattern split on "/", without leading or trailing slash
	anchored bool     // matched from the repo root
	dirOnly  bool     // trailing slash: matches a directory, not a file
	owned    bool     // the rule names at least one owner
}

// LoadCodeowners reads the first CODEOWNERS file GitHub would use in
// repoPath and returns it with its repo-relative path. A missing or
// unreadable file yields an empty set (nothing covered) and "".
func LoadCodeowners(repoPath string) (CodeownersFile, string) {
	for _, rel := range codeownersLocations {
		if b, err := os.ReadFile(filepath.Join(repoPath, rel)); err == nil {
			return ParseCodeowners(string(b)), rel
		}
	}
	return CodeownersFile{}, ""
}

// ParseCodeowners parses CODEOWNERS content (gitignore-style patterns).
func ParseCodeowners(content string) CodeownersFile {
	var c CodeownersFile
	for _, line := range strings.Split(content, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 || strings.HasPrefix(fields[0], "#") {
			continue
		}
		p := fields[0]
		r := ownerRule{owned: len(fields) > 1 && !strings.HasPrefix(fields[1], "#")}
		if strings.HasPrefix(p, "/") {
			r.anchored, p = true, strings.TrimPrefix(p, "/")
		}
		if strings.HasSuffix(p, "/") {
			r.dirOnly, p = true, strings.TrimSuffix(p, "/")
		}
		if strings.Contains(p, "/") {
			r.anchored = true // a slash in the middle anchors, as in gitignore
		}
		if p == "" {
			continue
		}
		r.segs = strings.Split(p, "/")
		c.rules = append(c.rules, r)
	}
	return c
}

// Covers reports whether the last rule matching a repo-relative path names
// an owner, the way GitHub resolves CODEOWNERS.
func (c CodeownersFile) Covers(p string) bool {
	if p == "" {
		return false
	}
	segs := strings.Split(p, "/")
	covered := false
	for _, r := range c.rules {
		if r.matches(segs) {
			covered = r.owned
		}
	}
	return covered
}

// matches tries the pattern against every directory prefix of the path
// (a matched directory covers what is inside) and, unanchored, at every depth.
func (r ownerRule) matches(segs []string) bool {
	starts := []int{0}
	if !r.anchored {
		for i := 1; i < len(segs); i++ {
			starts = append(starts, i)
		}
	}
	for _, s := range starts {
		for end := s + 1; end <= len(segs); end++ {
			isFile := end == len(segs)
			if r.dirOnly && isFile {
				continue
			}
			if matchSegs(r.segs, segs[s:end]) {
				return true
			}
		}
	}
	return false
}

// matchSegs matches pattern segments against path segments; "**" spans any
// number of segments, other segments use path.Match.
func matchSegs(pat, segs []string) bool {
	if len(pat) == 0 {
		return len(segs) == 0
	}
	if pat[0] == "**" {
		for i := 0; i <= len(segs); i++ {
			if matchSegs(pat[1:], segs[i:]) {
				return true
			}
		}
		return false
	}
	if len(segs) == 0 {
		return false
	}
	if ok, err := path.Match(pat[0], segs[0]); err != nil || !ok {
		return false
	}
	return matchSegs(pat[1:], segs[1:])
}
