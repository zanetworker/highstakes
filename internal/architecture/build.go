package architecture

import (
	"errors"
	"fmt"
	"math"
	"path"
	"regexp"
	"sort"
	"strings"

	"github.com/zanetworker/highstakes/internal/tour"
	"github.com/zanetworker/highstakes/internal/types"
	"github.com/zanetworker/highstakes/internal/units"
)

// Node kinds.
const (
	KindUnit    = "unit"
	KindLibrary = "library"
	KindOther   = "other"
)

// Edge and group sources: imports and manifests are inferred, declared
// comes from .heatmap/architecture.yaml.
const (
	SourceImports  = "imports"
	SourceDeclared = "declared"
	SourceInferred = "inferred"
)

const (
	defaultMaxNodes = 9
	maxMaxNodes     = 24
	filesPerNode    = 3
	otherID         = "other"
)

var (
	// ErrNoUnits means the heatmap has no runtime units.
	ErrNoUnits = errors.New("no runtime units")
	// ErrOnlySupporting means every unit is supporting and they are hidden.
	ErrOnlySupporting = errors.New("only supporting units")
	// ErrInvalidMaxNodes means MaxNodes is outside 2-24.
	ErrInvalidMaxNodes = errors.New("max nodes must be 2-24")
)

// Options controls Build.
type Options struct {
	MaxNodes          int  // 0 = 9; 2-24
	MaxEdges          int  // 0 = the SeeCode budget for MaxNodes (12 up to 9 nodes)
	IncludeSupporting bool // keep supporting units (examples, helper bins)
	// Source optionally returns a file's content, for the evidence line
	// (first top-level declaration). Errors leave line 1.
	Source func(path string) ([]byte, error)
}

// Diagram is a generated architecture picture: units as nodes, imports and
// declared links as edges, placements as groups.
type Diagram struct {
	Title            string  `json:"title"`
	Repo             string  `json:"repo"`
	Commit           string  `json:"commit"`
	Nodes            []Node  `json:"nodes"`
	Edges            []Edge  `json:"edges"`
	Groups           []Group `json:"groups"`
	ProductUnits     int     `json:"product_units"`     // product units in the heatmap
	SupportingHidden int     `json:"supporting_hidden"` // supporting units left out
	DroppedEdges     int     `json:"dropped_edges"`     // edges over the budget, left out
}

// Node is one box: a unit, the most shared library, or the other units.
type Node struct {
	ID              string    `json:"id"`
	Label           string    `json:"label"`
	Sub             string    `json:"sub"`
	Tag             string    `json:"tag,omitempty"`
	Kind            string    `json:"kind"`              // KindUnit, KindLibrary, KindOther
	Members         []string  `json:"members,omitempty"` // other: unit names; library: component
	MaxImpact       int       `json:"max_impact"`
	ImpactAssessed  bool      `json:"impact_assessed"`
	FragileCritical int       `json:"fragile_critical"`
	Focal           bool      `json:"focal,omitempty"`
	Group           string    `json:"group,omitempty"`
	Row             int       `json:"row"`
	Col             int       `json:"col"`
	Files           []FileRef `json:"files"` // top files by impact, the node's evidence
}

// FileRef is one file behind a node. Note is the quadrant and first action.
type FileRef struct {
	Path     string         `json:"path"`
	Line     int            `json:"line"`
	Impact   int            `json:"impact"`
	Quadrant types.Quadrant `json:"quadrant,omitempty"`
	Note     string         `json:"note"`
}

// Edge is an import link (From's files import To's; Count importing files)
// or a declared runtime link.
type Edge struct {
	From     string `json:"from"`
	To       string `json:"to"`
	Label    string `json:"label"`
	Source   string `json:"source"` // SourceImports or SourceDeclared
	Count    int    `json:"count,omitempty"`
	Both     bool   `json:"both,omitempty"`
	Evidence string `json:"evidence,omitempty"`
}

// Group is where its nodes run: a declared placement or the first inferred
// deployment hint.
type Group struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	Source   string `json:"source"` // SourceDeclared or SourceInferred
	Evidence string `json:"evidence,omitempty"`
}

