package units

import (
	"reflect"
	"strings"
	"testing"

	"github.com/zanetworker/highstakes/internal/types"
)

// fixture builds a heatmap from importer -> imported edges.
func fixture(edges map[string][]string, files ...*types.FileHeat) *types.Heatmap {
	hm := &types.Heatmap{Files: map[string]*types.FileHeat{}}
	for _, f := range files {
		hm.Files[f.Path] = f
	}
	for from, tos := range edges {
		for _, to := range tos {
			f := hm.Files[to]
			f.Dependencies.ImportedBy = append(f.Dependencies.ImportedBy, types.Dependency{Path: from})
		}
	}
	return hm
}

func fh(p string, impact int, q types.Quadrant) *types.FileHeat {
	return &types.FileHeat{Path: p, Impact: impact, ImpactAssessed: impact > 0, Quadrant: q, Size: types.FileSize{Lines: 10}}
}

func rustRepo() (*types.Heatmap, map[string]bool) {
	hm := fixture(map[string][]string{
		"crates/sbx/src/main.rs":    {"crates/core/src/lib.rs"},
		"crates/sup/src/main.rs":    {"crates/sup/src/lib.rs"}, // use sup::...
		"crates/drv/src/main.rs":    {"crates/drv/src/lib.rs"},
		"crates/sup/src/lib.rs":     {"crates/net/src/proxy.rs"},
		"crates/net/src/proxy.rs":   {"crates/core/src/policy.rs"},
		"crates/core/src/policy.rs": {"crates/core/src/lib.rs"},
		"crates/core/src/lib.rs":    {"crates/core/src/policy.rs"}, // cycle
		"crates/sup/tests/it.rs":    {"crates/net/src/proxy.rs", "crates/orphan/src/lib.rs"},
		"crates/gw/src/main.rs":     {"crates/drv/src/lib.rs"},
	},
		fh("crates/sbx/src/main.rs", 40, types.QuadrantLow),
		fh("crates/sup/src/main.rs", 50, types.QuadrantWatch),
		fh("crates/sup/src/lib.rs", 30, types.QuadrantLow),
		fh("crates/sup/tests/it.rs", 0, ""),
		fh("crates/net/src/proxy.rs", 92, types.QuadrantFragileCritical),
		fh("crates/core/src/lib.rs", 70, types.QuadrantProtect),
		fh("crates/core/src/policy.rs", 88, types.QuadrantFragileCritical),
		fh("crates/orphan/src/lib.rs", 99, types.QuadrantFragileCritical),
		fh("crates/gw/src/main.rs", 0, ""),
		fh("crates/drv/src/main.rs", 0, ""),
		fh("crates/drv/src/lib.rs", 0, ""),
		fh("crates/sbx/src/bin/perf.rs", 0, ""),
		fh("crates/sup/src/bin/helper.rs", 0, ""), // never uses the sup library
	)
	markers := map[string]bool{}
	for _, c := range []string{"sbx", "sup", "net", "core", "orphan", "gw", "drv"} {
		markers["crates/"+c+"/Cargo.toml"] = true
	}
	return hm, markers
}

