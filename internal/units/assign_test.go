package units

import (
	"reflect"
	"testing"

	"github.com/zanetworker/highstakes/internal/types"
)

func TestAssign(t *testing.T) {
	mk := func(p, comp string, us ...string) *types.FileHeat {
		return &types.FileHeat{Path: p, Component: comp, Units: us}
	}
	hm := &types.Heatmap{Files: map[string]*types.FileHeat{}, Metadata: types.Metadata{Units: []types.Unit{
		{Name: "sbx", Home: "crates/sbx", Shared: []string{"crates/core"}},
		{Name: "sup", Home: "crates/sup", Shared: []string{"crates/core"}},
		{Name: "sbx-perf", Home: "crates/sbx"},
	}}}
	for _, f := range []*types.FileHeat{
		mk("crates/core/src/lib.rs", "crates/core", "sbx", "sup"),
		mk("crates/sbx/src/util.rs", "crates/sbx", "sbx", "sbx-perf"),
		mk("crates/sbx/src/bin/perf.rs", "crates/sbx", "sbx-perf"),
		mk("crates/net/src/proxy.rs", "crates/net", "sup"),
		mk("crates/tool/src/x.rs", "crates/tool"),
		mk("crates/sup/tests/it.rs", "crates/sup"),
		mk("crates/core/src/dead.rs", "crates/core"),
		mk("crates/core/tests/dead_test.rs", "crates/core"),
	} {
		hm.Files[f.Path] = f
	}
	got, unlinked := Assign(hm)
	want := map[string]string{
		"crates/core/src/lib.rs":     "c:crates/core",
		"crates/sbx/src/util.rs":     "u:sbx",
		"crates/sbx/src/bin/perf.rs": "u:sbx-perf",
		"crates/net/src/proxy.rs":    "u:sup",
	}
	if !reflect.DeepEqual(got, want) || unlinked != 2 {
		t.Errorf("Assign = %v, %d; want %v, 2 (unreachable files in a shared component are unlinked; test files are not counted)", got, unlinked, want)
	}

	t.Run("no units", func(t *testing.T) {
		got, n := Assign(&types.Heatmap{Files: map[string]*types.FileHeat{"a.go": mk("a.go", ".")}})
		if len(got) != 0 || n != 0 {
			t.Errorf("Assign = %v, %d; want empty", got, n)
		}
	})
}
