package analyzer

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/zanetworker/highstakes/internal/types"
)

// Analyzer analyzes a codebase to extract dependency and complexity metrics
type Analyzer struct {
	repoPath    string
	modulePath  string // Go module path from go.mod, empty if none
	files       map[string]*FileInfo
	excludeDirs map[string]bool
	excludeGlob []string
}

// FileInfo holds analysis results for a single file
type FileInfo struct {
	Path            string
	Language        string
	Lines           int
	Bytes           int
	ImportedBy      []string
	Imports         []string
	ExportedSymbols int
	Cyclomatic      int
	Cognitive       int
	FunctionCount   int
}

// New creates a new analyzer for the given repository path
func New(repoPath string, excludeDirs []string, excludePatterns []string) *Analyzer {
	dirs := make(map[string]bool)
	for _, d := range excludeDirs {
		dirs[d] = true
	}
	return &Analyzer{
		repoPath:    repoPath,
		files:       make(map[string]*FileInfo),
		excludeDirs: dirs,
		excludeGlob: excludePatterns,
	}
}

// Analyze scans the repository and builds dependency graph
func (a *Analyzer) Analyze() error {
	a.modulePath = readModulePath(a.repoPath)

	// First pass: discover all files
	if err := a.discoverFiles(); err != nil {
		return fmt.Errorf("discover files: %w", err)
	}

	// Second pass: analyze each file
	for path := range a.files {
		if err := a.analyzeFile(path); err != nil {
			// Log error but continue
			fmt.Fprintf(os.Stderr, "Warning: failed to analyze %s: %v\n", path, err)
		}
	}

	// Third pass: build reverse dependencies (imported_by)
	a.buildReverseDependencies()

	return nil
}

// GetFileInfo returns analysis results for a file
func (a *Analyzer) GetFileInfo(path string) (*FileInfo, bool) {
	info, ok := a.files[path]
	return info, ok
}

// GetAllFiles returns all analyzed files
func (a *Analyzer) GetAllFiles() map[string]*FileInfo {
	return a.files
}

// CalculateDependencyCentrality computes centrality score for a file
func (a *Analyzer) CalculateDependencyCentrality(path string) types.DependencyCentrality {
	info, ok := a.files[path]
	if !ok {
		return types.DependencyCentrality{}
	}

	// Find max import count across all files for normalization
	maxImports := 0
	for _, f := range a.files {
		if len(f.ImportedBy) > maxImports {
			maxImports = len(f.ImportedBy)
		}
	}

	// Normalize to 0-1
	score := 0.0
	if maxImports > 0 {
		score = float64(len(info.ImportedBy)) / float64(maxImports)
	}

	return types.DependencyCentrality{
		Score:           score,
		ImportCount:     len(info.ImportedBy),
		ExportedSymbols: info.ExportedSymbols,
	}
}

// discoverFiles walks the repo and discovers all source files
func (a *Analyzer) discoverFiles() error {
	return filepath.WalkDir(a.repoPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		// Skip hidden directories and common excludes
		if d.IsDir() {
			name := d.Name()
			if strings.HasPrefix(name, ".") && name != ".github" {
				return filepath.SkipDir
			}
			if a.excludeDirs[name] {
				return filepath.SkipDir
			}
			for _, pat := range a.excludeGlob {
				if matched, _ := filepath.Match(pat, name); matched {
					return filepath.SkipDir
				}
			}
			return nil
		}

		// Only analyze source files
		lang := detectLanguage(path)
		if lang == "" {
			return nil
		}

		// Get relative path
		relPath, err := filepath.Rel(a.repoPath, path)
		if err != nil {
			return err
		}

		// Get file size
		stat, err := os.Stat(path)
		if err != nil {
			return err
		}

		// Count lines
		lines, err := countLines(path)
		if err != nil {
			lines = 0 // Best effort
		}

		a.files[relPath] = &FileInfo{
			Path:     relPath,
			Language: lang,
			Lines:    lines,
			Bytes:    int(stat.Size()),
			Imports:  []string{},
			ImportedBy: []string{},
		}

		return nil
	})
}

