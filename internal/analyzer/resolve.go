package analyzer

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// resolveImports maps each tree-sitter file's raw import specs to repo-relative files.
// Specs that do not name a file in the repo (stdlib, third-party, unresolvable) are dropped;
// there is no fuzzy matching.
func (a *Analyzer) resolveImports() {
	r := newResolver(a)
	for path, info := range a.files {
		if info.parsed == nil {
			continue
		}
		seen := map[string]bool{}
		resolved := []string{}
		for _, spec := range info.parsed.Imports {
			for _, target := range r.resolve(path, info.Language, spec) {
				if target != path && !seen[target] {
					seen[target] = true
					resolved = append(resolved, target)
				}
			}
		}
		sort.Strings(resolved)
		info.Imports = resolved
		info.importsResolved = true
		info.parsed = nil
	}
}

type resolver struct {
	a        *Analyzer
	packages map[string][]string // Java/Kotlin package or C# namespace -> files
	symbols  map[string][]string // "Java:" Kotlin or "C#:" qualified declaration -> files
	crates   map[string]string   // Rust crate import name -> crate dir
	tsconfig map[string]*tsPaths // dir -> governing tsconfig (nil when none)
}

func newResolver(a *Analyzer) *resolver {
	r := &resolver{
		a:        a,
		packages: map[string][]string{},
		symbols:  map[string][]string{},
		crates:   map[string]string{},
		tsconfig: map[string]*tsPaths{},
	}
	for path, info := range a.files {
		if info.parsed != nil {
			for _, pkg := range info.parsed.Packages {
				key := info.Language + ":" + pkg
				if info.Language == "Kotlin" {
					key = "Java:" + pkg // Java and Kotlin share packages
				}
				r.packages[key] = append(r.packages[key], path)
			}
			for _, sym := range info.parsed.Symbols {
				key := "C#:" + sym
				if info.Language == "Kotlin" {
					key = "Java:" + sym
				}
				r.symbols[key] = append(r.symbols[key], path)
			}
		}
		if filepath.Base(path) == "Cargo.toml" {
			if name := crateName(filepath.Join(a.repoPath, path)); name != "" {
				r.crates[name] = filepath.Dir(path)
			}
		}
	}
	return r
}

func (r *resolver) exists(p string) bool {
	_, ok := r.a.files[p]
	return ok
}

// clean normalises a slash path and rejects paths that leave the repo.
func clean(p string) (string, bool) {
	c := filepath.Clean(filepath.FromSlash(p))
	if c == ".." || strings.HasPrefix(c, ".."+string(filepath.Separator)) || filepath.IsAbs(c) {
		return "", false
	}
	return c, true
}

func (r *resolver) first(candidates ...string) []string {
	for _, cand := range candidates {
		if c, ok := clean(cand); ok && r.exists(c) {
			return []string{c}
		}
	}
	return nil
}

func (r *resolver) resolve(from, lang, spec string) []string {
	switch lang {
	case "Rust":
		return r.rust(from, spec)
	case "TypeScript", "JavaScript":
		return r.js(from, spec)
	case "Java", "Kotlin":
		return r.jvm(spec)
	case "C#":
		return r.csharp(spec)
	case "C", "C++":
		return r.cInclude(from, spec)
	case "Ruby":
		return r.ruby(from, spec)
	}
	return nil
}

// Rust

// crateName reads the import name of a crate ([lib] name, else [package] name with - as _).
func crateName(cargoToml string) string {
	content, err := os.ReadFile(cargoToml)
	if err != nil {
		return ""
	}
	section, pkgName, libName := "", "", ""
	for _, line := range strings.Split(string(content), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "[") {
			section = strings.Trim(line, "[] ")
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok || strings.TrimSpace(key) != "name" {
			continue
		}
		value = strings.Trim(strings.TrimSpace(value), `"'`)
		switch section {
		case "package":
			pkgName = value
		case "lib":
			libName = value
		}
	}
	if libName != "" {
		return strings.ReplaceAll(libName, "-", "_")
	}
	return strings.ReplaceAll(pkgName, "-", "_")
}

// rustCrateRoot returns the module directory of the crate whose module tree
// contains file, and the crate root file when it is not dir/lib.rs or
// dir/main.rs. Each binary under src/bin/ is its own crate: src/bin/<name>.rs
// is its root with modules beside it, src/bin/<name>/main.rs keeps its
// modules in src/bin/<name>/. tests/, examples/ and benches/ are not resolved.
func (r *resolver) rustCrateRoot(file string) (dir, root string, ok bool) {
	for d := filepath.Dir(file); ; d = filepath.Dir(d) {
		if r.exists(filepath.Join(d, "Cargo.toml")) {
			src := filepath.Join(d, "src")
			rel, err := filepath.Rel(src, file)
			if err != nil || strings.HasPrefix(rel, "..") {
				return "", "", false
			}
			binRel, isBin := strings.CutPrefix(filepath.ToSlash(rel), "bin/")
			if !isBin {
				return src, "", true
			}
			bin := filepath.Join(src, "bin")
			name, _, nested := strings.Cut(binRel, "/")
			if !nested {
				return bin, file, true
			}
			return filepath.Join(bin, name), filepath.Join(bin, name, "main.rs"), true
		}
		if d == "." || d == string(filepath.Separator) {
			return "", "", false
		}
	}
}

