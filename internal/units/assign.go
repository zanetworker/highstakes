package units

import (
	"github.com/zanetworker/highstakes/internal/tour"
	"github.com/zanetworker/highstakes/internal/types"
)

// Assign places every linked file in one box of a unit picture: a file in a
// shared component goes to "c:<component>"; any other linked file goes to
// "u:<unit>", the unit that links it alone, else the unit owning its
// component (see Owners). Files no unit links are left out; unlinked counts
// the non-test ones. With no units it returns an empty map.
func Assign(hm *types.Heatmap) (fileNode map[string]string, unlinked int) {
	fileNode = map[string]string{}
	if len(hm.Metadata.Units) == 0 {
		return fileNode, 0
	}
	shared := map[string]bool{}
	for _, u := range hm.Metadata.Units {
		for _, c := range u.Shared {
			shared[c] = true
		}
	}
	owner := Owners(hm.Metadata.Units)
	for p, f := range hm.Files {
		switch {
		case len(f.Units) == 0:
			if !tour.IsTestFile(p) {
				unlinked++
			}
		case shared[f.Component]:
			fileNode[p] = "c:" + f.Component
		case len(f.Units) == 1 && f.Units[0] != owner[f.Component]:
			fileNode[p] = "u:" + f.Units[0] // e.g. a helper bin's own file in another unit's crate
		case owner[f.Component] != "":
			fileNode[p] = "u:" + owner[f.Component]
		default:
			fileNode[p] = "u:" + f.Units[0]
		}
	}
	return fileNode, unlinked
}