// analyzeFile performs deep analysis on a single file
func (a *Analyzer) analyzeFile(relPath string) error {
	info := a.files[relPath]
	absPath := filepath.Join(a.repoPath, relPath)

	switch info.Language {
	case "Go":
		return a.analyzeGoFile(absPath, info)
	case "TypeScript", "JavaScript":
		return a.analyzeJSFile(absPath, info)
	case "Python":
		return a.analyzePythonFile(absPath, info)
	case "Rust", "C", "C++", "Java", "Ruby", "C#":
		return a.analyzeGenericFile(absPath, info)
	default:
		return nil
	}
}

// analyzeGoFile analyzes a Go source file
func (a *Analyzer) analyzeGoFile(absPath string, info *FileInfo) error {
	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, absPath, nil, parser.ParseComments)
	if err != nil {
		return err
	}

	// Extract imports
	for _, imp := range node.Imports {
		importPath := strings.Trim(imp.Path.Value, `"`)
		// Only track imports of packages in the same module
		if _, ok := a.goImportDir(importPath); ok {
			info.Imports = append(info.Imports, importPath)
		}
	}

	// Count exported symbols and functions
	info.FunctionCount = 0
	info.ExportedSymbols = 0

	ast.Inspect(node, func(n ast.Node) bool {
		switch decl := n.(type) {
		case *ast.FuncDecl:
			info.FunctionCount++
			if decl.Name.IsExported() {
				info.ExportedSymbols++
			}
			// Calculate cyclomatic complexity for this function
			info.Cyclomatic += calculateCyclomatic(decl)
			info.Cognitive += calculateCognitive(decl)

		case *ast.GenDecl:
			// Count exported types, vars, consts
			if decl.Tok == token.TYPE || decl.Tok == token.VAR || decl.Tok == token.CONST {
				for _, spec := range decl.Specs {
					switch s := spec.(type) {
					case *ast.TypeSpec:
						if s.Name.IsExported() {
							info.ExportedSymbols++
						}
					case *ast.ValueSpec:
						for _, name := range s.Names {
							if name.IsExported() {
								info.ExportedSymbols++
							}
						}
					}
				}
			}
		}
		return true
	})

	return nil
}

// analyzeJSFile analyzes JavaScript/TypeScript files (basic implementation)
func (a *Analyzer) analyzeJSFile(absPath string, info *FileInfo) error {
	content, err := os.ReadFile(absPath)
	if err != nil {
		return err
	}

	text := string(content)

	// Simple regex-based import extraction
	// TODO: Use proper parser for production
	// Pattern: import ... from "..."  or  require("...")

	// Count exports (rough heuristic)
	info.ExportedSymbols = strings.Count(text, "export ")

	// Count functions (rough heuristic)
	info.FunctionCount = strings.Count(text, "function ") + strings.Count(text, "=>")

	// Basic cyclomatic complexity (count branches)
	info.Cyclomatic = strings.Count(text, "if ") +
		strings.Count(text, "else if ") +
		strings.Count(text, "for ") +
		strings.Count(text, "while ") +
		strings.Count(text, "case ") +
		strings.Count(text, "catch ")

	return nil
}

