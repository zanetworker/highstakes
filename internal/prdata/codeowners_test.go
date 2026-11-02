package prdata

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCodeOwners_Covers(t *testing.T) {
	co := ParseCodeOwners(`# comment
*.md            @docs
/build/         @infra
docs/           @docs
/src/api/**/*.go @api
Makefile        @infra
/vendor/
/exact/file.go  @team # trailing comment
`)
	tests := []struct {
		path string
		want bool
	}{
		{"README.md", true},
		{"pkg/notes.md", true},     // unanchored glob matches at any depth
		{"build/x.sh", true},       // anchored directory
		{"sub/build/x.sh", false},  // anchored: only at the root
		{"docs/a.txt", true},       // unanchored directory at the root
		{"sub/docs/a.txt", true},   // and at any depth
		{"src/api/v1/h.go", true},  // ** spans directories
		{"src/api/h.go", true},     // ** matches zero directories
		{"src/api/v1/h.ts", false}, // extension must match
		{"tools/Makefile", true},   // bare name matches at any depth
		{"vendor/lib.go", false},   // pattern without owners un-owns
		{"exact/file.go", true},
		{"exact/file.go.bak", false},
		{"main.go", false},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			if got := co.Covers(tt.path); got != tt.want {
				t.Errorf("Covers(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

func TestCodeOwners_LastMatchWins(t *testing.T) {
	co := ParseCodeOwners("* @all\n/gen/\n")
	if !co.Covers("a.go") || co.Covers("gen/x.go") {
		t.Errorf("want a.go covered and gen/x.go un-owned by the later rule")
	}
	if ParseCodeOwners("").Covers("a.go") {
		t.Error("empty CODEOWNERS covers nothing")
	}
}

func TestLoadCodeOwners(t *testing.T) {
	dir := t.TempDir()
	co, path, err := LoadCodeOwners(dir)
	if err != nil || co != nil || path != "" {
		t.Fatalf("no CODEOWNERS: got (%v, %q, %v), want (nil, \"\", nil)", co, path, err)
	}
	if err := os.MkdirAll(filepath.Join(dir, ".github"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".github", "CODEOWNERS"), []byte("*.go @g\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	co, path, err = LoadCodeOwners(dir)
	if err != nil || co == nil || path != ".github/CODEOWNERS" || !co.Covers("x.go") {
		t.Errorf("got (%v, %q, %v), want .github/CODEOWNERS covering x.go", co, path, err)
	}
}
