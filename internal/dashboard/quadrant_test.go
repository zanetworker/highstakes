package dashboard

import (
	"math"
	"reflect"
	"testing"
)

func qf(path string, likelihood, impact, lines int, assessed bool) fileEntry {
	return fileEntry{Path: path, Likelihood: likelihood, Impact: impact, Lines: lines, Assessed: assessed}
}

func TestBuildQuadrant_Radius(t *testing.T) {
	q := buildQuadrant([]fileEntry{
		qf("small.go", 10, 10, 10, true),
		qf("quarter.go", 20, 20, 250, true),
		qf("big.go", 30, 30, 1000, true),
	}, 0, 0)
	tests := []struct {
		path string
		want float64
	}{
		{"small.go", 6 + 10*math.Sqrt(10.0/1000)},
		{"quarter.go", 11}, // sqrt(1/4) of the way from 6 to 16
		{"big.go", 16},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			if got := q.Points[tt.path].R; math.Abs(got-tt.want) > 0.051 {
				t.Errorf("r = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestBuildQuadrant_SkipsTests(t *testing.T) {
	f := qf("a_test.go", 90, 90, 100, true)
	f.Test = true
	q := buildQuadrant([]fileEntry{f, qf("a.go", 1, 1, 10, true)}, 0, 0)
	if _, ok := q.Points["a_test.go"]; ok || len(q.Points) != 1 {
		t.Errorf("points = %v, want only a.go", q.Points)
	}
}

func TestBuildQuadrant_Jitter(t *testing.T) {
	files := []fileEntry{
		qf("alone.go", 50, 50, 10, true),
		qf("p/a.yaml", 0, 0, 10, false),
		qf("p/b.yaml", 0, 0, 10, false),
		qf("p/c.yaml", 0, 0, 10, false),
		qf("p/d.yaml", 0, 0, 10, false),
	}
	q := buildQuadrant(files, 0, 0)
	if p := q.Points["alone.go"]; p.DX != 0 || p.DY != 0 {
		t.Errorf("a point on its own must not move: %+v", p)
	}
	seen := map[[2]float64]bool{}
	centred := 0
	for _, path := range []string{"p/a.yaml", "p/b.yaml", "p/c.yaml", "p/d.yaml"} {
		p := q.Points[path]
		off := [2]float64{p.DX, p.DY}
		if seen[off] {
			t.Errorf("%s shares offset %v with another coincident point", path, off)
		}
		seen[off] = true
		if d := math.Hypot(p.DX, p.DY); d > maxJitter+1e-9 {
			t.Errorf("%s moved %vpx, more than %v", path, d, maxJitter)
		}
		if p.DX == 0 && p.DY == 0 {
			centred++
		}
	}
	if centred != 1 {
		t.Errorf("%d coincident points stay centred, want exactly 1", centred)
	}
	// Deterministic: seeded by path, independent of input order.
	rev := make([]fileEntry, len(files))
	for i, f := range files {
		rev[len(files)-1-i] = f
	}
	if again := buildQuadrant(rev, 0, 0); !reflect.DeepEqual(again.Points, q.Points) {
		t.Errorf("jitter depends on input order:\n%v\n%v", again.Points, q.Points)
	}
}

func TestBuildQuadrant_Range(t *testing.T) {
	tests := []struct {
		name         string
		files        []fileEntry
		likely, bad  int
		wantX, wantY [2]int
	}{
		{"empty: full scale", nil, 36, 61, [2]int{0, 100}, [2]int{0, 100}},
		{"zoom to data and keep the cutoffs in view",
			[]fileEntry{qf("a", 0, 0, 10, false), qf("b", 14, 66, 10, false)}, 36, 61,
			[2]int{0, 50}, [2]int{0, 80}},
		{"lower bound rises with the data",
			[]fileEntry{qf("a", 40, 70, 10, true), qf("b", 62, 93, 10, true)}, 55, 61,
			[2]int{30, 70}, [2]int{50, 100}},
		{"data at the edges: full scale",
			[]fileEntry{qf("a", 0, 0, 10, true), qf("b", 100, 100, 10, true)}, 50, 61,
			[2]int{0, 100}, [2]int{0, 100}},
		{"a single point keeps a span of at least 20",
			[]fileEntry{qf("a", 50, 50, 10, true)}, 0, 0,
			[2]int{40, 60}, [2]int{40, 60}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q := buildQuadrant(tt.files, tt.likely, tt.bad)
			if q.X != tt.wantX || q.Y != tt.wantY {
				t.Errorf("range x=%v y=%v, want x=%v y=%v", q.X, q.Y, tt.wantX, tt.wantY)
			}
		})
	}
}

func TestBuildQuadrant_Labels(t *testing.T) {
	tests := []struct {
		name  string
		files []fileEntry
		want  []string
	}{
		{"assessed: by how bad, ties by will it break then path", []fileEntry{
			qf("a", 10, 90, 10, true), qf("b", 50, 90, 10, true), qf("c", 1, 80, 10, true),
			qf("d", 1, 70, 10, true), qf("e", 1, 70, 10, true), qf("f", 99, 60, 10, true),
			qf("est", 99, 99, 10, false), // estimates rank after assessed files
		}, []string{"b", "a", "c", "d", "e"}},
		{"not assessed: by will it break", []fileEntry{
			qf("a", 10, 90, 10, false), qf("b", 80, 10, 10, false), qf("c", 80, 20, 10, false),
		}, []string{"c", "b", "a"}},
		{"none", nil, []string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := buildQuadrant(tt.files, 0, 0).Labels; !reflect.DeepEqual(got, tt.want) {
				t.Errorf("labels = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestBuildQuadrant_Posture(t *testing.T) {
	assessed := func(path, quadrant string) fileEntry {
		f := qf(path, 0, 0, 10, true)
		f.Quadrant = quadrant
		return f
	}
	files := []fileEntry{
		assessed("hard.go", "fragile-critical"),
		assessed("prot.go", "protect"),
		assessed("watch.go", "watch"),
		assessed("low.go", "low"),
		assessed("none.go", ""),
		// estimates take the quadrant their scores fall in against the cutoffs
		qf("est-hard.go", 40, 70, 10, false),
		qf("est-prot.go", 10, 70, 10, false),
		qf("est-watch.go", 40, 10, 10, false),
		qf("est-low.go", 39, 69, 10, false),
	}
	want := map[string]string{
		"hard.go": "critical", "prot.go": "high", "watch.go": "medium", "low.go": "low", "none.go": "low",
		"est-hard.go": "critical", "est-prot.go": "high", "est-watch.go": "medium", "est-low.go": "low",
	}
	q := buildQuadrant(files, 40, 70)
	for path, w := range want {
		if got := q.Points[path].Posture; got != w {
			t.Errorf("%s posture = %q, want %q", path, got, w)
		}
	}
	t.Run("no cutoffs: estimates stay routine", func(t *testing.T) {
		if got := buildQuadrant(files, 0, 0).Points["est-hard.go"].Posture; got != "low" {
			t.Errorf("posture = %q, want low", got)
		}
	})
}
