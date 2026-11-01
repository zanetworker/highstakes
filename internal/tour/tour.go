// Package tour builds a reading path through a heatmap for learning a
// codebase: directories in dependency order (dependencies first), the most
// impactful files first within each level. It makes no LLM calls and reads
// only the heatmap.
package tour

import (
	"errors"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"

	"github.com/zanetworker/highstakes/internal/types"
)

// SchemaVersion is the version of the tour JSON shape.
const SchemaVersion = "1"

// maxNeighbours caps depends_on / used_by lists per stop (hottest kept).
const maxNeighbours = 5

// maxOwners caps the owners list per stop.
const maxOwners = 3

var (
	// ErrNoFiles means no heatmap file matched the options.
	ErrNoFiles = errors.New("no files match")
	// ErrInvalidLimit means a negative limit was given.
	ErrInvalidLimit = errors.New("limit must be >= 0")
)

// Options controls tour selection.
type Options struct {
	Limit int    // keep the N most impactful files; 0 = all
	Focus string // directory or file path prefix; "" or "." = whole repo
	// Source optionally returns a file's content so stops get a line hint
	// pointing at the first top-level declaration. Errors leave line 1.
	Source func(path string) ([]byte, error)
}

// Tour is the reading path. Sections are read in order; stops within a
// section are read in order. Stop.Order is the global 1-based position.
type Tour struct {
	Version    string    `json:"version"`
	Repo       string    `json:"repo"`
	Commit     string    `json:"commit"`
	Branch     string    `json:"branch,omitempty"`
	AnalyzedAt string    `json:"analyzed_at,omitempty"`
	Focus      string    `json:"focus,omitempty"`
	Ordering   string    `json:"ordering"`
	TotalStops int       `json:"total_stops"`
	Sections   []Section `json:"sections"`
}

// Section groups the stops of one directory (Go package).
type Section struct {
	Order     int        `json:"order"`
	Title     string     `json:"title"`
	Dir       string     `json:"dir"`
	Level     int        `json:"level"` // 0 = depends on no other directory
	MaxHeat   int        `json:"max_heat"`
	MaxImpact int        `json:"max_impact"`
	Tier      types.Tier `json:"tier"`       // most severe tier among its stops
	DependsOn []string   `json:"depends_on"` // directories this one imports
	UsedBy    []string   `json:"used_by"`    // directories importing this one
	Stops     []Stop     `json:"stops"`
}

// Stop is one file to read.
type Stop struct {
	Order          int            `json:"order"`
	Path           string         `json:"path"`
	Line           int            `json:"line"`
	Anchor         string         `json:"anchor"` // "path:line"
	Title          string         `json:"title"`
	Heat           int            `json:"heat"`
	Tier           types.Tier     `json:"tier"`
	Impact         int            `json:"impact"`
	Likelihood     int            `json:"likelihood"`
	ImpactAssessed bool           `json:"impact_assessed"`
	Quadrant       types.Quadrant `json:"quadrant,omitempty"`
	Why            string         `json:"why"`
	Summary        string         `json:"summary,omitempty"`
	CriticalReason string         `json:"critical_reason,omitempty"`
	TopImpact      string         `json:"top_impact,omitempty"`
	DependsOn      []string       `json:"depends_on"`
	DependsOnCount int            `json:"depends_on_count"`
	UsedBy         []string       `json:"used_by"`
	UsedByCount    int            `json:"used_by_count"`
	Owners         []string       `json:"owners"`
	Notes          string         `json:"notes,omitempty"`
}

// Dir returns the directory of a slash-separated repo path ("." for root files).
func Dir(p string) string { return path.Dir(p) }

// IsTestFile reports whether a path is a test file by its name. Tests are
// left out of tours and the package graph: they import everything and teach
// little about where the stakes are.
func IsTestFile(p string) bool {
	base := path.Base(p)
	for _, s := range []string{"_test.go", ".test.ts", ".test.js", ".spec.ts", ".spec.js", "_test.py"} {
		if strings.HasSuffix(base, s) {
			return true
		}
	}
	return strings.HasPrefix(base, "test_")
}

