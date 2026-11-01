package units

import (
	"strings"

	"github.com/zanetworker/highstakes/internal/tour"
	"github.com/zanetworker/highstakes/internal/types"
)

// toolingWords mark a src/bin target as tooling rather than a runtime entry point.
var toolingWords = map[string]bool{
	"perf": true, "bench": true, "gen": true, "codegen": true, "xtask": true,
	"tool": true, "dev": true, "debug": true, "test": true, "fixture": true,
}

// MarkSupporting sets Supporting on units around the product: units whose
// home is a supporting component (tour.IsSupporting with globs), and helper
// bins, meaning Rust src/bin/ targets whose name or path has a tooling word
// (perf, bench, gen, ...). Other src/bin/ targets can be real runtime entry
// points (an init process inside a VM), so they stay product units.
func MarkSupporting(us []types.Unit, globs []string) {
	for i, u := range us {
		helper := u.Kind == KindRustBin && inSrcBin(u.Entry) && hasToolingWord(u.Name+"/"+u.Entry)
		us[i].Supporting = helper || tour.IsSupporting(u.Home, globs)
	}
}

// hasToolingWord reports whether any word of s (split on / - _ .) is a
// tooling word or its plural.
func hasToolingWord(s string) bool {
	words := strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return r == '/' || r == '-' || r == '_' || r == '.'
	})
	for _, w := range words {
		if toolingWords[w] || toolingWords[strings.TrimSuffix(w, "s")] {
			return true
		}
	}
	return false
}

func inSrcBin(entry string) bool {
	return strings.HasPrefix(entry, "src/bin/") || strings.Contains(entry, "/src/bin/")
}
