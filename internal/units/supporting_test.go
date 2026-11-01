package units

import (
	"testing"

	"github.com/zanetworker/highstakes/internal/types"
)

func TestMarkSupporting(t *testing.T) {
	u := func(name, kind, entry, home string) types.Unit {
		return types.Unit{Name: name, Kind: kind, Entry: entry, Home: home}
	}
	tests := []struct {
		name  string
		units []types.Unit
		globs []string
		want  map[string]bool
	}{
		{
			name: "product crate main is product",
			units: []types.Unit{
				u("sbx", KindRustBin, "crates/sbx/src/main.rs", "crates/sbx"),
			},
			want: map[string]bool{"sbx": false},
		},
		{
			name: "entry in an examples component is supporting",
			units: []types.Unit{
				u("demo", KindRustBin, "examples/demo/src/main.rs", "examples/demo"),
				u("tool", KindGoMain, "scripts/tool/main.go", "scripts"),
			},
			want: map[string]bool{"demo": true, "tool": true},
		},
		{
			name: "helper bin next to the crate main is supporting",
			units: []types.Unit{
				u("sbx", KindRustBin, "crates/sbx/src/main.rs", "crates/sbx"),
				u("perf", KindRustBin, "crates/sbx/src/bin/perf.rs", "crates/sbx"),
			},
			want: map[string]bool{"sbx": false, "perf": true},
		},
		{
			name: "src/bin bins of a crate without a main stay product",
			units: []types.Unit{
				u("a", KindRustBin, "crates/tools/src/bin/a.rs", "crates/tools"),
				u("b", KindRustBin, "crates/tools/src/bin/b.rs", "crates/tools"),
			},
			want: map[string]bool{"a": false, "b": false},
		},
		{
			name: "root crate helper bin (boundary: no directory prefix)",
			units: []types.Unit{
				u("app", KindRustBin, "src/main.rs", "."),
				u("gen", KindRustBin, "src/bin/gen.rs", "."),
			},
			want: map[string]bool{"app": false, "gen": true},
		},
		{
			name:  "config glob marks a component supporting",
			units: []types.Unit{u("bench", KindGoMain, "tools/bench/main.go", "tools")},
			globs: []string{"tools"},
			want:  map[string]bool{"bench": true},
		},
		{name: "no units", units: nil, want: map[string]bool{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			MarkSupporting(tt.units, tt.globs)
			if len(tt.units) != len(tt.want) {
				t.Fatalf("got %d units, want %d", len(tt.units), len(tt.want))
			}
			for _, x := range tt.units {
				if x.Supporting != tt.want[x.Name] {
					t.Errorf("%s: supporting = %v, want %v", x.Name, x.Supporting, tt.want[x.Name])
				}
			}
		})
	}
}
