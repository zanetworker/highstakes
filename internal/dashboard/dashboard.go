package dashboard

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/zanetworker/highstakes/internal/explain"
	"github.com/zanetworker/highstakes/internal/tour"
	"github.com/zanetworker/highstakes/internal/types"
)

type fileEntry struct {
	Path         string `json:"path"`
	Dir          string `json:"dir"`
	Name         string `json:"name"`
	HeatScore    int    `json:"heat_score"`
	Tier         string `json:"tier"`
	Reason       string `json:"reason"`
	CritReason   string `json:"crit_reason"`
	Security     int    `json:"security"`
	Data         int    `json:"data"`
	Availability int    `json:"availability"`
	User         int    `json:"user"`
	AutoMerge    bool   `json:"auto_merge"`
	Reviewers    int    `json:"reviewers"`
	Lines        int    `json:"lines"`
	Language     string `json:"language"`
	Complexity   int    `json:"complexity"`
	ImportedBy   int    `json:"imported_by"`
	Likelihood   int    `json:"likelihood"`
	Impact       int    `json:"impact"`
	Assessed     bool   `json:"impact_assessed"`
	Quadrant     string `json:"quadrant"`
	Test         bool   `json:"test"`
	UnitNode     string `json:"unit_node,omitempty"` // node id in the Architecture view grouped by unit
	Autonomy     *types.Autonomy `json:"autonomy,omitempty"` // agent autonomy recorded by analyze

	Explanation *types.Explanation `json:"explanation,omitempty"`
	ImpactTitle string             `json:"impact_title,omitempty"`
	LikelyTitle string             `json:"likely_title,omitempty"`
}

// Generate writes the dashboard. decl holds the runtime links and placements
// declared in .heatmap/architecture.yaml; the unit view draws them apart
// from inferred ones.
func Generate(hm *types.Heatmap, decl types.DeclaredArchitecture, outputPath string) error {
	unitView, fileNode := buildUnitView(hm)
	var files []fileEntry
	for _, f := range hm.Files {
		reason, critReason := "", ""
		security, data, avail, user := 0, 0, 0, 0
		if f.Factors.BlastRadius.Assessed {
			reason = f.Factors.BlastRadius.Summary
			critReason = f.Factors.BlastRadius.CriticalReason
			security = f.Factors.BlastRadius.SecurityImpact
			data = f.Factors.BlastRadius.DataImpact
			avail = f.Factors.BlastRadius.AvailabilityImpact
			user = f.Factors.BlastRadius.UserImpact
		}

		parts := strings.Split(f.Path, "/")
		dir := "."
		name := f.Path
		if len(parts) > 1 {
			dir = strings.Join(parts[:len(parts)-1], "/")
			name = parts[len(parts)-1]
		}

		lines := f.Size.Lines
		if lines < 10 {
			lines = 10
		}

		impactTitle, likelyTitle := "", ""
		if f.Explanation != nil {
			impactTitle, likelyTitle = explain.Titles(*f.Explanation, f.ImpactAssessed)
		}
		files = append(files, fileEntry{
			Path: f.Path, Dir: dir, Name: name,
			HeatScore: f.HeatScore, Tier: string(f.Tier),
			Reason: reason, CritReason: critReason,
			Security: security, Data: data, Availability: avail, User: user,
			AutoMerge: f.ReviewRequirements.AutoMerge,
			Reviewers: f.ReviewRequirements.MinReviewers,
			Lines: lines, Language: f.Language,
			Complexity: f.Factors.Complexity.Cyclomatic,
			ImportedBy: len(f.Dependencies.ImportedBy),
			Likelihood: f.Likelihood, Impact: f.Impact, Assessed: f.ImpactAssessed,
			Quadrant: string(f.Quadrant), Test: tour.IsTestFile(f.Path),
			Explanation: f.Explanation, ImpactTitle: impactTitle, LikelyTitle: likelyTitle,
			UnitNode: fileNode[f.Path], Autonomy: f.Autonomy,
		})
	}

	sort.Slice(files, func(i, j int) bool { return files[i].HeatScore > files[j].HeatScore })

	filesJSON, _ := json.Marshal(files)

	tierCounts := map[string]int{"critical": 0, "high": 0, "medium": 0, "low": 0}
	for _, f := range files {
		tierCounts[f.Tier]++
	}

	inboxJSON, _ := json.Marshal(buildInbox(hm))

	statsJSON, _ := json.Marshal(map[string]interface{}{
		"total":            len(files),
		"tiers":            tierCounts,
		"repo":             hm.Metadata.RepoPath,
		"analyzed_at":      hm.Metadata.AnalyzedAt.Format("2006-01-02 15:04"),
		"commit":           hm.Metadata.CommitSHA,
		"branch":           hm.Metadata.Branch,
		"languages":        hm.Metadata.Languages,
		"likely_cutoff":    hm.Metadata.LikelyCutoff,
		"impact_threshold": hm.Metadata.ImpactThreshold,
	})

	archJSON, _ := json.Marshal(buildArchitecture(hm))
	unitsJSON, _ := json.Marshal(map[string]interface{}{
		"nodes": unitView.Nodes, "edges": unitView.Edges, "unlinked": unitView.Unlinked, "units": hm.Metadata.Units, "declared": decl,
	})

	html := strings.ReplaceAll(tmpl, "/*__FILES__*/[]", string(filesJSON))
	html = strings.ReplaceAll(html, "/*__ARCH__*/{}", string(archJSON))
	html = strings.ReplaceAll(html, "/*__UNITS__*/{}", string(unitsJSON))
	html = strings.ReplaceAll(html, "/*__STATS__*/{}", string(statsJSON))
	html = strings.ReplaceAll(html, "/*__INBOX__*/{}", string(inboxJSON))
	evidenceJSON, _ := json.Marshal(types.AxisEvidence)
	html = strings.ReplaceAll(html, "/*__EVIDENCE__*/''", string(evidenceJSON))

	if err := os.WriteFile(outputPath, []byte(html), 0644); err != nil {
		return fmt.Errorf("write dashboard: %w", err)
	}

	return nil
}

const tmpl = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>HighStakes Dashboard</title>
<link href="https://fonts.googleapis.com/css2?family=Red+Hat+Display:wght@400;500;600;700;900&family=Red+Hat+Text:wght@400;500;600;700&family=Red+Hat+Mono:wght@400;500;600&display=swap" rel="stylesheet">
<style>
*{margin:0;padding:0;box-sizing:border-box}
:root{--bg:#0b0b0c;--panel:#131315;--raise:#1b1b1e;--border:rgba(255,255,255,.08);--text:#ececec;--muted:#a8a8ad;--dim:#8c8c92;
--accent:#ee0000;--critical:#f2555a;--high:#e0a43c;--medium:#6aa6e0;--low:#5fae6b;
--font-d:'Red Hat Display',sans-serif;--font-t:'Red Hat Text',sans-serif;--font-m:'Red Hat Mono',monospace;--ease:cubic-bezier(.2,0,0,1)}
body{background:var(--bg);color:var(--text);font-family:var(--font-t);font-variant-numeric:tabular-nums}
button{font:inherit;color:inherit}
:focus-visible{outline:2px solid var(--medium);outline-offset:2px}

