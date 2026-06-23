package analyzer

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDetectLanguage(t *testing.T) {
	tests := []struct {
		path     string
		expected string
	}{
		{"main.go", "Go"},
		{"app.ts", "TypeScript"},
		{"app.tsx", "TypeScript"},
		{"app.js", "JavaScript"},
		{"app.jsx", "JavaScript"},
		{"app.py", "Python"},
		{"app.java", "Java"},
		{"app.rs", "Rust"},
		{"app.rb", "Ruby"},
		{"app.c", "C"},
		{"app.cpp", "C++"},
		{"README.md", ""},
		{"config.yaml", ""},
		{"image.png", ""},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			lang := detectLanguage(tt.path)
			if lang != tt.expected {
				t.Errorf("detectLanguage(%q) = %q, want %q", tt.path, lang, tt.expected)
			}
		})
	}
}

func TestCountLines(t *testing.T) {
	// Create temp file
	dir := t.TempDir()
	path := filepath.Join(dir, "test.go")

	tests := []struct {
		name     string
		content  string
		expected int
	}{
		{"empty", "", 0},
		{"one line no newline", "hello", 1},
		{"one line with newline", "hello\n", 1},
		{"three lines", "one\ntwo\nthree\n", 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := os.WriteFile(path, []byte(tt.content), 0644); err != nil {
				t.Fatal(err)
			}

			lines, err := countLines(path)
			if err != nil {
				t.Fatal(err)
			}
			if lines != tt.expected {
				t.Errorf("countLines(%q) = %d, want %d", tt.content, lines, tt.expected)
			}
		})
	}
}

func TestCountLines_MissingFile(t *testing.T) {
	_, err := countLines("/nonexistent/file.go")
	if err == nil {
		t.Error("expected error for missing file")
	}
}

func TestAnalyzer_DiscoverFiles(t *testing.T) {
	// Create temp repo structure
	dir := t.TempDir()
	files := map[string]string{
		"main.go":          "package main\n\nfunc main() {}\n",
		"pkg/util.go":      "package pkg\n\nfunc Helper() {}\n",
		"pkg/util_test.go": "package pkg\n",
		"README.md":        "# Project\n",
		".hidden/skip.go":  "package hidden\n",
	}

	for path, content := range files {
		fullPath := filepath.Join(dir, path)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	a := New(dir)
	if err := a.discoverFiles(); err != nil {
		t.Fatal(err)
	}

	allFiles := a.GetAllFiles()

	// Should find Go files but not markdown or hidden dirs
	if _, ok := allFiles["main.go"]; !ok {
		t.Error("should find main.go")
	}
	if _, ok := allFiles["pkg/util.go"]; !ok {
		t.Error("should find pkg/util.go")
	}
	if _, ok := allFiles["README.md"]; ok {
		t.Error("should skip README.md (not a source file)")
	}
	if _, ok := allFiles[".hidden/skip.go"]; ok {
		t.Error("should skip hidden directories")
	}
}

func TestAnalyzer_AnalyzeGoFile(t *testing.T) {
	dir := t.TempDir()
	content := `package main

import "fmt"

// Exported function
func ProcessData(input string) (string, error) {
	if input == "" {
		return "", fmt.Errorf("empty input")
	}
	for i := 0; i < len(input); i++ {
		if input[i] == ' ' {
			continue
		}
	}
	return input, nil
}

// unexported
func helper() {}

type Config struct {
	Name string
}
`
	path := filepath.Join(dir, "main.go")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	info := &FileInfo{
		Path:     "main.go",
		Language: "Go",
	}

	a := New(dir)
	if err := a.analyzeGoFile(path, info); err != nil {
		t.Fatal(err)
	}

	if info.FunctionCount != 2 {
		t.Errorf("expected 2 functions, got %d", info.FunctionCount)
	}
	// ProcessData + Config are exported
	if info.ExportedSymbols < 2 {
		t.Errorf("expected at least 2 exported symbols, got %d", info.ExportedSymbols)
	}
	if info.Cyclomatic < 3 {
		t.Errorf("expected cyclomatic >= 3 (if, for, if), got %d", info.Cyclomatic)
	}
}

func TestCalculateCyclomatic_SimpleFunction(t *testing.T) {
	dir := t.TempDir()
	content := `package test

func Simple() int {
	return 42
}
`
	path := filepath.Join(dir, "simple.go")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	info := &FileInfo{Path: "simple.go", Language: "Go"}
	a := New(dir)
	if err := a.analyzeGoFile(path, info); err != nil {
		t.Fatal(err)
	}

	// A function with no branches has cyclomatic complexity of 1
	if info.Cyclomatic != 1 {
		t.Errorf("simple function should have cyclomatic complexity 1, got %d", info.Cyclomatic)
	}
}

func TestCalculateCyclomatic_ComplexFunction(t *testing.T) {
	dir := t.TempDir()
	content := `package test

func Complex(x int) string {
	if x > 0 {
		if x > 10 {
			return "big"
		}
		return "small"
	} else if x == 0 {
		return "zero"
	}
	for i := 0; i < -x; i++ {
		if i%2 == 0 {
			continue
		}
	}
	return "negative"
}
`
	path := filepath.Join(dir, "complex.go")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	info := &FileInfo{Path: "complex.go", Language: "Go"}
	a := New(dir)
	if err := a.analyzeGoFile(path, info); err != nil {
		t.Fatal(err)
	}

	// Base 1 + if + if + else-if + for + if = 6
	if info.Cyclomatic < 5 {
		t.Errorf("complex function should have high cyclomatic complexity, got %d", info.Cyclomatic)
	}
}

func TestAnalyzer_DependencyCentrality_NoImports(t *testing.T) {
	a := New("")
	a.files = map[string]*FileInfo{
		"a.go": {Path: "a.go", ImportedBy: []string{}},
		"b.go": {Path: "b.go", ImportedBy: []string{}},
	}

	centrality := a.CalculateDependencyCentrality("a.go")

	if centrality.Score != 0 {
		t.Errorf("file with no importers should have centrality 0, got %f", centrality.Score)
	}
}

func TestAnalyzer_DependencyCentrality_HighImports(t *testing.T) {
	a := New("")
	a.files = map[string]*FileInfo{
		"core.go":   {Path: "core.go", ImportedBy: []string{"a.go", "b.go", "c.go", "d.go"}},
		"a.go":      {Path: "a.go", ImportedBy: []string{"b.go"}},
		"b.go":      {Path: "b.go", ImportedBy: []string{}},
		"c.go":      {Path: "c.go", ImportedBy: []string{}},
		"d.go":      {Path: "d.go", ImportedBy: []string{}},
	}

	coreCentrality := a.CalculateDependencyCentrality("core.go")
	leafCentrality := a.CalculateDependencyCentrality("b.go")

	if coreCentrality.Score != 1.0 {
		t.Errorf("most imported file should have centrality 1.0, got %f", coreCentrality.Score)
	}
	if leafCentrality.Score >= coreCentrality.Score {
		t.Errorf("leaf (%f) should be less central than core (%f)", leafCentrality.Score, coreCentrality.Score)
	}
	if coreCentrality.ImportCount != 4 {
		t.Errorf("expected import count 4, got %d", coreCentrality.ImportCount)
	}
}
