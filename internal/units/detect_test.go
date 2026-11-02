package units

import (
	"reflect"
	"testing"
	"testing/fstest"
)

func file(s string) *fstest.MapFile { return &fstest.MapFile{Data: []byte(s)} }

func TestDetect(t *testing.T) {
	tests := []struct {
		name string
		fs   fstest.MapFS
		skip []string
		want []Entry
	}{
		{
			name: "rust implicit main and explicit bin",
			fs: fstest.MapFS{
				"crates/sbx/Cargo.toml":     file("[package]\nname = \"sbx\"\n"),
				"crates/sbx/src/main.rs":    file("//! sandbox\n\nfn main() {}\n"),
				"crates/sup/Cargo.toml":     file("[package]\nname = \"sup-lib\"\n\n[[bin]]\nname = \"sup\"\npath = \"src/bin/run.rs\"\n\n[dependencies]\nname = \"x\"\n"),
				"crates/sup/src/bin/run.rs": file("fn main() {}\n"),
				"crates/lib/Cargo.toml":     file("[package]\nname = \"lib\"\n"),
				"crates/lib/src/lib.rs":     file("pub fn f() {}\n"),
				"Cargo.toml":                file("[workspace]\nmembers = [\"crates/*\"]\n"),
			},
			want: []Entry{
				{Name: "sbx", Kind: KindRustBin, Entry: "crates/sbx/src/main.rs", Evidence: "crates/sbx/src/main.rs:3"},
				{Name: "sup", Kind: KindRustBin, Entry: "crates/sup/src/bin/run.rs", Evidence: "crates/sup/Cargo.toml:4"},
			},
		},
		{
			name: "rust src/bin files are bins",
			fs: fstest.MapFS{
				"Cargo.toml":       file("[package]\nname = \"tool\"\n"),
				"src/bin/extra.rs": file("fn main() {}\n"),
			},
			want: []Entry{{Name: "extra", Kind: KindRustBin, Entry: "src/bin/extra.rs", Evidence: "src/bin/extra.rs:1"}},
		},
		{
			name: "rust src/bin/<name>/main.rs directories are bins",
			fs: fstest.MapFS{
				"Cargo.toml":                  file("[package]\nname = \"tool\"\n\n[[bin]]\nname = \"named\"\n"),
				"src/bin/vm-init/main.rs":     file("mod util;\n\nfn main() {}\n"),
				"src/bin/vm-init/util.rs":     file("pub fn f() {}\n"),
				"src/bin/named/main.rs":       file("fn main() {}\n"),
				"src/bin/deep/nested/main.rs": file("fn main() {}\n"),
			},
			want: []Entry{
				{Name: "named", Kind: KindRustBin, Entry: "src/bin/named/main.rs", Evidence: "Cargo.toml:4"},
				{Name: "vm-init", Kind: KindRustBin, Entry: "src/bin/vm-init/main.rs", Evidence: "src/bin/vm-init/main.rs:3"},
			},
		},
		{
			name: "rust autobins = false keeps only explicit bins",
			fs: fstest.MapFS{
				"Cargo.toml":       file("[package]\nname = \"tool\"\nautobins = false # explicit only\n\n[[bin]]\nname = \"cli\"\npath = \"src/cli.rs\"\n"),
				"src/main.rs":      file("fn main() {}\n"),
				"src/bin/extra.rs": file("fn main() {}\n"),
				"src/cli.rs":       file("fn main() {}\n"),
			},
			want: []Entry{{Name: "cli", Kind: KindRustBin, Entry: "src/cli.rs", Evidence: "Cargo.toml:5"}},
		},
		{
			name: "rust autobins = false with no explicit bins has none",
			fs: fstest.MapFS{
				"Cargo.toml":  file("[package]\nname = \"tool\"\nautobins=false\n"),
				"src/main.rs": file("fn main() {}\n"),
			},
			want: []Entry{},
		},
		{
			name: "rust autobins = true still discovers",
			fs: fstest.MapFS{
				"Cargo.toml":  file("[package]\nname = \"tool\"\nautobins = true\n"),
				"src/main.rs": file("fn main() {}\n"),
			},
			want: []Entry{{Name: "tool", Kind: KindRustBin, Entry: "src/main.rs", Evidence: "src/main.rs:1"}},
		},
		{
			name: "go main packages, tests and libraries ignored",
			fs: fstest.MapFS{
				"go.mod":                file("module example.com/m\n"),
				"cmd/server/main.go":    file("// Command server.\npackage main\n\nfunc main() {}\n"),
				"cmd/server/flags.go":   file("package main\n"),
				"cmd/server/x_test.go":  file("package main\nfunc main() {}\n"),
				"internal/core/core.go": file("package core\nfunc main() {}\n"),
				"main.go":               file("package main\n\nfunc main() {\n}\n"),
			},
			want: []Entry{
				{Name: "m", Kind: KindGoMain, Entry: "main.go", Evidence: "main.go:3"},
				{Name: "server", Kind: KindGoMain, Entry: "cmd/server/main.go", Evidence: "cmd/server/main.go:4"},
			},
		},
		{
			name: "package.json bin string, bin object, start script with dist mapped to src",
			fs: fstest.MapFS{
				"packages/cli/package.json":   file("{\n  \"name\": \"@acme/cli\",\n  \"bin\": \"./bin/cli.js\"\n}\n"),
				"packages/cli/bin/cli.js":     file("#!/usr/bin/env node\n"),
				"packages/multi/package.json": file("{\n  \"name\": \"multi\",\n  \"bin\": {\"a\": \"a.js\", \"b\": \"missing.js\"}\n}\n"),
				"packages/multi/a.js":         file(""),
				"apps/web/package.json":       file("{\n  \"name\": \"web\",\n  \"scripts\": {\n    \"start\": \"node dist/server.js --port 3000\"\n  }\n}\n"),
				"apps/web/src/server.ts":      file(""),
				"libs/ui/package.json":        file("{\"name\": \"ui\", \"main\": \"index.js\"}"),
			},
			want: []Entry{
				{Name: "a", Kind: KindNodeBin, Entry: "packages/multi/a.js", Evidence: "packages/multi/package.json:3"},
				{Name: "cli", Kind: KindNodeBin, Entry: "packages/cli/bin/cli.js", Evidence: "packages/cli/package.json:3"},
				{Name: "web", Kind: KindNodeStart, Entry: "apps/web/src/server.ts", Evidence: "apps/web/package.json:4"},
			},
		},
		{
			name: "python scripts and __main__",
			fs: fstest.MapFS{
				"pyproject.toml":            file("[project]\nname = \"tool\"\n\n[project.scripts]\ntool = \"tool.cli:main\"\nother = \"tool.gone:main\"\n"),
				"src/tool/cli.py":           file("def main(): pass\n"),
				"src/tool/__init__.py":      file(""),
				"svc/__main__.py":           file("print(1)\n"),
				"tests/fixture/__main__.py": file(""),
			},
			want: []Entry{
				{Name: "svc", Kind: KindPythonMain, Entry: "svc/__main__.py", Evidence: "svc/__main__.py:1"},
				{Name: "tool", Kind: KindPythonScript, Entry: "src/tool/cli.py", Evidence: "pyproject.toml:5"},
			},
		},
		{
			name: "skipped dirs and duplicate names get the directory",
			fs: fstest.MapFS{
				"node_modules/x/package.json": file("{\"name\":\"x\",\"bin\":\"x.js\"}"),
				"node_modules/x/x.js":         file(""),
				"a/server/main.go":            file("package main\nfunc main() {}\n"),
				"b/server/main.go":            file("package main\nfunc main() {}\n"),
			},
			skip: []string{"node_modules"},
			want: []Entry{
				{Name: "server (a/server)", Kind: KindGoMain, Entry: "a/server/main.go", Evidence: "a/server/main.go:2"},
				{Name: "server (b/server)", Kind: KindGoMain, Entry: "b/server/main.go", Evidence: "b/server/main.go:2"},
			},
		},
		{
			name: "toml literal strings name cargo bins",
			fs: fstest.MapFS{
				"Cargo.toml":        file("[package]\nname = 'tool'\n\n[[bin]]\nname = 'cli'\npath = 'src/cli.rs'\n"),
				"src/cli.rs":        file("fn main() {}\n"),
				"other/Cargo.toml":  file("[package]\nname = 'other'\n"),
				"other/src/main.rs": file("fn main() {}\n"),
			},
			want: []Entry{
				{Name: "cli", Kind: KindRustBin, Entry: "src/cli.rs", Evidence: "Cargo.toml:4"},
				{Name: "other", Kind: KindRustBin, Entry: "other/src/main.rs", Evidence: "other/src/main.rs:1"},
			},
		},
		{
			name: "toml literal strings in python scripts",
			fs: fstest.MapFS{
				"pyproject.toml":  file("[project]\nname = 'tool'\n\n[project.scripts]\ntool = 'tool.cli:main'\n'tool-dev' = \"tool.cli:dev\"\n"),
				"src/tool/cli.py": file("def main(): pass\n"),
			},
			want: []Entry{
				{Name: "tool", Kind: KindPythonScript, Entry: "src/tool/cli.py", Evidence: "pyproject.toml:5"},
				{Name: "tool-dev", Kind: KindPythonScript, Entry: "src/tool/cli.py", Evidence: "pyproject.toml:6"},
			},
		},
		{
			name: "empty repo",
			fs:   fstest.MapFS{"README.md": file("hi")},
			want: []Entry{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Detect(tt.fs, tt.skip)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Detect =\n%+v\nwant\n%+v", got, tt.want)
			}
		})
	}
}

