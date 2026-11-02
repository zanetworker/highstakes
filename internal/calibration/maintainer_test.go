package calibration

import (
	"bytes"
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/zanetworker/highstakes/internal/types"
)

// sampleHeatmap has n source files f00..f(n-1) with heat i and, when assessed,
// blast radius (n-1-i), so the two rankings point in opposite directions.
func sampleHeatmap(n int, assessed bool) *types.Heatmap {
	hm := &types.Heatmap{Files: map[string]*types.FileHeat{}}
	for i := 0; i < n; i++ {
		p := fmt.Sprintf("f%02d.go", i)
		fh := fileHeat(p, i, types.TierLow, 0, 0, 0)
		fh.Factors.BlastRadius = types.BlastRadius{Score: n - 1 - i, Assessed: assessed}
		hm.Files[p] = fh
	}
	hm.Files["f00_test.go"] = fileHeat("f00_test.go", 100, types.TierLow, 0, 0, 0)
	return hm
}

func TestSampleForLabels(t *testing.T) {
	sorted := func(ps []string) []string {
		out := append([]string(nil), ps...)
		sort.Strings(out)
		return out
	}
	tests := []struct {
		name         string
		hm           *types.Heatmap
		top, random  int
		wantRankedBy string
		wantTop      []string // must all be present
		wantLen      int
	}{
		{"blast radius ranks when assessed", sampleHeatmap(10, true), 2, 3, "blast_radius", []string{"f00.go", "f01.go"}, 5},
		{"heat fallback without assessment", sampleHeatmap(10, false), 2, 3, "heat", []string{"f09.go", "f08.go"}, 5},
		{"random capped by remaining files", sampleHeatmap(4, true), 2, 10, "blast_radius", []string{"f00.go", "f01.go"}, 4},
		{"top capped by population", sampleHeatmap(3, true), 10, 10, "blast_radius", []string{"f00.go", "f01.go", "f02.go"}, 3},
		{"zero random", sampleHeatmap(5, true), 1, 0, "blast_radius", []string{"f00.go"}, 1},
		{"empty heatmap", &types.Heatmap{Files: map[string]*types.FileHeat{}}, 5, 5, "heat", nil, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			paths, rankedBy := SampleForLabels(tt.hm, tt.top, tt.random, 7)
			if rankedBy != tt.wantRankedBy || len(paths) != tt.wantLen {
				t.Fatalf("got %v ranked by %s, want %d paths ranked by %s", paths, rankedBy, tt.wantLen, tt.wantRankedBy)
			}
			seen := map[string]bool{}
			for _, p := range paths {
				if seen[p] {
					t.Errorf("duplicate %s", p)
				}
				if strings.HasSuffix(p, "_test.go") {
					t.Errorf("test file %s sampled", p)
				}
				seen[p] = true
			}
			for _, p := range tt.wantTop {
				if !seen[p] {
					t.Errorf("top file %s missing from %v", p, paths)
				}
			}
		})
	}

	t.Run("same seed is reproducible, other seed differs", func(t *testing.T) {
		hm := sampleHeatmap(40, true)
		a, _ := SampleForLabels(hm, 5, 5, 1)
		b, _ := SampleForLabels(hm, 5, 5, 1)
		c, _ := SampleForLabels(hm, 5, 5, 2)
		if strings.Join(a, ",") != strings.Join(b, ",") {
			t.Errorf("seed 1 twice: %v vs %v", a, b)
		}
		if strings.Join(sorted(a), ",") == strings.Join(sorted(c), ",") && strings.Join(a, ",") == strings.Join(c, ",") {
			t.Errorf("seeds 1 and 2 gave the same sheet %v", a)
		}
	})

	t.Run("top files are not listed first (blind order)", func(t *testing.T) {
		hm := sampleHeatmap(40, true)
		firstTop := 0
		for seed := int64(0); seed < 20; seed++ {
			paths, _ := SampleForLabels(hm, 5, 5, seed)
			if paths[0] <= "f04.go" {
				firstTop++
			}
		}
		if firstTop == 20 {
			t.Error("top files always come first; the sheet must be shuffled")
		}
	})
}

func TestWriteLabelSheet(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteLabelSheet(&buf, []string{"a.go", "odd,name.go"}); err != nil {
		t.Fatal(err)
	}
	want := "path,maintainer_rating,notes\na.go,,\n\"odd,name.go\",,\n"
	if buf.String() != want {
		t.Errorf("sheet =\n%q\nwant\n%q", buf.String(), want)
	}
	labels, err := ReadLabels(&buf)
	if err != nil || len(labels) != 0 {
		t.Errorf("blank sheet read back = %v, %v; want no labels", labels, err)
	}
}

