package tour

import (
	"fmt"
	"html"
	"math"
	"path"
	"regexp"
	"strings"

	"github.com/zanetworker/highstakes/internal/types"
)

// The show-me walkthrough template draws a fixed 1280x680 map with its
// key in the bottom-left corner. Boxes are about 250px wide and 80px tall.
const (
	mapW                = 1280
	gridTop, gridBottom = 95, 600 // y range of box centers' rows
	maxWalkthroughCols  = 5
	maxWalkthroughRows  = 6
	maxWalkthroughNodes = maxWalkthroughCols*maxWalkthroughRows - 2 // two bottom-left cells stay free for the key
	keyRight            = 340                                       // the key's right edge on the map
	boxHalfWidth        = 125
	maxNodeName         = 20
	maxNodeSub          = 26
	maxWalkthroughLinks = 30 // strongest component links drawn; more turn the map into a hairball
	importsColor        = "#9aa3b2"
	// linksFlowID sorts after "product" and "supporting": the page lists
	// flow buttons in JSON key order, and the tour should come first.
	linksFlowID = "wiring"
)

// walkthroughLegend replaces the template's channel key (browser, server to
// server, refused), which a code tour does not use, with HighStakes' own:
// legendExtra is the template's only key slot, so a scoped style hides the
// built-in entries (and their emptied column) and keeps the .hs ones. It
// also hides the caption's channel label ("SERVER TO SERVER"), which names
// network channels a tour does not have.
const walkthroughLegend = `<style>.legend .lcol > span:not(.hs){display:none}.legend .lcol:not(:has(.hs)){display:none}.caption .chan{display:none}</style>` +
	`<span class="hs"><span class="sw b"></span>reading order</span>` +
	`<span class="hs"><span class="sw" style="border-color:` + importsColor + `"></span>imports (A to B)</span>` +
	`<span class="hs"><i class="dsw" style="background:#22252c;border:1px solid #373c47;border-radius:2px"></i>shared library</span>`

// WalkthroughPage is the data file of the show-me rich step-through
// (development-tools show-me references/walkthrough/data-model.md), built
// into HTML by its walkthrough_build.py. All text is HTML-escaped.
type WalkthroughPage struct {
	Title    string              `json:"title"`
	Heading  string              `json:"heading"`
	Kicker   string              `json:"kicker"`
	Intro    []string            `json:"intro"`
	Colors   map[string]string   `json:"colors"`
	PlaneKey [][2]string         `json:"planeKey"`
	Zones    [][3]any            `json:"zones"`
	Nodes    map[string]WalkNode `json:"nodes"`
	Frames   []any               `json:"frames"`
	Flows    map[string]WalkFlow `json:"flows"`
	Start    string              `json:"start"`
	// LegendExtra is raw HTML for the key (walkthroughLegend).
	LegendExtra string            `json:"legendExtra"`
	NodeOf      map[string]string `json:"-"` // component -> node id
}

// WalkNode is one component box; X and Y are its center on the map.
type WalkNode struct {
	X    float64 `json:"x"`
	Y    float64 `json:"y"`
	Name string  `json:"name"`
	Sub  string  `json:"sub"`
	Tp   bool    `json:"tp,omitempty"` // shared library: darker box
}

// WalkFlow is one selectable path through the map.
type WalkFlow struct {
	Label string    `json:"label"`
	Plane string    `json:"plane"`
	Hops  []WalkHop `json:"hops"`
}

// WalkHop is one tour stop: travel from the previous stop's component.
type WalkHop struct {
	F     string `json:"f"`
	T     string `json:"t"`
	Ch    string `json:"ch"`
	Pay   string `json:"pay"`
	Short string `json:"short"`
	Txt   string `json:"txt"`
	Plane string `json:"plane"`
	Brisk int    `json:"brisk,omitempty"` // 1: faster travel, shorter dwell
}

// quadrantPlanes maps quadrants to walkthrough planes and colors.
var quadrantPlanes = []struct {
	q            types.Quadrant
	plane, color string
	label        string
}{
	{types.QuadrantFragileCritical, "fragile", "#e06f70", types.QuadrantFragileCritical.Label()},
	{types.QuadrantProtect, "protect", "#cea451", types.QuadrantProtect.Label()},
	{types.QuadrantWatch, "watch", "#7ea6d9", types.QuadrantWatch.Label()},
	{types.QuadrantLow, "low", "#8fbf88", types.QuadrantLow.Label()},
	{"", "estimate", "#9aa3b2", "impact estimate"},
}

