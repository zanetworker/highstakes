package units

import (
	"strings"

	"github.com/zanetworker/highstakes/internal/tour"
	"github.com/zanetworker/highstakes/internal/types"
)

// MarkSupporting sets Supporting on units around the product: units whose
// home is a supporting component (tour.IsSupporting with globs), and helper
// bins, meaning Rust bins under src/bin/ of a crate that also has a main
// unit outside src/bin/ (benchmarks, generators, init helpers).
func MarkSupporting(us []types.Unit, globs []string) {
	hasMain := map[string]bool{} // home -> has a rust bin outside src/bin/
	for _, u := range us {
		if u.Kind == KindRustBin && !inSrcBin(u.Entry) {
			hasMain[u.Home] = true
		}
	}
	for i, u := range us {
		helper := u.Kind == KindRustBin && inSrcBin(u.Entry) && hasMain[u.Home]
		us[i].Supporting = helper || tour.IsSupporting(u.Home, globs)
	}
}

func inSrcBin(entry string) bool {
	return strings.HasPrefix(entry, "src/bin/") || strings.Contains(entry, "/src/bin/")
}