// Build makes the diagram: the top product units by max impact (ties: more
// fragile-critical files, then name), the library shared by the most units
// when the budget allows, and one "other units" node for the rest. Edges are
// declared links first, then imports by importing-file count, up to
// MaxEdges. It reads only hm and decl (validate decl first).
func Build(hm *types.Heatmap, decl types.DeclaredArchitecture, opts Options) (*Diagram, error) {
	if opts.MaxNodes == 0 {
		opts.MaxNodes = defaultMaxNodes
	}
	if opts.MaxNodes < 2 || opts.MaxNodes > maxMaxNodes {
		return nil, fmt.Errorf("%w (got %d)", ErrInvalidMaxNodes, opts.MaxNodes)
	}
	if opts.MaxEdges == 0 {
		opts.MaxEdges = edgeBudget(opts.MaxNodes)
	}
	if len(hm.Metadata.Units) == 0 {
		return nil, ErrNoUnits
	}

	d := &Diagram{Repo: path.Base(hm.Metadata.RepoPath), Commit: hm.Metadata.CommitSHA,
		Nodes: []Node{}, Edges: []Edge{}, Groups: []Group{}}
	d.Title = d.Repo + " runtime architecture"
	var product []types.Unit
	for _, u := range hm.Metadata.Units {
		if u.Supporting && !opts.IncludeSupporting {
			d.SupportingHidden++
			continue
		}
		product = append(product, u)
	}
	if len(product) == 0 {
		return nil, ErrOnlySupporting
	}
	d.ProductUnits = len(product)
	sort.SliceStable(product, func(i, j int) bool {
		a, b := product[i], product[j]
		if a.MaxImpact != b.MaxImpact {
			return a.MaxImpact > b.MaxImpact
		}
		if a.FragileCritical != b.FragileCritical {
			return a.FragileCritical > b.FragileCritical
		}
		return a.Name < b.Name
	})

	fileNode, _ := units.Assign(hm)
	lib := mostShared(hm, product, fileNode)
	shown, rest := product, []types.Unit(nil)
	switch {
	case len(product) > opts.MaxNodes:
		keep := opts.MaxNodes - 1
		if lib != "" && keep >= 3 {
			keep--
		} else {
			lib = ""
		}
		shown, rest = product[:keep], product[keep:]
	case len(product)+1 > opts.MaxNodes:
		lib = ""
	}

	// Map heatmap boxes (units.Assign ids) and unit names to diagram ids.
	ids := idMaker()
	nodeOf := map[string]string{} // "u:<unit>" / "c:<component>" -> node id
	byName := map[string]string{} // unit name -> node id
	for _, u := range shown {
		id := ids(u.Name)
		nodeOf["u:"+u.Name], byName[u.Name] = id, id
		d.Nodes = append(d.Nodes, unitNode(id, u))
	}
	if lib != "" {
		id := ids("lib-" + path.Base(lib))
		nodeOf["c:"+lib] = id
		d.Nodes = append(d.Nodes, libNode(id, lib, product, hm, fileNode))
	}
	if len(rest) > 0 {
		d.Nodes = append(d.Nodes, otherNode(rest))
		for _, u := range rest {
			nodeOf["u:"+u.Name], byName[u.Name] = otherID, otherID
		}
	}
	d.Nodes[0].Focal = true

	addFiles(d, hm, fileNode, nodeOf, opts.Source)
	all, skipped := buildEdges(hm, decl, fileNode, nodeOf, byName)
	d.Groups = assignGroups(d, shown, decl, byName)
	layout(d, all) // rank on every edge, so dropping one does not move boxes
	d.Edges = all[:min(len(all), opts.MaxEdges)]
	d.DroppedEdges = skipped + len(all) - len(d.Edges)
	return d, nil
}

// edgeBudget is SeeCode's edge limit for a node count (balanced, wide,
// faithful budgets).
func edgeBudget(nodes int) int {
	switch {
	case nodes <= 9:
		return 12
	case nodes <= 16:
		return 20
	}
	return 36
}

var nonID = regexp.MustCompile(`[^a-z0-9-]+`)