var nonNodeID = regexp.MustCompile(`[^a-z0-9]+`)

// Walkthrough turns a tour into walkthrough page data: one node per
// component (in tour order, laid out left to right, top to bottom), one
// flow per part (product, supporting) and one hop per stop, travelling from
// the previous stop's component. A caption gives the stop's why (critical
// reason or summary) and first likelihood reason; the payload is its file anchor and the gist its
// first action. roles labels components (owning unit, "shared by N units");
// a role starting with "shared" draws a darker box. Components past
// maxWalkthroughNodes are left out and counted in the intro. links (from
// ComponentLinks) become a third flow, "How components connect": one hop
// per link from the importing component to its dependency, the strongest
// maxWalkthroughLinks among drawn components.
func Walkthrough(tr *Tour, roles map[string]string, links []Link) (*WalkthroughPage, error) {
	var comps []string
	seen := map[string]bool{}
	stops := 0
	for _, s := range tr.Sections {
		stops += len(s.Stops)
		if len(s.Stops) > 0 && !seen[s.Component] {
			seen[s.Component] = true
			comps = append(comps, s.Component)
		}
	}
	if stops == 0 {
		return nil, fmt.Errorf("%w: the tour has no stops", ErrNoFiles)
	}
	left := 0
	if len(comps) > maxWalkthroughNodes {
		left = len(comps) - maxWalkthroughNodes
		comps = comps[:maxWalkthroughNodes]
	}

	repo := path.Base(tr.Repo)
	p := &WalkthroughPage{
		Title: repo + " code tour", Heading: html.EscapeString(repo) + " code tour: where the stakes are",
		Colors: map[string]string{}, PlaneKey: [][2]string{}, Zones: [][3]any{}, Frames: []any{},
		Nodes: map[string]WalkNode{}, Flows: map[string]WalkFlow{}, NodeOf: map[string]string{},
		LegendExtra: walkthroughLegend,
	}
	used := map[string]bool{}
	cells := gridCells(len(comps))
	for i, c := range comps {
		id := strings.Trim(nonNodeID.ReplaceAllString(strings.ToLower(c), "_"), "_")
		for base, n := id, 2; used[id] || id == ""; n++ {
			id = fmt.Sprintf("%s_%d", base, n)
		}
		used[id] = true
		p.NodeOf[c] = id
		role := roles[c]
		p.Nodes[id] = WalkNode{
			X:    cells[i][0],
			Y:    cells[i][1],
			Name: html.EscapeString(clipText(nodeName(c, repo), maxNodeName)),
			Sub:  html.EscapeString(clipText(componentSub(tr, c, roleText(role, repo)), maxNodeSub)),
			Tp:   strings.HasPrefix(role, "shared"),
		}
	}
	if len(comps) > 0 {
		p.Zones = append(p.Zones, [3]any{"READ IN ORDER: DEPENDENCIES FIRST", []string{p.NodeOf[comps[0]]}, 6})
	}

	planes := map[string]bool{}
	for _, kind := range []string{KindProduct, KindSupporting} {
		var hops []WalkHop
		prev := ""
		for _, s := range tr.Sections {
			id := p.NodeOf[s.Component]
			if s.Kind != kind || id == "" {
				continue
			}
			for _, st := range s.Stops {
				from := prev
				if from == "" {
					from = id
				}
				h := stopHop(st, from, id, tr.LikelyCutoff, tr.ImpactThreshold)
				planes[h.Plane] = true
				hops = append(hops, h)
				prev = id
			}
		}
		if len(hops) == 0 {
			continue
		}
		label := "Product tour"
		if kind == KindSupporting {
			label = "Supporting code"
		}
		p.Flows[kind] = WalkFlow{Label: fmt.Sprintf("%s (%d %s)", label, len(hops), plural(len(hops), "stop")), Plane: hops[0].Plane, Hops: hops}
		if p.Start == "" {
			p.Start = kind
		}
	}
	if f, ok := linkFlow(links, p.NodeOf, repo); ok {
		p.Flows[linksFlowID] = f
		p.Colors["imports"] = importsColor
	}
	for _, qp := range quadrantPlanes {
		if planes[qp.plane] {
			p.Colors[qp.plane] = qp.color
			p.PlaneKey = append(p.PlaneKey, [2]string{qp.plane, qp.label})
		}
	}

	commit := tr.Commit
	if len(commit) > 7 {
		commit = commit[:7]
	}
	hops := len(p.Flows[KindProduct].Hops) + len(p.Flows[KindSupporting].Hops)
	p.Kicker = fmt.Sprintf("HIGHSTAKES TOUR · %d STOPS · %d COMPONENTS", hops, len(comps))
	p.Intro = []string{
		"<b>Order:</b> dependencies first; within a component, the files that matter most if they break.",
		"<b>Color</b> is the file's quadrant: impact if it breaks against likelihood it needs fixes. Darker boxes are libraries several units share.",
		"<b>How components connect</b> draws the import graph: each link runs from the importing component to its dependency, labelled with the number of importing files.",
		"<b>Each step</b> says why the file is impactful, why it is likely to break, and what to do first.",
		fmt.Sprintf("<b>Generated</b> by highstakes from %s@%s; nothing here is hand-written.", html.EscapeString(repo), html.EscapeString(commit)),
	}
	if left > 0 {
		p.Intro = append(p.Intro, fmt.Sprintf("<b>%d more components</b> are not drawn (map holds %d). Narrow with --components, --focus or --min-impact.", left, maxWalkthroughNodes))
	}
	return p, nil
}