func TestReadLabels(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    []Label
		wantErr string // substring; empty means success
	}{
		{"filled with blanks and notes", "path,maintainer_rating,notes\na.go,5,\"auth, tokens\"\nb.go,,\nc.go, 1 ,x\n",
			[]Label{{Path: "a.go", Rating: 5}, {Path: "c.go", Rating: 1}}, ""},
		{"columns in any order, extra column", "notes,Maintainer_Rating,owner,Path\nn,4,me,a.go\n", []Label{{Path: "a.go", Rating: 4}}, ""},
		{"header only", "path,maintainer_rating,notes\n", nil, ""},
		{"empty file", "", nil, "missing header"},
		{"missing rating column", "path,notes\na.go,x\n", nil, "line 1: header needs path and maintainer_rating columns"},
		{"rating above 5", "path,maintainer_rating\na.go,3\nb.go,6\n", nil, "line 3: rating \"6\" for b.go must be 1-5"},
		{"rating zero", "path,maintainer_rating\na.go,0\n", nil, "line 2: rating \"0\" for a.go must be 1-5"},
		{"rating not a number", "path,maintainer_rating\na.go,high\n", nil, "line 2: rating \"high\""},
		{"blank path with rating", "path,maintainer_rating\n,4\n", nil, "line 2: empty path"},
		{"duplicate path", "path,maintainer_rating\na.go,4\na.go,2\n", nil, "line 3: duplicate path a.go"},
		{"unterminated quote", "path,maintainer_rating\n\"a.go,4\n", nil, "line 2"},
		{"short row", "path,maintainer_rating,notes\na.go\n", nil, "line 2"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ReadLabels(strings.NewReader(tt.in))
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if fmt.Sprint(got) != fmt.Sprint(tt.want) {
				t.Errorf("labels = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestScoreLabels(t *testing.T) {
	labels := []Label{
		{"f00.go", 5}, {"f01.go", 4}, {"f02.go", 3}, {"f05.go", 2}, {"f09.go", 1}, {"gone.go", 5},
	}
	sig := func(r *LabelReport, name string) LabelSignal {
		for _, s := range r.Signals {
			if s.Name == name {
				return s
			}
		}
		t.Fatalf("signal %s missing", name)
		return LabelSignal{}
	}

	t.Run("blast radius agrees, heat disagrees", func(t *testing.T) {
		r := ScoreLabels(sampleHeatmap(10, true), labels, 2)
		if r.Rated != 6 || r.Matched != 5 || r.Positives != 2 || !near(r.BaseRate, 0.4) || fmt.Sprint(r.Unmatched) != "[gone.go]" {
			t.Fatalf("header = %+v", r)
		}
		br, heat := sig(r, "blast_radius"), sig(r, "heat")
		if !br.Available || br.Files != 5 || br.Spearman == nil || !near(*br.Spearman, 1) || !near(br.PrecisionAtK, 1) || !near(br.Lift, 2.5) {
			t.Errorf("blast_radius = %+v", br)
		}
		if heat.Spearman == nil || !near(*heat.Spearman, -1) || heat.PrecisionAtK != 0 {
			t.Errorf("heat = %+v", heat)
		}
		if !r.TooFew {
			t.Error("5 matched files is under MinFilesWithEvents and must be flagged")
		}
	})

	t.Run("no assessment", func(t *testing.T) {
		r := ScoreLabels(sampleHeatmap(10, false), labels, 2)
		if br := sig(r, "blast_radius"); br.Available || br.Note == "" {
			t.Errorf("blast_radius without assessment = %+v", br)
		}
		if heat := sig(r, "heat"); !heat.Available || heat.Files != 5 {
			t.Errorf("heat = %+v", heat)
		}
	})

	t.Run("no labels", func(t *testing.T) {
		r := ScoreLabels(sampleHeatmap(10, true), nil, 2)
		if r.Rated != 0 || r.Matched != 0 || !r.TooFew {
			t.Fatalf("empty = %+v", r)
		}
		if s := sig(r, "heat"); s.Spearman != nil || s.PrecisionAtK != 0 {
			t.Errorf("heat with no labels = %+v", s)
		}
	})

	t.Run("enough labels clears the flag", func(t *testing.T) {
		var many []Label
		for i := 0; i < MinFilesWithEvents; i++ {
			many = append(many, Label{fmt.Sprintf("f%02d.go", i), 1 + i%5})
		}
		if r := ScoreLabels(sampleHeatmap(20, true), many, 3); r.TooFew || r.Matched != MinFilesWithEvents {
			t.Errorf("10 matched: TooFew=%v matched=%d", r.TooFew, r.Matched)
		}
	})
}
