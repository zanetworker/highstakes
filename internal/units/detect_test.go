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