header{padding:12px 24px;border-bottom:1px solid var(--border);display:flex;align-items:baseline;gap:16px}
header h1{font:600 24px var(--font-d);color:#fff}
header .meta{font:12px var(--font-m);color:var(--muted)}

.inbox{flex:1;overflow-y:auto;padding:20px 24px 32px;display:none}
.inbox.active{display:block}
.inbox-intro{font:14px/1.5 var(--font-t);color:var(--muted);max-width:36em;margin-bottom:20px}
.inbox-cols{display:grid;grid-template-columns:repeat(auto-fit,minmax(380px,1fr));gap:24px;align-items:start}
.inbox-cols>section{min-width:0}
.inbox h2{font:500 18px var(--font-d);color:#fff;display:flex;align-items:baseline;gap:8px;margin-bottom:4px}
.inbox h2 .count{font:12px var(--font-m);color:var(--dim)}
.inbox .sub{font:13px var(--font-t);color:var(--muted);margin-bottom:12px}
.card{background:var(--panel);border:1px solid var(--border);border-radius:8px;padding:12px 16px;margin-bottom:8px;display:grid;grid-template-columns:12px minmax(0,1fr) auto;column-gap:8px;row-gap:4px}
.card:hover{background:var(--raise)}
.card .dot{width:8px;height:8px;border-radius:50%;margin-top:6px}
.card .do{font:500 14px/1.4 var(--font-t);color:#fff;grid-column:2}
.card .why{font:13px/1.4 var(--font-t);color:var(--muted);grid-column:2}
.card .who{font:12px var(--font-m);color:var(--dim);grid-column:2;display:flex;flex-wrap:wrap;gap:4px 12px}
.card .file{background:none;border:0;padding:0;font:12px var(--font-m);color:var(--text);cursor:pointer;text-align:left;overflow-wrap:anywhere}
.card .file:hover{color:#fff;text-decoration:underline}
.card .side{grid-row:1/span 3;grid-column:3;display:flex;flex-direction:column;align-items:flex-end;gap:8px}
.card .posture{font:12px var(--font-t)}
.card .file.title{grid-column:2;font:500 14px/1.4 var(--font-m);color:#fff}
.card ul.steps{grid-column:2;list-style:none;margin-top:4px}
.card ul.steps li,.grp ul.gfiles li{display:grid;grid-template-columns:28px minmax(0,1fr);gap:4px;margin-bottom:4px;font:13px/1.45 var(--font-t);color:var(--text)}
.card ul.steps .box{font:12px var(--font-m);color:var(--dim);padding-top:1px}
.card .sig{display:block;color:var(--dim);font-size:12px}
details.grp{display:block;padding:0}
details.grp summary{list-style:none;cursor:pointer;display:grid;grid-template-columns:12px minmax(0,1fr) auto;column-gap:8px;align-items:baseline;padding:12px 16px}
details.grp summary::-webkit-details-marker{display:none}
details.grp summary .dot{align-self:start}
details.grp summary .meta{font:12px var(--font-m);color:var(--dim)}
details.grp summary .do::after{content:' +';color:var(--dim)}
details.grp[open] summary .do::after{content:' \2212'}
details.grp[open]{background:var(--raise)}
.grp ul.gfiles{list-style:none;padding:0 16px 12px 36px}
.grp ul.gfiles li{grid-template-columns:minmax(0,1fr);gap:0}
.snip{display:flex;gap:8px;align-items:flex-start;background:var(--bg);border:1px solid var(--border);border-radius:4px;padding:8px;margin:0 16px 12px 36px}
.snip code{flex:1;min-width:0;max-height:192px;overflow:auto;font:12px/1.5 var(--font-m);color:var(--muted);white-space:pre}
.cmd{grid-column:2/span 2;min-width:0;display:flex;gap:8px;align-items:center;background:var(--bg);border:1px solid var(--border);border-radius:4px;padding:4px 4px 4px 8px;margin-top:4px}
.cmd code{flex:1;min-width:0;font:12px var(--font-m);color:var(--muted);overflow-x:auto;white-space:nowrap}
.copy{border:1px solid var(--border);background:var(--raise);border-radius:4px;padding:4px 8px;font:12px var(--font-t);color:var(--text);cursor:pointer;flex-shrink:0}
.copy:hover{border-color:var(--muted)}
.more{background:none;border:1px solid var(--border);border-radius:4px;padding:4px 12px;font:13px var(--font-t);color:var(--muted);cursor:pointer;margin-top:4px}
.more:hover{color:#fff;border-color:var(--muted)}

.meter{position:relative;height:4px;background:var(--raise);border-radius:2px;margin:4px 0 0}
.meter .fill{position:absolute;left:0;top:0;bottom:0;border-radius:2px}
.meter .tick{position:absolute;top:-3px;width:2px;height:10px;background:var(--text);border-radius:1px}
.main{display:flex;height:calc(100vh - 106px)}

.treemap-wrap{flex:1;padding:8px;position:relative;overflow:hidden}
.treemap{width:100%;height:100%;position:relative}
.tm-node{position:absolute;border:1px solid rgba(0,0,0,.6);cursor:pointer;overflow:hidden;transition:opacity .15s var(--ease);display:flex;flex-direction:column;justify-content:flex-end;padding:3px 5px}
.tm-node:hover{opacity:.85;z-index:10;border-color:#fff}
.tm-node.dimmed{opacity:.2}
.tm-label{font:500 12px var(--font-m);color:#fff;text-shadow:0 1px 3px rgba(0,0,0,.8);overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.tm-score{font:600 14px var(--font-d);color:#fff;text-shadow:0 1px 3px rgba(0,0,0,.8)}
.breadcrumb{position:absolute;top:10px;left:12px;z-index:20;display:flex;gap:4px;align-items:center}
.breadcrumb span{font:500 12px var(--font-m);color:var(--muted);cursor:pointer;padding:2px 6px;border-radius:3px;background:rgba(0,0,0,.7)}
.breadcrumb span:hover{color:#fff}
.breadcrumb span.current{color:#fff}

.detail{width:380px;border-left:1px solid var(--border);padding:20px;overflow-y:auto;background:var(--panel)}
.detail h3{font:500 14px var(--font-m);color:#fff;word-break:break-all;margin-bottom:8px}
.detail .verdict{font:500 18px var(--font-d);margin-bottom:12px}
.detail .q{margin:12px 0}
.detail .q .ql{display:flex;justify-content:space-between;align-items:baseline;font:13px var(--font-t)}
.detail .q .qv{font:500 14px var(--font-m);color:#fff}
.detail .q .qc{font:12px var(--font-m);color:var(--dim)}
.detail .note{font:12px/1.5 var(--font-t);color:var(--dim);margin-top:8px}
.detail ul.check{list-style:none}
.detail ul.check li{display:grid;grid-template-columns:28px minmax(0,1fr);gap:4px;margin-bottom:8px;font:13px/1.45 var(--font-t)}
.detail ul.check .box{font:12px var(--font-m);color:var(--dim);padding-top:1px;white-space:nowrap}
.detail ul.check .sig{display:block;color:var(--muted);font-size:12px}
.detail ul.check .cmd{grid-column:2}
.detail details{margin-top:16px;border-top:1px solid var(--border);padding-top:12px}
.detail summary{cursor:pointer;font:500 13px var(--font-d);color:var(--muted)}
.detail summary:hover{color:#fff}
.detail .tier-badge{display:inline-block;padding:2px 8px;border-radius:4px;font:500 12px var(--font-t);margin:4px 0}
.detail .sec{margin-top:16px}
.detail .sec-title{font:500 13px var(--font-d);color:#fff;margin-bottom:8px}
.detail .reason{font:13px/1.5 var(--font-t);margin-bottom:4px}
.detail .crit{font:500 12px var(--font-t);color:var(--accent)}
.bar-r{display:flex;align-items:center;gap:6px;margin:3px 0}
.bar-r .bl{font:12px var(--font-t);color:var(--muted);width:80px;text-align:right}
.bar-r .b{flex:1;height:5px;background:var(--border);border-radius:3px;overflow:hidden}
.bar-r .bf{height:100%;border-radius:3px}
.bar-r .bv{font:12px var(--font-m);width:24px}
.rev{background:var(--bg);border:1px solid var(--border);border-radius:5px;padding:8px;font:12px var(--font-t);margin-top:6px}
.rev div{margin:1px 0}
.atag{display:inline-block;padding:2px 6px;border-radius:3px;font:600 10px var(--font-d);margin-top:6px}
.atag.block{background:#5F0000;color:var(--critical)}
.atag.safe{background:#0A3D0A;color:var(--low)}
.empty{padding:32px;text-align:center;color:var(--dim);font:14px var(--font-t)}

.explorer{flex:1;overflow-y:auto;padding:4px 0;display:none}
.explorer.active{display:block}
.treemap-wrap.active{display:block}
.treemap-wrap{display:block}
.ex-row{display:flex;align-items:center;padding:4px 8px;cursor:pointer;gap:4px;min-height:28px}
.ex-row:hover{background:var(--panel)}
.ex-row.selected{background:var(--raise)}
.ex-dir{font:600 12px var(--font-d);color:var(--muted)}
.ex-dir .arrow{display:inline-block;width:12px;font-size:12px;color:var(--dim)}
.ex-dir .dname{color:#fff}
.ex-dir .dheat{margin-left:auto;font:12px var(--font-t)}
.ex-file{font:12px var(--font-m)}
.ex-file .fname{color:var(--text)}
.ex-file .fheat{margin-left:auto;font:12px var(--font-t);min-width:80px;text-align:right}
.ex-file .ftier{width:8px;height:8px;border-radius:50%;flex-shrink:0}
.ex-file .freason{color:var(--dim);font:12px var(--font-t);overflow:hidden;text-overflow:ellipsis;white-space:nowrap;max-width:350px;margin-left:8px}
.ex-heat-bar{width:60px;height:4px;background:var(--border);border-radius:2px;overflow:hidden;flex-shrink:0;margin-left:8px}
.ex-heat-bar .fill{height:100%;border-radius:2px}
.view-toggle{display:flex;gap:4px;padding:4px;border:1px solid var(--border);border-radius:8px}
.view-toggle button{padding:4px 12px;border:none;border-radius:4px;background:none;color:var(--muted);cursor:pointer;font:13px var(--font-t);white-space:nowrap}
.view-toggle button:hover{color:#fff}
.view-toggle button.on{background:var(--raise);color:#fff;box-shadow:inset 0 0 0 1px var(--border)}
.arch{flex:1;overflow:hidden;display:none;flex-direction:column;position:relative;min-width:0}
.arch-hint{padding:8px 12px;font:12px var(--font-t);color:var(--muted);background:var(--bg);border-bottom:1px solid var(--border);z-index:2}
.arch-bar{display:flex;gap:6px;align-items:center;margin-bottom:5px}
.arch-bar label{font:12px var(--font-t);color:var(--muted)}
.arch-bar .fbtn:disabled{opacity:.4;cursor:not-allowed}
.arch-zoom{font:12px var(--font-m);color:var(--dim);margin-left:4px}
.arch-vp{flex:1;position:relative;overflow:hidden;touch-action:none;cursor:grab}
.arch-vp.panning{cursor:grabbing}
.arch .band{fill:#ffffff08;stroke:var(--border);stroke-dasharray:4 4}
.arch .band-label{font:500 12px var(--font-d);fill:var(--muted)}
.arch svg{display:block}
.arch .an{cursor:pointer}
.arch .an rect{stroke-width:1.5}
.arch .an:hover rect{stroke-width:2.5}
.arch .an.sel rect{stroke:var(--text);stroke-width:2.5}
.arch .an.dim,.arch .ae.dim{opacity:.15}
.arch .an-dir{font:600 12px var(--font-m);fill:var(--text)}
.arch .an-hot{font:12px var(--font-t);fill:var(--muted)}
.arch .an-heat{font:600 13px var(--font-d)}
.arch .ae path{fill:none;stroke:var(--dim)}
.arch .ae.hl path{stroke:var(--text)}
.arch .ae text{font:12px var(--font-m);fill:var(--muted)}
.arch .ae.hl text{fill:var(--text)}
.arch .ae{cursor:pointer}
.arch .ae:hover path{stroke:var(--muted)}
.arch .ae.esel path{stroke:var(--accent)}
.arch .ae path.ae-hit{stroke:transparent;stroke-width:12}
.arch .ae.declared{cursor:default}
.arch .ae.declared path{stroke:var(--accent);stroke-dasharray:6 4}
.arch .ae.declared text{fill:var(--accent)}
.elink{font:12px var(--font-m);padding:4px 0;border-bottom:1px solid var(--border);word-break:break-all}
.elink-file{cursor:pointer;color:var(--text)}
.elink-file:hover{color:#fff}
.elink-file .ftier{display:inline-block;width:7px;height:7px;border-radius:50%;margin-right:4px}
.earrow{color:var(--dim);margin:0 6px}
.fmeta{margin-left:auto;font:12px var(--font-m);color:var(--muted);white-space:nowrap;padding-left:8px}
.arch .arrow{fill:var(--dim)}
.arch .an-frag{fill:none;stroke:var(--medium);stroke-width:1.5;stroke-dasharray:4 3}
.arch .an-likely{font:500 12px var(--font-m);fill:var(--medium)}
.arch-legend{display:flex;flex-wrap:wrap;gap:14px;align-items:center;margin-top:4px}
.arch-legend span{display:inline-flex;align-items:center;gap:5px}
.arch-legend i{display:inline-block;width:12px;height:10px;border-radius:2px;border:1.5px solid}
.arch-legend i.frag{border:1.5px dashed var(--medium);background:none}
.quad{flex:1;display:none;position:relative;overflow:hidden}
.quad svg{display:block}
.quad .qd{cursor:pointer;stroke:#000;stroke-width:.5}
.quad .qd:hover,.quad .qd.sel{stroke:#fff;stroke-width:2}
.quad .qline{stroke:var(--muted);stroke-dasharray:5 4}
.quad .qaxis{stroke:var(--border)}
.quad .qtick{font:12px var(--font-m);fill:var(--dim)}
.quad .qtitle{font:500 13px var(--font-d)}
.quad .qguide{font:12px var(--font-t);fill:var(--muted);paint-order:stroke;stroke:#000;stroke-width:3px}
.quad .qlab{font:500 12px var(--font-d);fill:var(--muted)}
.flist .ex-row{padding:4px 0}
.controls{display:flex;flex-wrap:wrap;gap:8px;padding:8px 24px;align-items:center;border-bottom:1px solid var(--border)}
.controls label{font:12px var(--font-t);color:var(--muted)}
.fbtn{padding:4px 8px;border:1px solid var(--border);border-radius:4px;background:var(--panel);color:var(--muted);cursor:pointer;font:12px var(--font-t)}
.fbtn.on{border-color:rgba(255,255,255,.24);color:#fff;background:var(--raise)}
.fbtn:hover{border-color:var(--muted)}
input[type=text]{background:var(--panel);border:1px solid var(--border);color:var(--text);padding:4px 8px;border-radius:4px;font:12px var(--font-m);width:180px}
input:focus-visible{outline:2px solid var(--medium);outline-offset:0}
input::placeholder{color:var(--dim)}
@media (max-width:760px){header{flex-wrap:wrap;padding:12px 16px}.view-toggle{max-width:100%;overflow-x:auto}.inbox,.detail{min-width:0}.main{flex-direction:column;height:auto}.detail{width:auto;border-left:0;border-top:1px solid var(--border)}.inbox{padding:16px}.inbox-cols{grid-template-columns:1fr}.controls{padding:8px 16px}}
@media (prefers-reduced-motion:reduce){*{transition:none!important}}
</style>
</head>
<body>
<header>
<h1>HighStakes</h1>
<span class="meta" id="meta"></span>
</header>
<div class="controls">
<div class="view-toggle" role="tablist" aria-label="Views">
<button role="tab" aria-selected="true" class="on" data-mode="inbox" onclick="setMode('inbox',this)">Action inbox</button>
<button role="tab" aria-selected="false" tabindex="-1" data-mode="treemap" onclick="setMode('treemap',this)">Treemap</button>
<button role="tab" aria-selected="false" tabindex="-1" data-mode="explorer" onclick="setMode('explorer',this)">Explorer</button>
<button role="tab" aria-selected="false" tabindex="-1" data-mode="arch" onclick="setMode('arch',this)">Architecture</button>
<button role="tab" aria-selected="false" tabindex="-1" data-mode="quadrant" onclick="setMode('quadrant',this)">Quadrant</button>
</div>
<span style="width:8px"></span>
<label>Posture</label>
<button class="fbtn on" aria-pressed="true" data-tier="all" onclick="setView('all',this)">All</button>
<button class="fbtn" aria-pressed="false" data-tier="critical" onclick="setView('critical',this)">Harden now</button>
<button class="fbtn" aria-pressed="false" data-tier="high" onclick="setView('high',this)">Protect</button>
<button class="fbtn" aria-pressed="false" data-tier="medium" onclick="setView('medium',this)">Stabilize</button>
<button class="fbtn" aria-pressed="false" data-tier="low" onclick="setView('low',this)">Routine</button>
<span style="width:8px"></span>
<label>Agents</label>
<button class="fbtn on" aria-pressed="true" data-auto="all" onclick="setAutonomy('all',this)">All</button>
<button class="fbtn" aria-pressed="false" data-auto="3" onclick="setAutonomy('3',this)" title="L3 auto-merge">L3</button>
<button class="fbtn" aria-pressed="false" data-auto="2" onclick="setAutonomy('2',this)" title="L2 auto-fix, human merges">L2</button>
<button class="fbtn" aria-pressed="false" data-auto="1" onclick="setAutonomy('1',this)" title="L1 propose only">L1</button>
<button class="fbtn" aria-pressed="false" data-auto="0" onclick="setAutonomy('0',this)" title="L0 assist only">L0</button>
<span style="flex:1"></span>
<label for="search">Search</label>
<input type=text id=search placeholder="Path or reason" oninput="onSearch()">
<label>Size</label>
<button class="fbtn on" aria-pressed="true" data-sz="lines" onclick="setSizing('lines',this)">Lines</button>
<button class="fbtn" aria-pressed="false" data-sz="impact" onclick="setSizing('impact',this)">Break cost</button>
</div>
<div class="main">
<div class="inbox active" id="inbox" role="tabpanel" aria-label="Action inbox"></div>
<div class="treemap-wrap" id="treemap-wrap" style="display:none">
<div class="breadcrumb" id="bread"></div>
<div class="treemap" id="treemap"></div>
</div>
<div class="explorer" id="explorer"></div>
<div class="arch" id="arch">
<div class="arch-hint"><div class="arch-bar"><label>Group by:</label>
<button class="fbtn on" data-g="package" onclick="setGroup('package',this)">Package</button>
<button class="fbtn" data-g="unit" onclick="setGroup('unit',this)">Unit</button>
<button class="fbtn" id="sup-toggle" onclick="toggleSupporting(this)" title="Unit view: show supporting units (examples, scripts, tests, helper bins)">Supporting units</button>
<label style="margin-left:12px">Zoom:</label>
<button class="fbtn" onclick="archZoomBy(1/1.25)" title="Zoom out">&#8722;</button>
<button class="fbtn" onclick="archZoomBy(1.25)" title="Zoom in">+</button>
<button class="fbtn" onclick="archFit()" title="Fit the whole graph on screen">Fit</button>
<button class="fbtn" onclick="archReset()" title="Actual size">1:1</button>
<span class="arch-zoom" id="arch-zoom"></span></div><div id="arch-text"></div></div>
<div class="arch-vp" id="archvp"></div>
</div>
<div class="quad" id="quadrant"></div>
<div class="detail" id="detail" aria-live="polite"><div class="empty">Pick a card or a file to see what to do and why</div></div>
</div>
<script>
const FILES=/*__FILES__*/[];
const STATS=/*__STATS__*/{};
const INBOX=/*__INBOX__*/{};
const ARCH=/*__ARCH__*/{};
const UNITS=/*__UNITS__*/{};
// One hue per posture, the same in every view. Tiers map 1:1 to postures.
const C={critical:'#f2555a',high:'#e0a43c',medium:'#6aa6e0',low:'#5fae6b'};
const POSTURE={critical:'Harden now',high:'Protect',medium:'Stabilize',low:'Routine'};
const VC={'Harden now':C.critical,'Protect':C.high,'Stabilize':C.medium,'Routine':C.low,'Not assessed':'#8c8c92'};
const QUAD={
  'fragile-critical':{label:'Harden now',guide:'likely to break and costly: owner sign-off, tests before merge'},
  'protect':{label:'Protect',guide:'costly if it breaks: owner review, no auto-merge'},
  'watch':{label:'Stabilize',guide:'breaks often, limited damage: regression tests, refactor'},
  'low':{label:'Routine',guide:'normal review'}};
const NOTE='Scores are a relative signal within this repo, not a probability.';
const EVIDENCE=/*__EVIDENCE__*/'';

document.getElementById('meta').textContent=(STATS.branch||'')+' @ '+(STATS.commit||'').slice(0,7)+'  |  '+STATS.analyzed_at+'  |  '+STATS.total+' files';

// Treemap
let currentFilter='all';
let currentAuto='all';
const AUTO=Object.fromEntries(FILES.map(f=>[f.path,f.autonomy?String(f.autonomy.level):'']));
let currentSizing='lines';
let currentPath=null;
let searchQ='';

function esc(s){return s.replace(/'/g,"\\'")}
function htmlEsc(s){return String(s).replace(/[&<>"']/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'})[c])}

function pressOnly(sel,btn){
  document.querySelectorAll(sel).forEach(b=>{const on=b===btn;b.classList.toggle('on',on);b.setAttribute('aria-pressed',on)});
}
function setView(tier,btn){
  currentFilter=tier;
  pressOnly('.fbtn[data-tier]',btn||document.querySelector('.fbtn[data-tier="'+tier+'"]'));
  rerender();
}

function setAutonomy(level,btn){
  currentAuto=level;
  pressOnly('.fbtn[data-auto]',btn);
  rerender();
}

function setSizing(mode,btn){
  currentSizing=mode;
  pressOnly('.fbtn[data-sz]',btn);
  renderTreemap();
}

function onSearch(){
  searchQ=document.getElementById('search').value.toLowerCase();
  rerender();
}

function getFiltered(){
  return FILES.filter(f=>{
    if(currentFilter!=='all'&&f.tier!==currentFilter)return false;
    if(currentAuto!=='all'&&AUTO[f.path]!==currentAuto)return false;
    if(searchQ&&!f.path.toLowerCase().includes(searchQ)&&!(f.reason||'').toLowerCase().includes(searchQ))return false;
    if(currentPath&&!f.dir.startsWith(currentPath)&&f.path!==currentPath)return false;
    return true;
  });
}

// Squarified treemap: produces near-square rectangles
function layoutSquarify(items,x,y,w,h){
  if(!items.length)return[];
  const sorted=[...items].sort((a,b)=>b.value-a.value);
  return _squarify(sorted,x,y,w,h);
}

function _squarify(items,x,y,w,h){
  if(items.length===0)return[];
  if(items.length===1)return[{...items[0],x,y,w,h}];

  const total=items.reduce((s,i)=>s+i.value,0);
  if(total<=0)return[];

  let bestIdx=1,bestWorst=Infinity;
  for(let i=1;i<=items.length;i++){
    const rowItems=items.slice(0,i);
    const rowTotal=rowItems.reduce((s,it)=>s+it.value,0);
    const rowFrac=rowTotal/total;

    let worst=0;
    for(const it of rowItems){
      const itFrac=it.value/rowTotal;
      let rw,rh;
      if(w>=h){rw=w*rowFrac;rh=h*itFrac}
      else{rh=h*rowFrac;rw=w*itFrac}
      const aspect=Math.max(rw/Math.max(rh,1),rh/Math.max(rw,1));
      if(aspect>worst)worst=aspect;
    }

    if(worst<=bestWorst){bestWorst=worst;bestIdx=i}
    else break;
  }

  const rowItems=items.slice(0,bestIdx);
  const rest=items.slice(bestIdx);
  const rowTotal=rowItems.reduce((s,it)=>s+it.value,0);
  const rowFrac=rowTotal/total;
  const rects=[];

  if(w>=h){
    const rw=w*rowFrac;
    let ry=y;
    for(const it of rowItems){
      const rh=h*(it.value/rowTotal);
      rects.push({...it,x,y:ry,w:rw,h:rh});
      ry+=rh;
    }
    rects.push(..._squarify(rest,x+rw,y,w-rw,h));
  }else{
    const rh=h*rowFrac;
    let rx=x;
    for(const it of rowItems){
      const rw=w*(it.value/rowTotal);
      rects.push({...it,x:rx,y,w:rw,h:rh});
      rx+=rw;
    }
    rects.push(..._squarify(rest,x,y+rh,w,h-rh));
  }

  return rects;
}

function getModuleName(path){
  const parts=path.split('/');
  // Group by top 2 levels: crates/core, services/api, packages/web
  if(parts.length>=2)return parts[0]+'/'+parts[1];
  return parts[0];
}

function renderTreemap(){
  const el=document.getElementById('treemap');
  const files=getFiltered();
  if(!files.length){el.innerHTML='<div class="empty">No files match filter</div>';return}

  const rect=el.getBoundingClientRect();
  const W=rect.width,H=rect.height;

  // Group files by top-level module
  const modules={};
  for(const f of files){
    const mod=currentPath?f.dir:getModuleName(f.path);
    if(!modules[mod])modules[mod]={name:mod,files:[],totalValue:0,maxHeat:0,tier:'low'};
    const val=currentSizing==='impact'?Math.max(f.impact*f.impact/10,5):Math.max(f.lines,10);
    modules[mod].files.push({...f,value:val});
    modules[mod].totalValue+=val;
    if(f.heat_score>modules[mod].maxHeat){modules[mod].maxHeat=f.heat_score;modules[mod].tier=f.tier}
  }

  const modList=Object.values(modules).sort((a,b)=>b.maxHeat-a.maxHeat||b.totalValue-a.totalValue);
  const modItems=modList.map(m=>({...m,value:m.totalValue}));

  // Layout modules with squarify
  const modRects=layoutSquarify(modItems,0,0,W,H);

  let html='';
  const PAD=2,HEADER=16;

  for(const mr of modRects){
    if(mr.w<6||mr.h<6)continue;

    const mc=C[mr.tier]||C.low;
    // Module container
    html+='<div class="tm-node" style="left:'+mr.x+'px;top:'+mr.y+'px;width:'+mr.w+'px;height:'+mr.h+'px;'+
      'background:'+mc+'0D;border-color:'+mc+'55;padding:0" onclick="drillDir(\''+esc(mr.name)+'\')">';

    // Module label
    if(mr.w>40&&mr.h>HEADER){
      const label=mr.name.split('/').pop()||mr.name;
      html+='<div style="padding:1px 5px;font:700 10px var(--font-d);color:'+mc+';letter-spacing:.5px;'+
        'background:'+mc+'18;border-bottom:1px solid '+mc+'33;overflow:hidden;white-space:nowrap;text-overflow:ellipsis">'+label+'</div>';
    }
    html+='</div>';

    // Layout files inside module
    const innerX=mr.x+PAD, innerY=mr.y+HEADER, innerW=mr.w-PAD*2, innerH=mr.h-HEADER-PAD;
    if(innerW<4||innerH<4)continue;

    const fileRects=layoutSquarify(mr.files,innerX,innerY,innerW,innerH);
    for(const fr of fileRects){
      if(fr.w<3||fr.h<3)continue;
      const fc=C[fr.tier]||C.low;
      const sat=30+fr.heat_score*0.7;
      html+='<div class="tm-node" style="left:'+fr.x+'px;top:'+fr.y+'px;width:'+fr.w+'px;height:'+fr.h+'px;'+
        'background:'+fc+';filter:saturate('+sat.toFixed(0)+'%);opacity:'+(0.35+fr.heat_score/100*0.65).toFixed(2)+'" '+
        'onclick="event.stopPropagation();selectFile(\''+esc(fr.path)+'\')" '+
        'title="'+attr(fr.path+'\n'+(POSTURE[fr.tier]||'')+'\n'+axesText(fr)+(fr.reason?'\n'+fr.reason:''))+'">';
      if(fr.w>55&&fr.h>30){
        html+='<div class="tm-label">'+htmlEsc(fr.name)+'</div>';
      }
      html+='</div>';
    }
  }

  el.innerHTML=html;
  updateBreadcrumb();
}

function drillDir(dir){
  if(currentPath===dir)currentPath=null;
  else currentPath=dir;
  renderTreemap();
}

function updateBreadcrumb(){
  const el=document.getElementById('bread');
  if(!currentPath){el.innerHTML='';return}
  const parts=currentPath.split('/');
  let html='<span onclick="currentPath=null;renderTreemap()">root</span>';
  let path='';
  for(const p of parts){
    path+=path?'/'+p:p;
    const pp=path;
    html+=' / <span onclick="currentPath=\''+esc(pp)+'\';renderTreemap()"'+(pp===currentPath?' class="current"':'')+'>'+p+'</span>';
  }
  el.innerHTML=html;
}

// meter is the signature readout: the score as a bar with a tick at the repo cutoff.
function meter(v,cut,color){
  return '<div class="meter" aria-hidden="true"><div class="fill" style="width:'+Math.max(0,Math.min(100,v))+'%;background:'+color+'"></div>'+
    (cut>0?'<div class="tick" style="left:calc('+Math.min(100,cut)+'% - 1px)"></div>':'')+'</div>';
}
function level(v,cut){return cut>0&&v>=cut?'high':'low'}
function axesText(f){
  return 'Will it break? '+f.likelihood+' (cutoff '+STATS.likely_cutoff+') · How bad if it breaks? '+
    (f.impact_assessed?f.impact+' (cutoff '+STATS.impact_threshold+')':'~'+f.impact+' estimate');
}
function verdictOf(f){return (f.explanation&&f.explanation.verdict)||(f.quadrant?QUAD[f.quadrant].label:'Not assessed')}

function cmdRow(cmd){
  return '<div class="cmd"><code>'+htmlEsc(cmd)+'</code><button class="copy" data-cmd="'+attr(cmd)+'" aria-label="Copy command">Copy</button></div>';
}
// copyCommand copies a backlog command; when the clipboard is blocked it
// selects the text so it can be copied by hand, and says so.
function copyCommand(btn){
  const cmd=btn.dataset.cmd;
  const done=ok=>{btn.textContent=ok?'Copied':'Select and copy';setTimeout(()=>{btn.textContent='Copy'},1600)};
  const fallback=()=>{
    const code=btn.parentElement.querySelector('code');const r=document.createRange();r.selectNodeContents(code);
    const sel=window.getSelection();sel.removeAllRanges();sel.addRange(r);
    let ok=false;try{ok=document.execCommand('copy')}catch(e){ok=false}
    done(ok);
  };
  if(navigator.clipboard&&window.isSecureContext)navigator.clipboard.writeText(cmd).then(()=>done(true),fallback);
  else fallback();
}
document.addEventListener('click',e=>{const b=e.target.closest('.copy');if(b)copyCommand(b)});

function selectFile(path){
  const f=FILES.find(x=>x.path===path);
  if(!f)return;
  const d=document.getElementById('detail');
  const v=verdictOf(f), vc=VC[v]||VC['Not assessed'];
  let h='<h3>'+htmlEsc(f.path)+'</h3><div class="verdict" style="color:'+vc+'">'+htmlEsc(v)+'</div>';
  const lc=STATS.likely_cutoff, ic=STATS.impact_threshold;
  h+='<div class="q"><div class="ql"><span>Will it break?</span><span><span class="qv">'+f.likelihood+'</span> <span class="qc">'+level(f.likelihood,lc)+' · repo cutoff '+lc+'</span></span></div>'+meter(f.likelihood,lc,vc)+'</div>';
  if(f.impact_assessed){
    h+='<div class="q"><div class="ql"><span>How bad if it breaks?</span><span><span class="qv">'+f.impact+'</span> <span class="qc">'+level(f.impact,ic)+' · repo cutoff '+ic+'</span></span></div>'+meter(f.impact,ic,vc)+'</div>';
  }else{
    h+='<div class="q"><div class="ql"><span>How bad if it breaks?</span><span><span class="qv">~'+f.impact+'</span> <span class="qc">estimate, run analyze with an LLM</span></span></div>'+meter(f.impact,0,'#8c8c92')+'</div>';
  }
  h+='<div class="note">'+NOTE+' '+htmlEsc(EVIDENCE)+'</div>';
  if(f.autonomy){
    h+='<div class="q"><div class="ql"><span>Agent autonomy</span><span><span class="qv">L'+f.autonomy.level+'</span> <span class="qc">'+htmlEsc(f.autonomy.label)+'</span></span></div><div class="note">'+htmlEsc(f.autonomy.reason)+'</div></div>';
  }

  const x=f.explanation;
  if(x&&x.actions){
    [['before_merge','Do before the next merge'],['backlog','Put on the backlog']].forEach(([scope,title])=>{
      const as=x.actions.filter(a=>a.scope===scope);
      if(!as.length)return;
      h+='<div class="sec"><div class="sec-title">'+title+'</div><ul class="check">';
      as.forEach(a=>{
        h+='<li><span class="box">'+(a.done?'[x]':'[ ]')+'</span><span>'+htmlEsc(a.text)+(a.signal?'<span class="sig">'+htmlEsc(a.signal)+'</span>':'')+'</span>'+(a.command?cmdRow(a.command):'')+'</li>';
      });
      h+='</ul></div>';
    });
  }

  h+='<details><summary>Why</summary>';
  if(x){
    [{t:f.likely_title,r:x.likelihood_reasons},{t:f.impact_title,r:x.impact_reasons}].forEach(sec=>{
      if(!sec.r||!sec.r.length)return;
      h+='<div class="sec"><div class="sec-title">'+htmlEsc(sec.t)+'</div>';
      sec.r.forEach(r=>{h+='<div class="reason">'+htmlEsc(r)+'</div>'});
      h+='</div>';
    });
  }else if(f.reason){
    h+='<div class="sec"><div class="reason">'+htmlEsc(f.reason)+'</div>'+(f.crit_reason?'<div class="reason">'+htmlEsc(f.crit_reason)+'</div>':'')+'</div>';
  }
  h+='<div class="sec"><div class="sec-title">Break cost by dimension</div>';
  [{l:'Security',k:'security'},{l:'Data',k:'data'},{l:'Availability',k:'availability'},{l:'User',k:'user'}].forEach(dm=>{
    const val=f[dm.k]||0;
    h+='<div class="bar-r"><span class="bl">'+dm.l+'</span><div class="b"><div class="bf" style="width:'+val+'%;background:'+vc+'"></div></div><span class="bv">'+val+'</span></div>';
  });
  h+='</div>';
  h+='<div class="sec"><div class="reason">'+f.imported_by+' files import it · '+f.lines+' lines · cyclomatic '+f.complexity+' · '+htmlEsc(f.language||'unknown language')+'</div></div>';
  h+='</details>';
  d.innerHTML=h;
}

// Action inbox: one before-merge card per Harden now or Protect file (its
// steps and owners) and one backlog card per action kind (its files and one
// combined snippet). Go does the grouping and ordering; this only renders.
// Postures from the static impact estimate carry INBOX.estimate_note.
// Posture filter and search apply.
const inboxOpen={before_merge:false,groups:new Set()};
function inboxMatch(path,verdict,text){
  const want=currentFilter==='all'?null:POSTURE[currentFilter];
  return (!want||verdict===want)&&(currentAuto==='all'||AUTO[path]===currentAuto)&&(!searchQ||path.toLowerCase().includes(searchQ)||(text||'').toLowerCase().includes(searchQ));
}
function fileCardHTML(c){
  const vc=VC[c.verdict]||VC['Not assessed'];
  let h='<article class="card"><span class="dot" style="background:'+vc+'" aria-hidden="true"></span>';
  h+='<button class="file title" data-path="'+attr(c.path)+'">'+htmlEsc(c.path)+'</button>';
  h+='<div class="side"><span class="posture" style="color:'+vc+'">'+htmlEsc(c.verdict)+'</span></div>';
  if(c.estimated)h+='<div class="why">'+htmlEsc(INBOX.estimate_note)+'</div>';
  h+='<ul class="steps">'+c.steps.map(s=>'<li><span class="box" aria-hidden="true">[ ]</span><span>'+htmlEsc(s.text)+(s.signal?'<span class="sig">'+htmlEsc(s.signal)+'</span>':'')+'</span></li>').join('')+'</ul>';
  if(c.owners&&c.owners.length)h+='<div class="who">Owners: '+c.owners.map(htmlEsc).join(', ')+'</div>';
  return h+'</article>';
}
function groupHTML(g,items){
  const vc=g.harden?VC['Harden now']:(VC[g.items[0].verdict]||VC['Not assessed']);
  const parts=[];
  if(g.harden)parts.push(g.harden+' Harden now');
  if(items.length<g.items.length)parts.push(items.length+' match the filter');
  let h='<details class="card grp" data-kind="'+attr(g.kind)+'"'+(inboxOpen.groups.has(g.kind)?' open':'')+'><summary><span class="dot" style="background:'+vc+'" aria-hidden="true"></span>';
  h+='<span class="do">'+htmlEsc(g.title)+'</span><span class="meta">'+htmlEsc(parts.join(' · '))+'</span></summary>';
  if(g.snippet)h+='<div class="snip"><code>'+htmlEsc(g.snippet)+'</code><button class="copy" data-cmd="'+attr(g.snippet)+'" aria-label="Copy all '+g.items.length+' lines">Copy</button></div>';
  h+='<ul class="gfiles">'+items.map(it=>'<li><button class="file" data-path="'+attr(it.path)+'">'+htmlEsc(it.path)+'</button><span class="sig">'+htmlEsc(it.verdict)+(it.estimated?' (estimated)':'')+(it.signal?' · '+htmlEsc(it.signal):'')+(it.owner?' · '+htmlEsc(it.owner):'')+'</span></li>').join('')+'</ul>';
  return h+'</details>';
}
function renderInbox(){
  const el=document.getElementById('inbox');
  const cards=(INBOX.before_merge||[]).filter(c=>inboxMatch(c.path,c.verdict,c.steps.map(s=>s.text+' '+(s.signal||'')).join(' ')));
  const top=INBOX.top||0;
  const shown=inboxOpen.before_merge?cards:cards.slice(0,top);
  const groups=(INBOX.backlog||[]).map(g=>({g,items:g.items.filter(it=>inboxMatch(it.path,it.verdict,g.title+' '+(it.signal||'')))})).filter(x=>x.items.length);
  const nFiles=new Set(groups.flatMap(x=>x.items.map(it=>it.path))).size;
  const lead=shown.length===1?'The file where a mistake would hurt most: any PR touching it':'The '+shown.length+' files where a mistake would hurt most: any PR touching them';
  let h='<p class="inbox-intro">'+(shown.length?lead+' needs these steps before merge. ':'')+'The backlog groups the fixes to make once, across files. '+(INBOX.estimate_note?'Files without an LLM assessment are placed by the static impact estimate and marked &ldquo;'+htmlEsc(INBOX.estimate_note)+'&rdquo;. ':'')+NOTE+'</p><div class="inbox-cols">';
  h+='<section aria-labelledby="h-before_merge"><h2 id="h-before_merge">Before merge <span class="count">'+shown.length+' of '+cards.length+' files</span></h2><div class="sub">Harden now first, then by how bad, then by will it break</div>';
  h+=cards.length?shown.map(fileCardHTML).join(''):'<div class="empty">Nothing here for this filter</div>';
  if(cards.length>top)h+='<button class="more" data-more="before_merge">'+(inboxOpen.before_merge?'Show top '+top:'Show all '+cards.length)+'</button>';
  h+='</section><section aria-labelledby="h-backlog"><h2 id="h-backlog">Backlog <span class="count">'+groups.length+' fixes across '+nFiles+' files</span></h2><div class="sub">Open each fix once, then work down its files. Copy takes every line in the group</div>';
  h+=groups.length?groups.map(x=>groupHTML(x.g,x.items)).join(''):'<div class="empty">Nothing here for this filter</div>';
  el.innerHTML=h+'</section></div>';
  el.querySelectorAll('.file').forEach(b=>b.addEventListener('click',()=>selectFile(b.dataset.path)));
  el.querySelectorAll('.more').forEach(b=>b.addEventListener('click',()=>{inboxOpen.before_merge=!inboxOpen.before_merge;renderInbox()}));
  el.querySelectorAll('details.grp').forEach(d=>d.addEventListener('toggle',()=>{if(d.open)inboxOpen.groups.add(d.dataset.kind);else inboxOpen.groups.delete(d.dataset.kind)}));
}

// View mode toggle
let viewMode='inbox';
function setMode(mode,btn){
  viewMode=mode;
  document.querySelectorAll('.view-toggle button').forEach(b=>{const on=b===btn;b.classList.toggle('on',on);b.setAttribute('aria-selected',on);b.tabIndex=on?0:-1});
  document.getElementById('inbox').style.display=mode==='inbox'?'block':'none';
  document.getElementById('treemap-wrap').style.display=mode==='treemap'?'block':'none';
  document.getElementById('explorer').style.display=mode==='explorer'?'block':'none';
  document.getElementById('arch').style.display=mode==='arch'?'flex':'none';
  document.getElementById('quadrant').style.display=mode==='quadrant'?'block':'none';
  rerender();
}

// Explorer view: file tree with heat indicators
function renderExplorer(){
  const el=document.getElementById('explorer');
  const files=getFiltered();
  if(!files.length){el.innerHTML='<div class="empty">No files match</div>';return}

  // Build tree structure
  const tree={children:{},files:[],maxHeat:0,tier:'low',name:'root'};
  for(const f of files){
    const parts=f.path.split('/');
    let node=tree;
    for(let i=0;i<parts.length-1;i++){
      const p=parts[i];
      if(!node.children[p])node.children[p]={children:{},files:[],maxHeat:0,tier:'low',name:p,expanded:true};
      node=node.children[p];
    }
    node.files.push(f);
    if(f.heat_score>node.maxHeat){node.maxHeat=f.heat_score;node.tier=f.tier}
  }

  // Propagate max heat up
  function prop(n){
    for(const c of Object.values(n.children)){
      prop(c);
      if(c.maxHeat>n.maxHeat){n.maxHeat=c.maxHeat;n.tier=c.tier}
    }
  }
  prop(tree);

  // Render tree
  let html='';
  function renderNode(node,depth,pathPrefix){
    // Sort dirs: hottest first
    const dirs=Object.entries(node.children).sort((a,b)=>b[1].maxHeat-a[1].maxHeat);
    // Sort files: hottest first
    const sortedFiles=[...node.files].sort((a,b)=>b.heat_score-a.heat_score);

    for(const [name,child] of dirs){
      const fullPath=pathPrefix?pathPrefix+'/'+name:name;
      const dc=C[child.tier]||C.low;
      const indent=depth*16;
      const id='dir-'+fullPath.replace(/[^a-zA-Z0-9]/g,'-');
      const fileCount=countFiles(child);

      html+='<div class="ex-row ex-dir" style="padding-left:'+(indent+8)+'px" onclick="toggleDir(\''+id+'\',this)">';
      html+='<span class="arrow" id="arr-'+id+'">▼</span>';
      html+='<span class="dname">'+name+'/</span>';
      html+='<span style="font:12px var(--font-t);color:var(--dim);margin-left:8px">'+fileCount+'</span>';
      html+='<span class="dheat" style="color:'+dc+'">'+POSTURE[child.tier]+'</span>';
      html+='</div>';
      html+='<div id="'+id+'">';
      renderNode(child,depth+1,fullPath);
      html+='</div>';
    }

    for(const f of sortedFiles){
      const fc=C[f.tier]||C.low;
      const indent=depth*16;
      const barW=Math.max(f.heat_score,2);
      html+='<div class="ex-row ex-file" style="padding-left:'+(indent+20)+'px" onclick="selectFile(\''+esc(f.path)+'\')">';
      html+='<span class="ftier" style="background:'+fc+'"></span>';
      html+='<span class="fname">'+f.name+'</span>';
      html+='<div class="ex-heat-bar"><div class="fill" style="width:'+barW+'%;background:'+fc+'"></div></div>';
      if(f.reason){html+='<span class="freason">'+f.reason+'</span>';}
      html+='<span class="fheat" style="color:'+fc+'">'+POSTURE[f.tier]+'</span>';
      html+='</div>';
    }
  }

  function countFiles(node){
    let c=node.files.length;
    for(const ch of Object.values(node.children))c+=countFiles(ch);
    return c;
  }

  renderNode(tree,0,'');
  el.innerHTML=html;
}

function toggleDir(id,row){
  const el=document.getElementById(id);
  const arr=document.getElementById('arr-'+id);
  if(!el)return;
  if(el.style.display==='none'){el.style.display='';arr.textContent='▼'}
  else{el.style.display='none';arr.textContent='▶'}
}

// Tabs: arrow keys, Home and End move between views.
document.querySelector('.view-toggle').addEventListener('keydown',e=>{
  const tabs=[...document.querySelectorAll('.view-toggle button')];
  const i=tabs.indexOf(document.activeElement);
  if(i<0)return;
  const j={ArrowRight:(i+1)%tabs.length,ArrowLeft:(i-1+tabs.length)%tabs.length,Home:0,End:tabs.length-1}[e.key];
  if(j===undefined)return;
  e.preventDefault();tabs[j].focus();tabs[j].click();
});

function rerender(){
  if(viewMode==='inbox')renderInbox();
  else if(viewMode==='treemap')renderTreemap();
  else if(viewMode==='arch')renderArch();
  else if(viewMode==='quadrant')renderQuadrant();
  else renderExplorer();
}

// Architecture view: nodes in dependency layers. Entry points on top,
// foundations at the bottom; arrows point from importer to dependency.
// Grouped by package (directory) or by runtime unit (binaries and scripts,
// with shared libraries as a band below). Wheel or pinch zooms, drag pans.
let archSel=null,archEdgeSel=null,archGroup='package',archLast=null,archDragged=false,showSupporting=false;
const archView={k:1,tx:0,ty:0,fit:true};
// unitData is the unit view without hidden supporting units, plus the links
// declared in .heatmap/architecture.yaml.
function unitData(){
  const hidden=new Set((UNITS.units||[]).filter(u=>u.supporting&&!showSupporting).map(u=>'u:'+u.name));
  const keep=e=>!hidden.has(e.from)&&!hidden.has(e.to);
  const declared=((UNITS.declared||{}).links||[]).map(l=>({from:'u:'+l.from,to:'u:'+l.to,label:l.label||'runtime link',both:!!l.both,evidence:l.evidence||''}));
  return {nodes:(UNITS.nodes||[]).filter(n=>!hidden.has(n.id)),edges:(UNITS.edges||[]).filter(keep),declared:declared.filter(keep),hidden:hidden.size};
}
function declaredPlacement(name){return ((UNITS.declared||{}).placement||[]).find(p=>p.unit===name)}
function toggleSupporting(btn){
  showSupporting=!showSupporting;btn.classList.toggle('on',showSupporting);
  archSel=null;archEdgeSel=null;archView.fit=true;renderArch();
}
function archData(){return archGroup==='unit'?unitData():ARCH}
function archLabel(n){return n.label||(n.id==='.'?'(root)':n.id)}
function archLayout(G){
  const NODE_H=50,GAP_X=28,GAP_Y=84,GAP_SUB=30,PAD=28;
  const maxLines=Math.max(1,...G.nodes.map(n=>n.lines));
  const width=n=>Math.round(Math.max(130+110*Math.sqrt(n.lines/maxLines),Math.min(300,archLabel(n).length*7.4+56)));
  const maxLevel=Math.max(0,...G.nodes.map(n=>n.level));
  const layers=[];
  for(let l=0;l<=maxLevel;l++)layers.push(G.nodes.filter(n=>n.level===l));
  const nbrs={};
  G.nodes.forEach(n=>nbrs[n.id]=[]);
  G.edges.forEach(e=>{if(nbrs[e.from]&&nbrs[e.to]){nbrs[e.from].push(e.to);nbrs[e.to].push(e.from)}});
  // Barycenter sweeps to reduce crossings; nodes arrive hottest-first.
  const pos={};
  layers.forEach(L=>L.forEach((n,i)=>pos[n.id]=i));
  for(let sweep=0;sweep<4;sweep++){
    const order=sweep%2?[...layers.keys()]:[...layers.keys()].reverse();
    for(const li of order){
      const L=layers[li];
      const bc=n=>{const ps=nbrs[n.id].map(m=>pos[m]);return ps.length?ps.reduce((a,b)=>a+b,0)/ps.length:pos[n.id]};
      const keyed=L.map(n=>[bc(n),n]);
      keyed.sort((a,b)=>a[0]-b[0]);
      layers[li]=keyed.map(k=>k[1]);
      layers[li].forEach((n,i)=>pos[n.id]=i);
    }
  }
  const rowW=L=>L.reduce((s,n)=>s+width(n),0)+GAP_X*Math.max(0,L.length-1);
  // Wrap wide layers into several rows so the graph stays near screen shape.
  const MAX_W=Math.max(1500,...G.nodes.map(width));
  const rows=[];
  for(let li=maxLevel;li>=0;li--){
    let row=[];
    for(const n of layers[li]){
      if(row.length&&rowW(row.concat([n]))>MAX_W){rows.push({li,row});row=[]}
      row.push(n);
    }
    rows.push({li,row});
  }
  const W=Math.max(...rows.map(r=>rowW(r.row)),300)+PAD*2;
  const boxes={};
  let y=PAD;
  rows.forEach((r,i)=>{
    if(i>0)y+=NODE_H+(rows[i-1].li===r.li?GAP_SUB:GAP_Y);
    let x=PAD+(W-PAD*2-rowW(r.row))/2;
    r.row.forEach(n=>{boxes[n.id]={n,x,y,w:width(n),h:NODE_H};x+=width(n)+GAP_X});
  });
  return {boxes,W,H:y+NODE_H+PAD};
}

// edgePath routes an arrow between two boxes: an arc above when they share
// a layer, else a vertical S-curve.
function edgePath(a,b){
  const x1=a.x+a.w/2,x2=b.x+b.w/2;
  let y1,y2,d;
  if(a.y===b.y){ // same layer (import cycle, or a unit linking another unit): arc above
    y1=a.y;y2=b.y;const lift=Math.min(60,20+Math.abs(x2-x1)/6);
    d='M'+x1+','+y1+' C'+x1+','+(y1-lift)+' '+x2+','+(y2-lift)+' '+x2+','+y2;
  }else{
    const down=a.y<b.y;
    y1=down?a.y+a.h:a.y;y2=down?b.y:b.y+b.h;
    const my=(y1+y2)/2;
    d='M'+x1+','+y1+' C'+x1+','+my+' '+x2+','+my+' '+x2+','+y2;
  }
  return {x1,y1,x2,y2,d};
}

function setGroup(g,btn){
  archGroup=g;archSel=null;archEdgeSel=null;archView.fit=true;renderArch();
}

function renderArch(){
  const G=archData(),unit=archGroup==='unit';
  const vp=document.getElementById('archvp'),txt=document.getElementById('arch-text');
  document.querySelectorAll('.arch-bar [data-g]').forEach(b=>b.classList.toggle('on',b.dataset.g===archGroup));
  if(!G.nodes||!G.nodes.length){
    archLast=null;txt.innerHTML='';
    vp.innerHTML='<div class="empty">'+(unit?'No runtime units: no Rust bins, Go main packages, package.json bin or start scripts, or Python scripts were found (or the heatmap predates units: re-run highstakes analyze).':'No files in heatmap')+'</div>';
    return;
  }
  archLast=archLayout(G);
  const {boxes,W,H}=archLast;
  const visible=new Set(getFiltered().map(f=>unit?f.unit_node:f.dir));
  const linked=new Set();
  const [eFrom,eTo]=archEdgeSel?archEdgeSel.split('\n'):[null,null];
  if(archSel){linked.add(archSel);G.edges.forEach(e=>{if(e.from===archSel)linked.add(e.to);if(e.to===archSel)linked.add(e.from)})}
  if(archEdgeSel){linked.add(eFrom);linked.add(eTo)}
  const focus=archSel||archEdgeSel;
  let s='<svg width="100%" height="100%" xmlns="http://www.w3.org/2000/svg">';
  s+='<defs><marker id="ah" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="7" markerHeight="7" orient="auto-start-reverse"><path class="arrow" d="M0,0L10,5L0,10z"/></marker></defs><g id="archg">';
  if(unit){
    const sh=Object.values(boxes).filter(b=>b.n.kind==='shared');
    if(sh.length){
      const y0=Math.min(...sh.map(b=>b.y))-26,y1=Math.max(...sh.map(b=>b.y+b.h))+12;
      s+='<rect class="band" x="8" y="'+y0+'" width="'+(W-16)+'" height="'+(y1-y0)+'" rx="8"/>';
      s+='<text class="band-label" x="18" y="'+(y0+15)+'">Shared libraries: linked by two or more units</text>';
    }
    s+='<text class="band-label" x="18" y="16">Runtime units: entry points and the code only they link</text>';
  }
  for(const e of G.edges){
    const a=boxes[e.from],b=boxes[e.to];
    if(!a||!b)continue;
    const {x1,y1,x2,y2,d}=edgePath(a,b);
    const sw=(1+Math.log2(e.count)).toFixed(2);
    const esel=archEdgeSel===e.from+'\n'+e.to;
    const hl=esel||(archSel&&(e.from===archSel||e.to===archSel));
    const dim=focus&&!hl;
    s+='<g class="ae'+(hl?' hl':'')+(esel?' esel':'')+(dim?' dim':'')+'" data-from="'+attr(e.from)+'" data-to="'+attr(e.to)+'"><path d="'+d+'" stroke-width="'+sw+'" marker-end="url(#ah)"/><path class="ae-hit" d="'+d+'"/>';
    s+='<text x="'+((x1+x2)/2+4)+'" y="'+((y1+y2)/2-(a.y===b.y?30:0))+'">'+e.count+'</text>';
    s+='<title>'+e.count+' file(s) in '+html(archLabel(a.n))+' import '+html(archLabel(b.n))+' ('+e.links_total+' file links). Click for the links.</title></g>';
  }
  for(const e of (unit&&G.declared)||[]){
    const a=boxes[e.from],b=boxes[e.to];
    if(!a||!b)continue;
    const {x1,y1,x2,y2,d}=edgePath(a,b);
    const dim=focus&&!(linked.has(e.from)&&linked.has(e.to));
    s+='<g class="ae declared'+(dim?' dim':'')+'"><path d="'+d+'" marker-end="url(#ah)"'+(e.both?' marker-start="url(#ah)"':'')+'/>';
    s+='<text x="'+((x1+x2)/2+4)+'" y="'+((y1+y2)/2-(a.y===b.y?30:0)+12)+'">'+html(e.label)+' (declared)</text>';
    s+='<title>Declared in .heatmap/architecture.yaml: '+html(archLabel(a.n))+(e.both?' &lt;-&gt; ':' -&gt; ')+html(archLabel(b.n))+': '+html(e.label)+(e.evidence?' ['+html(e.evidence)+']':'')+'</title></g>';
  }
  for(const id in boxes){
    const {n,x,y,w,h}=boxes[id];
    const c=C[n.impact_tier||n.tier]||C.low;
    const dim=(!visible.has(n.id))||(focus&&!linked.has(n.id));
    const score=n.max_impact?n.max_impact:Math.max(0,n.max_heat);
    let meta;
    const dp=unit&&n.kind==='unit'?declaredPlacement(n.label):null;
    if(unit)meta=n.kind==='unit'?(dp?'declared: '+dp.runs_in:n.placement?'inferred: '+n.placement:n.files+' own files · no placement found'):n.files+' files';
    else meta=(n.hottest||'').split('/').pop()+' · '+n.files+' files'+(n.likely>0?'':' · '+n.lines+' lines');
    s+='<g class="an'+(archSel===n.id?' sel':'')+(dim?' dim':'')+'" data-dir="'+attr(n.id)+'">';
    if(n.likely>0)s+='<rect class="an-frag" x="'+(x-4)+'" y="'+(y-4)+'" width="'+(w+8)+'" height="'+(h+8)+'" rx="9"/>';
    s+='<rect x="'+x+'" y="'+y+'" width="'+w+'" height="'+h+'" rx="6" fill="'+c+'2E" stroke="'+c+'"/>';
    s+='<text class="an-dir" x="'+(x+10)+'" y="'+(y+20)+'">'+html(clip(archLabel(n),w-50,7.2))+'</text>';
    s+='<text class="an-heat" x="'+(x+w-10)+'" y="'+(y+20)+'" text-anchor="end" fill="'+c+'">'+score+'</text>';
    s+='<text class="an-hot" x="'+(x+10)+'" y="'+(y+38)+'">'+html(clip(meta,w-(n.likely>0?70:20),6))+'</text>';
    if(n.likely>0)s+='<text class="an-likely" x="'+(x+w-10)+'" y="'+(y+38)+'" text-anchor="end">'+n.likely+' likely</text>';
    const members=unit&&n.members?'\ncomponents: '+n.members.join(', '):'';
    const place=(dp?'\ndeclared placement: '+html(dp.runs_in)+(dp.evidence?' ['+html(dp.evidence)+']':''):'')+(unit&&n.placement?'\ninferred placement: '+n.placement:'');
    s+='<title>'+html(archLabel(n))+members+place+'\nmax impact: '+(n.max_impact||'not assessed')+(n.hottest?'\nhottest: '+html(n.hottest)+' ('+n.max_heat+')':'')+'\n'+n.likely+' file(s) likely to need fixes\n'+n.files+' files, '+n.lines+' lines</title></g>';
  }
  s+='</g></svg>';
  const legend='<div class="arch-legend">'+['critical','high','medium','low'].map(t=>'<span><i style="border-color:'+C[t]+';background:'+C[t]+'2E"></i>'+t+' impact</span>').join('')+
    '<span><i class="frag"></i>contains files likely to need fixes</span></div>';
  const nav=' Click an arrow for the file links behind it. Wheel or pinch to zoom, drag to pan.';
  txt.innerHTML=(unit?
    'Boxes on top are runtime units: entry points found from manifests and main files, holding the components only they link. The band below holds shared libraries linked by two or more units. Placement labels are inferred from Dockerfiles, Helm values and Kubernetes manifests, not observed. Arrows point from importer to dependency (label = importing files); color = max impact; number = max impact. Dashed arrows and "declared" placements come from .heatmap/architecture.yaml.'+(G.hidden?' '+G.hidden+' supporting unit(s) hidden (Supporting units shows them).':'')+(UNITS.unlinked?' '+UNITS.unlinked+' non-test file(s) are linked by no unit.':''):
    'Packages by dependency layer: entry points on top, foundations below. Arrows point from importer to dependency (label = importing files). Size = lines; color = max impact if a file breaks (heat tier when not assessed); number = max impact. A dashed outline marks packages with files likely to need fixes. Click a package to list its files.')+nav+legend;
  vp.innerHTML=s;
  vp.querySelectorAll('.an').forEach(g=>g.addEventListener('click',()=>{if(!archDragged)selectDir(g.dataset.dir)}));
  vp.querySelectorAll('.ae:not(.declared)').forEach(g=>g.addEventListener('click',()=>{if(!archDragged)selectEdge(g.dataset.from,g.dataset.to)}));
  if(archView.fit)archFit();else archApply();
}

function archApply(){
  const g=document.getElementById('archg');
  if(g)g.setAttribute('transform','translate('+archView.tx.toFixed(1)+','+archView.ty.toFixed(1)+') scale('+archView.k.toFixed(4)+')');
  const z=document.getElementById('arch-zoom');
  if(z)z.textContent=Math.round(archView.k*100)+'%';
}
// archFit scales the whole graph into the visible part of the viewport
// (never above 100%).
function archFit(){
  const r=document.getElementById('archvp').getBoundingClientRect();
  const h=Math.min(r.height,window.innerHeight-Math.max(0,r.top));
  if(!archLast||!r.width||h<=0){archView.fit=true;return}
  const k=Math.min(r.width/archLast.W,h/archLast.H,1);
  archView.k=k;archView.tx=(r.width-archLast.W*k)/2;archView.ty=Math.max(0,(h-archLast.H*k)/2);
  archView.fit=false;archApply();
}
function archReset(){archView.k=1;archView.tx=0;archView.ty=0;archView.fit=false;archApply()}
function archZoomAt(f,px,py){
  const k2=Math.min(4,Math.max(0.03,archView.k*f));
  archView.tx=px-(px-archView.tx)*k2/archView.k;archView.ty=py-(py-archView.ty)*k2/archView.k;
  archView.k=k2;archView.fit=false;archApply();
}
function archZoomBy(f){const r=document.getElementById('archvp').getBoundingClientRect();archZoomAt(f,r.width/2,r.height/2)}
(function(){
  const vp=document.getElementById('archvp');
  const pts=new Map();let start=null,pinch=null;
  vp.addEventListener('wheel',e=>{e.preventDefault();const r=vp.getBoundingClientRect();archZoomAt(Math.exp(-e.deltaY*(e.ctrlKey?0.01:0.0015)),e.clientX-r.left,e.clientY-r.top)},{passive:false});
  vp.addEventListener('pointerdown',e=>{
    pts.set(e.pointerId,{x:e.clientX,y:e.clientY});archDragged=false;
    if(pts.size===1)start={x:e.clientX,y:e.clientY,tx:archView.tx,ty:archView.ty};
    if(pts.size===2){const [a,b]=[...pts.values()];pinch={d:Math.hypot(a.x-b.x,a.y-b.y)||1,k:archView.k};start=null}
  });
  vp.addEventListener('pointermove',e=>{
    if(!pts.has(e.pointerId))return;
    pts.set(e.pointerId,{x:e.clientX,y:e.clientY});
    if(pts.size===2&&pinch){
      const [a,b]=[...pts.values()],r=vp.getBoundingClientRect();
      archZoomAt(pinch.k*Math.hypot(a.x-b.x,a.y-b.y)/pinch.d/archView.k,(a.x+b.x)/2-r.left,(a.y+b.y)/2-r.top);
      archDragged=true;return;
    }
    if(!start)return;
    const dx=e.clientX-start.x,dy=e.clientY-start.y;
    if(Math.abs(dx)+Math.abs(dy)>4){archDragged=true;vp.classList.add('panning')}
    if(archDragged){archView.tx=start.tx+dx;archView.ty=start.ty+dy;archView.fit=false;archApply()}
  });
  const end=e=>{pts.delete(e.pointerId);if(pts.size<2)pinch=null;if(!pts.size){start=null;vp.classList.remove('panning')}};
  ['pointerup','pointercancel','pointerleave'].forEach(t=>vp.addEventListener(t,end));
  if(!(UNITS.nodes||[]).length){const b=document.querySelector('.arch-bar [data-g="unit"]');b.disabled=true;b.title='No runtime units in this heatmap'}
  if(!(UNITS.units||[]).some(u=>u.supporting)){const b=document.getElementById('sup-toggle');b.disabled=true;b.title='No supporting units in this heatmap'}
})();

// Quadrant view: x = likelihood of needing fixes, y = impact if it breaks.
let quadSel=null;
function renderQuadrant(){
  const el=document.getElementById('quadrant');
  const files=getFiltered().filter(f=>!f.test);
  if(!files.length){el.innerHTML='<div class="empty">No files match filter</div>';return}
  const assessed=files.some(f=>f.impact_assessed);
  const rect=el.getBoundingClientRect();
  const W=Math.max(rect.width,420),H=Math.max(rect.height-44,320);
  const L=54,R=18,T=34,B=44,pw=W-L-R,ph=H-T-B;
  const X=v=>L+v/100*pw, Y=v=>T+(100-v)/100*ph;
  const cut=STATS.likely_cutoff>0&&STATS.likely_cutoff<=100?STATS.likely_cutoff:null;
  const thr=STATS.impact_threshold||61;
  let s='<svg width="'+W+'" height="'+H+'" xmlns="http://www.w3.org/2000/svg">';
  s+='<rect class="qaxis" x="'+L+'" y="'+T+'" width="'+pw+'" height="'+ph+'" fill="none"/>';
  for(let v=0;v<=100;v+=20){
    s+='<text class="qtick" x="'+X(v)+'" y="'+(T+ph+14)+'" text-anchor="middle">'+v+'</text>';
    s+='<text class="qtick" x="'+(L-6)+'" y="'+(Y(v)+3)+'" text-anchor="end">'+v+'</text>';
  }
  s+='<text class="qlab" x="'+(L+pw/2)+'" y="'+(H-8)+'" text-anchor="middle">Will it break? (churn, complexity) &#8594;</text>';
  s+='<text class="qlab" transform="translate(14,'+(T+ph/2)+') rotate(-90)" text-anchor="middle">How bad if it breaks? &#8594;</text>';
  const xm=cut?X(cut):X(80), ym=Y(thr);
  if(cut)s+='<line class="qline" x1="'+xm+'" y1="'+T+'" x2="'+xm+'" y2="'+(T+ph)+'"><title>will it break: top 20% of non-test files (&#8805; '+cut+')</title></line>';
  s+='<line class="qline" x1="'+L+'" y1="'+ym+'" x2="'+(L+pw)+'" y2="'+ym+'"><title>how bad: &#8805; '+thr+'</title></line>';
  const corner=(q,x,y,anchor)=>{const c=q==='fragile-critical'?C.critical:q==='protect'?C.high:q==='watch'?C.medium:C.low;
    const room=anchor==='end'?(L+pw-xm-16):(xm-L-16);
    return '<text class="qtitle" x="'+x+'" y="'+y+'" text-anchor="'+anchor+'" fill="'+c+'">'+QUAD[q].label+'</text>'+
      QUAD[q].guide.split(': ').map((g,i)=>'<text class="qguide" x="'+x+'" y="'+(y+14+i*13)+'" text-anchor="'+anchor+'">'+html(clip(g,room,5.6))+'</text>').join('')};
  const jit=p=>{let h=0;for(const ch of p)h=(h*31+ch.charCodeAt(0))|0;return ((h%1000)/1000-0.5)*1.6};
  const sorted=[...files].sort((a,b)=>(a.impact+a.likelihood)-(b.impact+b.likelihood));
  for(const f of sorted){
    const c=C[f.tier]||C.low;
    const cx=X(Math.min(100,Math.max(0,f.likelihood+jit(f.path)))), cy=Y(Math.min(100,Math.max(0,f.impact+jit(f.path+'#'))));
    s+='<circle class="qd'+(quadSel===f.path?' sel':'')+'" data-path="'+attr(f.path)+'" cx="'+cx.toFixed(1)+'" cy="'+cy.toFixed(1)+'" r="'+(f.impact_assessed?5:4)+'" fill="'+c+'" fill-opacity="'+(f.impact_assessed?0.85:0.45)+'">'+
      '<title>'+html(f.path+'\n'+verdictOf(f)+'\n'+axesText(f)+(f.reason?'\n'+f.reason:''))+'</title></circle>';
  }
  // Labels after the dots so they stay readable on top.
  s+=corner('protect',L+8,T+16,'start')+corner('fragile-critical',L+pw-8,T+16,'end');
  s+=corner('low',L+8,T+ph-34,'start')+corner('watch',L+pw-8,T+ph-34,'end');
  s+='</svg>';
  const note=assessed?'':' Impact is estimated from centrality and path keywords (no LLM assessment); tiers come from heat.';
  el.innerHTML='<div class="arch-hint">Each dot is a file (tests left out). Right of the vertical line: Will it break? at or above the repo cutoff ('+(cut||'n/a')+'). Above the horizontal line: How bad if it breaks? at or above '+thr+'. Color = posture. '+NOTE+' Pick a dot for details.'+note+'</div>'+s;
  el.querySelectorAll('.qd').forEach(d=>d.addEventListener('click',()=>{quadSel=d.dataset.path;renderQuadrant();selectFile(d.dataset.path)}));
}

function clip(t,px,charW){const n=Math.max(4,Math.floor(px/charW));return t.length>n?t.slice(0,n-1)+'…':t}
function html(t){return String(t).replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;')}
function attr(t){return html(t).replace(/"/g,'&quot;')}

// fileRow renders a clickable file; with axes it adds impact, likelihood and quadrant.
function fileRow(f,label,axes){
  const fc=C[f.tier]||C.low;
  const meta=axes?'<span class="fmeta">break '+f.likelihood+' · bad '+(f.impact_assessed?'':'~')+f.impact+' · '+verdictOf(f)+'</span>':'<span class="fheat" style="color:'+fc+'">'+POSTURE[f.tier]+'</span>';
  return '<div class="ex-row ex-file" data-path="'+attr(f.path)+'"><span class="ftier" style="background:'+fc+'"></span><span class="fname">'+html(label)+'</span>'+meta+'</div>';
}

function selectDir(dir){
  archSel=archSel===dir?null:dir;archEdgeSel=null;
  renderArch();
  if(!archSel)return;
  const G=archData(),unit=archGroup==='unit';
  const n=G.nodes.find(x=>x.id===dir);
  const c=C[unit?(n.impact_tier||n.tier):n.tier]||C.low;
  const files=FILES.filter(f=>!f.test&&(unit?f.unit_node===dir:f.dir===dir));
  if(unit)files.sort((a,b)=>b.impact-a.impact||b.likelihood-a.likelihood||a.path.localeCompare(b.path));
  else files.sort((a,b)=>b.heat_score-a.heat_score);
  const name=id=>{const m=G.nodes.find(x=>x.id===id);return m?archLabel(m):id};
  const deps=G.edges.filter(e=>e.from===dir).map(e=>name(e.to));
  const users=G.edges.filter(e=>e.to===dir).map(e=>name(e.from));
  const sec=(t,b)=>'<div class="sec"><div class="sec-title">'+t+'</div>'+b+'</div>';
  const list=xs=>'<div class="reason">'+(xs&&xs.length?xs.map(html).join(', '):'none')+'</div>';
  let h='<h3>'+html(archLabel(n))+(unit?'':'/')+'</h3>';
  if(!unit){
    h+='<span class="tier-badge" style="background:'+c+'22;color:'+c+'">'+n.tier+' '+n.max_heat+'</span>';
  }else{
    h+='<span class="tier-badge" style="background:'+c+'22;color:'+c+'">'+(n.kind==='unit'?'runtime unit':'shared library')+' · impact '+(n.max_impact||'n/a')+'</span>';
    const u=(UNITS.units||[]).find(x=>'u:'+x.name===dir);
    if(u){
      h+=sec('Entry point','<div class="reason">'+html(u.entry)+' ('+u.kind+', '+html(u.evidence)+')</div>');
      h+=sec('Inferred placement',(u.inferred_placement||[]).length?u.inferred_placement.map(p=>'<div class="reason">'+html(p.detail)+' <span class="fmeta">'+html(p.evidence)+'</span></div>').join(''):'<div class="reason">none found in Dockerfiles, Helm values or Kubernetes manifests</div>');
      h+=sec('Links in total','<div class="reason">'+u.files+' files across '+u.components.length+' components; max impact '+u.max_impact+(u.impact_assessed?'':' (estimate)')+', '+u.fragile_critical+' fragile-critical</div>');
      h+=sec('Own components',list(n.members));
      h+=sec('Shared libraries it links',list(u.shared));
    }else{
      h+=sec('Linked by',list((UNITS.units||[]).filter(x=>(x.shared||[]).includes(n.label)).map(x=>x.name)));
    }
  }
  h+=sec(unit?'Files, most impactful first':'Files, hottest first','<div class="flist">'+files.map(f=>fileRow(f,unit?f.path:f.name,unit)).join('')+'</div>');
  h+=sec('Imports',list(deps));
  h+=sec('Imported by',list(users));
  const d=document.getElementById('detail');
  d.innerHTML=h;
  d.querySelectorAll('[data-path]').forEach(r=>r.addEventListener('click',()=>selectFile(r.dataset.path)));
}

// selectEdge lists the file-level imports behind an arrow.
function selectEdge(from,to){
  const G=archData();
  const e=G.edges.find(x=>x.from===from&&x.to===to);
  if(!e)return;
  const key=from+'\n'+to;
  archEdgeSel=archEdgeSel===key?null:key;archSel=null;
  renderArch();
  if(!archEdgeSel)return;
  const name=id=>{const m=G.nodes.find(x=>x.id===id);return m?archLabel(m):id};
  const byPath={};FILES.forEach(f=>byPath[f.path]=f);
  const side=p=>{const f=byPath[p],fc=f?(C[f.tier]||C.low):C.low;return '<span class="elink-file" data-path="'+attr(p)+'"><span class="ftier" style="background:'+fc+'"></span>'+html(p)+(f?' <span class="fmeta">I '+f.impact+'</span>':'')+'</span>'};
  let h='<h3>'+html(name(from))+' &#8594; '+html(name(to))+'</h3>';
  h+='<div class="reason">'+e.count+' importing file(s), '+e.links_total+' file link(s)'+(e.links_total>e.links.length?', showing the first '+e.links.length:'')+'.</div>';
  h+='<div class="sec"><div class="sec-title">File links: importer &#8594; imported</div>'+e.links.map(([a,b])=>'<div class="elink">'+side(a)+'<span class="earrow">&#8594;</span>'+side(b)+'</div>').join('')+'</div>';
  const d=document.getElementById('detail');
  d.innerHTML=h;
  d.querySelectorAll('[data-path]').forEach(r=>r.addEventListener('click',()=>selectFile(r.dataset.path)));
}

window.addEventListener('resize',()=>{if(viewMode==='treemap')renderTreemap();else if(viewMode==='quadrant')renderQuadrant()});
renderInbox();
</script>
</body>
</html>`