// linkFlow is one brisk hop per link between drawn components, strongest
// first, capped at maxWalkthroughLinks. ok is false when no link is drawn.
func linkFlow(links []Link, nodeOf map[string]string, repo string) (WalkFlow, bool) {
	drawn := make([]Link, 0, len(links))
	for _, l := range links {
		if nodeOf[l.From] != "" && nodeOf[l.To] != "" && l.From != l.To {
			drawn = append(drawn, l)
		}
	}
	sortLinks(drawn)
	drawn = drawn[:min(len(drawn), maxWalkthroughLinks)]
	if len(drawn) == 0 {
		return WalkFlow{}, false
	}
	hops := make([]WalkHop, 0, len(drawn))
	for _, l := range drawn {
		from, to := html.EscapeString(nodeName(l.From, repo)), html.EscapeString(nodeName(l.To, repo))
		files := fmt.Sprintf("%d %s", l.Files, plural(l.Files, "file"))
		hops = append(hops, WalkHop{
			F: nodeOf[l.From], T: nodeOf[l.To], Ch: "f", Plane: "imports", Brisk: 1,
			Pay:   files,
			Short: from + " imports " + to,
			Txt: fmt.Sprintf("<b>%s</b> depends on <b>%s</b>: %s in %s import it. A break in %s can reach %s.",
				html.EscapeString(l.From), html.EscapeString(l.To), files, html.EscapeString(l.From), to, from),
		})
	}
	return WalkFlow{Label: fmt.Sprintf("How components connect (%d %s)", len(hops), plural(len(hops), "link")), Plane: "imports", Hops: hops}, true
}

// stopHop is the hop arriving at one stop.
func stopHop(st Stop, from, to string, likelyCutoff, impactCutoff int) WalkHop {
	h := WalkHop{F: from, T: to, Ch: "b", Pay: html.EscapeString(path.Base(st.Anchor)), Plane: "estimate"}
	if from == to {
		h.Ch = "s"
	}
	if st.ImpactAssessed {
		for _, qp := range quadrantPlanes {
			if qp.q == st.Quadrant && qp.q != "" {
				h.Plane = qp.plane
			}
		}
	}
	// Why is the LLM critical reason or summary (else static signals); the
	// first impact reason, the dimension scores, is only a fallback.
	why, likely := st.Why, ""
	if e := st.Explanation; e != nil {
		if why == "" && len(e.ImpactReasons) > 0 {
			why = e.ImpactReasons[0]
		}
		if len(e.LikelihoodReasons) > 0 {
			likely = e.LikelihoodReasons[0]
		}
	}
	h.Short = "read " + html.EscapeString(path.Base(st.Path))
	if a := st.TopBeforeMerge; a != nil {
		h.Short = "do: " + html.EscapeString(a.Text)
	}
	h.Txt = fmt.Sprintf("<b>%s</b> · %s<br>%s", html.EscapeString(st.Anchor), html.EscapeString(st.Verdict),
		html.EscapeString(StopAxes(st, likelyCutoff, impactCutoff)))
	h.Txt += "<br>Why it matters: " + html.EscapeString(why)
	if likely != "" {
		h.Txt += "<br>Break signals: " + html.EscapeString(likely)
	}
	if a := st.TopBeforeMerge; a != nil {
		h.Txt += "<br>Before merge: " + html.EscapeString(a.Text)
	}
	if a := st.TopBacklog; a != nil {
		h.Txt += "<br>Backlog: " + html.EscapeString(a.Text)
	}
	return h
}

