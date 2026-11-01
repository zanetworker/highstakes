package units

import (
	"path"
	"sort"
	"strings"

	"github.com/zanetworker/highstakes/internal/tour"
	"github.com/zanetworker/highstakes/internal/types"
)

// Imports returns the forward file import graph (importer -> imported files)
// built from ImportedBy, without test files or files missing from the heatmap.
func Imports(hm *types.Heatmap) map[string][]string {
	g := map[string][]string{}
	for _, f := range hm.Files {
		if tour.IsTestFile(f.Path) {
			continue
		}
		for _, dep := range f.Dependencies.ImportedBy {
			if _, ok := hm.Files[dep.Path]; ok && !tour.IsTestFile(dep.Path) && dep.Path != f.Path {
				g[dep.Path] = append(g[dep.Path], f.Path)
			}
		}
	}
	for k := range g {
		sort.Strings(g[k])
	}
	return g
}

// seeds are the files a unit links even when the entry file does not import
// them: the rest of a Go main package (Go builds whole directories) and the
// crate's src/lib.rs (a Rust bin links its own library).
func seeds(hm *types.Heatmap, e Entry) []string {
	out := []string{e.Entry}
	switch e.Kind {
	case KindGoMain:
		dir := path.Dir(e.Entry)
		for p := range hm.Files {
			if p != e.Entry && path.Dir(p) == dir && path.Ext(p) == ".go" && !tour.IsTestFile(p) {
				out = append(out, p)
			}
		}
	case KindRustBin:
		if i := strings.LastIndex("/"+e.Entry, "/src/"); i >= 0 {
			lib := e.Entry[:i] + "src/lib.rs" // i indexes "/"+Entry, so Entry[:i] ends with "/" or is ""
			if _, ok := hm.Files[lib]; ok && lib != e.Entry {
				out = append(out, lib)
			}
		}
	}
	return out
}

// Link computes, for every entry, the non-test files it reaches through the
// import graph, and returns one unit per entry in entry order. It sets each
// file's Component and Units. A component is shared when two or more units
// link it and no unit owns it. A unit owns its home (entry component) when it
// is the only unit there, or the one named after it (a crate's main bin next
// to helper bins): a binary crate that another binary also links stays with
// its own unit.
func Link(hm *types.Heatmap, entries []Entry, markers map[string]bool) []types.Unit {
	for p, f := range hm.Files {
		f.Component = tour.Component(p, markers)
		f.Units = nil
	}
	graph := Imports(hm)

	reach := make([]map[string]bool, len(entries))
	linkedBy := map[string]map[int]bool{} // component -> unit indexes
	homes := make([]types.Unit, len(entries))
	for i, e := range entries {
		homes[i] = types.Unit{Name: e.Name, Home: tour.Component(e.Entry, markers)}
	}
	owner := Owners(homes)
	for i, e := range entries {
		seen := map[string]bool{}
		var stack []string
		for _, s := range seeds(hm, e) {
			if _, ok := hm.Files[s]; ok && !seen[s] {
				seen[s] = true
				stack = append(stack, s)
			}
		}
		for len(stack) > 0 {
			cur := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			for _, next := range graph[cur] {
				if !seen[next] {
					seen[next] = true
					stack = append(stack, next)
				}
			}
		}
		reach[i] = seen
		for p := range seen {
			c := hm.Files[p].Component
			if linkedBy[c] == nil {
				linkedBy[c] = map[int]bool{}
			}
			linkedBy[c][i] = true
		}
	}

	out := make([]types.Unit, 0, len(entries))
	for i, e := range entries {
		u := types.Unit{Name: e.Name, Kind: e.Kind, Entry: e.Entry, Evidence: e.Evidence,
			Home: tour.Component(e.Entry, markers), Shared: []string{}}
		comps := map[string]bool{u.Home: true}
		for p := range reach[i] {
			f := hm.Files[p]
			f.Units = append(f.Units, e.Name)
			comps[f.Component] = true
			u.Files++
			u.Lines += f.Size.Lines
			u.MaxImpact, u.ImpactAssessed = RaiseImpact(u.MaxImpact, u.ImpactAssessed, f)
			if f.Quadrant == types.QuadrantFragileCritical {
				u.FragileCritical++
			}
		}
		for c := range comps {
			if c == u.Home {
				continue
			}
			u.Components = append(u.Components, c)
			if len(linkedBy[c]) > 1 && owner[c] == "" {
				u.Shared = append(u.Shared, c)
			}
		}
		sort.Strings(u.Components)
		sort.Strings(u.Shared)
		u.Components = append([]string{u.Home}, u.Components...)
		if len(linkedBy[u.Home]) > 1 && owner[u.Home] == "" {
			u.Shared = append([]string{u.Home}, u.Shared...)
		}
		out = append(out, u)
	}
	for _, f := range hm.Files {
		sort.Strings(f.Units)
	}
	return out
}

// RaiseImpact folds file f into a running maximum impact. assessed says
// whether the file supplying the maximum was LLM-assessed, so an estimate
// is never shown as assessed; on a tie the assessed value wins.
func RaiseImpact(maxImpact int, assessed bool, f *types.FileHeat) (int, bool) {
	switch {
	case f.Impact > maxImpact:
		return f.Impact, f.ImpactAssessed
	case f.Impact == maxImpact:
		return maxImpact, assessed || f.ImpactAssessed
	}
	return maxImpact, assessed
}

// Owners maps each unit home (entry component) to the unit that owns it:
// the only unit there, or the one named after the component when several
// share it. Homes with no owner are left out.
func Owners(us []types.Unit) map[string]string {
	byHome := map[string][]string{}
	for _, u := range us {
		byHome[u.Home] = append(byHome[u.Home], u.Name)
	}
	out := map[string]string{}
	for h, names := range byHome {
		var named []string
		for _, n := range names {
			if n == path.Base(h) {
				named = append(named, n)
			}
		}
		switch {
		case len(names) == 1:
			out[h] = names[0]
		case len(named) == 1:
			out[h] = named[0]
		}
	}
	return out
}