// analyzePythonFile analyzes Python files
func (a *Analyzer) analyzePythonFile(absPath string, info *FileInfo) error {
	content, err := os.ReadFile(absPath)
	if err != nil {
		return err
	}

	text := string(content)
	lines := strings.Split(text, "\n")

	// Extract imports and resolve to local file paths
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		var modulePath string
		if strings.HasPrefix(trimmed, "from ") {
			// "from foo.bar import baz" -> "foo.bar"
			parts := strings.Fields(trimmed)
			if len(parts) >= 2 {
				modulePath = parts[1]
			}
		} else if strings.HasPrefix(trimmed, "import ") {
			parts := strings.Fields(trimmed)
			if len(parts) >= 2 {
				modulePath = strings.TrimSuffix(parts[1], ",")
			}
		}

		if modulePath != "" && !strings.HasPrefix(modulePath, "_") {
			// Convert dotted module path to file path for local resolution
			filePath := strings.ReplaceAll(modulePath, ".", "/")
			info.Imports = append(info.Imports, filePath)
		}
	}

	// Count functions (exported = no leading underscore)
	info.FunctionCount = 0
	info.ExportedSymbols = 0
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "def ") {
			info.FunctionCount++
			// Python: functions starting with _ are private
			name := strings.TrimPrefix(trimmed, "def ")
			if !strings.HasPrefix(name, "_") {
				info.ExportedSymbols++
			}
		}
		if strings.HasPrefix(trimmed, "class ") {
			info.ExportedSymbols++
		}
	}

	// Cyclomatic complexity
	info.Cyclomatic = 1 // Base
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "if ") || strings.HasPrefix(trimmed, "elif ") {
			info.Cyclomatic++
		}
		if strings.HasPrefix(trimmed, "for ") || strings.HasPrefix(trimmed, "while ") {
			info.Cyclomatic++
		}
		if strings.HasPrefix(trimmed, "except") {
			info.Cyclomatic++
		}
		// Boolean operators add branches
		info.Cyclomatic += strings.Count(trimmed, " and ")
		info.Cyclomatic += strings.Count(trimmed, " or ")
	}

	// Cognitive complexity: nesting depth
	info.Cognitive = 0
	nestLevel := 0
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		// Approximate nesting by indent level
		indent := len(line) - len(strings.TrimLeft(line, " \t"))
		currentLevel := indent / 4
		if currentLevel > nestLevel {
			nestLevel = currentLevel
		}
		if strings.HasPrefix(trimmed, "if ") || strings.HasPrefix(trimmed, "for ") ||
			strings.HasPrefix(trimmed, "while ") || strings.HasPrefix(trimmed, "try:") {
			info.Cognitive += 1 + currentLevel
		}
	}

	return nil
}

// analyzeGenericFile handles languages without AST parsing using line-based heuristics
func (a *Analyzer) analyzeGenericFile(absPath string, info *FileInfo) error {
	content, err := os.ReadFile(absPath)
	if err != nil {
		return err
	}

	text := string(content)
	lines := strings.Split(text, "\n")

	// Count functions/methods (language-agnostic patterns)
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "fn ") || strings.HasPrefix(trimmed, "pub fn ") || // Rust
			strings.HasPrefix(trimmed, "func ") || // Go
			strings.HasPrefix(trimmed, "def ") || // Python/Ruby
			strings.HasPrefix(trimmed, "public ") || strings.HasPrefix(trimmed, "private ") { // Java/C#
			info.FunctionCount++
		}
	}

	// Exported symbols (pub in Rust, public in Java/C#)
	info.ExportedSymbols = strings.Count(text, "pub fn ") + strings.Count(text, "pub struct ") +
		strings.Count(text, "pub enum ") + strings.Count(text, "pub trait ") +
		strings.Count(text, "public ")

	// Cyclomatic complexity
	info.Cyclomatic = 1
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "if ") || strings.HasPrefix(trimmed, "} else if ") ||
			strings.HasPrefix(trimmed, "else if ") {
			info.Cyclomatic++
		}
		if strings.HasPrefix(trimmed, "for ") || strings.HasPrefix(trimmed, "while ") ||
			strings.HasPrefix(trimmed, "loop ") {
			info.Cyclomatic++
		}
		if strings.HasPrefix(trimmed, "match ") || strings.Contains(trimmed, "=> ") {
			info.Cyclomatic++
		}
		if strings.HasPrefix(trimmed, "catch") || strings.Contains(trimmed, "Err(") {
			info.Cyclomatic++
		}
	}

	return nil
}

// readModulePath returns the module path declared in repoPath/go.mod, or "".
func readModulePath(repoPath string) string {
	content, err := os.ReadFile(filepath.Join(repoPath, "go.mod"))
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(content), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[0] == "module" {
			return strings.Trim(fields[1], `"`)
		}
	}
	return ""
}

// goImportDir maps a same-module Go import path to its repo-relative directory.
func (a *Analyzer) goImportDir(importPath string) (string, bool) {
	if a.modulePath == "" {
		return "", false
	}
	if importPath == a.modulePath {
		return ".", true
	}
	if rest, ok := strings.CutPrefix(importPath, a.modulePath+"/"); ok {
		return filepath.FromSlash(rest), true
	}
	return "", false
}