func TestLink_Rust(t *testing.T) {
	hm, markers := rustRepo()
	entries := []Entry{
		{Name: "drv", Kind: KindRustBin, Entry: "crates/drv/src/main.rs", Evidence: "crates/drv/src/main.rs:1"},
		{Name: "gw", Kind: KindRustBin, Entry: "crates/gw/src/main.rs", Evidence: "crates/gw/src/main.rs:1"},
		{Name: "sbx", Kind: KindRustBin, Entry: "crates/sbx/src/main.rs", Evidence: "crates/sbx/src/main.rs:1"},
		{Name: "sup", Kind: KindRustBin, Entry: "crates/sup/src/main.rs", Evidence: "crates/sup/Cargo.toml:4"},
		// A second bin in sbx's crate: the crate stays sbx's (the unit named
		// after it), not a shared library.
		{Name: "sbx-perf", Kind: KindRustBin, Entry: "crates/sbx/src/bin/perf.rs", Evidence: "crates/sbx/Cargo.toml:9"},
		{Name: "helper", Kind: KindRustBin, Entry: "crates/sup/src/bin/helper.rs", Evidence: "crates/sup/src/bin/helper.rs:1"},
	}
	got := Link(hm, entries, markers)
	byName := map[string]types.Unit{}
	for _, u := range got {
		byName[u.Name] = u
	}

	tests := []struct {
		unit            string
		home            string
		components      []string
		shared          []string
		files           int
		maxImpact       int
		fragileCritical int
	}{
		// sbx and sup both link core: shared. sup reaches its lib.rs only
		// because main.rs imports it.
		{"sbx", "crates/sbx", []string{"crates/sbx", "crates/core"}, []string{"crates/core"}, 3, 88, 1},
		{"sup", "crates/sup", []string{"crates/sup", "crates/core", "crates/net"}, []string{"crates/core"}, 5, 92, 2},
		// drv is the home of exactly one unit, so gw linking it does not
		// make it shared.
		{"gw", "crates/gw", []string{"crates/gw", "crates/drv"}, []string{}, 2, 0, 0},
		{"drv", "crates/drv", []string{"crates/drv"}, []string{}, 2, 0, 0},
		{"sbx-perf", "crates/sbx", []string{"crates/sbx"}, []string{}, 1, 0, 0},
		// A helper bin next to a library it never imports does not inherit
		// the library's components.
		{"helper", "crates/sup", []string{"crates/sup"}, []string{}, 1, 0, 0},
	}
	for _, tt := range tests {
		t.Run(tt.unit, func(t *testing.T) {
			u, ok := byName[tt.unit]
			if !ok {
				t.Fatalf("unit %q missing from %+v", tt.unit, got)
			}
			if u.Home != tt.home || !reflect.DeepEqual(u.Components, tt.components) || !reflect.DeepEqual(u.Shared, tt.shared) {
				t.Errorf("home=%q components=%v shared=%v, want %q %v %v", u.Home, u.Components, u.Shared, tt.home, tt.components, tt.shared)
			}
			if u.Files != tt.files || u.Lines != tt.files*10 || u.MaxImpact != tt.maxImpact || u.FragileCritical != tt.fragileCritical {
				t.Errorf("files=%d lines=%d max_impact=%d fragile=%d, want %d %d %d %d", u.Files, u.Lines, u.MaxImpact, u.FragileCritical, tt.files, tt.files*10, tt.maxImpact, tt.fragileCritical)
			}
		})
	}

	t.Run("file membership", func(t *testing.T) {
		want := map[string][]string{
			"crates/core/src/lib.rs":   {"sbx", "sup"},
			"crates/net/src/proxy.rs":  {"sup"},
			"crates/drv/src/lib.rs":    {"drv", "gw"},
			"crates/sup/tests/it.rs":   nil, // tests are never linked
			"crates/orphan/src/lib.rs": nil, // only a test imports it
		}
		for p, units := range want {
			if f := hm.Files[p]; !reflect.DeepEqual(f.Units, units) {
				t.Errorf("%s units = %v, want %v", p, f.Units, units)
			}
		}
		if c := hm.Files["crates/net/src/proxy.rs"].Component; c != "crates/net" {
			t.Errorf("component = %q, want crates/net", c)
		}
	})
}

func TestLink_GoSeedsWholePackage(t *testing.T) {
	// main.go imports nothing; flags.go (same package main) imports core.
	hm := fixture(map[string][]string{"cmd/srv/flags.go": {"internal/core/core.go"}},
		fh("cmd/srv/main.go", 10, ""), fh("cmd/srv/flags.go", 10, ""), fh("cmd/srv/main_test.go", 10, ""),
		fh("internal/core/core.go", 60, types.QuadrantProtect))
	got := Link(hm, []Entry{{Name: "srv", Kind: KindGoMain, Entry: "cmd/srv/main.go"}}, nil)
	if len(got) != 1 {
		t.Fatalf("units = %+v", got)
	}
	u := got[0]
	if !reflect.DeepEqual(u.Components, []string{"cmd/srv", "internal/core"}) || u.Files != 3 || u.MaxImpact != 60 {
		t.Errorf("unit = %+v, want cmd/srv + internal/core, 3 files, impact 60", u)
	}
}

func TestLink_EntryMissingFromHeatmap(t *testing.T) {
	hm := fixture(nil, fh("a.go", 1, ""))
	got := Link(hm, []Entry{{Name: "x", Kind: KindGoMain, Entry: "gone/main.go"}}, nil)
	if len(got) != 1 || got[0].Files != 0 || !reflect.DeepEqual(got[0].Components, []string{"gone"}) {
		t.Errorf("units = %+v, want one unit with 0 files and its home component", got)
	}
	if got := Link(hm, nil, nil); len(got) != 0 {
		t.Errorf("no entries: units = %+v, want none", got)
	}
}

