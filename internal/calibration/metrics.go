// Package calibration checks heat scores against what actually happened in
// git history: reverts, bug fixes, security fixes and recorded incidents.
package calibration

import (
	"math"
	"sort"
)

// Item is one file with a predicted score and the number of observed events.
type Item struct {
	Path   string
	Score  float64
	Events int
}

// expectedHitsAtK returns the expected number of files with events among the
// top k by score, and the effective k (k clamped to len(items)). Files that tie
// with the k-th score share the remaining slots, so the result is the average
// over every ordering of the tied files and does not depend on input order.
func expectedHitsAtK(items []Item, k int) (float64, int) {
	if k <= 0 || len(items) == 0 {
		return 0, 0
	}
	if k > len(items) {
		k = len(items)
	}
	sorted := make([]Item, len(items))
	copy(sorted, items)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Score > sorted[j].Score })

	boundary := sorted[k-1].Score
	hits := 0.0
	above := 0
	tieSize, tieHits := 0, 0
	for _, it := range sorted {
		switch {
		case it.Score > boundary:
			above++
			if it.Events > 0 {
				hits++
			}
		case it.Score == boundary:
			tieSize++
			if it.Events > 0 {
				tieHits++
			}
		}
	}
	slots := k - above
	hits += float64(slots) * float64(tieHits) / float64(tieSize)
	return hits, k
}

// PrecisionAtK is the share of the top k files (by score) that had at least
// one event. k larger than the number of files is clamped; k <= 0 gives 0.
func PrecisionAtK(items []Item, k int) float64 {
	hits, kEff := expectedHitsAtK(items, k)
	if kEff == 0 {
		return 0
	}
	return hits / float64(kEff)
}

// RecallAtK is the share of all files with events that land in the top k.
func RecallAtK(items []Item, k int) float64 {
	positives := countPositives(items)
	if positives == 0 {
		return 0
	}
	hits, _ := expectedHitsAtK(items, k)
	return hits / float64(positives)
}

// BaseRate is the share of files with at least one event: the precision a
// random ranking would get on average.
func BaseRate(items []Item) float64 {
	if len(items) == 0 {
		return 0
	}
	return float64(countPositives(items)) / float64(len(items))
}

// Lift is precision divided by the base rate. 1 means no better than random.
func Lift(precision, baseRate float64) float64 {
	if baseRate == 0 {
		return 0
	}
	return precision / baseRate
}

// Spearman returns the rank correlation of x and y, using average ranks for
// ties. ok is false when it is undefined (fewer than two points, different
// lengths, or no variation in either input).
func Spearman(x, y []float64) (float64, bool) {
	if len(x) != len(y) || len(x) < 2 {
		return 0, false
	}
	rx, ry := averageRanks(x), averageRanks(y)
	n := float64(len(x))
	var mx, my float64
	for i := range rx {
		mx += rx[i]
		my += ry[i]
	}
	mx /= n
	my /= n
	var cov, vx, vy float64
	for i := range rx {
		dx, dy := rx[i]-mx, ry[i]-my
		cov += dx * dy
		vx += dx * dx
		vy += dy * dy
	}
	if vx == 0 || vy == 0 {
		return 0, false
	}
	return cov / math.Sqrt(vx*vy), true
}

func averageRanks(v []float64) []float64 {
	idx := make([]int, len(v))
	for i := range idx {
		idx[i] = i
	}
	sort.SliceStable(idx, func(a, b int) bool { return v[idx[a]] < v[idx[b]] })
	ranks := make([]float64, len(v))
	for i := 0; i < len(idx); {
		j := i
		for j+1 < len(idx) && v[idx[j+1]] == v[idx[i]] {
			j++
		}
		avg := float64(i+j)/2 + 1
		for m := i; m <= j; m++ {
			ranks[idx[m]] = avg
		}
		i = j + 1
	}
	return ranks
}

func countPositives(items []Item) int {
	n := 0
	for _, it := range items {
		if it.Events > 0 {
			n++
		}
	}
	return n
}
