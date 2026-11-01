package tour

import (
	"path"
	"strings"
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
