package policy

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCodeowners_Covers(t *testing.T) {
	content := `# comment
/.github/workflows/ @org/team
*.md @docs
/crates/net/src/proxy.rs @ana @bo
crates/**/vendor/ @vendor
docs/*.txt @writers
/crates/net/src/unowned.rs
build @builders
`
	tests := []struct {
		path string
		want bool
	}{
		{".github/workflows/ci.yml", true},     // anchored directory
		{".github/workflowsx/ci.yml", false},   // prefix is not a directory match
		{"README.md", true},                    // unanchored glob matches any basename
		{"a/b/notes.md", true},                 // at any depth
		{"crates/net/src/proxy.rs", true},      // anchored file
		{"crates/net/src/proxy.rs.bak", false}, // file rule is exact
		{"crates/net/src/x.rs", false},         // nothing matches
		{"crates/a/b/vendor/lib.rs", true},     // ** spans directories
		{"crates/vendor/lib.rs", true},         // ** matches zero directories
		{"docs/a.txt", true},                   // slash in the middle anchors
		{"x/docs/a.txt", false},                // ... so it is not matched deeper
		{"crates/net/src/unowned.rs", false},   // last match has no owners
		{"src/build/main.go", true},            // bare name matches a directory anywhere
		{"", false},
	}
	c := ParseCodeowners(content)
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			if got := c.Covers(tt.path); got != tt.want {
				t.Errorf("Covers(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
	t.Run("star covers everything", func(t *testing.T) {
		if !ParseCodeowners("* @all\n").Covers("deep/x.go") {
			t.Error("* should cover every file")
		}
	})
	t.Run("later rule wins", func(t *testing.T) {
		c := ParseCodeowners("* @all\n/vendor/\n")
		if c.Covers("vendor/x.go") {
			t.Error("an owner-less later rule clears ownership")
		}
	})
}

func TestLoadCodeowners(t *testing.T) {
	t.Run("github location wins over root", func(t *testing.T) {
		dir := t.TempDir()
		write := func(rel, s string) {
			p := filepath.Join(dir, rel)
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		write(".github/CODEOWNERS", "/a.go @x\n")
		write("CODEOWNERS", "/b.go @y\n")
		c, file := LoadCodeowners(dir)
		if file != filepath.Join(".github", "CODEOWNERS") {
			t.Errorf("file = %q", file)
		}
		if !c.Covers("a.go") || c.Covers("b.go") {
			t.Error("rules not read from .github/CODEOWNERS")
		}
	})
	t.Run("no file covers nothing", func(t *testing.T) {
		c, file := LoadCodeowners(t.TempDir())
		if file != "" || c.Covers("a.go") {
			t.Errorf("file = %q, covers = %v", file, c.Covers("a.go"))
		}
	})
}