func TestOwners(t *testing.T) {
	got := Owners([]types.Unit{
		{Name: "sbx", Home: "crates/sbx"}, {Name: "sbx-perf", Home: "crates/sbx"}, // named after the crate wins
		{Name: "a", Home: "pkg/x"}, {Name: "b", Home: "pkg/x"}, // no name match: no owner
		{Name: "solo", Home: "cmd/solo"},
	})
	want := map[string]string{"crates/sbx": "sbx", "cmd/solo": "solo"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Owners = %v, want %v", got, want)
	}
	if got := Owners(nil); len(got) != 0 {
		t.Errorf("Owners(nil) = %v, want empty", got)
	}
}

func TestLink_MaxImpactAssessedFollowsTheMaxFile(t *testing.T) {
	est := func(p string, impact int) *types.FileHeat {
		f := fh(p, impact, "")
		f.ImpactAssessed = false
		return f
	}
	tests := []struct {
		name         string
		dep          *types.FileHeat
		wantImpact   int
		wantAssessed bool
	}{
		{"estimated max beats assessed 20, shown as estimate", est("lib/a.go", 90), 90, false},
		{"assessed max stays assessed", fh("lib/a.go", 70, ""), 70, true},
		{"tie prefers the assessed value", est("lib/a.go", 20), 20, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hm := fixture(map[string][]string{"cmd/srv/main.go": {"lib/a.go"}},
				fh("cmd/srv/main.go", 20, ""), tt.dep)
			u := Link(hm, []Entry{{Name: "srv", Kind: KindGoMain, Entry: "cmd/srv/main.go"}}, nil)[0]
			if u.MaxImpact != tt.wantImpact || u.ImpactAssessed != tt.wantAssessed {
				t.Errorf("max impact %d assessed %v, want %d %v", u.MaxImpact, u.ImpactAssessed, tt.wantImpact, tt.wantAssessed)
			}
		})
	}
}

// windowsRepo rebuilds a slash-keyed fixture with backslash keys and import
// paths, as the analyzer produces them on Windows, and switches the package
// separator to match for the test.
func windowsRepo(t *testing.T, hm *types.Heatmap) *types.Heatmap {
	t.Helper()
	old := separator
	separator = '\\'
	t.Cleanup(func() { separator = old })
	win := func(p string) string { return strings.ReplaceAll(p, "/", `\`) }
	out := &types.Heatmap{Files: map[string]*types.FileHeat{}}
	for p, f := range hm.Files {
		f.Path = win(p)
		for i := range f.Dependencies.ImportedBy {
			f.Dependencies.ImportedBy[i].Path = win(f.Dependencies.ImportedBy[i].Path)
		}
		out.Files[win(p)] = f
	}
	return out
}

func TestLink_WindowsPaths(t *testing.T) {
	tests := []struct {
		name       string
		kind       string
		entry      string
		files      int
		components []string
	}{
		{"go main seeds its whole package", KindGoMain, "cmd/srv/main.go", 3, []string{"cmd/srv", "internal/core"}},
		{"rust bin follows imports", KindRustBin, "cmd/srv/flags.go", 2, []string{"cmd/srv", "internal/core"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hm := windowsRepo(t, fixture(map[string][]string{"cmd/srv/flags.go": {"internal/core/core.go"}},
				fh("cmd/srv/main.go", 10, ""), fh("cmd/srv/flags.go", 10, ""),
				fh("internal/core/core.go", 60, types.QuadrantProtect)))
			got := Link(hm, []Entry{{Name: "srv", Kind: tt.kind, Entry: tt.entry}}, nil)
			if len(got) != 1 {
				t.Fatalf("units = %+v", got)
			}
			if u := got[0]; u.Files != tt.files || !reflect.DeepEqual(u.Components, tt.components) {
				t.Errorf("files=%d components=%v, want %d %v", u.Files, u.Components, tt.files, tt.components)
			}
			if f := hm.Files[`internal\core\core.go`]; f.Component != "internal/core" || !reflect.DeepEqual(f.Units, []string{"srv"}) {
				t.Errorf("core component=%q units=%v, want internal/core [srv]", f.Component, f.Units)
			}
		})
	}
}