// idMaker returns a function turning names into unique SeeCode-safe ids
// (no dots: SeeCode reads "a.b" as field b of node a).
func idMaker() func(string) string {
	used := map[string]bool{otherID: true}
	return func(name string) string {
		base := strings.Trim(nonID.ReplaceAllString(strings.ToLower(name), "-"), "-")
		if base == "" {
			base = "n"
		}
		id := base
		for i := 2; used[id]; i++ {
			id = fmt.Sprintf("%s-%d", base, i)
		}
		used[id] = true
		return id
	}
}

// mostShared returns the shared component the most product units import
// directly (their own files import one of its files), ties broken by max
// impact, then name; "" when no shared component has two direct importers.
// Direct imports are what the picture draws, so the hub is visible.
func mostShared(hm *types.Heatmap, product []types.Unit, fileNode map[string]string) string {
	isProduct := map[string]bool{}
	for _, u := range product {
		isProduct["u:"+u.Name] = true
	}
	direct := map[string]map[string]bool{} // component -> importing units
	for from, tos := range units.Imports(hm) {
		if !isProduct[fileNode[from]] {
			continue
		}
		for _, to := range tos {
			if c, ok := strings.CutPrefix(fileNode[to], "c:"); ok {
				if direct[c] == nil {
					direct[c] = map[string]bool{}
				}
				direct[c][fileNode[from]] = true
			}
		}
	}
	impact := map[string]int{}
	for p, n := range fileNode {
		if c, ok := strings.CutPrefix(n, "c:"); ok {
			impact[c] = max(impact[c], hm.Files[p].Impact)
		}
	}
	best := ""
	for c, us := range direct {
		n, b := len(us), len(direct[best])
		if n < 2 {
			continue
		}
		if best == "" || n > b || (n == b && (impact[c] > impact[best] || (impact[c] == impact[best] && c < best))) {
			best = c
		}
	}
	return best
}

func unitNode(id string, u types.Unit) Node {
	return Node{ID: id, Label: u.Name, Kind: KindUnit, Tag: kindTag(u.Kind), MaxImpact: u.MaxImpact,
		ImpactAssessed: u.ImpactAssessed, FragileCritical: u.FragileCritical,
		Sub: impactText(u.MaxImpact, u.ImpactAssessed) + fmt.Sprintf(" · %d Harden now", u.FragileCritical)}
}

func libNode(id, comp string, product []types.Unit, hm *types.Heatmap, fileNode map[string]string) Node {
	n := Node{ID: id, Label: path.Base(comp), Kind: KindLibrary, Tag: "LIB", Members: []string{comp}}
	linkers := 0
	for _, u := range product {
		for _, c := range u.Shared {
			if c == comp {
				linkers++
			}
		}
	}
	for p, box := range fileNode {
		if f := hm.Files[p]; box == "c:"+comp && !tour.IsTestFile(p) {
			n.MaxImpact, n.ImpactAssessed = units.RaiseImpact(n.MaxImpact, n.ImpactAssessed, f)
			if f.Quadrant == types.QuadrantFragileCritical {
				n.FragileCritical++
			}
		}
	}
	n.Sub = fmt.Sprintf("shared by %d units · %s", linkers, impactText(n.MaxImpact, n.ImpactAssessed))
	return n
}

func otherNode(rest []types.Unit) Node {
	n := Node{ID: otherID, Label: fmt.Sprintf("%d other units", len(rest)), Kind: KindOther}
	for _, u := range rest {
		n.Members = append(n.Members, u.Name)
		if u.MaxImpact > n.MaxImpact {
			n.MaxImpact, n.ImpactAssessed = u.MaxImpact, u.ImpactAssessed
		}
		n.FragileCritical += u.FragileCritical
	}
	n.Sub = strings.Join(n.Members[:min(3, len(n.Members))], ", ")
	if len(n.Members) > 3 {
		n.Sub += fmt.Sprintf(" +%d", len(n.Members)-3)
	}
	return n
}

func impactText(impact int, assessed bool) string {
	if !assessed {
		return fmt.Sprintf("impact ~%d", impact)
	}
	return fmt.Sprintf("impact %d", impact)
}