// DirGraph aggregates the file import graph to directories:
// graph[a][b] = number of distinct files in a that import a file in b.
// Same-directory imports and importers missing from the heatmap are skipped.
func DirGraph(hm *types.Heatmap) map[string]map[string]int {
	seen := map[[3]string]bool{}
	g := map[string]map[string]int{}
	for _, f := range hm.Files {
		to := Dir(f.Path)
		if IsTestFile(f.Path) {
			continue
		}
		for _, dep := range f.Dependencies.ImportedBy {
			if _, ok := hm.Files[dep.Path]; !ok || IsTestFile(dep.Path) {
				continue
			}
			from := Dir(dep.Path)
			key := [3]string{dep.Path, from, to}
			if from == to || seen[key] {
				continue
			}
			seen[key] = true
			if g[from] == nil {
				g[from] = map[string]int{}
			}
			g[from][to]++
		}
	}
	return g
}

// Levels assigns each node its dependency depth: 0 when it depends on
// nothing, else 1 + the deepest dependency. Nodes in an import cycle form one
// strongly connected component and share a level.
func Levels(nodes []string, graph map[string]map[string]int) map[string]int {
	all := map[string]bool{}
	for _, n := range nodes {
		all[n] = true
	}
	for a, tos := range graph {
		all[a] = true
		for b := range tos {
			all[b] = true
		}
	}
	names := make([]string, 0, len(all))
	for n := range all {
		names = append(names, n)
	}
	sort.Strings(names)

	comp := sccs(names, graph)
	memo := map[int]int{}
	var level func(c int, members map[int][]string) int
	level = func(c int, members map[int][]string) int {
		if v, ok := memo[c]; ok {
			return v
		}
		best := 0
		for _, n := range members[c] {
			for dep := range graph[n] {
				if dc := comp[dep]; dc != c {
					if l := level(dc, members) + 1; l > best {
						best = l
					}
				}
			}
		}
		memo[c] = best
		return best
	}
	members := map[int][]string{}
	for _, n := range names {
		members[comp[n]] = append(members[comp[n]], n)
	}

	out := make(map[string]int, len(nodes))
	for _, n := range nodes {
		out[n] = level(comp[n], members)
	}
	return out
}

// sccs labels each node with its strongly connected component (Tarjan).
func sccs(names []string, graph map[string]map[string]int) map[string]int {
	index, low := map[string]int{}, map[string]int{}
	onStack := map[string]bool{}
	var stack []string
	comp := map[string]int{}
	next, compID := 0, 0

	var visit func(v string)
	visit = func(v string) {
		index[v], low[v] = next, next
		next++
		stack = append(stack, v)
		onStack[v] = true
		succ := make([]string, 0, len(graph[v]))
		for w := range graph[v] {
			succ = append(succ, w)
		}
		sort.Strings(succ)
		for _, w := range succ {
			if _, ok := index[w]; !ok {
				visit(w)
				low[v] = min(low[v], low[w])
			} else if onStack[w] {
				low[v] = min(low[v], index[w])
			}
		}
		if low[v] == index[v] {
			for {
				w := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				onStack[w] = false
				comp[w] = compID
				if w == v {
					break
				}
			}
			compID++
		}
	}
	for _, n := range names {
		if _, ok := index[n]; !ok {
			visit(n)
		}
	}
	return comp
}

// inFocus reports whether p is the focus path or inside the focus directory.
func inFocus(p, focus string) bool {
	focus = strings.TrimSuffix(strings.TrimPrefix(focus, "./"), "/")
	if focus == "" || focus == "." {
		return true
	}
	return p == focus || strings.HasPrefix(p, focus+"/")
}

// moreImpactful orders files by impact, then centrality, then heat (for
// heatmaps written before impact existed), then path.
func moreImpactful(a, b *types.FileHeat) bool {
	if a.Impact != b.Impact {
		return a.Impact > b.Impact
	}
	if ac, bc := a.Factors.DependencyCentrality.Score, b.Factors.DependencyCentrality.Score; ac != bc {
		return ac > bc
	}
	if a.HeatScore != b.HeatScore {
		return a.HeatScore > b.HeatScore
	}
	return a.Path < b.Path
}

func hotter(a, b *types.FileHeat) bool {
	if a.HeatScore != b.HeatScore {
		return a.HeatScore > b.HeatScore
	}
	return a.Path < b.Path
}

var tierRank = map[types.Tier]int{types.TierLow: 0, types.TierMedium: 1, types.TierHigh: 2, types.TierCritical: 3}