// rustModule returns the module path of file relative to its crate module dir.
func rustModule(src, root, file string) []string {
	if file == root {
		return nil
	}
	rel, _ := filepath.Rel(src, file)
	segs := strings.Split(strings.TrimSuffix(filepath.ToSlash(rel), ".rs"), "/")
	last := segs[len(segs)-1]
	if last == "mod" || (len(segs) == 1 && (last == "lib" || last == "main")) {
		segs = segs[:len(segs)-1]
	}
	return segs
}

// rustModuleFile finds the file defining the longest module prefix of segs; later
// segments are items inside it. With no matching module, the crate root is the
// target: root when set, else src/lib.rs or src/main.rs.
func (r *resolver) rustModuleFile(src, root string, segs []string) []string {
	for k := len(segs); k >= 1; k-- {
		base := filepath.Join(append([]string{src}, segs[:k]...)...)
		if hit := r.first(base+".rs", filepath.Join(base, "mod.rs")); hit != nil {
			return hit
		}
	}
	if root != "" {
		return r.first(root)
	}
	return r.first(filepath.Join(src, "lib.rs"), filepath.Join(src, "main.rs"))
}

func (r *resolver) rust(from, spec string) []string {
	if name, ok := strings.CutPrefix(spec, "mod:"); ok {
		// mod.rs, lib.rs, main.rs and crate roots outside src/ declare children next to
		// themselves; any other module file declares them in a directory named after it.
		base := filepath.Dir(from)
		switch filepath.Base(from) {
		case "mod.rs", "lib.rs", "main.rs":
		default:
			if _, root, ok := r.rustCrateRoot(from); ok && from != root {
				base = strings.TrimSuffix(from, ".rs")
			}
		}
		return r.first(filepath.Join(base, name+".rs"), filepath.Join(base, name, "mod.rs"))
	}

	if path, ok := strings.CutPrefix(spec, "path:"); ok {
		// Qualified paths in code resolve only against repo crates: a leading
		// segment can also be a local type or module (Vec::new, config::load).
		segs := strings.Split(strings.TrimPrefix(path, "::"), "::")
		if dir, ok := r.crates[segs[0]]; ok && len(segs) > 1 {
			return r.rustModuleFile(filepath.Join(dir, "src"), "", segs[1:])
		}
		return nil
	}

	segs := strings.Split(strings.TrimPrefix(spec, "::"), "::")
	switch segs[0] {
	case "crate", "self", "super":
		src, root, ok := r.rustCrateRoot(from)
		if !ok {
			return nil
		}
		if segs[0] == "crate" {
			return r.rustModuleFile(src, root, segs[1:])
		}
		mod := rustModule(src, root, from)
		if segs[0] == "self" {
			return r.rustModuleFile(src, root, append(mod, segs[1:]...))
		}
		for len(segs) > 0 && segs[0] == "super" {
			if len(mod) == 0 {
				return nil
			}
			mod, segs = mod[:len(mod)-1], segs[1:]
		}
		return r.rustModuleFile(src, root, append(append([]string{}, mod...), segs...))
	}
	if dir, ok := r.crates[segs[0]]; ok {
		return r.rustModuleFile(filepath.Join(dir, "src"), "", segs[1:])
	}
	return nil
}

// TypeScript / JavaScript

var (
	jsExtensions    = []string{".ts", ".tsx", ".d.ts", ".js", ".jsx", ".mjs", ".cjs", ".mts", ".cts"}
	jsIndexExts     = []string{".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs"}
	jsToTSExtension = map[string][]string{".js": {".ts", ".tsx"}, ".jsx": {".tsx"}, ".mjs": {".mts"}, ".cjs": {".cts"}}
)

