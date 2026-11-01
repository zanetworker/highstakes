package main

import (
	"strings"

	"github.com/zanetworker/highstakes/internal/explain"
	"github.com/zanetworker/highstakes/internal/types"
)

// explanationBlock renders a file's explanation for `get`: headline, why
// impact, why likely, what to do. Empty for heatmaps written before
// explanations existed.
func explanationBlock(f *types.FileHeat) string {
	e := f.Explanation
	if e == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString(e.Headline + "\n")
	impactTitle, likelyTitle := explain.Titles(*e, f.ImpactAssessed)
	writeSection(&b, impactTitle, e.ImpactReasons, true)
	writeSection(&b, likelyTitle, e.LikelihoodReasons, false)
	if len(e.Actions) > 0 {
		b.WriteString("What to do:\n")
		for _, a := range e.Actions {
			b.WriteString("  • " + a.Text + "\n")
		}
	}
	return b.String()
}

// writeSection prints a title and its reasons, the first on the title line
// when inline is set.
func writeSection(b *strings.Builder, title string, reasons []string, inline bool) {
	if len(reasons) == 0 {
		return
	}
	rest := reasons
	if inline {
		b.WriteString(title + ": " + reasons[0] + "\n")
		rest = reasons[1:]
	} else {
		b.WriteString(title + ":\n")
	}
	for _, r := range rest {
		b.WriteString("  " + r + "\n")
	}
}

// topActions returns a file's first n actions, most important first.
func topActions(f *types.FileHeat, n int) []types.Action {
	if f.Explanation == nil || n <= 0 {
		return nil
	}
	return f.Explanation.Actions[:min(n, len(f.Explanation.Actions))]
}

// attachActions gives each HIGH or CRITICAL changed file its top 3 actions.
func attachActions(risk *types.PRRisk, hm *types.Heatmap) {
	for i := range risk.FilesChanged {
		fc := &risk.FilesChanged[i]
		if f, ok := hm.Files[fc.Path]; ok && (fc.Tier == types.TierCritical || fc.Tier == types.TierHigh) {
			fc.Actions = topActions(f, 3)
		}
	}
}