// kindTag is a SeeCode tag (at most 8 characters) for a unit kind.
func kindTag(kind string) string {
	switch {
	case strings.HasPrefix(kind, "rust"):
		return "RUST"
	case strings.HasPrefix(kind, "go"):
		return "GO"
	case strings.HasPrefix(kind, "node"):
		return "NODE"
	case strings.HasPrefix(kind, "python"):
		return "PY"
	}
	return ""
}

// addFiles attaches each node's top files by impact (then likelihood, path).
func addFiles(d *Diagram, hm *types.Heatmap, fileNode, nodeOf map[string]string, source func(string) ([]byte, error)) {
	per := map[string][]*types.FileHeat{}
	for p, box := range fileNode {
		if id := nodeOf[box]; id != "" && !tour.IsTestFile(p) {
			per[id] = append(per[id], hm.Files[p])
		}
	}
	for i := range d.Nodes {
		fs := per[d.Nodes[i].ID]
		sort.Slice(fs, func(a, b int) bool {
			if fs[a].Impact != fs[b].Impact {
				return fs[a].Impact > fs[b].Impact
			}
			if fs[a].Likelihood != fs[b].Likelihood {
				return fs[a].Likelihood > fs[b].Likelihood
			}
			return fs[a].Path < fs[b].Path
		})
		d.Nodes[i].Files = []FileRef{}
		for _, f := range fs[:min(filesPerNode, len(fs))] {
			d.Nodes[i].Files = append(d.Nodes[i].Files, FileRef{Path: f.Path, Line: lineOf(f.Path, source),
				Impact: f.Impact, Quadrant: f.Quadrant, Note: note(f)})
		}
	}
}

func lineOf(p string, source func(string) ([]byte, error)) int {
	if source == nil {
		return 1
	}
	b, err := source(p)
	if err != nil {
		return 1 // unreadable: point at the top of the file
	}
	return tour.LineHint(string(b))
}

// note is the posture (or impact estimate) and the first action.
func note(f *types.FileHeat) string {
	q := f.Quadrant.Label()
	if !f.ImpactAssessed || q == "" {
		q = fmt.Sprintf("impact ~%d (estimate)", f.Impact)
	}
	if f.Explanation != nil && len(f.Explanation.Actions) > 0 {
		return q + ": " + f.Explanation.Actions[0].Text
	}
	return q
}

// buildEdges returns declared links (in file order, merged when both ends
// fold into the same nodes) and then import edges by importing-file count.
// skipped counts declared links with an end not drawn.
func buildEdges(hm *types.Heatmap, decl types.DeclaredArchitecture, fileNode, nodeOf, byName map[string]string) (all []Edge, skipped int) {
	type pair struct{ from, to string }
	var declared []Edge
	seen := map[pair]bool{}
	for _, l := range decl.Links {
		a, b := byName[l.From], byName[l.To]
		if a == "" || b == "" || a == b {
			skipped++ // a hidden supporting unit, or both ends in "other"
			continue
		}
		if seen[pair{a, b}] || (l.Both && seen[pair{b, a}]) {
			continue // merged into an earlier link between the same nodes
		}
		seen[pair{a, b}] = true
		label := l.Label
		if label == "" {
			label = "runtime link"
		}
		declared = append(declared, Edge{From: a, To: b, Label: label, Source: SourceDeclared, Both: l.Both, Evidence: l.Evidence})
	}

	importers := map[pair]map[string]bool{}
	for from, tos := range units.Imports(hm) {
		a := nodeOf[fileNode[from]]
		for _, to := range tos {
			b := nodeOf[fileNode[to]]
			if a == "" || b == "" || a == b {
				continue
			}
			k := pair{a, b}
			if importers[k] == nil {
				importers[k] = map[string]bool{}
			}
			importers[k][from] = true
		}
	}
	var imports []Edge
	for k, fs := range importers {
		label := fmt.Sprintf("%d files", len(fs))
		if len(fs) == 1 {
			label = "1 file"
		}
		imports = append(imports, Edge{From: k.from, To: k.to, Label: label, Source: SourceImports, Count: len(fs)})
	}
	sort.Slice(imports, func(i, j int) bool {
		a, b := imports[i], imports[j]
		if a.Count != b.Count {
			return a.Count > b.Count
		}
		if a.From != b.From {
			return a.From < b.From
		}
		return a.To < b.To
	})
	all = append(all, declared...)
	all = append(all, imports...)
	if all == nil {
		all = []Edge{}
	}
	return all, skipped
}

