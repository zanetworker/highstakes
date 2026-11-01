package analyzer

import (
	"os"
	"path/filepath"
	"testing"
)

func TestHasTestFile(t *testing.T) {
	tests := []struct {
		name      string
		files     map[string]string // relPath -> content
		path      string
		wantFound bool
		wantKnown bool
	}{
		{"go sibling", map[string]string{"pkg/a.go": "", "pkg/a_test.go": ""}, "pkg/a.go", true, true},
		{"go no sibling", map[string]string{"pkg/a.go": "", "pkg/b_test.go": ""}, "pkg/a.go", false, true},
		{"python test_ prefix", map[string]string{"x/foo.py": "", "x/test_foo.py": ""}, "x/foo.py", true, true},
		{"python _test suffix", map[string]string{"x/foo.py": "", "x/foo_test.py": ""}, "x/foo.py", true, true},
		{"python tests dir", map[string]string{"x/foo.py": "", "x/tests/test_foo.py": ""}, "x/foo.py", true, true},
		{"ts dot test", map[string]string{"src/foo.ts": "", "src/foo.test.ts": ""}, "src/foo.ts", true, true},
		{"tsx spec", map[string]string{"src/Foo.tsx": "", "src/Foo.spec.tsx": ""}, "src/Foo.tsx", true, true},
		{"js __tests__", map[string]string{"src/foo.js": "", "src/__tests__/foo.test.js": ""}, "src/foo.js", true, true},
		{"java maven mirror", map[string]string{"src/main/java/a/Foo.java": "", "src/test/java/a/FooTest.java": ""}, "src/main/java/a/Foo.java", true, true},
		{"kotlin sibling Tests", map[string]string{"a/Foo.kt": "", "a/FooTests.kt": ""}, "a/Foo.kt", true, true},
		{"ruby spec", map[string]string{"lib/foo.rb": "", "lib/foo_spec.rb": ""}, "lib/foo.rb", true, true},
		{"rust cfg(test) module", map[string]string{"src/a.rs": "fn x() {}\n#[cfg(test)]\nmod tests {}\n"}, "src/a.rs", true, true},
		{"rust without tests", map[string]string{"src/a.rs": "fn x() {}\n"}, "src/a.rs", false, true},
		{"rust cfg(not(test)) is not a test module", map[string]string{"src/a.rs": "#[cfg(not(test))]\n"}, "src/a.rs", false, true},
		{"c has no convention", map[string]string{"a.c": ""}, "a.c", false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			for p, c := range tt.files {
				abs := filepath.Join(dir, p)
				if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(abs, []byte(c), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			found, known := HasTestFile(dir, tt.path)
			if found != tt.wantFound || known != tt.wantKnown {
				t.Errorf("HasTestFile(%s) = (%v, %v), want (%v, %v)", tt.path, found, known, tt.wantFound, tt.wantKnown)
			}
		})
	}
}

func TestHasTestFile_MissingRustFile(t *testing.T) {
	found, known := HasTestFile(t.TempDir(), "src/gone.rs")
	if found || !known {
		t.Errorf("missing rust file = (%v, %v), want (false, true)", found, known)
	}
}

func TestAnalyze_SetsHasTestFile(t *testing.T) {
	dir := t.TempDir()
	for p, c := range map[string]string{
		"a.go":      "package x\n",
		"a_test.go": "package x\n",
		"b.go":      "package x\n",
		"m.c":       "int main(){return 0;}\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, p), []byte(c), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	a := New(dir, nil, nil)
	if err := a.Analyze(); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		path string
		want *bool
	}{
		{"a.go", boolPtr(true)},
		{"b.go", boolPtr(false)},
		{"m.c", nil},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got := a.files[tt.path].HasTestFile
			if (got == nil) != (tt.want == nil) || (got != nil && *got != *tt.want) {
				t.Errorf("HasTestFile = %v, want %v", ptrStr(got), ptrStr(tt.want))
			}
		})
	}
}

func boolPtr(b bool) *bool { return &b }

func ptrStr(b *bool) string {
	if b == nil {
		return "nil"
	}
	if *b {
		return "true"
	}
	return "false"
}