func TestDetect_BadManifestIsSkipped(t *testing.T) {
	fs := fstest.MapFS{
		"package.json": file("{not json"),
		"main.go":      file("package main\nfunc main() {}\n"),
	}
	got, err := Detect(fs, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Kind != KindGoMain {
		t.Errorf("Detect = %+v, want only the Go main", got)
	}
}

func TestTOMLStrings_QuoteStyles(t *testing.T) {
	tests := []struct {
		name      string
		line      string
		wantKey   string
		wantValue string
		wantFound bool
	}{
		{"basic string", `name = "a"`, "name", "a", true},
		{"literal string", `name = 'a'`, "name", "a", true},
		{"literal string keeps backslashes", `path = 'C:\bin'`, "path", `C:\bin`, true},
		{"literal string may hold double quotes", `msg = 'say "hi"'`, "msg", `say "hi"`, true},
		{"quoted basic key", `"my-tool" = "m:main"`, "my-tool", "m:main", true},
		{"quoted literal key", `'my-tool' = 'm:main'`, "my-tool", "m:main", true},
		{"empty literal string", `name = ''`, "name", "", true},
		{"trailing comment", `name = 'a' # c`, "name", "a", true},
		{"unterminated literal is skipped", `name = 'a`, "", "", false},
		{"non-string value is skipped", `autobins = false`, "", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got *tomlEntry
			for _, e := range tomlStrings([]byte("[s]\n" + tt.line + "\n")) {
				if e.key != "" {
					e := e
					got = &e
				}
			}
			if (got != nil) != tt.wantFound {
				t.Fatalf("found = %v, want %v (%+v)", got != nil, tt.wantFound, got)
			}
			if got != nil && (got.key != tt.wantKey || got.value != tt.wantValue) {
				t.Errorf("key/value = %q/%q, want %q/%q", got.key, got.value, tt.wantKey, tt.wantValue)
			}
		})
	}
}

