package dashboard

import (
	"hash/fnv"
	"math"
	"sort"

	"github.com/zanetworker/highstakes/internal/scorer"
	"github.com/zanetworker/highstakes/internal/types"
)

// Quadrant dot sizes in pixels and the cap on how far a coincident dot moves.
const (
	minDotR     = 6.0
	maxDotR     = 16.0
	maxJitter   = 24.0
	quadLabels  = 5
	goldenAngle = 2.399963229728653 // radians; spreads coincident dots evenly
)

// quadPoint is how one file is drawn: radius R (by sqrt of lines), a pixel
// offset DX, DY that separates files sharing exact scores, and Posture, the
// tier of its quadrant (critical = Harden now ... low = Routine) that sets
// its colour and shape.
type quadPoint struct {
	R       float64 `json:"r"`
	DX      float64 `json:"dx"`
	DY      float64 `json:"dy"`
	Posture string  `json:"posture"`
}

// quadrantTier maps a quadrant to the tier its posture shares.
var quadrantTier = map[string]types.Tier{
	string(types.QuadrantFragileCritical): types.TierCritical,
	string(types.QuadrantProtect):         types.TierHigh,
	string(types.QuadrantWatch):           types.TierMedium,
}

// posture is a file's quadrant tier: the assessed quadrant, or for an
// estimated impact the quadrant its scores fall in against the cutoffs
// (Routine when a cutoff is missing), as the action inbox places it.
func posture(f fileEntry, likelyCutoff, impactCutoff int) string {
	if f.Assessed {
		if t, ok := quadrantTier[f.Quadrant]; ok {
			return string(t)
		}
		return string(types.TierLow)
	}
	if likelyCutoff <= 0 || impactCutoff <= 0 {
		return string(types.TierLow)
	}
	_, t := scorer.QuadrantOf(f.Likelihood >= likelyCutoff, f.Impact >= impactCutoff)
	return string(t)
}

// quadView is the Quadrant layout: the zoomed axis ranges X (will it break)
// and Y (how bad), a point per non-test file, and the files labelled on the
// plot, worst first.
type quadView struct {
	X      [2]int               `json:"x"`
	Y      [2]int               `json:"y"`
	Points map[string]quadPoint `json:"points"`
	Labels []string             `json:"labels"`
}

// buildQuadrant lays out the Quadrant view for the non-test files. The axis
// ranges zoom to the data (padded to tens) and always include the cutoffs
// likelyCutoff and impactCutoff (0 = none) so their lines stay visible.
func buildQuadrant(files []fileEntry, likelyCutoff, impactCutoff int) quadView {
	var fs []fileEntry
	maxLines := 1
	for _, f := range files {
		if !f.Test {
			fs = append(fs, f)
			maxLines = max(maxLines, f.Lines)
		}
	}
	q := quadView{Points: map[string]quadPoint{}, Labels: []string{}}
	xs, ys := make([]int, len(fs)), make([]int, len(fs))
	groups := map[[2]int][]string{}
	for i, f := range fs {
		xs[i], ys[i] = f.Likelihood, f.Impact
		r := minDotR + (maxDotR-minDotR)*math.Sqrt(float64(max(f.Lines, 0))/float64(maxLines))
		q.Points[f.Path] = quadPoint{R: round1(r), Posture: posture(f, likelyCutoff, impactCutoff)}
		k := [2]int{f.Likelihood, f.Impact}
		groups[k] = append(groups[k], f.Path)
	}
	for _, paths := range groups {
		sort.Slice(paths, func(i, j int) bool {
			hi, hj := pathHash(paths[i]), pathHash(paths[j])
			if hi != hj {
				return hi < hj
			}
			return paths[i] < paths[j]
		})
		for i, p := range paths[1:] {
			n := float64(i + 1)
			d := math.Min(maxJitter, 5*math.Sqrt(n))
			pt := q.Points[p]
			pt.DX, pt.DY = round1(d*math.Cos(n*goldenAngle)), round1(d*math.Sin(n*goldenAngle))
			q.Points[p] = pt
		}
	}
	q.X, q.Y = axisRange(xs, likelyCutoff), axisRange(ys, impactCutoff)
	q.Labels = topLabels(fs)
	return q
}

// axisRange pads the data range by 5 and rounds out to tens within 0..100,
// widens it to include cutoff (when 0 < cutoff <= 100), and keeps a span of
// at least 20. No data gives the full scale.
func axisRange(vs []int, cutoff int) [2]int {
	if len(vs) == 0 {
		return [2]int{0, 100}
	}
	lo, hi := vs[0], vs[0]
	for _, v := range vs {
		lo, hi = min(lo, v), max(hi, v)
	}
	if cutoff > 0 && cutoff <= 100 {
		lo, hi = min(lo, cutoff), max(hi, cutoff)
	}
	r := [2]int{max(0, floor10(lo-5)), min(100, ceil10(hi+5))}
	for r[1]-r[0] < 20 {
		if r[1] < 100 {
			r[1] += 10
		} else {
			r[0] -= 10
		}
	}
	return r
}

// topLabels picks the files named on the plot: by how bad when any impact
// is LLM-assessed (assessed files first), else by will it break.
func topLabels(fs []fileEntry) []string {
	assessed := false
	for _, f := range fs {
		assessed = assessed || f.Assessed
	}
	s := append([]fileEntry(nil), fs...)
	sort.Slice(s, func(i, j int) bool {
		a, b := s[i], s[j]
		if assessed {
			if a.Assessed != b.Assessed {
				return a.Assessed
			}
			if a.Impact != b.Impact {
				return a.Impact > b.Impact
			}
		}
		if a.Likelihood != b.Likelihood {
			return a.Likelihood > b.Likelihood
		}
		if !assessed && a.Impact != b.Impact {
			return a.Impact > b.Impact
		}
		return a.Path < b.Path
	})
	out := []string{}
	for i := 0; i < len(s) && i < quadLabels; i++ {
		out = append(out, s[i].Path)
	}
	return out
}

func pathHash(p string) uint32 {
	h := fnv.New32a()
	_, _ = h.Write([]byte(p)) // hash.Hash.Write never fails
	return h.Sum32()
}

func floor10(v int) int { return int(math.Floor(float64(v)/10)) * 10 }
func ceil10(v int) int  { return int(math.Ceil(float64(v)/10)) * 10 }
func round1(v float64) float64 {
	return math.Round(v*10) / 10
}
