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
			UnitNode: fileNode[f.Path],
		})
	}

	sort.Slice(files, func(i, j int) bool { return files[i].HeatScore > files[j].HeatScore })

	filesJSON, _ := json.Marshal(files)

	tierCounts := map[string]int{"critical": 0, "high": 0, "medium": 0, "low": 0}
	for _, f := range files {
		tierCounts[f.Tier]++
	}

	// Top action items: highest heat files that need human review
	var actionItems []fileEntry
	for _, f := range files {
		if !f.AutoMerge && len(actionItems) < 5 {
			actionItems = append(actionItems, f)
		}
	}
	actionsJSON, _ := json.Marshal(actionItems)

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
	html = strings.ReplaceAll(html, "/*__ACTIONS__*/[]", string(actionsJSON))

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
:root{--bg:#000;--panel:#0a0a0a;--border:#292929;--text:#E0E0E0;--muted:#A3A3A3;--dim:#4D4D4D;
--accent:#EE0000;--critical:#EE0000;--high:#E85D75;--medium:#D4874D;--low:#2D8A2D;
--font-d:'Red Hat Display',sans-serif;--font-t:'Red Hat Text',sans-serif;--font-m:'Red Hat Mono',monospace}
body{background:var(--bg);color:var(--text);font-family:var(--font-t)}

