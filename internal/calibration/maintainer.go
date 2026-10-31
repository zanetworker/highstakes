package calibration

import (
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"sort"
	"strconv"
	"strings"

	"github.com/zanetworker/highstakes/internal/tour"
	"github.com/zanetworker/highstakes/internal/types"
)

// SampleForLabels picks files for a blind maintainer labelling sheet: the top
// files by blast radius (by heat when no file has an LLM assessment) plus
// random others drawn from the remaining non-test files, shuffled together so
// the order does not reveal which group a file came from. The same seed gives
// the same sheet. rankedBy is "blast_radius" or "heat".
func SampleForLabels(hm *types.Heatmap, top, random int, seed int64) (paths []string, rankedBy string) {
	var files []*types.FileHeat
	assessed := false
	for p, fh := range hm.Files {
		if tour.IsTestFile(p) {
			continue
		}
		files = append(files, fh)
		assessed = assessed || fh.Factors.BlastRadius.Assessed
	}
	rankedBy = "heat"
	score := func(f *types.FileHeat) int { return f.HeatScore }
	if assessed {
		rankedBy = "blast_radius"
		score = func(f *types.FileHeat) int {
			if !f.Factors.BlastRadius.Assessed {
				return -1
			}
			return f.Factors.BlastRadius.Score
		}
	}
	sort.Slice(files, func(i, j int) bool {
		if si, sj := score(files[i]), score(files[j]); si != sj {
			return si > sj
		}
		return files[i].Path < files[j].Path
	})

	top = min(max(top, 0), len(files))
	for _, fh := range files[:top] {
		paths = append(paths, fh.Path)
	}
	rng := rand.New(rand.NewSource(seed))
	rest := files[top:]
	for _, i := range rng.Perm(len(rest))[:min(max(random, 0), len(rest))] {
		paths = append(paths, rest[i].Path)
	}
	rng.Shuffle(len(paths), func(i, j int) { paths[i], paths[j] = paths[j], paths[i] })
	return paths, rankedBy
}

// LabelSheetHeader is the header row of a maintainer labelling sheet.
var LabelSheetHeader = []string{"path", "maintainer_rating", "notes"}

// WriteLabelSheet writes a blank labelling sheet: one row per path with empty
// rating and notes. It carries no HighStakes scores, so labelling stays blind.
func WriteLabelSheet(w io.Writer, paths []string) error {
	cw := csv.NewWriter(w)
	if err := cw.Write(LabelSheetHeader); err != nil {
		return err
	}
	for _, p := range paths {
		if err := cw.Write([]string{p, "", ""}); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}

// Label is one maintainer rating: 1 (harmless) to 5 (a bug here would be a
// serious incident).
type Label struct {
	Path   string `json:"path"`
	Rating int    `json:"rating"`
}

// ReadLabels parses a filled labelling sheet. Columns are found by header
// name (case-insensitive, any order, extra columns allowed); rows with a blank
// rating are skipped. Errors name the CSV line.
func ReadLabels(r io.Reader) ([]Label, error) {
	cr := csv.NewReader(r)
	cr.TrimLeadingSpace = true
	header, err := cr.Read()
	if errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("missing header row (want %s)", strings.Join(LabelSheetHeader, ","))
	}
	if err != nil {
		return nil, err
	}
	pathCol, ratingCol := -1, -1
	for i, h := range header {
		switch strings.ToLower(strings.TrimSpace(h)) {
		case "path":
			pathCol = i
		case "maintainer_rating":
			ratingCol = i
		}
	}
	if pathCol < 0 || ratingCol < 0 {
		return nil, fmt.Errorf("line 1: header needs path and maintainer_rating columns, got %q", strings.Join(header, ","))
	}

	var out []Label
	seen := map[string]bool{}
	for {
		rec, err := cr.Read()
		if errors.Is(err, io.EOF) {
			return out, nil
		}
		if err != nil {
			return nil, err
		}
		line, _ := cr.FieldPos(0)
		path, raw := strings.TrimSpace(rec[pathCol]), strings.TrimSpace(rec[ratingCol])
		if raw == "" {
			continue
		}
		if path == "" {
			return nil, fmt.Errorf("line %d: empty path with rating %q", line, raw)
		}
		rating, err := strconv.Atoi(raw)
		if err != nil || rating < 1 || rating > 5 {
			return nil, fmt.Errorf("line %d: rating %q for %s must be 1-5", line, raw, path)
		}
		if seen[path] {
			return nil, fmt.Errorf("line %d: duplicate path %s", line, path)
		}
		seen[path] = true
		out = append(out, Label{Path: path, Rating: rating})
	}
}

