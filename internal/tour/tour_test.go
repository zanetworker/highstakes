package tour

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/zanetworker/highstakes/internal/types"
)

// file builds a FileHeat; importedBy lists files that import this one.
func file(path string, heat int, tier types.Tier, importedBy ...string) *types.FileHeat {
	// Impact mirrors heat so fixtures read as "how much this file matters".
	f := &types.FileHeat{Path: path, HeatScore: heat, Impact: heat, Tier: tier, Size: types.FileSize{Lines: 100}}
	for _, p := range importedBy {
		f.Dependencies.ImportedBy = append(f.Dependencies.ImportedBy, types.Dependency{Path: p})
	}
	return f
}

func hmOf(files ...*types.FileHeat) *types.Heatmap {
	hm := &types.Heatmap{Files: map[string]*types.FileHeat{}}
	for _, f := range files {
		hm.Files[f.Path] = f
	}
	return hm
}

// layered: cmd -> api -> types; cmd -> types; api/handler is the hottest file.
func layered() *types.Heatmap {
	return hmOf(
		file("cmd/main.go", 40, types.TierMedium),
		file("api/handler.go", 90, types.TierCritical, "cmd/main.go"),
		file("api/routes.go", 50, types.TierMedium, "cmd/main.go"),
		file("types/types.go", 20, types.TierLow, "api/handler.go", "api/routes.go", "cmd/main.go"),
		file("util/str.go", 70, types.TierHigh),
	)
}