header{padding:14px 24px;border-bottom:2px solid var(--accent);display:flex;align-items:center;gap:16px}
header h1{font:700 18px var(--font-d);color:#fff}
header .meta{font:12px var(--font-m);color:var(--muted)}

.top{display:flex;gap:0;border-bottom:1px solid var(--border)}

.actions{flex:1;padding:16px 24px;border-right:1px solid var(--border)}
.actions h2{font:600 11px var(--font-d);text-transform:uppercase;letter-spacing:1.5px;color:var(--accent);margin-bottom:10px}
.action-card{display:flex;gap:10px;padding:8px 0;border-bottom:1px solid var(--border);cursor:pointer}
.action-card:hover{background:var(--panel)}
.action-card:last-child{border:none}
.action-score{font:700 20px var(--font-d);width:36px;text-align:center;flex-shrink:0}
.action-body{flex:1;min-width:0}
.action-path{font:500 12px var(--font-m);color:#fff;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.action-reason{font:11px var(--font-t);color:var(--muted);margin-top:2px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.action-tag{display:inline-block;font:600 9px var(--font-d);padding:1px 5px;border-radius:2px;text-transform:uppercase;margin-top:3px}

.tier-bar{width:280px;padding:16px 20px;display:flex;flex-direction:column;gap:6px;justify-content:center}
.tier-row{display:flex;align-items:center;gap:8px;height:22px}
.tier-label{font:600 10px var(--font-d);text-transform:uppercase;letter-spacing:1px;width:65px;text-align:right;flex-shrink:0}
.tier-fill{height:20px;border-radius:3px;transition:width .3s;cursor:pointer;display:flex;align-items:center;padding:0 6px;min-width:24px}
.tier-fill span{font:600 11px var(--font-m);color:#000}

.main{display:flex;height:calc(100vh - 200px)}

.treemap-wrap{flex:1;padding:8px;position:relative;overflow:hidden}
.treemap{width:100%;height:100%;position:relative}
.tm-node{position:absolute;border:1px solid rgba(0,0,0,.6);cursor:pointer;overflow:hidden;transition:opacity .15s;display:flex;flex-direction:column;justify-content:flex-end;padding:3px 5px}
.tm-node:hover{opacity:.85;z-index:10;border-color:#fff}
.tm-node.dimmed{opacity:.2}
.tm-label{font:500 10px var(--font-m);color:#fff;text-shadow:0 1px 3px rgba(0,0,0,.8);overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.tm-score{font:700 14px var(--font-d);color:#fff;text-shadow:0 1px 3px rgba(0,0,0,.8)}
.breadcrumb{position:absolute;top:10px;left:12px;z-index:20;display:flex;gap:4px;align-items:center}
.breadcrumb span{font:500 11px var(--font-m);color:var(--muted);cursor:pointer;padding:2px 6px;border-radius:3px;background:rgba(0,0,0,.7)}
.breadcrumb span:hover{color:#fff}
.breadcrumb span.current{color:#fff}

.detail{width:340px;border-left:1px solid var(--border);padding:16px;overflow-y:auto;background:var(--panel)}
.detail h3{font:600 14px var(--font-d);color:#fff;word-break:break-all;margin-bottom:4px}
.detail .tier-badge{display:inline-block;padding:2px 8px;border-radius:3px;font:600 11px var(--font-d);text-transform:uppercase;margin:6px 0}
.detail .sec{margin-top:14px}
.detail .sec-title{font:600 10px var(--font-d);text-transform:uppercase;letter-spacing:1.2px;color:var(--muted);margin-bottom:6px;padding-bottom:3px;border-bottom:1px solid var(--border)}
.detail .reason{font:13px/1.5 var(--font-t);margin-bottom:4px}
.detail .crit{font:500 12px var(--font-t);color:var(--accent)}
.bar-r{display:flex;align-items:center;gap:6px;margin:3px 0}
.bar-r .bl{font:11px var(--font-t);color:var(--muted);width:72px;text-align:right}
.bar-r .b{flex:1;height:5px;background:var(--border);border-radius:3px;overflow:hidden}
.bar-r .bf{height:100%;border-radius:3px}
.bar-r .bv{font:11px var(--font-m);width:24px}
.rev{background:var(--bg);border:1px solid var(--border);border-radius:5px;padding:8px;font:12px var(--font-t);margin-top:6px}
.rev div{margin:1px 0}
.atag{display:inline-block;padding:2px 6px;border-radius:3px;font:600 10px var(--font-d);margin-top:6px}
.atag.block{background:#5F0000;color:var(--critical)}
.atag.safe{background:#0A3D0A;color:var(--low)}
.empty{padding:30px;text-align:center;color:var(--dim);font:14px var(--font-t)}

.explorer{flex:1;overflow-y:auto;padding:4px 0;display:none}
.explorer.active{display:block}
.treemap-wrap.active{display:block}
.treemap-wrap{display:block}
.ex-row{display:flex;align-items:center;padding:3px 8px;cursor:pointer;border-left:3px solid transparent;gap:4px}
.ex-row:hover{background:var(--panel)}
.ex-row.selected{background:#111;border-left-color:var(--accent)}
.ex-dir{font:600 12px var(--font-d);color:var(--muted)}
.ex-dir .arrow{display:inline-block;width:12px;font-size:10px;color:var(--dim)}
.ex-dir .dname{color:#fff}
.ex-dir .dheat{margin-left:auto;font:600 11px var(--font-m)}
.ex-file{font:12px var(--font-m)}
.ex-file .fname{color:var(--text)}
.ex-file .fheat{margin-left:auto;font:600 12px var(--font-d);min-width:28px;text-align:right}
.ex-file .ftier{width:8px;height:8px;border-radius:50%;flex-shrink:0}
.ex-file .freason{color:var(--dim);font:11px var(--font-t);overflow:hidden;text-overflow:ellipsis;white-space:nowrap;max-width:350px;margin-left:8px}
.ex-heat-bar{width:60px;height:4px;background:var(--border);border-radius:2px;overflow:hidden;flex-shrink:0;margin-left:8px}
.ex-heat-bar .fill{height:100%;border-radius:2px}
.view-toggle{display:flex;border:1px solid var(--border);border-radius:4px;overflow:hidden;margin-left:8px}
.view-toggle button{padding:3px 10px;border:none;background:var(--panel);color:var(--muted);cursor:pointer;font:11px var(--font-d)}
.view-toggle button.on{background:var(--accent);color:#fff}
.arch{flex:1;overflow:hidden;display:none;flex-direction:column;position:relative;min-width:0}
.arch-hint{padding:6px 12px;font:11px var(--font-t);color:var(--muted);background:var(--bg);border-bottom:1px solid var(--border);z-index:2}
.arch-bar{display:flex;gap:6px;align-items:center;margin-bottom:5px}
.arch-bar label{font:10px var(--font-d);color:var(--muted);text-transform:uppercase;letter-spacing:1px}
.arch-bar .fbtn:disabled{opacity:.4;cursor:not-allowed}
.arch-zoom{font:11px var(--font-m);color:var(--dim);margin-left:4px}
.arch-vp{flex:1;position:relative;overflow:hidden;touch-action:none;cursor:grab}
.arch-vp.panning{cursor:grabbing}
.arch .band{fill:#ffffff08;stroke:var(--border);stroke-dasharray:4 4}
.arch .band-label{font:600 10px var(--font-d);fill:var(--muted);text-transform:uppercase;letter-spacing:1px}
.arch svg{display:block}
.arch .an{cursor:pointer}
.arch .an rect{stroke-width:1.5}
.arch .an:hover rect{stroke-width:2.5}
.arch .an.sel rect{stroke:var(--text);stroke-width:2.5}
.arch .an.dim,.arch .ae.dim{opacity:.15}
.arch .an-dir{font:600 12px var(--font-m);fill:var(--text)}
.arch .an-hot{font:11px var(--font-t);fill:var(--muted)}
.arch .an-heat{font:700 13px var(--font-d)}
.arch .ae path{fill:none;stroke:var(--dim)}
.arch .ae.hl path{stroke:var(--text)}
.arch .ae text{font:10px var(--font-m);fill:var(--muted)}
.arch .ae.hl text{fill:var(--text)}
.arch .ae{cursor:pointer}
.arch .ae:hover path{stroke:var(--muted)}
.arch .ae.esel path{stroke:var(--accent)}
.arch .ae path.ae-hit{stroke:transparent;stroke-width:12}
.arch .ae.declared{cursor:default}
.arch .ae.declared path{stroke:var(--accent);stroke-dasharray:6 4}
.arch .ae.declared text{fill:var(--accent)}
.elink{font:11px var(--font-m);padding:4px 0;border-bottom:1px solid var(--border);word-break:break-all}
.elink-file{cursor:pointer;color:var(--text)}
.elink-file:hover{color:#fff}
.elink-file .ftier{display:inline-block;width:7px;height:7px;border-radius:50%;margin-right:4px}
.earrow{color:var(--dim);margin:0 6px}
.fmeta{margin-left:auto;font:11px var(--font-m);color:var(--muted);white-space:nowrap;padding-left:8px}
.arch .arrow{fill:var(--dim)}
.arch .an-frag{fill:none;stroke:var(--medium);stroke-width:1.5;stroke-dasharray:4 3}
.arch .an-likely{font:600 10px var(--font-m);fill:var(--medium)}
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
.quad .qtick{font:10px var(--font-m);fill:var(--dim)}
.quad .qtitle{font:600 12px var(--font-d)}
.quad .qguide{font:11px var(--font-t);fill:var(--muted);paint-order:stroke;stroke:#000;stroke-width:3px}
.quad .qlab{font:600 11px var(--font-d);fill:var(--muted);text-transform:uppercase;letter-spacing:1px}
.flist .ex-row{padding:4px 0}
.controls{display:flex;gap:6px;padding:6px 12px;align-items:center;border-bottom:1px solid var(--border)}
.controls label{font:10px var(--font-d);color:var(--muted);text-transform:uppercase;letter-spacing:1px}
.fbtn{padding:3px 8px;border:1px solid var(--border);border-radius:3px;background:var(--panel);color:var(--muted);cursor:pointer;font:11px var(--font-t)}
.fbtn.on{border-color:var(--accent);color:#fff;background:rgba(238,0,0,.1)}
.fbtn:hover{border-color:var(--muted)}
input[type=text]{background:var(--panel);border:1px solid var(--border);color:var(--text);padding:3px 8px;border-radius:3px;font:12px var(--font-m);width:160px}
input:focus{outline:none;border-color:var(--accent)}
</style>
</head>
<body>
<header>
<h1>HighStakes</h1>
<span class="meta" id="meta"></span>
</header>
<div class="top">
<div class="actions" id="actions"></div>
<div class="tier-bar" id="tierbar"></div>
</div>
<div class="controls">
<div class="view-toggle">
<button class="on" onclick="setMode('treemap',this)">Treemap</button>
<button onclick="setMode('explorer',this)">Explorer</button>
<button onclick="setMode('arch',this)">Architecture</button>
<button onclick="setMode('quadrant',this)">Quadrant</button>
</div>
<label style="margin-left:12px">Tier:</label>
<button class="fbtn on" onclick="setView('all',this)">All</button>
<button class="fbtn" onclick="setView('critical',this)">Critical</button>
<button class="fbtn" onclick="setView('high',this)">High</button>
<button class="fbtn" onclick="setView('medium',this)">Medium</button>
<button class="fbtn" onclick="setView('low',this)">Low</button>
<span style="flex:1"></span>
<label>Search:</label>
<input type=text id=search placeholder="path or reason..." oninput="onSearch()">
<label style="margin-left:12px">Size:</label>
<button class="fbtn on" data-sz="lines" onclick="setSizing('lines',this)">Lines</button>
<button class="fbtn" data-sz="heat" onclick="setSizing('heat',this)">Heat</button>
</div>
<div class="main">
<div class="treemap-wrap active" id="treemap-wrap">
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
<div class="detail" id="detail"><div class="empty">Click a file to see its blast radius</div></div>
</div>
<script>
const FILES=/*__FILES__*/[];
const STATS=/*__STATS__*/{};
const ACTIONS=/*__ACTIONS__*/[];
const ARCH=/*__ARCH__*/{};
const UNITS=/*__UNITS__*/{};
const C={critical:'#EE0000',high:'#E85D75',medium:'#D4874D',low:'#2D8A2D'};
const QUAD={
  'fragile-critical':{label:'Fragile and critical',guide:'likely to break and costly when it does: senior review, add tests first'},
  'protect':{label:'Protect',guide:'stable but costly if broken: keep owners on review, never auto-merge'},
  'watch':{label:'Watch',guide:'changes and breaks often, limited damage: refactor or add tests'},
  'low':{label:'Low stakes',guide:'light review'}};

document.getElementById('meta').textContent=(STATS.branch||'')+' @ '+(STATS.commit||'').slice(0,7)+'  |  '+STATS.analyzed_at+'  |  '+STATS.total+' files';

// Action cards
const actEl=document.getElementById('actions');
actEl.innerHTML='<h2>Needs Your Attention</h2>'+ACTIONS.map(f=>{
  const c=C[f.tier];
  return '<div class="action-card" onclick="selectFile(\''+esc(f.path)+'\')">'+
    '<div class="action-score" style="color:'+c+'">'+f.heat_score+'</div>'+
    '<div class="action-body">'+
    '<div class="action-path">'+f.path+'</div>'+
    '<div class="action-reason">'+(f.reason||'')+'</div>'+
    '<span class="action-tag" style="background:'+c+'22;color:'+c+'">'+f.tier+'</span>'+
    '</div></div>';
}).join('');

// Tier bar
const tb=document.getElementById('tierbar');
const mx=Math.max(...Object.values(STATS.tiers),1);
['critical','high','medium','low'].forEach(t=>{
  const n=STATS.tiers[t]||0;
  const w=Math.max(n/mx*100,n?8:0);
  tb.innerHTML+='<div class="tier-row"><span class="tier-label" style="color:'+C[t]+'">'+t+'</span>'+
    '<div class="tier-fill" style="width:'+w+'%;background:'+C[t]+'" onclick="setView(\''+t+'\')"><span>'+n+'</span></div></div>';
});

// Treemap
let currentFilter='all';
let currentSizing='lines';
let currentPath=null;
let searchQ='';

function esc(s){return s.replace(/'/g,"\\'")}
function htmlEsc(s){return String(s).replace(/[&<>"']/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'})[c])}

function setView(tier,btn){
  currentFilter=tier;
  document.querySelectorAll('.controls .fbtn:not([data-sz])').forEach(b=>b.classList.remove('on'));
  if(btn)btn.classList.add('on');
  else document.querySelector('.controls .fbtn[onclick*="'+tier+'"]')?.classList.add('on');
  rerender();
}

function setSizing(mode,btn){
  currentSizing=mode;
  document.querySelectorAll('.fbtn[data-sz]').forEach(b=>b.classList.remove('on'));
  btn.classList.add('on');
  renderTreemap();
}

function onSearch(){
  searchQ=document.getElementById('search').value.toLowerCase();
  rerender();
}

function getFiltered(){
  return FILES.filter(f=>{
    if(currentFilter!=='all'&&f.tier!==currentFilter)return false;
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
    const val=currentSizing==='heat'?Math.max(f.heat_score*f.heat_score/10,5):Math.max(f.lines,10);
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
        'title="'+fr.path+'  (score: '+fr.heat_score+')\n'+(fr.reason||'')+'">';
      if(fr.w>55&&fr.h>30){
        html+='<div class="tm-score">'+fr.heat_score+'</div><div class="tm-label">'+fr.name+'</div>';
      }else if(fr.w>35&&fr.h>18){
        html+='<div class="tm-label" style="font-size:9px">'+fr.name+'</div>';
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

function selectFile(path){
  const f=FILES.find(x=>x.path===path);
  if(!f)return;
  const d=document.getElementById('detail');
  const c=C[f.tier]||C.low;

  let h='<h3>'+f.path+'</h3>';
  h+='<span class="tier-badge" style="background:'+c+'22;color:'+c+'">'+f.tier+' '+f.heat_score+'</span>';

  h+='<div class="sec"><div class="sec-title">Two axes</div>';
  [{l:'Impact',v:f.impact,n:f.impact_assessed?'damage if it breaks':'estimate (no LLM)'},{l:'Likelihood',v:f.likelihood,n:'chance it needs fixes'}].forEach(a=>{
    const v=a.v||0, bc=v>=86?C.critical:v>=61?C.high:v>=31?C.medium:C.low;
    h+='<div class="bar-r"><span class="bl">'+a.l+'</span><div class="b"><div class="bf" style="width:'+v+'%;background:'+bc+'"></div></div><span class="bv" style="color:'+bc+'">'+v+'</span></div>';
    h+='<div class="reason" style="font-size:11px;color:var(--muted);margin:0 0 4px 78px">'+a.n+'</div>';
  });
  if(f.quadrant)h+='<div class="reason"><strong>'+QUAD[f.quadrant].label+'</strong>: '+QUAD[f.quadrant].guide+'</div>';
  h+='</div>';

  const x=f.explanation;
  if(x){
    h+='<div class="reason" style="font-size:11px;color:var(--muted)">'+htmlEsc(x.headline)+'</div>';
    [{t:f.impact_title,r:x.impact_reasons},{t:f.likely_title,r:x.likelihood_reasons}].forEach(sec=>{
      if(!sec.r||!sec.r.length)return;
      h+='<div class="sec"><div class="sec-title">'+htmlEsc(sec.t)+'</div>';
      sec.r.forEach(r=>{h+='<div class="reason">'+htmlEsc(r)+'</div>'});
      h+='</div>';
    });
    if(x.actions&&x.actions.length){
      h+='<div class="sec"><div class="sec-title">What to do</div>';
      x.actions.forEach(a=>{h+='<div class="reason">&#8226; '+htmlEsc(a.text)+'</div>'});
      h+='</div>';
    }
  }

  if(f.reason&&!x){
    h+='<div class="sec"><div class="sec-title">Blast Radius</div>';
    h+='<div class="reason">'+f.reason+'</div>';
    if(f.crit_reason)h+='<div class="crit">'+f.crit_reason+'</div>';
    h+='</div>';
  }

  h+='<div class="sec"><div class="sec-title">Impact Dimensions</div>';
  [{l:'Security',k:'security'},{l:'Data',k:'data'},{l:'Availability',k:'availability'},{l:'User',k:'user'}].forEach(d=>{
    const v=f[d.k]||0;
    const bc=v>=80?C.critical:v>=60?C.high:v>=40?C.medium:C.low;
    h+='<div class="bar-r"><span class="bl">'+d.l+'</span><div class="b"><div class="bf" style="width:'+v+'%;background:'+bc+'"></div></div><span class="bv" style="color:'+bc+'">'+v+'</span></div>';
  });
  h+='</div>';

  if(f.imported_by>0){
    h+='<div class="sec"><div class="sec-title">Breakage Impact</div>';
    h+='<div class="reason" style="color:var(--critical)"><strong>'+f.imported_by+' files break</strong> if this changes</div>';
    h+='</div>';
  }

  h+='<div class="sec"><div class="sec-title">Details</div><div class="rev">';
  h+='<div>Reviewers: <strong>'+f.reviewers+'</strong></div>';
  h+='<div>Complexity: <strong>'+f.complexity+'</strong></div>';
  h+='<div>Lines: <strong>'+f.lines+'</strong></div>';
  h+='<div>Language: <strong>'+(f.language||'?')+'</strong></div>';
  h+='</div></div>';

  h+=f.auto_merge?'<span class="atag safe">AUTO-MERGE OK</span>':'<span class="atag block">HUMAN REVIEW REQUIRED</span>';

  d.innerHTML=h;
}

// View mode toggle
let viewMode='treemap';
function setMode(mode,btn){
  viewMode=mode;
  document.querySelectorAll('.view-toggle button').forEach(b=>b.classList.remove('on'));
  btn.classList.add('on');
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
      html+='<span style="font:11px var(--font-t);color:var(--dim);margin-left:6px">'+fileCount+'</span>';
      html+='<span class="dheat" style="color:'+dc+'">'+child.maxHeat+'</span>';
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
      html+='<span class="fheat" style="color:'+fc+'">'+f.heat_score+'</span>';
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

function rerender(){
  if(viewMode==='treemap')renderTreemap();
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
  s+='<text class="qlab" x="'+(L+pw/2)+'" y="'+(H-8)+'" text-anchor="middle">Likelihood of needing fixes (churn, complexity) &#8594;</text>';
  s+='<text class="qlab" transform="translate(14,'+(T+ph/2)+') rotate(-90)" text-anchor="middle">Impact if it breaks &#8594;</text>';
  const xm=cut?X(cut):X(80), ym=Y(thr);
  if(cut)s+='<line class="qline" x1="'+xm+'" y1="'+T+'" x2="'+xm+'" y2="'+(T+ph)+'"><title>likely: top 20% of non-test files (likelihood &#8805; '+cut+')</title></line>';
  s+='<line class="qline" x1="'+L+'" y1="'+ym+'" x2="'+(L+pw)+'" y2="'+ym+'"><title>impactful: impact &#8805; '+thr+'</title></line>';
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
      '<title>'+html(f.path)+'\nimpact '+f.impact+(f.impact_assessed?'':' (estimate)')+', likelihood '+f.likelihood+(f.quadrant?'\n'+QUAD[f.quadrant].label:'')+(f.reason?'\n'+html(f.reason):'')+'</title></circle>';
  }
  // Labels after the dots so they stay readable on top.
  s+=corner('protect',L+8,T+16,'start')+corner('fragile-critical',L+pw-8,T+16,'end');
  s+=corner('low',L+8,T+ph-34,'start')+corner('watch',L+pw-8,T+ph-34,'end');
  s+='</svg>';
  const note=assessed?'':' Impact is estimated from centrality and path keywords (no LLM assessment); tiers come from heat.';
  el.innerHTML='<div class="arch-hint">Each dot is a file (tests left out). Right of the vertical line: top 20% likelihood in this repo. Above the horizontal line: impact &#8805; '+thr+'. Color = tier. Click a dot for details.'+note+'</div>'+s;
  el.querySelectorAll('.qd').forEach(d=>d.addEventListener('click',()=>{quadSel=d.dataset.path;renderQuadrant();selectFile(d.dataset.path)}));
}

function clip(t,px,charW){const n=Math.max(4,Math.floor(px/charW));return t.length>n?t.slice(0,n-1)+'…':t}
function html(t){return String(t).replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;')}
function attr(t){return html(t).replace(/"/g,'&quot;')}

// fileRow renders a clickable file; with axes it adds impact, likelihood and quadrant.
function fileRow(f,label,axes){
  const fc=C[f.tier]||C.low;
  const meta=axes?'<span class="fmeta">I '+f.impact+(f.impact_assessed?'':'~')+' · L '+f.likelihood+(f.quadrant?' · '+QUAD[f.quadrant].label:'')+'</span>':'<span class="fheat" style="color:'+fc+'">'+f.heat_score+'</span>';
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
renderTreemap();
</script>
</body>
</html>`