// RatingPositive is the lowest maintainer rating that counts as high stakes
// for precision@K.
const RatingPositive = 4

// LabelReport compares HighStakes scores with maintainer ratings.
type LabelReport struct {
	Rated     int           `json:"rated"`     // rows with a rating
	Matched   int           `json:"matched"`   // rated files found in the heatmap
	Unmatched []string      `json:"unmatched"` // rated paths not in the heatmap
	Positives int           `json:"positives"` // matched files rated >= RatingPositive
	BaseRate  float64       `json:"base_rate"` // Positives / Matched
	K         int           `json:"k"`
	TooFew    bool          `json:"too_few"` // Matched under MinFilesWithEvents
	Signals   []LabelSignal `json:"signals"`
}

// LabelSignal is how well one score ranks files by maintainer rating.
type LabelSignal struct {
	Name         string   `json:"name"`
	Available    bool     `json:"available"`
	Note         string   `json:"note,omitempty"`
	Files        int      `json:"files"`
	PrecisionAtK float64  `json:"precision_at_k"` // share of the top K rated >= RatingPositive
	Lift         float64  `json:"lift"`
	Spearman     *float64 `json:"spearman"` // score vs rating; nil when undefined
}

// ScoreLabels reports Spearman (score vs rating) and precision@K (positive =
// rating >= RatingPositive) for blast radius and heat over the rated files.
func ScoreLabels(hm *types.Heatmap, labels []Label, k int) *LabelReport {
	r := &LabelReport{Rated: len(labels), K: k, Unmatched: []string{}}
	type rated struct {
		fh     *types.FileHeat
		rating int
	}
	var matched []rated
	for _, l := range labels {
		fh, ok := hm.Files[l.Path]
		if !ok {
			r.Unmatched = append(r.Unmatched, l.Path)
			continue
		}
		matched = append(matched, rated{fh, l.Rating})
		if l.Rating >= RatingPositive {
			r.Positives++
		}
	}
	r.Matched = len(matched)
	if r.Matched > 0 {
		r.BaseRate = float64(r.Positives) / float64(r.Matched)
	}
	r.TooFew = r.Matched < MinFilesWithEvents

	for _, name := range []string{"blast_radius", "heat"} {
		s := LabelSignal{Name: name, Available: true}
		var items []Item
		var xs, ys []float64
		for _, m := range matched {
			score := float64(m.fh.HeatScore)
			if name == "blast_radius" {
				if !m.fh.Factors.BlastRadius.Assessed {
					continue
				}
				score = float64(m.fh.Factors.BlastRadius.Score)
			}
			positive := 0
			if m.rating >= RatingPositive {
				positive = 1
			}
			items = append(items, Item{Path: m.fh.Path, Score: score, Events: positive})
			xs, ys = append(xs, score), append(ys, float64(m.rating))
		}
		s.Files = len(items)
		if name == "blast_radius" && s.Files == 0 && r.Matched > 0 {
			s.Available = false
			s.Note = "no rated file has an LLM assessment (analyzed with --no-llm or no API key)"
		}
		s.PrecisionAtK = PrecisionAtK(items, k)
		s.Lift = Lift(s.PrecisionAtK, BaseRate(items))
		if rho, ok := Spearman(xs, ys); ok {
			s.Spearman = &rho
		}
		r.Signals = append(r.Signals, s)
	}
	return r
}
