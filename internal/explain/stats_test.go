package explain

import (
	"fmt"
	"strings"
	"testing"

	"github.com/zanetworker/highstakes/internal/types"
)

func TestTopPercent(t *testing.T) {
	sorted := []int{0, 0, 1, 2, 3, 4, 5, 6, 7, 10} // 10 files
	tests := []struct {
		name string
		v    int
		want int
	}{
		{"max value is top 10%", 10, 10},
		{"ties count as at-or-above", 7, 20},
		{"middle", 3, 60},
		{"zero is never notable", 0, 0},
		{"negative is never notable", -1, 0},
		{"above max still top 1 file", 99, 10},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := topPercent(sorted, tt.v); got != tt.want {
				t.Errorf("topPercent(%d) = %d, want %d", tt.v, got, tt.want)
			}
		})
	}
	t.Run("empty repo", func(t *testing.T) {
		if got := topPercent(nil, 5); got != 0 {
			t.Errorf("topPercent on empty = %d, want 0", got)
		}
	})
	t.Run("rounds up to at least 1", func(t *testing.T) {
		big := make([]int, 1000)
		big[999] = 5
		if got := topPercent(big, 5); got != 1 {
			t.Errorf("1 of 1000 = %d, want 1", got)
		}
	})
}

func TestBuildRepoStats(t *testing.T) {
	hm := &types.Heatmap{
		Metadata: types.Metadata{LikelyCutoff: 33, ImpactThreshold: 61},
		Files:    map[string]*types.FileHeat{},
	}
	for i := 1; i <= 10; i++ {
		p := fmt.Sprintf("f%d.go", i)
		f := &types.FileHeat{Path: p}
		f.Factors.ChangeFrequency.CommitsLast90d = i
		f.Factors.ChangeFrequency.UniqueAuthors = i
		f.Factors.Complexity.Cyclomatic = i * 10
		f.Factors.Complexity.FunctionCount = i * 2
		f.Size.Lines = i * 100
		for j := 0; j < i; j++ {
			f.Dependencies.ImportedBy = append(f.Dependencies.ImportedBy, types.Dependency{Path: "x"})
		}
		hm.Files[p] = f
	}
	// A test file with huge numbers must not shift the percentiles.
	tf := &types.FileHeat{Path: "f_test.go"}
	tf.Factors.ChangeFrequency.CommitsLast90d = 1000
	hm.Files[tf.Path] = tf

	s := BuildRepoStats(hm, func(p string) bool { return strings.HasSuffix(p, "_test.go") })
	if s.LikelyCutoff != 33 || s.ImpactThreshold != 61 {
		t.Errorf("thresholds = %d/%d, want 33/61", s.LikelyCutoff, s.ImpactThreshold)
	}
	tests := []struct {
		name   string
		sorted []int
		v      int
		want   int
	}{
		{"churn", s.churn, 10, 10},
		{"authors", s.authors, 9, 20},
		{"cyclomatic", s.cyclomatic, 100, 10},
		{"functions", s.functions, 2, 100},
		{"importers", s.importers, 10, 10},
		{"lines", s.lines, 1000, 10},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := topPercent(tt.sorted, tt.v); got != tt.want {
				t.Errorf("top%% = %d, want %d", got, tt.want)
			}
		})
	}
	if !s.isTest("a_test.go") || s.isTest("a.go") {
		t.Error("isTest predicate not kept")
	}
}

func TestBuildRepoStats_NilPredicate(t *testing.T) {
	hm := &types.Heatmap{Files: map[string]*types.FileHeat{"a.go": {Path: "a.go"}}}
	s := BuildRepoStats(hm, nil)
	if len(s.churn) != 1 || s.isTest("a_test.go") {
		t.Errorf("nil predicate should count every file and treat none as tests")
	}
}
