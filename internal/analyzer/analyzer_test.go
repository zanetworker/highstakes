package analyzer

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
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
		{"config.yaml", "YAML"},
		{".github/workflows/ci.yml", "YAML"},
		{"pyproject.toml", "TOML"},
		{"tsconfig.json", "JSON"},
		{"Dockerfile", "Dockerfile"},
		{"build/Dockerfile.ubi", "Dockerfile"},
		{"infra/main.tf", "Terraform"},
		{"go.sum", ""},
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
		".github/workflows/ci.yml": "on: push\n",
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

	a := New(dir, nil, nil)
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
	if _, ok := allFiles[".github/workflows/ci.yml"]; !ok {
		t.Error("should find CI workflows under .github")
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

	a := New(dir, nil, nil)
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
	a := New(dir, nil, nil)
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
	a := New(dir, nil, nil)
	if err := a.analyzeGoFile(path, info); err != nil {
		t.Fatal(err)
	}

	// Base 1 + if + if + else-if + for + if = 6
	if info.Cyclomatic < 5 {
		t.Errorf("complex function should have high cyclomatic complexity, got %d", info.Cyclomatic)
	}
}

func TestAnalyzer_DependencyCentrality_NoImports(t *testing.T) {
	a := New("", nil, nil)
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
	a := New("", nil, nil)
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

func writeRepo(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for path, content := range files {
		fullPath := filepath.Join(dir, path)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestAnalyzer_GoModuleImports(t *testing.T) {
	withMod := map[string]string{
		"go.mod":            "module example.com/m\n\ngo 1.22\n",
		"core/core.go":      "package core\n\nimport \"strings\"\n\nfunc Up(s string) string { return strings.ToUpper(s) }\n",
		"core/types.go":     "package core\n\ntype T struct{}\n",
		"core/core_test.go": "package core\n",
		"corelib/x.go":      "package corelib\n",
		"app/app.go":        "package app\n\nimport (\n\t\"fmt\"\n\t\"example.com/m/core\"\n)\n\nfunc Run() { fmt.Println(core.Up(\"x\")) }\n",
		"app/handler.go":    "package app\n\nimport \"example.com/m/core\"\n\nvar _ core.T\n",
		"main.go":           "package main\n\nimport (\n\t\"os\"\n\t\"example.com/m/app\"\n\t\"example.com/m/core\"\n)\n\nfunc main() { app.Run(); _ = core.T{}; os.Exit(0) }\n",
	}
	noMod := map[string]string{
		"core/core.go": "package core\n",
		"main.go":      "package main\n\nimport \"example.com/m/core\"\n\nfunc main() {}\n",
	}

	tests := []struct {
		name           string
		files          map[string]string
		wantImportedBy map[string][]string
		wantImports    map[string][]string
	}{
		{
			name:  "same-module package import credits every non-test file in the package once",
			files: withMod,
			wantImportedBy: map[string][]string{
				"core/core.go":      {"app/app.go", "app/handler.go", "main.go"},
				"core/types.go":     {"app/app.go", "app/handler.go", "main.go"},
				"core/core_test.go": {},
				"corelib/x.go":      {},
				"app/app.go":        {"main.go"},
				"app/handler.go":    {"main.go"},
				"main.go":           {},
			},
			wantImports: map[string][]string{
				"core/core.go": {},
				"app/app.go":   {"example.com/m/core"},
				"main.go":      {"example.com/m/app", "example.com/m/core"},
			},
		},
		{
			name:  "no go.mod tracks no Go imports and does not fail",
			files: noMod,
			wantImportedBy: map[string][]string{
				"core/core.go": {},
				"main.go":      {},
			},
			wantImports: map[string][]string{
				"main.go": {},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := writeRepo(t, tt.files)
			a := New(dir, nil, nil)
			if err := a.Analyze(); err != nil {
				t.Fatal(err)
			}
			for path, want := range tt.wantImportedBy {
				info, ok := a.GetFileInfo(path)
				if !ok {
					t.Fatalf("missing file %s", path)
				}
				got := append([]string{}, info.ImportedBy...)
				sort.Strings(got)
				if !reflect.DeepEqual(got, want) {
					t.Errorf("%s ImportedBy = %v, want %v", path, got, want)
				}
				if c := a.CalculateDependencyCentrality(path).ImportCount; c != len(want) {
					t.Errorf("%s ImportCount = %d, want %d", path, c, len(want))
				}
			}
			for path, want := range tt.wantImports {
				info, _ := a.GetFileInfo(path)
				got := append([]string{}, info.Imports...)
				sort.Strings(got)
				if !reflect.DeepEqual(got, want) {
					t.Errorf("%s Imports = %v, want %v", path, got, want)
				}
			}
		})
	}
}
