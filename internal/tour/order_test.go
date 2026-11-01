package tour

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/zanetworker/highstakes/internal/types"
)

func TestIsSupporting(t *testing.T) {
	tests := []struct {
		name      string
		component string
		globs     []string
		want      bool
	}{
		{"examples dir", "examples", nil, true},
		{"crate under examples", "examples/token-issuer", nil, true},
		{"github workflows", ".github", nil, true},
		{"scripts", "scripts", nil, true},
		{"docs", "docs", nil, true},
		{"deploy", "deploy", nil, true},
		{"tests", "tests", nil, true},
		{"e2e", "e2e", nil, true},
		{"benchmarks", "benchmarks", nil, true},
		{"nested fixtures", "crates/core/fixtures", nil, true},
		{"vendor", "vendor", nil, true},
		{"third_party", "third_party", nil, true},
		{"product crate", "crates/openshell-core", nil, false},
		{"go internal package", "internal/tour", nil, false},
		{"repo root", ".", nil, false},
		{"whole segments only", "docsite", nil, false},
		{"segment prefix is not a match", "examples-lib", nil, false},
		{"glob adds a component", "tools", []string{"tools"}, true},
		{"glob matches a child component", "tools/gen", []string{"tools"}, true},
		{"glob with wildcard", "crates/net-demo", []string{"crates/*-demo"}, true},
		{"glob does not match others", "crates/net", []string{"crates/*-demo"}, false},
		{"globs extend the defaults", "examples", []string{"tools"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsSupporting(tt.component, tt.globs); got != tt.want {
				t.Errorf("IsSupporting(%q, %v) = %v, want %v", tt.component, tt.globs, got, tt.want)
			}
		})
	}
}

// mixed: product crates plus examples/, scripts/ and .github/ files. The
// example token issuer is the most impactful file in the repo.
func mixed() (*types.Heatmap, map[string]bool) {
	hm, markers := crates()
	for _, f := range []*types.FileHeat{
		file("examples/issuer/main.py", 99, types.TierCritical),
		file("scripts/release.sh", 60, types.TierMedium),
		file(".github/workflows/ci.yml", 90, types.TierHigh),
	} {
		f.ImpactAssessed, f.Quadrant = true, types.QuadrantFragileCritical
		hm.Files[f.Path] = f
	}
	return hm, markers
}

func sectionsOf(tr *Tour) (comps, kinds []string) {
	for _, s := range tr.Sections {
		comps = append(comps, s.Component)
		kinds = append(kinds, s.Kind)
	}
	return comps, kinds
}

func TestBuild_Supporting(t *testing.T) {
	hm, markers := mixed()
	tests := []struct {
		name                       string
		opts                       Options
		wantComps, wantKinds       []string
		wantProduct, wantSupp, hid int
	}{
		{
			name:        "product only by default",
			opts:        Options{PerComponent: 1, Markers: markers},
			wantComps:   []string{"crates/net", "crates/core", "crates/ui"},
			wantKinds:   []string{"product", "product", "product"},
			wantProduct: 3, hid: 3,
		},
		{
			name:        "include supporting appends a final part",
			opts:        Options{PerComponent: 1, Markers: markers, IncludeSupporting: true},
			wantComps:   []string{"crates/net", "crates/core", "crates/ui", "examples", ".github", "scripts"},
			wantKinds:   []string{"product", "product", "product", "supporting", "supporting", "supporting"},
			wantProduct: 3, wantSupp: 3,
		},
		{
			name:        "configured glob moves a crate to supporting",
			opts:        Options{PerComponent: 1, Markers: markers, Supporting: []string{"crates/ui"}},
			wantComps:   []string{"crates/net", "crates/core"},
			wantKinds:   []string{"product", "product"},
			wantProduct: 2, hid: 4,
		},
		{
			name:        "naming a supporting component keeps it",
			opts:        Options{PerComponent: 1, Markers: markers, Components: []string{"examples", "crates/ui"}},
			wantComps:   []string{"crates/ui", "examples"},
			wantKinds:   []string{"product", "supporting"},
			wantProduct: 1, wantSupp: 1,
		},
		{
			name:      "focus inside a supporting dir keeps it",
			opts:      Options{PerComponent: 1, Markers: markers, Focus: "examples"},
			wantComps: []string{"examples"}, wantKinds: []string{"supporting"},
			wantSupp: 1,
		},
		{
			name:      "directory mode hides supporting too",
			opts:      Options{Limit: 1, Markers: markers},
			wantComps: []string{"crates/net"}, wantKinds: []string{"product"},
			wantProduct: 1, hid: 3,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tr, err := Build(hm, tt.opts)
			if err != nil {
				t.Fatal(err)
			}
			comps, kinds := sectionsOf(tr)
			// Order within a part is covered by TestBuild_ImportanceOrder.
			if !sameParts(comps, kinds, tt.wantComps, tt.wantKinds) {
				t.Errorf("sections = %v %v, want %v %v", comps, kinds, tt.wantComps, tt.wantKinds)
			}
			if tr.ProductComponents != tt.wantProduct || tr.SupportingComponents != tt.wantSupp || tr.SupportingHidden != tt.hid {
				t.Errorf("counts product %d supporting %d hidden %d, want %d %d %d",
					tr.ProductComponents, tr.SupportingComponents, tr.SupportingHidden, tt.wantProduct, tt.wantSupp, tt.hid)
			}
		})
	}

	t.Run("only supporting files left is ErrNoFiles with a hint", func(t *testing.T) {
		only := hmOf(file("examples/a.py", 50, types.TierHigh))
		_, err := Build(only, Options{PerComponent: 1})
		if !errors.Is(err, ErrOnlySupporting) || !errors.Is(err, ErrNoFiles) {
			t.Errorf("err = %v, want ErrOnlySupporting wrapping ErrNoFiles", err)
		}
	})
	t.Run("bad glob is rejected", func(t *testing.T) {
		_, err := Build(hm, Options{Supporting: []string{"crates/["}})
		if !errors.Is(err, ErrInvalidSupporting) || !strings.Contains(err.Error(), "crates/[") {
			t.Errorf("err = %v, want ErrInvalidSupporting naming the glob", err)
		}
	})
}

