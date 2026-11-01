package tour

import (
	"path"
	"sort"
	"strings"

	"github.com/zanetworker/highstakes/internal/types"
)

// markerNames are files that make their directory a component: a Rust
// crate, a JS/TS package, a Python package, a Go module.
var markerNames = []string{"Cargo.toml", "package.json", "pyproject.toml", "__init__.py", "go.mod"}

// Component returns the top-level unit a repo path belongs to. markers holds
// the repo-relative paths of marker files that exist (see Markers). Rules, in
// order:
//   - the shallowest non-root directory holding a marker file (crate,
//     package, Python package, Go module); nested units fold into it
//   - for cmd/, internal/ and pkg/, the top two path segments (a Go package root)
//   - else the top-level directory; "." for files at the repo root
func Component(p string, markers map[string]bool) string {
	dir := path.Dir(p)
	if dir == "." || dir == "/" {
		return "."
	}
	segs := strings.Split(dir, "/")
	for i := 1; i <= len(segs); i++ {
		d := strings.Join(segs[:i], "/")
		for _, m := range markerNames {
			if markers[d+"/"+m] {
				return d
			}
		}
	}
	switch segs[0] {
	case "cmd", "internal", "pkg":
		if len(segs) >= 2 {
			return segs[0] + "/" + segs[1]
		}
	}
	return segs[0]
}

// Markers returns the marker files that exist in the non-root ancestor
// directories of paths, for Component. exists is asked once per candidate.
func Markers(paths []string, exists func(string) bool) map[string]bool {
	out := map[string]bool{}
	seen := map[string]bool{}
	for _, p := range paths {
		for d := path.Dir(p); d != "." && d != "/" && !seen[d]; d = path.Dir(d) {
			seen[d] = true
			for _, m := range markerNames {
				if mp := d + "/" + m; exists(mp) {
					out[mp] = true
				}
			}
		}
	}
	return out
}

// supportingDirs are directory names that hold code around the product
// rather than the product itself: CI, deployment, scripts, examples, docs,
// tests, benchmarks, fixtures and vendored code.
var supportingDirs = map[string]bool{
	"examples": true, "scripts": true, ".github": true, "docs": true, "deploy": true,
	"tests": true, "e2e": true, "benchmarks": true, "fixtures": true, "vendor": true, "third_party": true,
}

// IsSupporting reports whether a component is supporting code rather than
// product: any of its path segments is a supporting directory name, or globs
// (path.Match syntax, from tour.supporting in .heatmap/config.yaml) match the
// component or one of its parent directories. Globs extend the defaults.
// Invalid globs never match; Build validates them.
func IsSupporting(component string, globs []string) bool {
	segs := strings.Split(component, "/")
	for _, s := range segs {
		if supportingDirs[s] {
			return true
		}
	}
	for i := 1; i <= len(segs); i++ {
		prefix := strings.Join(segs[:i], "/")
		for _, g := range globs {
			if ok, _ := path.Match(g, prefix); ok {
				return true
			}
		}
	}
	return false
}

// Link is an import dependency between two components: files in From
// import files in To. Files counts the distinct non-test importing files.
type Link struct {
	From  string `json:"from"`
	To    string `json:"to"`
	Files int    `json:"files"`
}

// ComponentLinks folds the file import graph into component links, sorted
// by Files (strongest first), then From, then To. Test files and imports
// within one component are ignored.
func ComponentLinks(hm *types.Heatmap, markers map[string]bool) []Link {
	count := map[[2]string]int{}
	for p, f := range hm.Files {
		if IsTestFile(p) {
			continue
		}
		from := Component(p, markers)
		seen := map[string]bool{}
		for _, d := range f.Dependencies.Imports {
			to := Component(d.Path, markers)
			if to != from && !seen[to] {
				seen[to] = true
				count[[2]string{from, to}]++
			}
		}
	}
	links := make([]Link, 0, len(count))
	for k, n := range count {
		links = append(links, Link{From: k[0], To: k[1], Files: n})
	}
	sortLinks(links)
	return links
}

func sortLinks(links []Link) {
	sort.Slice(links, func(i, j int) bool {
		a, b := links[i], links[j]
		if a.Files != b.Files {
			return a.Files > b.Files
		}
		if a.From != b.From {
			return a.From < b.From
		}
		return a.To < b.To
	})
}