func TestDirGraph(t *testing.T) {
	tests := []struct {
		name string
		hm   *types.Heatmap
		want map[string]map[string]int
	}{
		{
			name: "counts distinct importer files per dir pair",
			hm:   layered(),
			want: map[string]map[string]int{
				"cmd": {"api": 1, "types": 1},
				"api": {"types": 2},
			},
		},
		{
			name: "same-dir imports are not edges",
			hm: hmOf(
				file("a/x.go", 10, types.TierLow, "a/y.go"),
				file("a/y.go", 10, types.TierLow),
			),
			want: map[string]map[string]int{},
		},
		{
			name: "importer missing from heatmap is ignored",
			hm:   hmOf(file("a/x.go", 10, types.TierLow, "gone/z.go")),
			want: map[string]map[string]int{},
		},
		{
			name: "empty heatmap",
			hm:   hmOf(),
			want: map[string]map[string]int{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DirGraph(tt.hm)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("DirGraph() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestLevels(t *testing.T) {
	tests := []struct {
		name  string
		nodes []string
		graph map[string]map[string]int
		want  map[string]int
	}{
		{
			name:  "chain puts dependencies first",
			nodes: []string{"a", "b", "c"},
			graph: map[string]map[string]int{"a": {"b": 1}, "b": {"c": 1}},
			want:  map[string]int{"c": 0, "b": 1, "a": 2},
		},
		{
			name:  "diamond uses longest path",
			nodes: []string{"top", "l", "r", "base"},
			graph: map[string]map[string]int{"top": {"l": 1, "r": 1, "base": 1}, "l": {"base": 1}, "r": {"base": 1}},
			want:  map[string]int{"base": 0, "l": 1, "r": 1, "top": 2},
		},
		{
			name:  "cycle members share a level and do not loop forever",
			nodes: []string{"a", "b", "c", "d"},
			graph: map[string]map[string]int{"a": {"b": 1}, "b": {"a": 1, "c": 1}, "d": {"a": 1}},
			want:  map[string]int{"c": 0, "a": 1, "b": 1, "d": 2},
		},
		{
			name:  "isolated nodes are level zero",
			nodes: []string{"x"},
			graph: map[string]map[string]int{},
			want:  map[string]int{"x": 0},
		},
		{
			name:  "no nodes",
			nodes: nil,
			graph: nil,
			want:  map[string]int{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Levels(tt.nodes, tt.graph)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Levels() = %v, want %v", got, tt.want)
			}
		})
	}
}

func stopPaths(tr *Tour) []string {
	var out []string
	for _, s := range tr.Sections {
		for _, st := range s.Stops {
			out = append(out, st.Path)
		}
	}
	return out
}

func TestBuild_Order(t *testing.T) {
	all := []string{"types/types.go", "api/handler.go", "api/routes.go", "util/str.go", "cmd/main.go"}
	tests := []struct {
		name string
		opts Options
		want []string
	}{
		{
			name: "most important first, each after its dependencies",
			opts: Options{},
			// api (90) needs types (20) first; then util (70); cmd (40) last
			want: all,
		},
		{
			name: "limit keeps the hottest files, by importance",
			opts: Options{Limit: 2},
			want: []string{"api/handler.go", "util/str.go"},
		},
		{name: "limit one", opts: Options{Limit: 1}, want: []string{"api/handler.go"}},
		{name: "limit equal to file count keeps all", opts: Options{Limit: 5}, want: all},
		{name: "limit above file count keeps all", opts: Options{Limit: 50}, want: all},
		{
			name: "focus restricts to a directory",
			opts: Options{Focus: "api/"},
			want: []string{"api/handler.go", "api/routes.go"},
		},
		{name: "focus does not match sibling prefixes", opts: Options{Focus: "ap"}, want: nil},
		{name: "focus on a single file", opts: Options{Focus: "types/types.go"}, want: []string{"types/types.go"}},
		{name: "focus dot means whole repo", opts: Options{Focus: "./"}, want: all},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tr, err := Build(layered(), tt.opts)
			if tt.want == nil {
				if !errors.Is(err, ErrNoFiles) {
					t.Fatalf("Build() err = %v, want ErrNoFiles", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Build() err = %v", err)
			}
			if got := stopPaths(tr); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("stops = %v, want %v", got, tt.want)
			}
			if tr.TotalStops != len(tt.want) {
				t.Errorf("TotalStops = %d, want %d", tr.TotalStops, len(tt.want))
			}
		})
	}
}

func TestBuild_NegativeLimit(t *testing.T) {
	if _, err := Build(layered(), Options{Limit: -1}); !errors.Is(err, ErrInvalidLimit) {
		t.Errorf("err = %v, want ErrInvalidLimit", err)
	}
}

func TestBuild_EmptyHeatmap(t *testing.T) {
	if _, err := Build(hmOf(), Options{}); !errors.Is(err, ErrNoFiles) {
		t.Errorf("err = %v, want ErrNoFiles", err)
	}
}

func TestBuild_CycleTerminates(t *testing.T) {
	hm := hmOf(
		file("a/a.go", 30, types.TierLow, "b/b.go"),
		file("b/b.go", 60, types.TierMedium, "a/a.go"),
	)
	tr, err := Build(hm, Options{})
	if err != nil {
		t.Fatal(err)
	}
	// Same level, so heat decides: b before a.
	if got, want := stopPaths(tr), []string{"b/b.go", "a/a.go"}; !reflect.DeepEqual(got, want) {
		t.Errorf("stops = %v, want %v", got, want)
	}
	if tr.Sections[0].Level != tr.Sections[1].Level {
		t.Errorf("cycle members should share a level: %d vs %d", tr.Sections[0].Level, tr.Sections[1].Level)
	}
}

func TestBuild_StopFields(t *testing.T) {
	hm := layered()
	h := hm.Files["api/handler.go"]
	h.Factors.BlastRadius = types.BlastRadius{Assessed: true, Summary: "Request routing fails", CriticalReason: "Auth bypass", SecurityImpact: 90, DataImpact: 10}
	now := time.Now()
	h.RecentChanges = []types.Change{
		{Author: "bob", Date: now}, {Author: "alice", Date: now}, {Author: "bob", Date: now},
	}
	hm.Annotations = map[string]types.Annotation{"api/handler.go": {Owner: "@sec-team", Notes: "Read the auth RFC first"}}

	tr, err := Build(hm, Options{})
	if err != nil {
		t.Fatal(err)
	}
	var st *Stop
	var sec *Section
	for i := range tr.Sections {
		for j := range tr.Sections[i].Stops {
			if tr.Sections[i].Stops[j].Path == "api/handler.go" {
				st = &tr.Sections[i].Stops[j]
				sec = &tr.Sections[i]
			}
		}
	}
	if st == nil {
		t.Fatal("handler stop missing")
	}
	if st.Anchor != "api/handler.go:1" || st.Line != 1 {
		t.Errorf("anchor = %q line = %d", st.Anchor, st.Line)
	}
	if st.Why != "Auth bypass" || st.Summary != "Request routing fails" {
		t.Errorf("why = %q summary = %q", st.Why, st.Summary)
	}
	if st.TopImpact != "security" {
		t.Errorf("top impact = %q", st.TopImpact)
	}
	if want := []string{"types/types.go"}; !reflect.DeepEqual(st.DependsOn, want) {
		t.Errorf("depends_on = %v, want %v", st.DependsOn, want)
	}
	if want := []string{"cmd/main.go"}; !reflect.DeepEqual(st.UsedBy, want) {
		t.Errorf("used_by = %v, want %v", st.UsedBy, want)
	}
	if want := []string{"@sec-team", "bob", "alice"}; !reflect.DeepEqual(st.Owners, want) {
		t.Errorf("owners = %v, want %v", st.Owners, want)
	}
	if st.Notes != "Read the auth RFC first" {
		t.Errorf("notes = %q", st.Notes)
	}
	if st.Order != 2 || st.Title != "handler.go" {
		t.Errorf("order = %d title = %q", st.Order, st.Title)
	}
	if sec.Dir != "api" || sec.MaxHeat != 90 || sec.Tier != types.TierCritical || sec.Level != 1 {
		t.Errorf("section = %+v", sec)
	}
	if want := []string{"types"}; !reflect.DeepEqual(sec.DependsOn, want) {
		t.Errorf("section depends_on = %v, want %v", sec.DependsOn, want)
	}
	if want := []string{"cmd"}; !reflect.DeepEqual(sec.UsedBy, want) {
		t.Errorf("section used_by = %v, want %v", sec.UsedBy, want)
	}
}

func TestBuild_NeighbourListsCapped(t *testing.T) {
	var importers []string
	files := []*types.FileHeat{}
	for i := 0; i < maxNeighbours+3; i++ {
		p := "u/f" + string(rune('a'+i)) + ".go"
		importers = append(importers, p)
		files = append(files, file(p, 10+i, types.TierLow))
	}
	files = append(files, file("core/core.go", 99, types.TierCritical, importers...))
	tr, err := Build(hmOf(files...), Options{Focus: "core"})
	if err != nil {
		t.Fatal(err)
	}
	st := tr.Sections[0].Stops[0]
	if len(st.UsedBy) != maxNeighbours || st.UsedByCount != maxNeighbours+3 {
		t.Errorf("used_by len = %d count = %d", len(st.UsedBy), st.UsedByCount)
	}
	// Hottest importer first.
	if st.UsedBy[0] != importers[len(importers)-1] {
		t.Errorf("used_by[0] = %s, want hottest importer", st.UsedBy[0])
	}
}

func TestWhy_Fallbacks(t *testing.T) {
	tests := []struct {
		name string
		f    *types.FileHeat
		want string
	}{
		{
			name: "summary when no critical reason",
			f:    &types.FileHeat{Factors: types.Factors{BlastRadius: types.BlastRadius{Assessed: true, Summary: "Cache goes stale"}}},
			want: "Cache goes stale",
		},
		{
			name: "static signals when not assessed",
			f: &types.FileHeat{
				Dependencies: types.Dependencies{ImportedBy: []types.Dependency{{Path: "a"}, {Path: "b"}}},
				Factors:      types.Factors{ChangeFrequency: types.ChangeFrequency{CommitsLast90d: 4}},
			},
			want: "imported by 2 files; 4 commits in 90d",
		},
		{
			name: "singular importer",
			f:    &types.FileHeat{Dependencies: types.Dependencies{ImportedBy: []types.Dependency{{Path: "a"}}}},
			want: "imported by 1 file",
		},
		{
			name: "nothing known",
			f:    &types.FileHeat{},
			want: "no blast radius assessment; run analyze with an LLM key",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := why(tt.f); got != tt.want {
				t.Errorf("why() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestLineHint(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    int
	}{
		{"go func after package", "package x\n\nimport \"fmt\"\n\n// Doc\nfunc Run() {}\n", 6},
		{"go type before func", "package x\n\ntype T struct{}\nfunc F() {}\n", 3},
		{"python def", "import os\n\n\ndef main():\n    pass\n", 4},
		{"python class", "class A:\n    pass\n", 1},
		{"ts export", "import x from 'y'\nexport function f() {}\n", 2},
		{"rust pub fn", "use std::io;\npub fn run() {}\n", 2},
		// boundary_server.rs: everything sits in a cfg-gated mod, so the only
		// earlier match was a pub fn near the end of a 10k-line file.
		{"rust cfg mod before late pub fn", "//! doc\n\nuse std::path::Path;\n\n#[cfg(target_os = \"linux\")]\nmod linux {\n    fn x() {}\n}\n\npub fn run() {}\n", 6},
		{"rust enum", "use a::b;\nenum State { A }\n", 2},
		{"rust trait", "use a::b;\ntrait Driver {}\n", 2},
		{"rust unsafe fn", "use a::b;\nunsafe fn raw() {}\n", 2},
		{"rust async fn", "use a::b;\nasync fn serve() {}\n", 2},
		{"word starting with mod is not a declaration", "modules: x\nfn f() {}\n", 2},
		{"indented method is not top level", "package x\n  func inner() {}\n", 1},
		{"no declaration falls back to 1", "key: value\nother: 1\n", 1},
		{"empty content", "", 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := LineHint(tt.content); got != tt.want {
				t.Errorf("LineHint() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestOwners(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name  string
		f     *types.FileHeat
		owner string
		want  []string
	}{
		{
			name: "ranked by commit count, ties by name, capped at 3",
			f: &types.FileHeat{RecentChanges: []types.Change{
				{Author: "d", Date: now}, {Author: "c", Date: now}, {Author: "c", Date: now},
				{Author: "b", Date: now}, {Author: "a", Date: now},
			}},
			want: []string{"c", "a", "b"},
		},
		{
			name:  "annotation owner first, not duplicated",
			f:     &types.FileHeat{RecentChanges: []types.Change{{Author: "x"}, {Author: "@lead"}}},
			owner: "@lead",
			want:  []string{"@lead", "x"},
		},
		{
			name: "no history",
			f:    &types.FileHeat{},
			want: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := owners(tt.f, tt.owner); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("owners() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestBuild_SourceLineHints(t *testing.T) {
	src := func(p string) ([]byte, error) {
		if p == "api/handler.go" {
			return []byte("package api\n\nfunc Handle() {}\n"), nil
		}
		return nil, errors.New("unreadable")
	}
	tr, err := Build(layered(), Options{Focus: "api", Source: src})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, st := range tr.Sections[0].Stops {
		got[st.Path] = st.Anchor
	}
	want := map[string]string{"api/handler.go": "api/handler.go:3", "api/routes.go": "api/routes.go:1"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("anchors = %v, want %v", got, want)
	}
}

func TestIsTestFile(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"internal/tour/tour_test.go", true},
		{"src/app.test.ts", true},
		{"src/app.spec.js", true},
		{"pkg/test_utils.py", true},
		{"pkg/utils_test.py", true},
		{"internal/tour/tour.go", false},
		{"internal/testing/helpers.go", false}, // directory name alone is not a test
		{"contest.go", false},                  // "test" inside a name is not a test
		{"src/jsx/dom/render.test.tsx", true},
		{"src/app.spec.jsx", true},
		{"lib/x.test.mjs", true},
		{"lib/x.spec.mts", true},
		{"src/test/java/org/FooTest.java", true},
		{"src/test/kotlin/BarTests.kt", true},
		{"spec/models/user_spec.rb", true},
		{"test/user_test.rb", true},
		{"tests/ServiceTests.cs", true},
		{"src/Testing.java", false},
		{"src/Latest.java", false},
		{"crates/a/tests/integration.rs", true},
		{"tests/e2e/sandbox.rs", true},
		{"crates/a/src/grpc/tests/mod.rs", true},
		{"crates/a/src/tests.rs", true},
		{"crates/a/src/lifecycle_tests.rs", true},
		{"crates/a/src/attests.rs", false},
		{"crates/a/src/testing/mod.rs", false},
		{"contest/main.rs", false},
		{"crates/tests-support/src/lib.rs", false},
		{"tests", false},
		{"", false},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			if got := IsTestFile(tt.path); got != tt.want {
				t.Errorf("IsTestFile(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

func TestBuild_SkipsTestFiles(t *testing.T) {
	// The test file is the hottest and imports types; it must not become a
	// stop, a "used by" entry, or a dependency edge.
	hm := hmOf(
		file("api/handler.go", 50, types.TierMedium),
		file("api/handler_test.go", 95, types.TierCritical),
		file("types/types.go", 20, types.TierLow, "api/handler.go", "api/handler_test.go", "cmd/main_test.go"),
		file("cmd/main_test.go", 10, types.TierLow),
	)

	tr, err := Build(hm, Options{})
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	for _, s := range tr.Sections {
		for _, st := range s.Stops {
			paths = append(paths, st.Path)
			if st.Path == "types/types.go" && !reflect.DeepEqual(st.UsedBy, []string{"api/handler.go"}) {
				t.Errorf("types.go UsedBy = %v, want [api/handler.go]", st.UsedBy)
			}
		}
	}
	if want := []string{"types/types.go", "api/handler.go"}; !reflect.DeepEqual(paths, want) {
		t.Errorf("stops = %v, want %v", paths, want)
	}
	if g := DirGraph(hm); g["cmd"] != nil {
		t.Errorf("DirGraph has edge from test-only dir cmd: %v", g["cmd"])
	}
}

func TestBuild_OnlyTestFiles(t *testing.T) {
	_, err := Build(hmOf(file("a/a_test.go", 10, types.TierLow)), Options{})
	if !errors.Is(err, ErrNoFiles) {
		t.Errorf("err = %v, want ErrNoFiles", err)
	}
}

func TestBuild_OrdersByImpact(t *testing.T) {
	mk := func(path string, heat, impact, likelihood int, centrality float64, importedBy ...string) *types.FileHeat {
		f := file(path, heat, types.TierLow, importedBy...)
		f.Impact, f.Likelihood, f.ImpactAssessed = impact, likelihood, true
		f.Factors.DependencyCentrality.Score = centrality
		return f
	}
	hm := hmOf(
		mk("core/churny.go", 95, 20, 95, 0.1), // hottest, low impact
		mk("core/auth.go", 70, 90, 10, 0.2),   // stable, impactful
		mk("core/tie_a.go", 10, 50, 5, 0.1),   // ties on impact: centrality decides
		mk("core/tie_b.go", 10, 50, 5, 0.9),
		mk("app/app.go", 99, 30, 99, 0, "x"), // level 0 too; lower max impact than core
		mk("svc/svc.go", 50, 99, 50, 0),      // level 1: imports core
	)
	hm.Files["core/auth.go"].Dependencies.ImportedBy = []types.Dependency{{Path: "svc/svc.go"}}
	hm.Files["core/auth.go"].Quadrant = types.QuadrantProtect

	tr, err := Build(hm, Options{})
	if err != nil {
		t.Fatal(err)
	}
	// svc (99) depends on core (90), so core comes first; app (30) last.
	want := []string{"core/auth.go", "core/tie_b.go", "core/tie_a.go", "core/churny.go", "svc/svc.go", "app/app.go"}
	if got := stopPaths(tr); !reflect.DeepEqual(got, want) {
		t.Errorf("stops = %v, want %v", got, want)
	}
	core := tr.Sections[0]
	if core.Dir != "core" || core.MaxImpact != 90 || core.MaxHeat != 95 {
		t.Errorf("core section = dir %s max_impact %d max_heat %d, want core 90 95", core.Dir, core.MaxImpact, core.MaxHeat)
	}
	st := core.Stops[0]
	if st.Impact != 90 || st.Likelihood != 10 || !st.ImpactAssessed || st.Quadrant != types.QuadrantProtect {
		t.Errorf("stop axes = impact %d likelihood %d assessed %v quadrant %q", st.Impact, st.Likelihood, st.ImpactAssessed, st.Quadrant)
	}

	t.Run("limit keeps the most impactful files", func(t *testing.T) {
		tr, err := Build(hm, Options{Limit: 2})
		if err != nil {
			t.Fatal(err)
		}
		if got, want := stopPaths(tr), []string{"core/auth.go", "svc/svc.go"}; !reflect.DeepEqual(got, want) {
			t.Errorf("stops = %v, want %v", got, want)
		}
	})
}

func TestComponent(t *testing.T) {
	markers := map[string]bool{
		"Cargo.toml":                    true, // workspace root: never a component
		"crates/net/Cargo.toml":         true,
		"crates/net/fuzz/Cargo.toml":    true, // nested crate folds into the outer one
		"packages/web/package.json":     true,
		"python/sdk/__init__.py":        true,
		"python/sdk/client/__init__.py": true,
		"tools/gen/pyproject.toml":      true,
		"staging/mod/go.mod":            true,
	}
	tests := []struct {
		name, path, want string
	}{
		{"rust crate", "crates/net/src/proxy/tls.rs", "crates/net"},
		{"nested crate folds into outer", "crates/net/fuzz/src/main.rs", "crates/net"},
		{"js package", "packages/web/src/app.tsx", "packages/web"},
		{"python package", "python/sdk/client/http.py", "python/sdk"},
		{"python pyproject", "tools/gen/src/gen/main.py", "tools/gen"},
		{"nested go module", "staging/mod/pkg/x.go", "staging/mod"},
		{"go internal package", "internal/tour/tour.go", "internal/tour"},
		{"go cmd package subdir", "cmd/highstakes/sub/x.go", "cmd/highstakes"},
		{"go pkg file at depth one", "pkg/doc.go", "pkg"},
		{"fallback top-level dir", "deploy/helm/values.yaml", "deploy"},
		{"crate without marker falls back", "crates/other/src/lib.rs", "crates"},
		{"root file", "Cargo.lock", "."},
		{"empty path", "", "."},
		{"nil markers", "crates/net/src/lib.rs", "crates"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := markers
			if tt.name == "nil markers" {
				m = nil
			}
			if got := Component(tt.path, m); got != tt.want {
				t.Errorf("Component(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

func TestMarkers(t *testing.T) {
	present := map[string]bool{"crates/net/Cargo.toml": true, "Cargo.toml": true, "web/package.json": true}
	exists := func(p string) bool { return present[p] }
	tests := []struct {
		name  string
		paths []string
		want  map[string]bool
	}{
		{"ancestors are checked once per dir", []string{"crates/net/src/a.rs", "crates/net/src/b.rs", "web/x.ts"},
			map[string]bool{"crates/net/Cargo.toml": true, "web/package.json": true}},
		{"root markers are not reported", []string{"main.rs"}, map[string]bool{}},
		{"no paths", nil, map[string]bool{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Markers(tt.paths, exists); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Markers() = %v, want %v", got, tt.want)
			}
		})
	}
}

// crates: core is imported by net; net has a fragile-critical file with
// lower impact than its protect files; ui is low impact.
func crates() (*types.Heatmap, map[string]bool) {
	mk := func(p string, impact, likelihood int, centrality float64, q types.Quadrant, importedBy ...string) *types.FileHeat {
		f := file(p, impact, types.TierHigh, importedBy...)
		f.Impact, f.Likelihood, f.ImpactAssessed, f.Quadrant = impact, likelihood, true, q
		f.Factors.DependencyCentrality.Score = centrality
		return f
	}
	hm := hmOf(
		mk("crates/core/src/lib.rs", 70, 10, 0.9, types.QuadrantProtect, "crates/net/src/proxy.rs"),
		mk("crates/core/src/jwt.rs", 80, 10, 0.1, types.QuadrantProtect),
		mk("crates/core/src/util.rs", 20, 10, 0.1, types.QuadrantLow),
		mk("crates/net/src/proxy.rs", 95, 20, 0.1, types.QuadrantProtect),
		mk("crates/net/src/policy.rs", 95, 40, 0.1, types.QuadrantProtect), // same impact, more likely
		mk("crates/net/src/tls.rs", 60, 90, 0.1, types.QuadrantFragileCritical),
		mk("crates/net/tests/e2e.rs", 99, 99, 1, types.QuadrantFragileCritical),
		mk("crates/ui/src/main.rs", 30, 10, 0, types.QuadrantLow),
	)
	markers := map[string]bool{"crates/core/Cargo.toml": true, "crates/net/Cargo.toml": true, "crates/ui/Cargo.toml": true}
	return hm, markers
}

func TestBuild_PerComponent(t *testing.T) {
	hm, markers := crates()
	tests := []struct {
		name         string
		opts         Options
		wantStops    []string
		wantSections []string
	}{
		{
			name:         "two per component, fragile-critical first, dependencies first",
			opts:         Options{PerComponent: 2, Markers: markers},
			wantStops:    []string{"crates/core/src/jwt.rs", "crates/core/src/lib.rs", "crates/net/src/tls.rs", "crates/net/src/policy.rs", "crates/ui/src/main.rs"},
			wantSections: []string{"crates/core", "crates/net", "crates/ui"},
		},
		{
			name:         "per-component wins over limit",
			opts:         Options{PerComponent: 1, Limit: 1, Markers: markers},
			wantStops:    []string{"crates/core/src/jwt.rs", "crates/net/src/tls.rs", "crates/ui/src/main.rs"},
			wantSections: []string{"crates/core", "crates/net", "crates/ui"},
		},
		{
			name:         "min impact drops low components",
			opts:         Options{PerComponent: 1, MinImpact: 50, Markers: markers},
			wantStops:    []string{"crates/core/src/jwt.rs", "crates/net/src/tls.rs"},
			wantSections: []string{"crates/core", "crates/net"},
		},
		{
			name:         "min impact equal to max impact keeps the component, not stops below it",
			opts:         Options{PerComponent: 1, MinImpact: 95, Markers: markers},
			wantStops:    []string{"crates/net/src/policy.rs"},
			wantSections: []string{"crates/net"},
		},
		{
			name:         "components restricts",
			opts:         Options{PerComponent: 5, Components: []string{"crates/net"}, Markers: markers},
			wantStops:    []string{"crates/net/src/tls.rs", "crates/net/src/policy.rs", "crates/net/src/proxy.rs"},
			wantSections: []string{"crates/net"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tr, err := Build(hm, tt.opts)
			if err != nil {
				t.Fatal(err)
			}
			if got := stopPaths(tr); !reflect.DeepEqual(got, tt.wantStops) {
				t.Errorf("stops = %v, want %v", got, tt.wantStops)
			}
			var secs []string
			for _, s := range tr.Sections {
				secs = append(secs, s.Component)
				if s.Title != s.Component || s.Dir != s.Component {
					t.Errorf("section title %q dir %q, want component %q", s.Title, s.Dir, s.Component)
				}
			}
			if !reflect.DeepEqual(secs, tt.wantSections) {
				t.Errorf("sections = %v, want %v", secs, tt.wantSections)
			}
		})
	}

	t.Run("component graph sets levels and neighbours", func(t *testing.T) {
		tr, err := Build(hm, Options{PerComponent: 2, Markers: markers})
		if err != nil {
			t.Fatal(err)
		}
		net := tr.Sections[1]
		if net.Level != 1 || !reflect.DeepEqual(net.DependsOn, []string{"crates/core"}) {
			t.Errorf("net level %d depends_on %v, want 1 [crates/core]", net.Level, net.DependsOn)
		}
		if net.MaxImpact != 95 {
			t.Errorf("net max_impact = %d, want 95 (policy.rs, listed after the fragile-critical tls.rs)", net.MaxImpact)
		}
		if core := tr.Sections[0]; !reflect.DeepEqual(core.UsedBy, []string{"crates/net"}) || core.MaxImpact != 80 {
			t.Errorf("core used_by %v max_impact %d, want [crates/net] 80", core.UsedBy, core.MaxImpact)
		}
	})
}

func TestBuild_PerComponentErrors(t *testing.T) {
	hm, markers := crates()
	tests := []struct {
		name    string
		opts    Options
		wantErr error
		wantMsg string
	}{
		{"negative per-component", Options{PerComponent: -1}, ErrInvalidPerComponent, "-1"},
		{"negative min impact", Options{MinImpact: -1}, ErrInvalidMinImpact, "-1"},
		{"min impact above 100", Options{MinImpact: 101}, ErrInvalidMinImpact, "101"},
		{"unknown component lists valid ones", Options{PerComponent: 1, Components: []string{"crates/nope"}, Markers: markers},
			ErrUnknownComponent, "crates/core, crates/net, crates/ui"},
		{"min impact above every component", Options{PerComponent: 1, MinImpact: 100, Markers: markers}, ErrBelowMinImpact, "100"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Build(hm, tt.opts)
			if !errors.Is(err, tt.wantErr) || !strings.Contains(err.Error(), tt.wantMsg) {
				t.Errorf("err = %v, want %v containing %q", err, tt.wantErr, tt.wantMsg)
			}
		})
	}
}

func TestBuild_LimitModeSetsComponent(t *testing.T) {
	hm, markers := crates()
	tr, err := Build(hm, Options{Limit: 1, Markers: markers})
	if err != nil {
		t.Fatal(err)
	}
	if s := tr.Sections[0]; s.Dir != "crates/net/src" || s.Component != "crates/net" {
		t.Errorf("section dir %q component %q, want crates/net/src crates/net", s.Dir, s.Component)
	}
}
