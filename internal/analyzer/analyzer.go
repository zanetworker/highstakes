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

	"github.com/zanetworker/code-heatmap/internal/types"
)

// Analyzer analyzes a codebase to extract dependency and complexity metrics
type Analyzer struct {
	repoPath string
	files    map[string]*FileInfo
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
func New(repoPath string) *Analyzer {
	return &Analyzer{
		repoPath: repoPath,
		files:    make(map[string]*FileInfo),
	}
}

// Analyze scans the repository and builds dependency graph
func (a *Analyzer) Analyze() error {
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
			skip := strings.HasPrefix(name, ".") ||
				name == "node_modules" || name == "vendor" ||
				name == "__pycache__" || name == ".venv" || name == "venv" ||
				name == ".cache" || name == "dist" || name == "build" ||
				name == ".tox" || name == ".mypy_cache" || name == ".pytest_cache" ||
				name == "target" || name == ".eggs" || name == "*.egg-info"
			if skip {
				return filepath.SkipDir
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
		// Only track local imports (relative paths or same module)
		if strings.HasPrefix(importPath, ".") || !strings.Contains(importPath, "/") {
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

// buildReverseDependencies builds the imported_by graph
func (a *Analyzer) buildReverseDependencies() {
	for path, info := range a.files {
		for _, imp := range info.Imports {
			// Try multiple resolution strategies
			candidates := []string{
				imp + ".py",
				imp + ".go",
				imp + ".ts",
				imp + ".js",
				imp + "/__init__.py",
				filepath.Join(imp, "__init__.py"),
			}

			for _, candidate := range candidates {
				if _, ok := a.files[candidate]; ok {
					a.files[candidate].ImportedBy = append(a.files[candidate].ImportedBy, path)
					break
				}
			}

			// Fallback: substring match for partial imports
			for otherPath := range a.files {
				if otherPath != path && strings.Contains(otherPath, imp) {
					a.files[otherPath].ImportedBy = append(a.files[otherPath].ImportedBy, path)
					break
				}
			}
		}
	}
}

// detectLanguage returns the programming language based on file extension
func detectLanguage(path string) string {
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
		switch n.(type) {
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
			if b, ok := n.(*ast.BinaryExpr); ok {
				if b.Op == token.LAND || b.Op == token.LOR {
					complexity++
				}
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
