// Package units finds a repo's runtime units: the deployable entry points
// (binaries, scripts) and the code each links through the import graph. It
// reads manifests and imports, not running systems, so a unit says what a
// binary can execute, not where or whether it runs.
package units

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"sort"
	"strings"

	"github.com/zanetworker/highstakes/internal/tour"
)

// Entry point kinds.
const (
	KindRustBin      = "rust-bin"
	KindGoMain       = "go-main"
	KindNodeBin      = "node-bin"
	KindNodeStart    = "node-start"
	KindPythonScript = "python-script"
	KindPythonMain   = "python-main"
)

// Entry is one detected entry point. Evidence is the file:line declaring it.
type Entry struct {
	Name     string
	Kind     string
	Entry    string
	Evidence string
}

// Detect walks fsys (rooted at the repo) for entry points: Rust bins
// (src/main.rs, src/bin/*.rs, [[bin]]), Go main packages, package.json bin
// entries or start scripts, pyproject [project.scripts] and __main__.py.
// Directories named in skipDirs, hidden directories and test files are
// skipped; unreadable or malformed manifests are skipped too, because one bad
// file should not hide every other unit. Entries are sorted by name; names
// shared by several entries get their directory appended.
func Detect(fsys fs.FS, skipDirs []string) ([]Entry, error) {
	files, err := walk(fsys, skipDirs)
	if err != nil {
		return nil, err
	}

	exists := map[string]bool{}
	for _, f := range files {
		exists[f] = true
	}
	read := func(p string) []byte {
		b, _ := fs.ReadFile(fsys, p) // unreadable: treated as empty
		return b
	}

	var out []Entry
	goMains := map[string]Entry{}
	for _, f := range files {
		if tour.IsTestFile(f) {
			continue
		}
		dir, base := path.Dir(f), path.Base(f)
		switch {
		case base == "Cargo.toml":
			out = append(out, rustBins(dir, read(f), exists, read)...)
		case base == "package.json":
			out = append(out, nodeEntries(dir, read(f), exists)...)
		case base == "pyproject.toml":
			out = append(out, pythonScripts(dir, read(f), exists)...)
		case base == "__main__.py":
			out = append(out, Entry{Name: path.Base(dir), Kind: KindPythonMain, Entry: f, Evidence: f + ":1"})
		case strings.HasSuffix(base, ".go"):
			if _, seen := goMains[dir]; !seen {
				if line := goMainLine(read(f)); line > 0 {
					goMains[dir] = Entry{Name: goName(dir, read, exists), Kind: KindGoMain, Entry: f, Evidence: fmt.Sprintf("%s:%d", f, line)}
				}
			}
		}
	}
	for _, e := range goMains {
		out = append(out, e)
	}
	return uniqueNames(out), nil
}

// walk lists the files under fsys, skipping directories named in skipDirs
// and hidden directories. Unreadable subtrees are skipped.
func walk(fsys fs.FS, skipDirs []string) ([]string, error) {
	skip := map[string]bool{}
	for _, d := range skipDirs {
		skip[d] = true
	}
	var files []string
	err := fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // unreadable subtree: skip it, keep the rest
		}
		if d.IsDir() {
			if p != "." && (skip[d.Name()] || strings.HasPrefix(d.Name(), ".")) {
				return fs.SkipDir
			}
			return nil
		}
		files = append(files, p)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk repo: %w", err)
	}
	return files, nil
}

// uniqueNames sorts entries by name and appends the entry directory to
// names used more than once.
func uniqueNames(es []Entry) []Entry {
	count := map[string]int{}
	for _, e := range es {
		count[e.Name]++
	}
	for i, e := range es {
		if count[e.Name] > 1 {
			es[i].Name = e.Name + " (" + path.Dir(e.Entry) + ")"
		}
	}
	sort.Slice(es, func(i, j int) bool {
		if es[i].Name != es[j].Name {
			return es[i].Name < es[j].Name
		}
		return es[i].Entry < es[j].Entry
	})
	if es == nil {
		es = []Entry{}
	}
	return es
}

func join(dir, p string) string {
	return path.Clean(path.Join(dir, strings.TrimPrefix(p, "./")))
}

var (
	tomlSection = regexp.MustCompile(`^\s*(\[\[?[^\]]+\]\]?)\s*(#.*)?$`)
	tomlString  = regexp.MustCompile(`^\s*("[A-Za-z0-9_.-]+"|'[A-Za-z0-9_.-]+'|[A-Za-z0-9_.-]+)\s*=\s*(?:"([^"]*)"|'([^']*)')`)
	tomlNoBins  = regexp.MustCompile(`^\s*autobins\s*=\s*false\s*(#.*)?$`)
	rustMain    = regexp.MustCompile(`^\s*(pub\s+)?(async\s+)?fn\s+main\s*\(`)
)