// jsFile resolves base the way Node/TypeScript do: the exact file and the TS
// source for a .js-style specifier ("./x.js" -> x.ts), then added extensions,
// then a directory index file. A TypeScript importer (tsImporter) tries the TS
// source before the exact file, as tsc does; a JavaScript importer the reverse.
func (r *resolver) jsFile(base string, tsImporter bool) []string {
	var cands []string
	ext := filepath.Ext(base)
	for _, tsExt := range jsToTSExtension[ext] {
		cands = append(cands, strings.TrimSuffix(base, ext)+tsExt)
	}
	if tsImporter {
		cands = append(cands, base)
	} else {
		cands = append([]string{base}, cands...)
	}
	for _, e := range jsExtensions {
		cands = append(cands, base+e)
	}
	for _, e := range jsIndexExts {
		cands = append(cands, filepath.Join(base, "index"+e))
	}
	for _, cand := range cands {
		c, ok := clean(cand)
		if !ok || !r.exists(c) {
			continue
		}
		if lang := r.a.files[c].Language; lang == "TypeScript" || lang == "JavaScript" {
			return []string{c}
		}
	}
	return nil
}

// isTSFile reports whether path is a TypeScript source file.
func isTSFile(path string) bool {
	switch filepath.Ext(path) {
	case ".ts", ".tsx", ".mts", ".cts":
		return true
	}
	return false
}

func (r *resolver) js(from, spec string) []string {
	ts := isTSFile(from)
	if spec == "." || spec == ".." || strings.HasPrefix(spec, "./") || strings.HasPrefix(spec, "../") {
		return r.jsFile(filepath.Join(filepath.Dir(from), spec), ts)
	}
	if strings.HasPrefix(spec, "/") {
		return nil
	}
	cfg := r.tsconfigFor(filepath.Dir(from))
	if cfg == nil {
		return nil // bare specifier with no path mapping: a package
	}
	if targets, ok := cfg.match(spec); ok {
		for _, t := range targets {
			if hit := r.jsFile(filepath.Join(cfg.pathsBase, t), ts); hit != nil {
				return hit
			}
		}
	}
	if cfg.baseURL != "" {
		return r.jsFile(filepath.Join(cfg.baseURL, spec), ts)
	}
	return nil
}

// tsPaths is the module-resolution part of a tsconfig.json / jsconfig.json.
type tsPaths struct {
	baseURL   string // repo-relative dir, "" when unset
	pathsBase string // dir that "paths" targets are relative to
	paths     map[string][]string
}

// match applies tsconfig "paths": an exact key wins, else the wildcard with the longest prefix.
func (c *tsPaths) match(spec string) ([]string, bool) {
	if t, ok := c.paths[spec]; ok {
		return t, true
	}
	best, bestStar := "", ""
	for pattern := range c.paths {
		prefix, suffix, ok := strings.Cut(pattern, "*")
		if !ok || !strings.HasPrefix(spec, prefix) || !strings.HasSuffix(spec, suffix) || len(spec) < len(prefix)+len(suffix) {
			continue
		}
		if best == "" || len(prefix) > len(strings.Split(best, "*")[0]) {
			best, bestStar = pattern, spec[len(prefix):len(spec)-len(suffix)]
		}
	}
	if best == "" {
		return nil, false
	}
	var out []string
	for _, t := range c.paths[best] {
		out = append(out, strings.Replace(t, "*", bestStar, 1))
	}
	return out, true
}

// tsconfigFor returns the nearest ancestor config that sets baseUrl or paths.
func (r *resolver) tsconfigFor(dir string) *tsPaths {
	if cfg, ok := r.tsconfig[dir]; ok {
		return cfg
	}
	var cfg *tsPaths
	for _, name := range []string{"tsconfig.json", "jsconfig.json"} {
		p := filepath.Join(dir, name)
		if !r.exists(p) {
			continue
		}
		if c := loadTSConfig(r.a.repoPath, p, 0); c != nil && (c.baseURL != "" || len(c.paths) > 0) {
			cfg = c
			break
		}
	}
	if cfg == nil && dir != "." && dir != string(filepath.Separator) {
		cfg = r.tsconfigFor(filepath.Dir(dir))
	}
	r.tsconfig[dir] = cfg
	return cfg
}

// loadTSConfig reads a tsconfig, following relative "extends" a few levels deep.
func loadTSConfig(repo, rel string, depth int) *tsPaths {
	if depth > 3 {
		return nil
	}
	content, err := os.ReadFile(filepath.Join(repo, rel))
	if err != nil {
		return nil
	}
	var raw struct {
		Extends         json.RawMessage `json:"extends"`
		CompilerOptions struct {
			BaseURL *string             `json:"baseUrl"`
			Paths   map[string][]string `json:"paths"`
		} `json:"compilerOptions"`
	}
	if err := json.Unmarshal(stripJSONC(content), &raw); err != nil {
		return nil
	}
	dir := filepath.Dir(rel)
	cfg := &tsPaths{}
	var parent string
	if json.Unmarshal(raw.Extends, &parent) == nil && (strings.HasPrefix(parent, "./") || strings.HasPrefix(parent, "../")) {
		if !strings.HasSuffix(parent, ".json") {
			parent += ".json"
		}
		if p, ok := clean(filepath.Join(dir, parent)); ok {
			if base := loadTSConfig(repo, p, depth+1); base != nil {
				*cfg = *base
			}
		}
	}
	if raw.CompilerOptions.BaseURL != nil {
		cfg.baseURL = filepath.Join(dir, *raw.CompilerOptions.BaseURL)
	}
	if raw.CompilerOptions.Paths != nil {
		cfg.paths = raw.CompilerOptions.Paths
		cfg.pathsBase = dir
		if cfg.baseURL != "" {
			cfg.pathsBase = cfg.baseURL
		}
	}
	return cfg
}

