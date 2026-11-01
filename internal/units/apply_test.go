package units

import (
	"testing"
	"testing/fstest"

	"github.com/zanetworker/highstakes/internal/types"
)

func TestApply(t *testing.T) {
	fs := fstest.MapFS{
		"crates/sbx/Cargo.toml":  file("[package]\nname = \"sbx\"\n"),
		"crates/sbx/src/main.rs": file("fn main() {}\n"),
		"crates/core/Cargo.toml": file("[package]\nname = \"core\"\n"),
		"crates/core/src/lib.rs": file(""),
		"deploy/Dockerfile.sbx":  file("ENTRYPOINT [\"/sbx\"]\n"),
	}
	hm := fixture(map[string][]string{"crates/sbx/src/main.rs": {"crates/core/src/lib.rs"}},
		fh("crates/sbx/src/main.rs", 10, ""), fh("crates/core/src/lib.rs", 80, types.QuadrantFragileCritical))
	if err := Apply(hm, fs, nil, nil); err != nil {
		t.Fatal(err)
	}
	us := hm.Metadata.Units
	if len(us) != 1 {
		t.Fatalf("units = %+v, want one", us)
	}
	u := us[0]
	if u.Name != "sbx" || u.Home != "crates/sbx" || len(u.Components) != 2 || u.MaxImpact != 80 || u.FragileCritical != 1 {
		t.Errorf("unit = %+v", u)
	}
	if len(u.InferredPlacement) != 1 || u.InferredPlacement[0].Evidence != "deploy/Dockerfile.sbx:1" {
		t.Errorf("placement = %+v, want the Dockerfile entrypoint", u.InferredPlacement)
	}
	if c := hm.Files["crates/core/src/lib.rs"].Component; c != "crates/core" {
		t.Errorf("component = %q, want crates/core (from the Cargo.toml marker)", c)
	}
}

func TestApply_NoEntryPoints(t *testing.T) {
	hm := fixture(nil, fh("lib/a.ts", 10, ""))
	if err := Apply(hm, fstest.MapFS{"lib/a.ts": file("")}, nil, nil); err != nil {
		t.Fatal(err)
	}
	if len(hm.Metadata.Units) != 0 || hm.Files["lib/a.ts"].Component != "lib" {
		t.Errorf("units = %+v component = %q, want none and lib", hm.Metadata.Units, hm.Files["lib/a.ts"].Component)
	}
}

func TestApply_MarksSupporting(t *testing.T) {
	fs := fstest.MapFS{
		"crates/sbx/Cargo.toml":      file("[package]\nname = \"sbx\"\n"),
		"crates/sbx/src/main.rs":     file("fn main() {}\n"),
		"crates/sbx/src/bin/perf.rs": file("fn main() {}\n"),
		"tools/gen/Cargo.toml":       file("[package]\nname = \"gen\"\n"),
		"tools/gen/src/main.rs":      file("fn main() {}\n"),
	}
	hm := fixture(nil, fh("crates/sbx/src/main.rs", 10, ""), fh("crates/sbx/src/bin/perf.rs", 10, ""), fh("tools/gen/src/main.rs", 10, ""))
	if err := Apply(hm, fs, nil, []string{"tools"}); err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, u := range hm.Metadata.Units {
		got[u.Name] = u.Supporting
	}
	want := map[string]bool{"sbx": false, "perf": true, "gen": true}
	for n, w := range want {
		if s, ok := got[n]; !ok || s != w {
			t.Errorf("%s: supporting = %v (found %v), want %v", n, s, ok, w)
		}
	}
}