// Build creates the tour. It returns ErrInvalidLimit for a negative limit and
// ErrNoFiles when nothing matches the focus.
func Build(hm *types.Heatmap, opts Options) (*Tour, error) {
	if opts.Limit < 0 {
		return nil, fmt.Errorf("%w: got %d", ErrInvalidLimit, opts.Limit)
	}

	var picked []*types.FileHeat
	for _, f := range hm.Files {
		if inFocus(f.Path, opts.Focus) && !IsTestFile(f.Path) {
			picked = append(picked, f)
		}
	}
	if len(picked) == 0 {
		return nil, fmt.Errorf("%w focus %q", ErrNoFiles, opts.Focus)
	}
	sort.Slice(picked, func(i, j int) bool { return moreImpactful(picked[i], picked[j]) })
	if opts.Limit > 0 && len(picked) > opts.Limit {
		picked = picked[:opts.Limit]
	}

	// Levels come from the whole repo graph so transitive order holds even
	// when intermediate directories were not selected.
	graph := DirGraph(hm)
	usedByDir := map[string][]string{}
	for from, tos := range graph {
		for to := range tos {
			usedByDir[to] = append(usedByDir[to], from)
		}
	}
	imports := map[string][]*types.FileHeat{} // file -> files it imports
	for _, f := range hm.Files {
		if IsTestFile(f.Path) {
			continue
		}
		for _, dep := range f.Dependencies.ImportedBy {
			imports[dep.Path] = append(imports[dep.Path], f)
		}
	}

	byDir := map[string]*Section{}
	var dirs []string
	for _, f := range picked {
		d := Dir(f.Path)
		s := byDir[d]
		if s == nil {
			s = &Section{Title: d, Dir: d, MaxImpact: f.Impact, Tier: f.Tier}
			byDir[d] = s
			dirs = append(dirs, d)
		}
		s.MaxHeat = max(s.MaxHeat, f.HeatScore)
		if tierRank[f.Tier] > tierRank[s.Tier] {
			s.Tier = f.Tier
		}
		byDir[d].Stops = append(byDir[d].Stops, newStop(hm, f, imports[f.Path], opts.Source))
	}
	levels := Levels(dirs, graph)

	sections := make([]Section, 0, len(dirs))
	for _, d := range dirs {
		s := byDir[d]
		s.Level = levels[d]
		s.DependsOn = sortedKeys(graph[d])
		s.UsedBy = sortedCopy(usedByDir[d])
		sections = append(sections, *s)
	}
	sort.Slice(sections, func(i, j int) bool {
		a, b := sections[i], sections[j]
		if a.Level != b.Level {
			return a.Level < b.Level
		}
		if a.MaxImpact != b.MaxImpact {
			return a.MaxImpact > b.MaxImpact
		}
		if a.MaxHeat != b.MaxHeat {
			return a.MaxHeat > b.MaxHeat
		}
		return a.Dir < b.Dir
	})

	order := 0
	for i := range sections {
		sections[i].Order = i + 1
		for j := range sections[i].Stops {
			order++
			sections[i].Stops[j].Order = order
		}
	}

	return &Tour{
		Version:    SchemaVersion,
		Repo:       hm.Metadata.RepoPath,
		Commit:     hm.Metadata.CommitSHA,
		Branch:     hm.Metadata.Branch,
		AnalyzedAt: formatTime(hm),
		Focus:      opts.Focus,
		Ordering:   "sections by dependency level (dependencies first), then max impact; stops by impact, then centrality",
		TotalStops: order,
		Sections:   sections,
	}, nil
}

func formatTime(hm *types.Heatmap) string {
	if hm.Metadata.AnalyzedAt.IsZero() {
		return ""
	}
	return hm.Metadata.AnalyzedAt.Format("2006-01-02T15:04:05Z07:00")
}