// stripJSONC removes comments and trailing commas, which tsconfig files
// allow. Comments go first so a comment after a trailing comma cannot hide it.
func stripJSONC(in []byte) []byte {
	return stripTrailingCommas(stripJSONComments(in))
}

// stripJSONComments removes // and /* */ comments outside strings.
func stripJSONComments(in []byte) []byte {
	out := make([]byte, 0, len(in))
	inString := false
	for i := 0; i < len(in); i++ {
		ch := in[i]
		if inString {
			out = append(out, ch)
			if ch == '\\' && i+1 < len(in) {
				i++
				out = append(out, in[i])
			} else if ch == '"' {
				inString = false
			}
			continue
		}
		switch {
		case ch == '"':
			inString = true
			out = append(out, ch)
		case ch == '/' && i+1 < len(in) && in[i+1] == '/':
			for i < len(in) && in[i] != '\n' {
				i++
			}
			out = append(out, '\n')
		case ch == '/' && i+1 < len(in) && in[i+1] == '*':
			i += 2
			for i+1 < len(in) && !(in[i] == '*' && in[i+1] == '/') {
				i++
			}
			i++
		default:
			out = append(out, ch)
		}
	}
	return out
}

// stripTrailingCommas removes commas outside strings that are followed only
// by whitespace and a closing brace or bracket.
func stripTrailingCommas(in []byte) []byte {
	out := make([]byte, 0, len(in))
	inString := false
	for i := 0; i < len(in); i++ {
		ch := in[i]
		if inString {
			out = append(out, ch)
			if ch == '\\' && i+1 < len(in) {
				i++
				out = append(out, in[i])
			} else if ch == '"' {
				inString = false
			}
			continue
		}
		if ch == '"' {
			inString = true
		}
		if ch == ',' {
			j := i + 1
			for j < len(in) && strings.ContainsRune(" \t\r\n", rune(in[j])) {
				j++
			}
			if j < len(in) && (in[j] == '}' || in[j] == ']') {
				continue
			}
		}
		out = append(out, ch)
	}
	return out
}

// Java / Kotlin / C#

// classFiles returns the files in package pkg that declare cls: Kotlin and C#
// files by their indexed symbols, Java files by file name (Java convention).
func (r *resolver) classFiles(key, cls string) []string {
	if files := r.symbols[key+"."+cls]; len(files) > 0 {
		return files
	}
	for _, f := range r.packages[key] {
		if strings.TrimSuffix(filepath.Base(f), filepath.Ext(f)) == cls {
			return []string{f}
		}
	}
	return nil
}

func (r *resolver) jvm(spec string) []string {
	if pkg, ok := strings.CutSuffix(spec, ".*"); ok {
		if files := r.packages["Java:"+pkg]; len(files) > 0 {
			return files
		}
		spec = pkg // static wildcard import of a class's members
	}
	segs := strings.Split(spec, ".")
	// The longest package+class split that names a file wins (handles static and nested imports).
	for k := len(segs) - 1; k >= 1; k-- {
		if hit := r.classFiles("Java:"+strings.Join(segs[:k], "."), segs[k]); hit != nil {
			return hit
		}
	}
	return nil
}

func (r *resolver) csharp(spec string) []string {
	if files := r.packages["C#:"+spec]; len(files) > 0 {
		return files
	}
	if i := strings.LastIndex(spec, "."); i > 0 {
		return r.classFiles("C#:"+spec[:i], spec[i+1:]) // using static Ns.Type
	}
	return nil
}

// C / C++ / Ruby

func (r *resolver) cInclude(from, spec string) []string {
	if len(spec) < 2 {
		return nil
	}
	p := spec[1 : len(spec)-1]
	var cands []string
	if spec[0] == '"' {
		cands = append(cands, filepath.Join(filepath.Dir(from), p))
	}
	cands = append(cands, p, filepath.Join("include", p), filepath.Join("src", p))
	return r.first(cands...)
}

func (r *resolver) ruby(from, spec string) []string {
	kind, p, _ := strings.Cut(spec, ":")
	if !strings.HasSuffix(p, ".rb") {
		p += ".rb"
	}
	if kind == "require_relative" {
		return r.first(filepath.Join(filepath.Dir(from), p))
	}
	return r.first(filepath.Join("lib", p), p)
}