func TestUniqueNames(t *testing.T) {
	tests := []struct {
		name string
		in   []Entry
		want []string
	}{
		{
			name: "no collision keeps names",
			in:   []Entry{{Name: "b", Entry: "b/main.go"}, {Name: "a", Entry: "a/main.go"}},
			want: []string{"a", "b"},
		},
		{
			name: "same name in different dirs gets the dir",
			in:   []Entry{{Name: "server", Entry: "a/server/main.go"}, {Name: "server", Entry: "b/server/main.go"}},
			want: []string{"server (a/server)", "server (b/server)"},
		},
		{
			name: "same name and dir gets the entry path",
			in: []Entry{
				{Name: "foo", Kind: KindPythonScript, Entry: "foo/__init__.py"},
				{Name: "foo", Kind: KindPythonMain, Entry: "foo/__main__.py"},
			},
			want: []string{"foo (foo/__init__.py)", "foo (foo/__main__.py)"},
		},
		{
			name: "a disambiguated name clashing with a literal one: both get the entry path",
			in: []Entry{
				{Name: "x", Entry: "d/a.go"},
				{Name: "x", Entry: "e/b.go"},
				{Name: "x (d)", Entry: "f/c.go"},
			},
			want: []string{"x (d) (f/c.go)", "x (d/a.go)", "x (e)"},
		},
		{
			name: "same name and entry gets a numeric suffix",
			in: []Entry{
				{Name: "foo", Kind: KindPythonScript, Entry: "foo.py"},
				{Name: "foo", Kind: KindPythonScript, Entry: "foo.py"},
			},
			want: []string{"foo (foo.py)", "foo (foo.py) #2"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := []string{}
			for _, e := range uniqueNames(tt.in) {
				got = append(got, e.Name)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("names = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDetect_PythonScriptAndMainSameDirAreDistinct(t *testing.T) {
	fs := fstest.MapFS{
		"pyproject.toml":  file("[project.scripts]\nfoo = \"foo:main\"\n"),
		"foo/__init__.py": file(""),
		"foo/__main__.py": file(""),
	}
	got, err := Detect(fs, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Name == got[1].Name {
		t.Errorf("Detect = %+v, want two entries with distinct names", got)
	}
}
