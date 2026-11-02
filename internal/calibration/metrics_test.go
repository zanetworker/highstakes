package calibration

import (
	"math"
	"testing"
)

const eps = 1e-4

func near(a, b float64) bool { return math.Abs(a-b) < eps }

var ranked = []Item{
	{Path: "a", Score: 9, Events: 1},
	{Path: "b", Score: 8, Events: 0},
	{Path: "c", Score: 7, Events: 2},
	{Path: "d", Score: 1, Events: 0},
	{Path: "e", Score: 0, Events: 0},
}

// Three files tie at the top; one of them has an event.
var tied = []Item{
	{Path: "a", Score: 5, Events: 1},
	{Path: "b", Score: 5, Events: 0},
	{Path: "c", Score: 5, Events: 0},
	{Path: "d", Score: 1, Events: 1},
}

func TestPrecisionAtK(t *testing.T) {
	tests := []struct {
		name  string
		items []Item
		k     int
		want  float64
	}{
		{"k zero", ranked, 0, 0},
		{"k negative", ranked, -3, 0},
		{"empty items", nil, 5, 0},
		{"k one hit", ranked, 1, 1},
		{"k two", ranked, 2, 0.5},
		{"k three", ranked, 3, 2.0 / 3.0},
		{"k equals len", ranked, 5, 0.4},
		{"k greater than len clamps", ranked, 10, 0.4},
		{"ties share boundary slot", tied, 1, 1.0 / 3.0},
		{"ties two slots", tied, 2, 1.0 / 3.0},
		{"ties fully inside k", tied, 3, 1.0 / 3.0},
		{"ties all", tied, 4, 0.5},
		{"input order irrelevant", []Item{tied[3], tied[2], tied[1], tied[0]}, 1, 1.0 / 3.0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := PrecisionAtK(tt.items, tt.k); !near(got, tt.want) {
				t.Errorf("PrecisionAtK(k=%d) = %v, want %v", tt.k, got, tt.want)
			}
		})
	}
}

func TestRecallAtK(t *testing.T) {
	tests := []struct {
		name  string
		items []Item
		k     int
		want  float64
	}{
		{"k zero", ranked, 0, 0},
		{"k one", ranked, 1, 0.5},
		{"k three catches both", ranked, 3, 1},
		{"k greater than len", ranked, 99, 1},
		{"ties fractional", tied, 1, (1.0 / 3.0) / 2.0},
		{"no positives", []Item{{Path: "x", Score: 1}}, 1, 0},
		{"empty", nil, 1, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := RecallAtK(tt.items, tt.k); !near(got, tt.want) {
				t.Errorf("RecallAtK(k=%d) = %v, want %v", tt.k, got, tt.want)
			}
		})
	}
}

func TestBaseRateAndLift(t *testing.T) {
	if got := BaseRate(ranked); !near(got, 0.4) {
		t.Errorf("BaseRate = %v, want 0.4", got)
	}
	if got := BaseRate(nil); got != 0 {
		t.Errorf("BaseRate(nil) = %v, want 0", got)
	}
	tests := []struct {
		name      string
		precision float64
		base      float64
		want      float64
	}{
		{"double", 0.5, 0.25, 2},
		{"equal is 1", 0.4, 0.4, 1},
		{"zero base", 0.5, 0, 0},
		{"zero precision", 0, 0.3, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Lift(tt.precision, tt.base); !near(got, tt.want) {
				t.Errorf("Lift = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSpearman(t *testing.T) {
	tests := []struct {
		name   string
		x, y   []float64
		want   float64
		wantOK bool
	}{
		{"perfect", []float64{1, 2, 3, 4}, []float64{10, 20, 30, 40}, 1, true},
		{"inverse", []float64{1, 2, 3, 4}, []float64{4, 3, 2, 1}, -1, true},
		{"monotone not linear", []float64{1, 2, 3}, []float64{1, 10, 1000}, 1, true},
		{"ties average ranks", []float64{1, 2, 2, 3}, []float64{1, 2, 3, 4}, 4.5 / math.Sqrt(22.5), true},
		{"ties on both sides", []float64{0, 0, 1, 1}, []float64{0, 1, 0, 1}, 0, true},
		{"constant x undefined", []float64{3, 3, 3}, []float64{1, 2, 3}, 0, false},
		{"single point undefined", []float64{1}, []float64{1}, 0, false},
		{"length mismatch", []float64{1, 2}, []float64{1}, 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := Spearman(tt.x, tt.y)
			if ok != tt.wantOK || !near(got, tt.want) {
				t.Errorf("Spearman = (%v, %v), want (%v, %v)", got, ok, tt.want, tt.wantOK)
			}
		})
	}
}
