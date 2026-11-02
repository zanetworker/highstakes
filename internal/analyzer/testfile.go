package analyzer

import (
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/zanetworker/highstakes/internal/tour"
)

// HasTestFile reports whether a test for relPath exists by its language's
// convention: a sibling test file (foo_test.go, test_foo.py, tests/test_foo.py,
// foo.test.ts, __tests__/foo.test.ts, FooTest.java, the Maven src/test mirror,
// foo_spec.rb) or, for Rust, a #[cfg(test)] module in the file itself.
// known is false for languages without a convention we check.
func HasTestFile(repoPath, relPath string) (found, known bool) {
	rel := filepath.ToSlash(relPath)
	ext := path.Ext(rel)
	if ext == ".rs" {
		content, err := os.ReadFile(filepath.Join(repoPath, relPath))
		return err == nil && strings.Contains(string(content), "#[cfg(test)]"), true
	}
	cands, known := testFileCandidates(rel)
	for _, c := range cands {
		if _, err := os.Stat(filepath.Join(repoPath, filepath.FromSlash(c))); err == nil {
			return true, true
		}
	}
	return false, known
}

// testFileCandidates lists conventional test paths for a slash-separated path.
func testFileCandidates(rel string) ([]string, bool) {
	dir, base := path.Split(rel)
	ext := path.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	switch ext {
	case ".go":
		return []string{dir + stem + "_test.go"}, true
	case ".py":
		return []string{dir + "test_" + base, dir + stem + "_test.py", dir + "tests/test_" + base}, true
	case ".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs", ".mts", ".cts":
		var c []string
		for _, kind := range []string{".test", ".spec"} {
			c = append(c, dir+stem+kind+ext, dir+"__tests__/"+stem+kind+ext)
		}
		return c, true
	case ".java", ".kt", ".cs":
		c := []string{dir + stem + "Test" + ext, dir + stem + "Tests" + ext}
		if strings.Contains(dir, "src/main/") {
			mirror := strings.Replace(dir, "src/main/", "src/test/", 1)
			c = append(c, mirror+stem+"Test"+ext, mirror+stem+"Tests"+ext)
		}
		return c, true
	case ".rb":
		return []string{dir + stem + "_spec.rb", dir + stem + "_test.rb"}, true
	}
	return nil, false
}

// IsAdjacentTest reports whether test (a slash-separated repo path) is a
// test for src: one of src's conventional test paths, a test file inside the
// module directory named after src (Rust src/proxy/tests/x.rs for
// src/proxy.rs), or a test in a sibling tests/ directory whose name
// contains src's stem.
func IsAdjacentTest(test, src string) bool {
	if test == src || !tour.IsTestFile(test) {
		return false
	}
	cands, _ := testFileCandidates(src)
	for _, c := range cands {
		if c == test {
			return true
		}
	}
	dir, base := path.Split(src)
	stem := strings.TrimSuffix(base, path.Ext(base))
	switch {
	case strings.HasPrefix(test, dir+stem+"/"):
		return true
	case path.Dir(test)+"/" == dir:
		return strings.HasPrefix(path.Base(test), stem+"_") // proxy_tests.rs, proxy_test.rs
	case strings.HasPrefix(test, dir+"tests/"), strings.HasPrefix(test, dir+"__tests__/"):
		return strings.Contains(path.Base(test), stem)
	}
	return false
}