// gridCells returns n box centers, row by row, left to right, in a grid
// about twice as wide as tall. From 3 rows on, the bottom row keeps clear of
// the key in the bottom-left corner: it uses only columns whose boxes start
// right of keyRight, filled right-aligned, and the rows above take the rest.
func gridCells(n int) [][2]float64 {
	cols := max(1, min(maxWalkthroughCols, int(math.Ceil(math.Sqrt(float64(n)*1.9)))))
	x := func(c int) float64 { return math.Round(float64(mapW) * (float64(c) + 0.5) / float64(cols)) }
	reserve := 0 // bottom-row columns under the key
	for c := 0; c < cols && x(c)-boxHalfWidth < keyRight; c++ {
		reserve++
	}
	rows := (n + cols - 1) / cols
	if rows >= 3 {
		for (rows-1)*cols+cols-reserve < n {
			rows++
		}
	}
	var pos [][2]int // row, col
	last := 0
	if rows >= 3 {
		last = min(n, cols-reserve)
	}
	for i := 0; i < n-last; i++ {
		pos = append(pos, [2]int{i / cols, i % cols})
	}
	for i := 0; i < last; i++ {
		pos = append(pos, [2]int{rows - 1, cols - last + i})
	}
	out := make([][2]float64, n)
	for i, rc := range pos {
		out[i] = [2]float64{x(rc[1]), math.Round(gridTop + float64(gridBottom-gridTop)*(float64(rc[0])+0.5)/float64(rows))}
	}
	return out
}

// roleText shortens a role for a box: a unit name without the "<repo>-"
// prefix, or "N units" for a shared library (the darker box says shared).
func roleText(role, repo string) string {
	if role == "" {
		return ""
	}
	if n, ok := strings.CutPrefix(role, "shared by "); ok {
		return n
	}
	return nodeName(role, repo)
}

// nodeName is the component's last segment without a "<repo>-" prefix.
func nodeName(component, repo string) string {
	name := path.Base(component)
	if component == "." {
		name = "(root)"
	}
	if rest, ok := strings.CutPrefix(name, repo+"-"); ok && rest != "" {
		return rest
	}
	return name
}

// componentSub is the component's max impact among its stops and its role.
func componentSub(tr *Tour, component, role string) string {
	top := 0
	for _, s := range tr.Sections {
		if s.Component == component {
			for _, st := range s.Stops {
				top = max(top, st.Impact)
			}
		}
	}
	sub := fmt.Sprintf("impact %d", top)
	if role != "" {
		sub += " · " + role
	}
	return sub
}

// clipText shortens s to at most n runes, ending in "…" when cut.
func clipText(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

// StopAxes states a stop's two axes as questions with the repo cutoffs:
// "Will it break? 50 HIGH, cutoff 33 · How bad if it breaks? 92 HIGH, cutoff 78".
func StopAxes(st Stop, likelyCutoff, impactCutoff int) string {
	axis := func(q string, score, cutoff int) string {
		if cutoff <= 0 {
			return fmt.Sprintf("%s %d", q, score)
		}
		return fmt.Sprintf("%s %d %s, cutoff %d", q, score, types.AxisLevel(score, cutoff), cutoff)
	}
	bad := axis(types.AxisImpact, st.Impact, impactCutoff)
	if !st.ImpactAssessed {
		bad = fmt.Sprintf("%s ~%d (estimate)", types.AxisImpact, st.Impact)
	}
	return axis(types.AxisLikelihood, st.Likelihood, likelyCutoff) + " · " + bad
}