// sameParts reports whether every product section comes before every
// supporting one and each part holds the wanted components.
func sameParts(comps, kinds, wantComps, wantKinds []string) bool {
	if len(comps) != len(wantComps) || !reflect.DeepEqual(kinds, wantKinds) {
		return false
	}
	got, want := map[string]string{}, map[string]string{}
	for i := range comps {
		got[comps[i]] = kinds[i]
		want[wantComps[i]] = wantKinds[i]
	}
	return reflect.DeepEqual(got, want)
}

func TestBuild_ImportanceOrder(t *testing.T) {
	mk := func(p string, impact int, q types.Quadrant, importedBy ...string) *types.FileHeat {
		f := file(p, impact, types.TierHigh, importedBy...)
		f.ImpactAssessed, f.Quadrant = true, q
		return f
	}
	tests := []struct {
		name  string
		files []*types.FileHeat
		want  []string
	}{
		{
			name: "high-impact component reads its low-impact dependency first",
			files: []*types.FileHeat{
				mk("sandbox/run.go", 95, types.QuadrantProtect),
				mk("util/str.go", 30, types.QuadrantLow, "sandbox/run.go"), // sandbox imports util
				mk("policy/eval.go", 70, types.QuadrantProtect),
			},
			want: []string{"util", "sandbox", "policy"},
		},
		{
			name: "no dependencies: by max impact",
			files: []*types.FileHeat{
				mk("a/a.go", 40, types.QuadrantLow),
				mk("b/b.go", 90, types.QuadrantLow),
				mk("c/c.go", 60, types.QuadrantLow),
			},
			want: []string{"b", "c", "a"},
		},
		{
			name: "equal impact: more fragile-critical files first",
			files: []*types.FileHeat{
				mk("a/x.go", 80, types.QuadrantProtect),
				mk("b/x.go", 80, types.QuadrantFragileCritical),
				mk("b/y.go", 10, types.QuadrantFragileCritical),
				mk("c/x.go", 80, types.QuadrantFragileCritical),
			},
			want: []string{"b", "c", "a"},
		},
		{
			name: "transitive dependency through an unselected component",
			files: []*types.FileHeat{
				mk("top/t.go", 95, types.QuadrantProtect),
				mk("mid/m.go", 10, types.QuadrantLow, "top/t.go"),
				mk("base/b.go", 50, types.QuadrantProtect, "mid/m.go"),
				mk("other/o.go", 60, types.QuadrantProtect),
			},
			// with min impact 40, mid is dropped; base must still precede top
			want: []string{"base", "top", "other"},
		},
		{
			name: "cycle members order by importance",
			files: []*types.FileHeat{
				mk("a/a.go", 30, types.QuadrantLow, "b/b.go"),
				mk("b/b.go", 60, types.QuadrantLow, "a/a.go"),
			},
			want: []string{"b", "a"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := Options{PerComponent: 1}
			if strings.HasPrefix(tt.name, "transitive") {
				opts.MinImpact = 40
			}
			tr, err := Build(hmOf(tt.files...), opts)
			if err != nil {
				t.Fatal(err)
			}
			if got, _ := sectionsOf(tr); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("sections = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestBuild_MinImpactAppliesToStops(t *testing.T) {
	mk := func(p string, impact int, q types.Quadrant) *types.FileHeat {
		f := file(p, impact, types.TierHigh)
		f.ImpactAssessed, f.Quadrant = true, q
		return f
	}
	hm := hmOf(
		mk("net/fragile.go", 78, types.QuadrantFragileCritical), // fragile but below 85
		mk("net/core.go", 90, types.QuadrantProtect),
		mk("net/edge.go", 85, types.QuadrantFragileCritical), // exactly at the threshold
		mk("ui/ui.go", 84, types.QuadrantFragileCritical),    // component below by one
	)
	tests := []struct {
		name string
		opts Options
		want []string
	}{
		{"fragile-critical first among passing stops", Options{PerComponent: 1, MinImpact: 85}, []string{"net/edge.go"}},
		{"stops below the threshold are dropped", Options{PerComponent: 5, MinImpact: 85}, []string{"net/edge.go", "net/core.go"}},
		{"directory mode filters stops too", Options{MinImpact: 86}, []string{"net/core.go"}},
		{"zero keeps everything", Options{PerComponent: 1}, []string{"net/edge.go", "ui/ui.go"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tr, err := Build(hm, tt.opts)
			if err != nil {
				t.Fatal(err)
			}
			if got := stopPaths(tr); !sameSet(got, tt.want) {
				t.Errorf("stops = %v, want %v", got, tt.want)
			}
			for _, s := range tr.Sections {
				if s.MaxImpact < tt.opts.MinImpact {
					t.Errorf("section %s max impact %d below min %d", s.Dir, s.MaxImpact, tt.opts.MinImpact)
				}
			}
		})
	}
}

func sameSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	seen := map[string]int{}
	for _, x := range a {
		seen[x]++
	}
	for _, x := range b {
		seen[x]--
	}
	for _, n := range seen {
		if n != 0 {
			return false
		}
	}
	return true
}
