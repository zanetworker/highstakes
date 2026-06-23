package dashboard

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/zanetworker/code-heatmap/internal/types"
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
}

func Generate(hm *types.Heatmap, outputPath string) error {
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

		files = append(files, fileEntry{
			Path: f.Path, Dir: dir, Name: name,
			HeatScore: f.HeatScore, Tier: string(f.Tier),
			Reason: reason, CritReason: critReason,
			Security: security, Data: data, Availability: avail, User: user,
			AutoMerge: f.ReviewRequirements.AutoMerge,
			Reviewers: f.ReviewRequirements.MinReviewers,
			Lines: lines, Language: f.Language,
			Complexity: f.Factors.Complexity.Cyclomatic,
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
		"total":       len(files),
		"tiers":       tierCounts,
		"repo":        hm.Metadata.RepoPath,
		"analyzed_at": hm.Metadata.AnalyzedAt.Format("2006-01-02 15:04"),
		"commit":      hm.Metadata.CommitSHA,
		"branch":      hm.Metadata.Branch,
		"languages":   hm.Metadata.Languages,
	})

	html := strings.ReplaceAll(tmpl, "/*__FILES__*/[]", string(filesJSON))
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
<title>Code Heatmap Dashboard</title>
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
<h1>Code Heatmap</h1>
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
<div class="detail" id="detail"><div class="empty">Click a file to see its blast radius</div></div>
</div>
<script>
const FILES=/*__FILES__*/[];
const STATS=/*__STATS__*/{};
const ACTIONS=/*__ACTIONS__*/[];
const C={critical:'#EE0000',high:'#E85D75',medium:'#D4874D',low:'#2D8A2D'};

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

function setView(tier,btn){
  currentFilter=tier;
  document.querySelectorAll('.controls .fbtn:not([data-sz])').forEach(b=>b.classList.remove('on'));
  if(btn)btn.classList.add('on');
  else document.querySelector('.controls .fbtn[onclick*="'+tier+'"]')?.classList.add('on');
  if(viewMode==='treemap')renderTreemap();else renderExplorer();
}

function setSizing(mode,btn){
  currentSizing=mode;
  document.querySelectorAll('.fbtn[data-sz]').forEach(b=>b.classList.remove('on'));
  btn.classList.add('on');
  renderTreemap();
}

function onSearch(){
  searchQ=document.getElementById('search').value.toLowerCase();
  if(viewMode==='treemap')renderTreemap();else renderExplorer();
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
  // Group by top 2 levels: crates/openshell-server, python/openshell, e2e/rust
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

  if(f.reason){
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
  if(mode==='treemap')renderTreemap();
  else renderExplorer();
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

window.addEventListener('resize',()=>{if(viewMode==='treemap')renderTreemap()});
renderTreemap();
</script>
</body>
</html>`