// resolveImport returns the repo files an import refers to.
func (a *Analyzer) resolveImport(importer *FileInfo, imp string) []string {
	if importer.Language == "Go" {
		dir, ok := a.goImportDir(imp)
		if !ok {
			return nil
		}
		// A Go import names a package: every non-test file in that directory.
		var targets []string
		for p, f := range a.files {
			if f.Language == "Go" && filepath.Dir(p) == dir && !strings.HasSuffix(p, "_test.go") {
				targets = append(targets, p)
			}
		}
		return targets
	}

	candidates := []string{
		imp + ".py",
		imp + ".ts",
		imp + ".js",
		filepath.Join(imp, "__init__.py"),
	}
	for _, candidate := range candidates {
		if _, ok := a.files[candidate]; ok {
			return []string{candidate}
		}
	}
	// Fallback: match on a whole path-segment suffix (e.g. src/foo/bar.py for foo/bar)
	for _, candidate := range candidates {
		for p := range a.files {
			if strings.HasSuffix(p, "/"+candidate) {
				return []string{p}
			}
		}
	}
	return nil
}

// buildReverseDependencies builds the imported_by graph
func (a *Analyzer) buildReverseDependencies() {
	seen := make(map[[2]string]bool)
	for path, info := range a.files {
		for _, imp := range info.Imports {
			for _, target := range a.resolveImport(info, imp) {
				key := [2]string{target, path}
				if target == path || seen[key] {
					continue
				}
				seen[key] = true
				a.files[target].ImportedBy = append(a.files[target].ImportedBy, path)
			}
		}
	}
}

// detectLanguage returns the programming language based on file extension
func detectLanguage(path string) string {
	if base := filepath.Base(path); base == "Dockerfile" || strings.HasPrefix(base, "Dockerfile.") {
		return "Dockerfile"
	}
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".go":
		return "Go"
	case ".ts", ".tsx":
		return "TypeScript"
	case ".js", ".jsx":
		return "JavaScript"
	case ".py":
		return "Python"
	case ".java":
		return "Java"
	case ".rb":
		return "Ruby"
	case ".rs":
		return "Rust"
	case ".cpp", ".cc", ".cxx":
		return "C++"
	case ".c":
		return "C"
	case ".cs":
		return "C#"
	case ".yaml", ".yml":
		return "YAML"
	case ".toml":
		return "TOML"
	case ".json":
		return "JSON"
	case ".tf":
		return "Terraform"
	default:
		return ""
	}
}

// countLines counts lines in a file
func countLines(path string) (int, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}

	lines := strings.Count(string(content), "\n")
	if len(content) > 0 && !strings.HasSuffix(string(content), "\n") {
		lines++ // Count last line if no trailing newline
	}

	return lines, nil
}

// calculateCyclomatic computes cyclomatic complexity for a Go function
func calculateCyclomatic(fn *ast.FuncDecl) int {
	complexity := 1 // Base complexity

	ast.Inspect(fn, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.IfStmt:
			complexity++
		case *ast.ForStmt, *ast.RangeStmt:
			complexity++
		case *ast.CaseClause:
			complexity++
		case *ast.CommClause:
			complexity++
		case *ast.BinaryExpr:
			// Count && and || as additional branches
			if n.Op == token.LAND || n.Op == token.LOR {
				complexity++
			}
		}
		return true
	})

	return complexity
}

// calculateCognitive computes cognitive complexity for a Go function
// Simplified version - production would need full cognitive complexity rules
func calculateCognitive(fn *ast.FuncDecl) int {
	cognitive := 0
	nestingLevel := 0

	ast.Inspect(fn, func(n ast.Node) bool {
		switch n.(type) {
		case *ast.IfStmt, *ast.ForStmt, *ast.RangeStmt, *ast.SwitchStmt:
			cognitive += 1 + nestingLevel
			nestingLevel++
		case *ast.FuncLit:
			// Nested functions add cognitive load
			cognitive += 1 + nestingLevel
		}
		return true
	})

	return cognitive
}
