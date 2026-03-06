package tracediffsvg

import (
	"io"
	"text/template"
)

var htmlTemplate = template.Must(template.New("diff-view").Parse(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8"/>
<meta name="viewport" content="width=device-width, initial-scale=1"/>
<title>Trace Diff Viewer</title>
<script src="https://d3js.org/d3.v7.min.js"></script>
<style>
  * { margin: 0; padding: 0; box-sizing: border-box; }
  body { font-family: 'SF Mono','Menlo','Monaco','Consolas',monospace; background: #0f1117; color: #e0e0e0; overflow: hidden; }

  .toolbar {
    position: fixed; top: 0; left: 0; right: 0; z-index: 100;
    background: #1a1b26; padding: 10px 20px;
    display: flex; align-items: center; gap: 10px;
    border-bottom: 1px solid #2a2b36;
    font-size: 12px;
  }
  .toolbar label { color: #888; font-size: 11px; }
  .toolbar input {
    background: #2a2b36; border: 1px solid #3a3b46; color: #e0e0e0;
    padding: 5px 10px; border-radius: 4px; font-family: inherit; font-size: 12px; width: 280px;
  }
  .toolbar input:focus { border-color: #7aa2f7; outline: none; }
  .toolbar button {
    background: #7aa2f7; color: #1a1b26; border: none;
    padding: 6px 18px; border-radius: 4px; cursor: pointer;
    font-family: inherit; font-size: 12px; font-weight: 700;
  }
  .toolbar button:hover { background: #89b4fa; }
  .toolbar .sep { width: 1px; height: 24px; background: #3a3b46; }
  .toolbar .zoom-btn {
    background: #2a2b36; color: #888; padding: 5px 10px; font-size: 13px; border: 1px solid #3a3b46;
  }
  .toolbar .zoom-btn:hover { background: #3a3b46; color: #e0e0e0; }
  .toolbar .info { margin-left: auto; color: #555; font-size: 10px; }

  .legend-bar {
    position: fixed; top: 44px; left: 0; right: 0; z-index: 99;
    background: #16161e; padding: 6px 20px;
    display: flex; align-items: center; gap: 16px; flex-wrap: wrap;
    border-bottom: 1px solid #2a2b36;
    font-size: 10px;
  }
  .legend-item { display: flex; align-items: center; gap: 5px; color: #565f89; }
  .legend-hex-svg { display: inline-block; width: 18px; height: 18px; vertical-align: middle; }
  .legend-sep { width: 1px; height: 16px; background: #2a2b36; }

  #canvas { width: 100%; height: calc(100vh - 74px); margin-top: 74px; }

  .edge { fill: none; stroke: #3a3b46; stroke-width: 1.5; }
  .edge-arrow { fill: #3a3b46; }
  .hex { stroke-width: 2; cursor: pointer; transition: filter 0.15s; }
  .hex:hover { filter: brightness(1.3); }
  .arrow-badge { font-size: 14px; font-weight: 900; text-anchor: middle; }
  .arrow-slower { fill: #f7768e; }
  .arrow-faster { fill: #9ece6a; }
  .node-group.dimmed { opacity: 0.25; }
  .edge.dimmed { opacity: 0.12; }
  .node-label { font-size: 11px; font-weight: 600; fill: #c0caf5; }
  .node-svc { font-size: 10px; fill: #565f89; }
  .node-kind { font-size: 8px; font-weight: 700; text-anchor: middle; }
  .dur-base { font-size: 10px; fill: #f7768e; font-weight: 700; }
  .dur-next { font-size: 10px; fill: #9ece6a; font-weight: 700; }
  .dur-delta { font-size: 10px; font-weight: 700; fill: #565f89; }
  .dur-delta.delta-pos { fill: #f7768e; }
  .dur-delta.delta-neg { fill: #9ece6a; }


  /* Loading / error overlays */
  .overlay {
    position: fixed; top: 44px; left: 0; right: 0; bottom: 0;
    display: flex; justify-content: center; align-items: center;
    font-size: 14px; color: #888; z-index: 50;
  }
  .overlay.error { color: #f7768e; }
  .spinner { animation: spin 1s linear infinite; }
  @keyframes spin { to { transform: rotate(360deg); } }

  /* Tooltip */
  .tooltip {
    position: fixed; z-index: 200; pointer-events: none;
    background: #1a1b26; color: #c0caf5; padding: 10px 14px;
    border: 1px solid #3a3b46; border-radius: 6px;
    font-size: 11px; line-height: 1.7;
    box-shadow: 0 8px 24px rgba(0,0,0,0.5);
    display: none; max-width: 420px;
  }
  .tooltip .tt-op { font-weight: 700; color: #7aa2f7; margin-bottom: 4px; }
  .tooltip .tt-svc { color: #565f89; }
  .tooltip .tt-dur { margin-top: 6px; }
  .tooltip .tt-base { color: #f7768e; }
  .tooltip .tt-next { color: #9ece6a; }
  .tooltip .tt-delta-pos { color: #f7768e; font-weight: 700; }
  .tooltip .tt-delta-neg { color: #9ece6a; font-weight: 700; }
</style>
</head>
<body>

<div class="toolbar">
  <form id="form" style="display:flex;align-items:center;gap:8px;" onsubmit="return handleSubmit(event)">
    <label>base</label>
    <input id="inp-base" name="base" value="{{.BaseID}}" placeholder="base trace ID"/>
    <label>next</label>
    <input id="inp-next" name="next" value="{{.NextID}}" placeholder="next trace ID"/>
    <button type="submit">Diff</button>
  </form>
  <div class="sep"></div>
  <label>min delta</label>
  <input id="inp-delta" type="number" value="{{.MinDelta}}" min="0" step="1" placeholder="ms"
    style="width:70px" oninput="applyHighlight()" title="highlight nodes where |delta| >= this value"/>
  <label>ms</label>
  <div class="sep"></div>
  <button class="zoom-btn" onclick="zoomIn()" title="Zoom in">+</button>
  <button class="zoom-btn" onclick="zoomOut()" title="Zoom out">-</button>
  <button class="zoom-btn" onclick="zoomReset()" title="Fit to screen">Fit</button>
  <span class="info">scroll to zoom, drag to pan</span>
</div>

<div class="legend-bar">
  <span class="legend-item"><svg class="legend-hex-svg" viewBox="-10 -10 20 20"><polygon points="8.7,5 0,10 -8.7,5 -8.7,-5 0,-10 8.7,-5" fill="#2d1f1f" stroke="#f7768e" stroke-width="2"/></svg>base-only (removed)</span>
  <span class="legend-item"><svg class="legend-hex-svg" viewBox="-10 -10 20 20"><polygon points="8.7,5 0,10 -8.7,5 -8.7,-5 0,-10 8.7,-5" fill="#1f2d1f" stroke="#9ece6a" stroke-width="2"/></svg>next-only (added)</span>
  <span class="legend-item"><svg class="legend-hex-svg" viewBox="-10 -10 20 20"><polygon points="8.7,5 0,10 -8.7,5 -8.7,-5 0,-10 8.7,-5" fill="#1f1f2d" stroke="#7aa2f7" stroke-width="2"/></svg>both (overlaid)</span>
  <span class="legend-item"><svg class="legend-hex-svg" viewBox="-10 -10 20 20"><polygon points="8.7,5 0,10 -8.7,5 -8.7,-5 0,-10 8.7,-5" fill="#2d2a1f" stroke="#e0af68" stroke-width="2"/></svg>modified</span>
  <span class="legend-item"><svg class="legend-hex-svg" viewBox="-10 -10 20 20"><polygon points="8.7,5 0,10 -8.7,5 -8.7,-5 0,-10 8.7,-5" fill="#1f1f1f" stroke="#565f89" stroke-width="2"/></svg>unchanged</span>
  <span class="legend-sep"></span>
  <span class="legend-item"><span style="color:#f7768e;font-size:14px;font-weight:900">&#9660;</span> got slower</span>
  <span class="legend-item"><span style="color:#9ece6a;font-size:14px;font-weight:900">&#9650;</span> got faster</span>
</div>
<svg id="canvas"></svg>
<div class="tooltip" id="tooltip"></div>
<div class="overlay" id="overlay"></div>

<script>
const DIFF_API = '/api/v2/traces/diff';
const LLM_ACCEPT = 'application/vnd.grafana.llm';

const HEX_R = 30;
const H_SPACE = 240;
const V_SPACE = 150;
const PAD_TOP = 60;
const PAD_X = 80;

const STATUS_COLORS = {
  'base-only': { fill: '#2d1f1f', stroke: '#f7768e', kind: '#f7768e' },
  'next-only': { fill: '#1f2d1f', stroke: '#9ece6a', kind: '#9ece6a' },
  'both':      { fill: '#1f1f2d', stroke: '#7aa2f7', kind: '#7aa2f7' },
  'modified':  { fill: '#2d2a1f', stroke: '#e0af68', kind: '#e0af68' },
  'unchanged': { fill: '#1f1f1f', stroke: '#565f89', kind: '#565f89' },
};

let svgEl, gRoot, zoomBehavior, lastRenderedNodes = [], lastRenderedEdges = [];

// --- Init ---
document.addEventListener('DOMContentLoaded', () => {
  svgEl = d3.select('#canvas');
  gRoot = svgEl.append('g');

  zoomBehavior = d3.zoom()
    .scaleExtent([0.1, 6])
    .on('zoom', (e) => gRoot.attr('transform', e.transform));
  svgEl.call(zoomBehavior);

  const params = new URLSearchParams(window.location.search);
  const base = params.get('base');
  const next = params.get('next');
  if (base && next) fetchAndRender(base, next);
});

function handleSubmit(e) {
  e.preventDefault();
  const base = document.getElementById('inp-base').value.trim();
  const next = document.getElementById('inp-next').value.trim();
  if (!base || !next) return false;
  const delta = document.getElementById('inp-delta').value;
  let qs = '?base=' + base + '&next=' + next;
  if (delta && parseFloat(delta) > 0) qs += '&minDelta=' + delta;
  history.pushState(null, '', qs);
  fetchAndRender(base, next);
  return false;
}

function zoomIn() { svgEl.transition().duration(300).call(zoomBehavior.scaleBy, 1.4); }
function zoomOut() { svgEl.transition().duration(300).call(zoomBehavior.scaleBy, 0.7); }
function zoomReset() {
  // Fit content to viewport
  const bbox = gRoot.node().getBBox();
  if (!bbox.width) return;
  const vw = window.innerWidth, vh = window.innerHeight - 74;
  const scale = Math.min(vw / (bbox.width + 120), vh / (bbox.height + 120), 1.5);
  const tx = (vw - bbox.width * scale) / 2 - bbox.x * scale;
  const ty = (vh - bbox.height * scale) / 2 - bbox.y * scale + 22;
  svgEl.transition().duration(500).call(zoomBehavior.transform,
    d3.zoomIdentity.translate(tx, ty).scale(scale));
}

// --- Data fetching ---
async function fetchAndRender(baseID, nextID) {
  const overlay = document.getElementById('overlay');
  overlay.className = 'overlay';
  overlay.innerHTML = '<span class="spinner">&#9881;</span>&nbsp; Loading traces...';

  try {
    const url = DIFF_API + '?base=' + encodeURIComponent(baseID) + '&next=' + encodeURIComponent(nextID);
    const resp = await fetch(url, { headers: { 'Accept': LLM_ACCEPT } });
    if (!resp.ok) {
      const body = await resp.text();
      throw new Error(resp.status + ': ' + body);
    }
    const data = await resp.json();
    overlay.style.display = 'none';
    renderTree(data, baseID, nextID);
    applyHighlight();
    setTimeout(zoomReset, 50);
  } catch (err) {
    overlay.className = 'overlay error';
    overlay.textContent = 'Error: ' + err.message;
  }
}

// --- Tree building ---
function shortKind(k) {
  return (k || '').replace('SPAN_KIND_', '');
}

function buildMergedTree(data) {
  // Extract flat spans with diff status
  const spans = [];
  for (const svc of (data.trace?.services || [])) {
    for (const scope of (svc.scopes || [])) {
      for (const span of (scope.spans || [])) {
        spans.push({
          spanId: span.spanId,
          parentId: span.parentSpanId || '',
          service: svc.serviceName || '',
          name: span.name,
          duration: span.durationMs || 0,
          status: (span.attributes || {})['tempo.diff.status'] || 'unchanged',
          kind: shortKind(span.kind),
        });
      }
    }
  }

  // Split by status
  const base = spans.filter(s => s.status === 'removed' || s.status === 'unchanged' || s.status === 'modified');
  const next = spans.filter(s => s.status === 'added' || s.status === 'unchanged' || s.status === 'modified');

  function buildTree(list) {
    const byId = new Map(list.map(s => [s.spanId, { ...s, children: [] }]));
    const roots = [];
    for (const n of byId.values()) {
      const parent = byId.get(n.parentId);
      if (parent) parent.children.push(n);
      else roots.push(n);
    }
    // Sort children by duration desc
    const sortNodes = (nodes) => {
      nodes.sort((a, b) => b.duration - a.duration);
      nodes.forEach(n => sortNodes(n.children));
    };
    sortNodes(roots);
    roots.sort((a, b) => b.duration - a.duration);
    return roots;
  }

  const baseTree = buildTree(base);
  const nextTree = buildTree(next);

  // Merge trees by matching name+kind at each level
  function matchKey(n) { return n.name + '\0' + n.kind; }

  function merge(baseNodes, nextNodes) {
    const nextByKey = new Map();
    for (const n of nextNodes) {
      const k = matchKey(n);
      if (!nextByKey.has(k)) nextByKey.set(k, []);
      nextByKey.get(k).push(n);
    }
    const consumed = new Map();
    const seen = new Set();
    const result = [];

    for (const bn of baseNodes) {
      const k = matchKey(bn);
      seen.add(k);
      const node = {
        name: bn.name, kind: bn.kind,
        service: bn.service,
        baseDur: bn.duration, nextDur: 0,
        status: 'base-only', children: [],
      };
      const nextList = nextByKey.get(k) || [];
      const idx = consumed.get(k) || 0;
      if (idx < nextList.length) {
        const nn = nextList[idx];
        consumed.set(k, idx + 1);
        node.nextDur = nn.duration;
        node.service = bn.service || nn.service;
        node.status = bn.status === 'unchanged' ? 'unchanged' : bn.status === 'modified' ? 'modified' : 'both';
        node.children = merge(bn.children, nn.children);
      } else {
        node.children = merge(bn.children, []);
      }
      result.push(node);
    }

    // Next-only
    for (const nn of nextNodes) {
      const k = matchKey(nn);
      const nextList = nextByKey.get(k) || [];
      const idx = consumed.get(k) || 0;
      if (seen.has(k) && idx > 0) {
        consumed.set(k, idx - 1);
        continue;
      }
      result.push({
        name: nn.name, kind: nn.kind,
        service: nn.service,
        baseDur: 0, nextDur: nn.duration,
        status: 'next-only',
        children: merge([], nn.children),
      });
    }
    return result;
  }

  return merge(baseTree, nextTree);
}

// --- Layout ---
function layoutTree(roots) {
  let leafIdx = 0;
  function assign(node, depth) {
    node.depth = depth;
    node.y = PAD_TOP + depth * V_SPACE;
    if (!node.children.length) {
      node.x = PAD_X + leafIdx * H_SPACE;
      leafIdx++;
      return;
    }
    node.children.forEach(c => assign(c, depth + 1));
    node.x = (node.children[0].x + node.children[node.children.length - 1].x) / 2;
  }
  roots.forEach(r => assign(r, 0));

  // Flatten
  const all = [];
  function collect(n) { all.push(n); n.children.forEach(collect); }
  roots.forEach(collect);
  return all;
}

// --- Rendering ---
function renderTree(data, baseID, nextID) {
  gRoot.selectAll('*').remove();

  const roots = buildMergedTree(data);
  const nodes = layoutTree(roots);

  // Draw edges and store references
  lastRenderedEdges = [];
  for (const n of nodes) {
    for (const c of n.children) {
      const midY = (n.y + HEX_R + c.y - HEX_R) / 2;
      const edgeEl = gRoot.append('path')
        .attr('class', 'edge')
        .attr('d', 'M' + n.x + ',' + (n.y + HEX_R + 2) +
              ' C' + n.x + ',' + midY + ' ' + c.x + ',' + midY + ' ' + c.x + ',' + (c.y - HEX_R - 6))
        .attr('marker-end', 'url(#arrow)');
      lastRenderedEdges.push({ el: edgeEl, parent: n, child: c });
    }
  }

  // Arrow marker
  const defs = gRoot.append('defs');
  defs.append('marker')
    .attr('id', 'arrow').attr('markerWidth', 10).attr('markerHeight', 7)
    .attr('refX', 10).attr('refY', 3.5).attr('orient', 'auto')
    .append('path').attr('d', 'M0,0 L10,3.5 L0,7z').attr('class', 'edge-arrow');

  // Draw nodes
  const tooltip = document.getElementById('tooltip');

  lastRenderedNodes = nodes;

  for (const n of nodes) {
    const absDelta = (n.baseDur > 0 && n.nextDur > 0) ? Math.abs(n.nextDur - n.baseDur) : 0;
    const g = gRoot.append('g')
      .attr('transform', 'translate(' + n.x + ',' + n.y + ')')
      .attr('class', 'node-group')
      .attr('data-abs-delta', absDelta.toFixed(2));
    n._el = g;
    const colors = STATUS_COLORS[n.status] || STATUS_COLORS['unchanged'];

    // Hexagon
    const pts = hexPoints(0, 0, HEX_R);
    const delta = (n.baseDur > 0 && n.nextDur > 0) ? n.nextDur - n.baseDur : 0;
    g.append('polygon')
      .attr('points', pts)
      .attr('class', 'hex')
      .attr('fill', colors.fill)
      .attr('stroke', colors.stroke);

    // Kind inside hex
    g.append('text')
      .attr('y', 3)
      .attr('class', 'node-kind')
      .attr('fill', colors.kind)
      .text(n.kind);

    // Arrow badge for delta direction (positioned to the left of the hex)
    if (delta > 0.5) {
      g.append('text')
        .attr('x', -(HEX_R + 10))
        .attr('y', 5)
        .attr('class', 'arrow-badge arrow-slower')
        .text('\u25BC'); // down triangle = slower (red)
    } else if (delta < -0.5) {
      g.append('text')
        .attr('x', -(HEX_R + 10))
        .attr('y', 5)
        .attr('class', 'arrow-badge arrow-faster')
        .text('\u25B2'); // up triangle = faster (green)
    }

    // Labels to the right
    const lx = HEX_R + 10;
    let ly = -16;

    let label = n.name;
    if (label.length > 28) label = label.slice(0, 25) + '...';
    g.append('text').attr('x', lx).attr('y', ly).attr('class', 'node-label').text(label);
    ly += 14;

    if (n.service) {
      let svc = n.service;
      if (svc.length > 28) svc = svc.slice(0, 25) + '...';
      g.append('text').attr('x', lx).attr('y', ly).attr('class', 'node-svc').text(svc);
      ly += 14;
    }

    if (n.baseDur > 0) {
      g.append('text').attr('x', lx).attr('y', ly).attr('class', 'dur-base').text('base: ' + n.baseDur.toFixed(1) + 'ms');
      ly += 13;
    }
    if (n.nextDur > 0) {
      g.append('text').attr('x', lx).attr('y', ly).attr('class', 'dur-next').text('next: ' + n.nextDur.toFixed(1) + 'ms');
      ly += 13;
    }
    if (n.baseDur > 0 && n.nextDur > 0) {
      const delta = n.nextDur - n.baseDur;
      let txt, cls;
      const pct = n.baseDur > 0 ? ((delta / n.baseDur) * 100).toFixed(0) : '0';
      if (delta > 0.5) { txt = '+' + delta.toFixed(1) + 'ms (+' + pct + '%)'; cls = 'delta-pos'; }
      else if (delta < -0.5) { txt = delta.toFixed(1) + 'ms (' + pct + '%)'; cls = 'delta-neg'; }
      else { txt = '~0ms'; cls = ''; }
      g.append('text').attr('x', lx).attr('y', ly).attr('class', 'dur-delta ' + cls).text(txt);
    }

    // Tooltip
    g.on('mouseenter', (e) => {
      let html = '<div class="tt-op">' + esc(n.name) + ' [' + n.kind + ']</div>';
      if (n.service) html += '<div class="tt-svc">' + esc(n.service) + '</div>';
      html += '<div class="tt-dur">';
      if (n.baseDur > 0) html += '<div class="tt-base">base: ' + n.baseDur.toFixed(2) + ' ms</div>';
      if (n.nextDur > 0) html += '<div class="tt-next">next: ' + n.nextDur.toFixed(2) + ' ms</div>';
      if (n.baseDur > 0 && n.nextDur > 0) {
        const d = n.nextDur - n.baseDur;
        const pct = n.baseDur > 0 ? ((d / n.baseDur) * 100).toFixed(1) : '0';
        const cls = d > 0.5 ? 'tt-delta-pos' : d < -0.5 ? 'tt-delta-neg' : '';
        html += '<div class="' + cls + '">delta: ' + (d > 0 ? '+' : '') + d.toFixed(2) + 'ms (' + (d > 0 ? '+' : '') + pct + '%)</div>';
      }
      html += '</div><div class="tt-svc">status: ' + n.status + '</div>';
      tooltip.innerHTML = html;
      tooltip.style.display = 'block';
    })
    .on('mousemove', (e) => {
      tooltip.style.left = (e.clientX + 16) + 'px';
      tooltip.style.top = (e.clientY + 16) + 'px';
    })
    .on('mouseleave', () => { tooltip.style.display = 'none'; });
  }
}

function hexPoints(cx, cy, r) {
  const pts = [];
  for (let i = 0; i < 6; i++) {
    const a = Math.PI / 6 + i * Math.PI / 3;
    pts.push((cx + r * Math.cos(a)).toFixed(1) + ',' + (cy + r * Math.sin(a)).toFixed(1));
  }
  return pts.join(' ');
}

// applyHighlight dims nodes below the delta threshold.
// The glow (red=slower, green=faster) is always present on nodes with a delta.
// minDelta controls the threshold: only nodes with |delta| >= |minDelta| stay visible.
// If minDelta is positive, only slower nodes above the threshold are highlighted.
// If minDelta is negative, only faster nodes above the threshold are highlighted.
// If minDelta is 0, all nodes are visible with their default glows.
function applyHighlight() {
  const raw = parseFloat(document.getElementById('inp-delta').value);
  const threshold = isNaN(raw) ? 0 : raw;

  const params = new URLSearchParams(window.location.search);
  if (threshold !== 0) params.set('minDelta', threshold);
  else params.delete('minDelta');
  history.replaceState(null, '', '?' + params.toString());

  if (threshold === 0 || !lastRenderedNodes.length) {
    d3.selectAll('.node-group').classed('dimmed', false);
    d3.selectAll('.edge').classed('dimmed', false);
    return;
  }

  const visible = new Set();
  for (const n of lastRenderedNodes) {
    const delta = (n.baseDur > 0 && n.nextDur > 0) ? n.nextDur - n.baseDur : 0;
    const passes = Math.abs(delta) >= threshold;
    n._el.classed('dimmed', !passes);
    if (passes) visible.add(n);
  }

  for (const e of lastRenderedEdges) {
    const show = visible.has(e.parent) || visible.has(e.child);
    e.el.classed('dimmed', !show);
  }
}

function esc(s) {
  return s.replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;');
}
</script>
</body>
</html>`))

var landingTemplate = template.Must(template.New("landing").Parse(`<!DOCTYPE html>
<html lang="en"><head>
<meta charset="utf-8"/><title>Trace Diff Viewer</title>
<style>
  body { font-family: 'SF Mono',monospace; background: #0f1117; color: #c0caf5;
    display: flex; justify-content: center; align-items: center; height: 100vh; }
  .card { background: #1a1b26; padding: 40px; border-radius: 8px; border: 1px solid #2a2b36; }
  h2 { margin-bottom: 20px; color: #7aa2f7; }
  label { display: block; margin: 10px 0 4px; color: #565f89; font-size: 12px; }
  input { width: 360px; padding: 8px; background: #2a2b36; border: 1px solid #3a3b46;
    border-radius: 4px; font-family: inherit; font-size: 13px; color: #c0caf5; }
  input:focus { border-color: #7aa2f7; outline: none; }
  button { margin-top: 16px; padding: 10px 28px; background: #7aa2f7; color: #1a1b26;
    border: none; border-radius: 4px; cursor: pointer; font-family: inherit; font-size: 13px; font-weight: 700; }
  button:hover { background: #89b4fa; }
</style></head><body>
<div class="card">
  <h2>Trace Diff Viewer</h2>
  <form method="get">
    <label>Base Trace ID</label><input name="base" placeholder="Enter base trace ID" autofocus/>
    <label>Next Trace ID</label><input name="next" placeholder="Enter next trace ID"/>
    <br/><button type="submit">View Diff</button>
  </form>
</div>
</body></html>`))

type htmlData struct {
	BaseID   string
	NextID   string
	MinDelta string
}

// RenderViewPage writes the diff viewer HTML page.
// If baseID and nextID are provided, it renders the app with those IDs pre-filled
// and auto-fetches on load. Otherwise it renders the landing page.
func RenderViewPage(w io.Writer, baseID, nextID, minDelta string) error {
	if baseID == "" || nextID == "" {
		return landingTemplate.Execute(w, nil)
	}
	if minDelta == "" {
		minDelta = "0"
	}
	return htmlTemplate.Execute(w, htmlData{BaseID: baseID, NextID: nextID, MinDelta: minDelta})
}