// tomlEntry is one string key in a TOML section, with its line.
type tomlEntry struct {
	section, key, value string
	line                int
	header              int // line of the section header
}

// tomlStrings reads `key = "value"` and `key = 'value'` pairs per section. It is a line scanner,
// not a TOML parser: enough for package names, bins and scripts.
func tomlStrings(data []byte) []tomlEntry {
	var out []tomlEntry
	section, header, n := "", 0, 0
	sc := bufio.NewScanner(bytes.NewReader(data))
	for sc.Scan() {
		n++
		line := sc.Text()
		if m := tomlSection.FindStringSubmatch(line); m != nil {
			section, header = m[1], n
			out = append(out, tomlEntry{section: section, line: n, header: n})
			continue
		}
		if m := tomlString.FindStringSubmatch(line); m != nil {
			out = append(out, tomlEntry{section: section, key: strings.Trim(m[1], `"'`), value: m[2] + m[3], line: n, header: header})
		}
	}
	return out
}

// rustBins lists a crate's binaries: [[bin]] tables, plus src/main.rs and
// src/bin/*.rs (Cargo's auto-discovery) unless [package] sets autobins =
// false. A workspace-only manifest has none.
func rustBins(dir string, data []byte, exists map[string]bool, read func(string) []byte) []Entry {
	pkg := ""
	type bin struct {
		name, path string
		header     int
	}
	var bins []bin
	for _, e := range tomlStrings(data) {
		switch {
		case e.section == "[package]" && e.key == "name":
			pkg = e.value
		case e.section == "[[bin]]" && e.key == "":
			bins = append(bins, bin{header: e.line})
		case e.section == "[[bin]]" && len(bins) > 0 && e.header == bins[len(bins)-1].header:
			if e.key == "name" {
				bins[len(bins)-1].name = e.value
			} else if e.key == "path" {
				bins[len(bins)-1].path = e.value
			}
		}
	}
	if pkg == "" {
		return nil
	}
	autobins := !packageDisablesAutobins(data)
	var out []Entry
	explicit := map[string]bool{}
	for _, b := range bins {
		name := b.name
		if name == "" {
			name = pkg
		}
		entry := join(dir, b.path)
		if b.path == "" {
			entry = join(dir, "src/bin/"+name+".rs")
			if !exists[entry] {
				entry = join(dir, "src/bin/"+name+"/main.rs")
			}
			if name == pkg {
				entry = join(dir, "src/main.rs")
			}
		}
		explicit[entry] = true
		if exists[entry] {
			out = append(out, Entry{Name: name, Kind: KindRustBin, Entry: entry, Evidence: fmt.Sprintf("%s:%d", join(dir, "Cargo.toml"), b.header)})
		}
	}
	auto := []struct{ name, entry string }{{pkg, join(dir, "src/main.rs")}}
	prefix := join(dir, "src/bin") + "/"
	for f := range exists {
		if !strings.HasPrefix(f, prefix) || path.Ext(f) != ".rs" {
			continue
		}
		// Cargo auto-discovers src/bin/<name>.rs and src/bin/<name>/main.rs.
		rest := f[len(prefix):]
		if !strings.Contains(rest, "/") {
			auto = append(auto, struct{ name, entry string }{strings.TrimSuffix(rest, ".rs"), f})
		} else if name, ok := strings.CutSuffix(rest, "/main.rs"); ok && !strings.Contains(name, "/") {
			auto = append(auto, struct{ name, entry string }{name, f})
		}
	}
	for _, a := range auto {
		if autobins && exists[a.entry] && !explicit[a.entry] && !tour.IsTestFile(a.entry) {
			out = append(out, Entry{Name: a.name, Kind: KindRustBin, Entry: a.entry, Evidence: fmt.Sprintf("%s:%d", a.entry, lineMatching(read(a.entry), rustMain))})
		}
	}
	return out
}

// packageDisablesAutobins reports whether the manifest's [package] table sets
// autobins = false. tomlStrings only reads string values, so scan for it here.
func packageDisablesAutobins(data []byte) bool {
	section := ""
	for _, line := range strings.Split(string(data), "\n") {
		if m := tomlSection.FindStringSubmatch(line); m != nil {
			section = m[1]
		} else if section == "[package]" && tomlNoBins.MatchString(line) {
			return true
		}
	}
	return false
}

// lineMatching returns the first line matching re, or 1 when none does.
func lineMatching(data []byte, re *regexp.Regexp) int {
	for i, l := range strings.Split(string(data), "\n") {
		if re.MatchString(l) {
			return i + 1
		}
	}
	return 1
}

