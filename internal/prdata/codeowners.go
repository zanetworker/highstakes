package prdata

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// CodeOwners is a parsed CODEOWNERS file.
type CodeOwners struct {
	rules []ownerRule
}

type ownerRule struct {
	re    *regexp.Regexp
	owned bool // false when the pattern lists no owners (un-owns matches)
}

// codeOwnersLocations are where GitHub looks for CODEOWNERS, in its order.
var codeOwnersLocations = []string{".github/CODEOWNERS", "CODEOWNERS", "docs/CODEOWNERS"}

// LoadCodeOwners reads the first CODEOWNERS file GitHub would use in
// repoDir. It returns nil and an empty path when there is none.
func LoadCodeOwners(repoDir string) (*CodeOwners, string, error) {
	for _, rel := range codeOwnersLocations {
		b, err := os.ReadFile(filepath.Join(repoDir, filepath.FromSlash(rel)))
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, "", fmt.Errorf("read %s: %w", rel, err)
		}
		return ParseCodeOwners(string(b)), rel, nil
	}
	return nil, "", nil
}

// ParseCodeOwners parses CODEOWNERS text (gitignore-style patterns, one rule
// per line, the last matching rule wins). Unparseable patterns are skipped.
func ParseCodeOwners(text string) *CodeOwners {
	co := &CodeOwners{}
	for _, line := range strings.Split(text, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 || strings.HasPrefix(fields[0], "#") {
			continue
		}
		owners := 0
		for _, f := range fields[1:] {
			if strings.HasPrefix(f, "#") {
				break
			}
			owners++
		}
		re, err := regexp.Compile(patternRegexp(fields[0]))
		if err != nil {
			continue
		}
		co.rules = append(co.rules, ownerRule{re: re, owned: owners > 0})
	}
	return co
}

// patternRegexp turns a CODEOWNERS pattern into an anchored regexp over
// repo-relative paths. A pattern with a slash before its end is anchored at
// the root; a trailing slash means a directory; a pattern also covers
// everything below a directory it names.
func patternRegexp(p string) string {
	dir := strings.HasSuffix(p, "/")
	p = strings.TrimSuffix(p, "/")
	anchored := strings.Contains(p, "/")
	p = strings.TrimPrefix(p, "/")

	var b strings.Builder
	if anchored {
		b.WriteString("^")
	} else {
		b.WriteString("^(.*/)?")
	}
	for i := 0; i < len(p); i++ {
		switch {
		case strings.HasPrefix(p[i:], "**/"):
			b.WriteString("(.*/)?")
			i += 2
		case strings.HasPrefix(p[i:], "**"):
			b.WriteString(".*")
			i++
		case p[i] == '*':
			b.WriteString("[^/]*")
		case p[i] == '?':
			b.WriteString("[^/]")
		default:
			b.WriteString(regexp.QuoteMeta(p[i : i+1]))
		}
	}
	if dir {
		b.WriteString("/.*$")
	} else {
		b.WriteString("(/.*)?$")
	}
	return b.String()
}

// Covers reports whether the last rule matching path assigns owners.
func (co *CodeOwners) Covers(path string) bool {
	if co == nil {
		return false
	}
	for i := len(co.rules) - 1; i >= 0; i-- {
		if co.rules[i].re.MatchString(path) {
			return co.rules[i].owned
		}
	}
	return false
}