func newStop(hm *types.Heatmap, f *types.FileHeat, deps []*types.FileHeat, source func(string) ([]byte, error)) Stop {
	br := f.Factors.BlastRadius
	ann := hm.Annotations[f.Path]

	var users []*types.FileHeat
	for _, d := range f.Dependencies.ImportedBy {
		if u, ok := hm.Files[d.Path]; ok && !IsTestFile(d.Path) {
			users = append(users, u)
		}
	}

	line := 1
	if source != nil {
		if content, err := source(f.Path); err == nil {
			line = LineHint(string(content))
		}
	}

	st := Stop{
		Path:           f.Path,
		Line:           line,
		Anchor:         fmt.Sprintf("%s:%d", f.Path, line),
		Title:          path.Base(f.Path),
		Heat:           f.HeatScore,
		Tier:           f.Tier,
		Impact:         f.Impact,
		Likelihood:     f.Likelihood,
		ImpactAssessed: f.ImpactAssessed,
		Quadrant:       f.Quadrant,
		Why:            why(f),
		TopImpact:      topImpact(br),
		DependsOn:      hottestPaths(deps),
		DependsOnCount: len(deps),
		UsedBy:         hottestPaths(users),
		UsedByCount:    len(users),
		Owners:         owners(f, ann.Owner),
		Notes:          ann.Notes,
	}
	if st.Owners == nil {
		st.Owners = []string{}
	}
	if br.Assessed {
		st.Summary = br.Summary
		st.CriticalReason = br.CriticalReason
	}
	return st
}

// why explains why a file matters: the LLM critical reason, else its
// summary, else the static signals that raised its heat.
func why(f *types.FileHeat) string {
	br := f.Factors.BlastRadius
	if br.Assessed && br.CriticalReason != "" {
		return br.CriticalReason
	}
	if br.Assessed && br.Summary != "" {
		return br.Summary
	}
	var parts []string
	if n := len(f.Dependencies.ImportedBy); n > 0 {
		parts = append(parts, fmt.Sprintf("imported by %d %s", n, plural(n, "file")))
	}
	if n := f.Factors.ChangeFrequency.CommitsLast90d; n > 0 {
		parts = append(parts, fmt.Sprintf("%d commits in 90d", n))
	}
	if n := f.Factors.IncidentHistory.IncidentCount; n > 0 {
		parts = append(parts, fmt.Sprintf("%d incidents", n))
	}
	if len(parts) == 0 {
		return "no blast radius assessment; run analyze with an LLM key"
	}
	return strings.Join(parts, "; ")
}

func plural(n int, word string) string {
	if n == 1 {
		return word
	}
	return word + "s"
}

func topImpact(br types.BlastRadius) string {
	if !br.Assessed {
		return ""
	}
	best, name := 0, ""
	for _, d := range []struct {
		n string
		v int
	}{{"security", br.SecurityImpact}, {"data", br.DataImpact}, {"availability", br.AvailabilityImpact}, {"user", br.UserImpact}} {
		if d.v > best {
			best, name = d.v, d.n
		}
	}
	return name
}

// owners ranks recent commit authors by commit count (ties by name), with the
// annotated owner first. At most maxOwners entries; nil when none are known.
func owners(f *types.FileHeat, annotated string) []string {
	counts := map[string]int{}
	for _, c := range f.RecentChanges {
		if c.Author != "" {
			counts[c.Author]++
		}
	}
	authors := make([]string, 0, len(counts))
	for a := range counts {
		if a != annotated {
			authors = append(authors, a)
		}
	}
	sort.Slice(authors, func(i, j int) bool {
		if counts[authors[i]] != counts[authors[j]] {
			return counts[authors[i]] > counts[authors[j]]
		}
		return authors[i] < authors[j]
	})
	var out []string
	if annotated != "" {
		out = append(out, annotated)
	}
	out = append(out, authors...)
	if len(out) > maxOwners {
		out = out[:maxOwners]
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func hottestPaths(fs []*types.FileHeat) []string {
	sorted := append([]*types.FileHeat(nil), fs...)
	sort.Slice(sorted, func(i, j int) bool { return hotter(sorted[i], sorted[j]) })
	out := []string{}
	for i, f := range sorted {
		if i == maxNeighbours {
			break
		}
		out = append(out, f.Path)
	}
	return out
}

func sortedKeys(m map[string]int) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func sortedCopy(s []string) []string {
	out := append([]string{}, s...)
	sort.Strings(out)
	return out
}

// declRe matches a top-level declaration in common languages.
var declRe = regexp.MustCompile(`^(func|type|class|def|async def|export|fn|pub|interface|struct|impl|public|abstract|final|module)\b`)

// LineHint returns the 1-based line of the first top-level declaration in
// content, or 1 when there is none.
func LineHint(content string) int {
	for i, line := range strings.Split(content, "\n") {
		if declRe.MatchString(line) {
			return i + 1
		}
	}
	return 1
}