// assignGroups puts each shown unit in its declared placement, else in its
// first inferred deployment hint. Units without either stay ungrouped.
func assignGroups(d *Diagram, shown []types.Unit, decl types.DeclaredArchitecture, byName map[string]string) []Group {
	declared := map[string]types.DeclaredPlacement{}
	for _, p := range decl.Placement {
		declared[p.Unit] = p
	}
	groups := []Group{}
	index := map[string]string{} // source + label -> group id
	ids := idMaker()
	nodeIdx := map[string]int{}
	for i, n := range d.Nodes {
		nodeIdx[n.ID] = i
	}
	for _, u := range shown {
		var g Group
		if p, ok := declared[u.Name]; ok {
			g = Group{Label: p.RunsIn, Source: SourceDeclared, Evidence: p.Evidence}
		} else if len(u.InferredPlacement) > 0 {
			p := u.InferredPlacement[0]
			g = Group{Label: hintLabel(p), Source: SourceInferred, Evidence: p.Evidence}
		} else {
			continue
		}
		key := g.Source + "\n" + g.Label
		if index[key] == "" {
			g.ID = ids("g-" + g.Label)
			index[key] = g.ID
			groups = append(groups, g)
		}
		d.Nodes[nodeIdx[byName[u.Name]]].Group = index[key]
	}
	return groups
}

// hintLabel shortens an inferred placement: the Dockerfile name, "helm
// <values key>", or "k8s <manifest name>".
func hintLabel(p types.Placement) string {
	file, _, _ := strings.Cut(p.Evidence, ":")
	switch p.Kind {
	case "helm":
		if _, rest, ok := strings.Cut(p.Detail, ": "); ok && len(strings.Fields(rest)) > 0 {
			return "helm " + strings.Fields(rest)[0]
		}
	case "k8s":
		return "k8s " + path.Base(file)
	}
	return path.Base(file)
}

// layout sets row and col for a top-to-bottom picture: callers above what
// they use (row by dependency level over edges, sources first) and, within
// a row, nodes ordered by the mean column of the nodes above that link to
// them (fewer crossings), then by rank. Rows are centered on a doubled grid
// (columns 0, 2, 4, ... for the widest row), so a lone gateway sits above
// the middle of its drivers. Top to bottom puts a fan-out on a box's wide
// side, so SeeCode has room for the connectors.
func layout(d *Diagram, edges []Edge) {
	ids := make([]string, len(d.Nodes))
	rank, idx := map[string]int{}, map[string]int{}
	for i, n := range d.Nodes {
		ids[i], rank[n.ID], idx[n.ID] = n.ID, i, i
	}
	graph := map[string]map[string]int{}
	for _, e := range edges {
		if graph[e.From] == nil {
			graph[e.From] = map[string]int{}
		}
		graph[e.From][e.To]++
	}
	levels := tour.Levels(ids, graph)
	top := 0
	for _, l := range levels {
		top = max(top, l)
	}
	rows := make([][]int, top+1)
	widest := 0
	for i, n := range d.Nodes {
		r := top - levels[n.ID]
		d.Nodes[i].Row = r
		rows[r] = append(rows[r], i)
		widest = max(widest, len(rows[r]))
	}
	for r, row := range rows {
		bary := map[int]float64{}
		for _, i := range row {
			sum, n := 0.0, 0
			for _, e := range edges {
				if j, ok := idx[e.From]; ok && e.To == d.Nodes[i].ID && d.Nodes[j].Row < r {
					sum, n = sum+float64(d.Nodes[j].Col), n+1
				}
			}
			bary[i] = math.Inf(1) // no caller above: after the ones that have one
			if n > 0 {
				bary[i] = sum / float64(n)
			}
		}
		sort.SliceStable(row, func(a, b int) bool {
			if bary[row[a]] != bary[row[b]] {
				return bary[row[a]] < bary[row[b]]
			}
			return rank[d.Nodes[row[a]].ID] < rank[d.Nodes[row[b]].ID]
		})
		for c, i := range row {
			d.Nodes[i].Col = widest - len(row) + 2*c
		}
	}
}