// goMainLine returns the line of `func main(` in a `package main` file, or 0.
func goMainLine(data []byte) int {
	isMain, n := false, 0
	sc := bufio.NewScanner(bytes.NewReader(data))
	for sc.Scan() {
		n++
		line := strings.TrimSpace(sc.Text())
		if strings.HasPrefix(line, "package ") {
			isMain = strings.TrimSpace(strings.TrimPrefix(line, "package ")) == "main"
			if !isMain {
				return 0
			}
		}
		if isMain && strings.HasPrefix(line, "func main(") {
			return n
		}
	}
	return 0
}

// goName names a Go main package after its directory, or after the module
// when it sits at the module root.
func goName(dir string, read func(string) []byte, exists map[string]bool) string {
	if dir != "." {
		return path.Base(dir)
	}
	if exists["go.mod"] {
		for _, line := range strings.Split(string(read("go.mod")), "\n") {
			if f := strings.Fields(line); len(f) >= 2 && f[0] == "module" {
				return path.Base(f[1])
			}
		}
	}
	return "main"
}

// sourceCandidates maps a built path (dist/x.js) to the sources it likely
// came from (src/x.ts), for manifests that point at build output.
func sourceCandidates(p string) []string {
	out := []string{p}
	stem := strings.TrimSuffix(p, path.Ext(p))
	for _, built := range []string{"dist/", "build/", "lib/", "out/"} {
		if strings.HasPrefix(p, built) {
			stem = "src/" + strings.TrimPrefix(stem, built)
			break
		}
	}
	for _, ext := range []string{".ts", ".tsx", ".mts", ".js", ".mjs"} {
		out = append(out, stem+ext)
	}
	return out
}

// nodeEntries lists package.json bin entries, or the start script's entry
// file when there is no bin.
func nodeEntries(dir string, data []byte, exists map[string]bool) []Entry {
	var pkg struct {
		Name    string            `json:"name"`
		Bin     json.RawMessage   `json:"bin"`
		Scripts map[string]string `json:"scripts"`
	}
	if json.Unmarshal(data, &pkg) != nil {
		return nil
	}
	short := pkg.Name
	if i := strings.LastIndex(short, "/"); i >= 0 {
		short = short[i+1:]
	}
	if short == "" {
		short = path.Base(dir)
	}
	lineOf := func(key string) string {
		for i, l := range strings.Split(string(data), "\n") {
			if strings.Contains(l, `"`+key+`"`) {
				return fmt.Sprintf("%s:%d", join(dir, "package.json"), i+1)
			}
		}
		return join(dir, "package.json") + ":1"
	}
	resolve := func(p string) string {
		for _, c := range sourceCandidates(strings.TrimPrefix(path.Clean(p), "./")) {
			if f := join(dir, c); exists[f] {
				return f
			}
		}
		return ""
	}

	bins := map[string]string{}
	var one string
	if json.Unmarshal(pkg.Bin, &one) == nil && one != "" {
		bins[short] = one
	} else {
		_ = json.Unmarshal(pkg.Bin, &bins) // absent or not an object: no bins
	}
	var out []Entry
	for name, p := range bins {
		if f := resolve(p); f != "" {
			out = append(out, Entry{Name: name, Kind: KindNodeBin, Entry: f, Evidence: lineOf("bin")})
		}
	}
	if len(bins) > 0 {
		return out
	}
	if start, ok := pkg.Scripts["start"]; ok {
		for _, tok := range strings.Fields(start) {
			switch path.Ext(tok) {
			case ".js", ".mjs", ".cjs", ".ts", ".mts", ".tsx":
				if f := resolve(tok); f != "" {
					return []Entry{{Name: short, Kind: KindNodeStart, Entry: f, Evidence: lineOf("start")}}
				}
			}
		}
	}
	return nil
}

// pythonScripts lists [project.scripts] (and Poetry's) entries whose module
// resolves to a file in the project.
func pythonScripts(dir string, data []byte, exists map[string]bool) []Entry {
	var out []Entry
	for _, e := range tomlStrings(data) {
		if e.key == "" || (e.section != "[project.scripts]" && e.section != "[tool.poetry.scripts]") {
			continue
		}
		mod := strings.TrimSpace(strings.SplitN(e.value, ":", 2)[0])
		rel := strings.ReplaceAll(mod, ".", "/")
		for _, c := range []string{rel + ".py", rel + "/__init__.py", "src/" + rel + ".py", "src/" + rel + "/__init__.py"} {
			if f := join(dir, c); exists[f] {
				out = append(out, Entry{Name: e.key, Kind: KindPythonScript, Entry: f, Evidence: fmt.Sprintf("%s:%d", join(dir, "pyproject.toml"), e.line)})
				break
			}
		}
	}
	return out
}
