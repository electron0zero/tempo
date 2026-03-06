package tracediffsvg

import (
	"io"
	"text/template"
)

var waterfallTemplate = template.Must(template.New("waterfall-view").Parse(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8"/>
<meta name="viewport" content="width=device-width, initial-scale=1"/>
<title>Trace Diff - Waterfall</title>
<style>
  * { margin: 0; padding: 0; box-sizing: border-box; }
  body { font-family: 'SF Mono','Menlo','Monaco','Consolas',monospace; background: #0f1117; color: #e0e0e0; }

  .toolbar {
    position: sticky; top: 0; z-index: 100;
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
  .toolbar .info { margin-left: auto; color: #555; font-size: 10px; }
  .toolbar .info a { color: #7aa2f7; }

  .legend-bar {
    position: sticky; top: 44px; z-index: 99;
    background: #16161e; padding: 6px 20px;
    display: flex; align-items: center; gap: 16px; flex-wrap: wrap;
    border-bottom: 1px solid #2a2b36;
    font-size: 10px;
  }
  .legend-item { display: flex; align-items: center; gap: 5px; color: #565f89; }
  .legend-swatch { width: 24px; height: 10px; border-radius: 2px; display: inline-block; }
  .legend-sep { width: 1px; height: 16px; background: #2a2b36; }

  .time-header {
    position: sticky; top: 72px; z-index: 98;
    background: #0f1117;
    display: flex; height: 24px;
    border-bottom: 1px solid #2a2b36;
  }
  .time-header-label { width: 320px; flex-shrink: 0; }
  .time-header-axis { flex: 1; position: relative; }
  .time-tick {
    position: absolute; top: 0; bottom: 0;
    border-left: 1px solid #1a1b26;
    font-size: 9px; color: #565f89;
    padding-left: 4px; line-height: 24px;
  }

  #rows { min-height: 200px; }

  .wf-row {
    display: flex; height: 36px; border-bottom: 1px solid #1a1b2600;
  }
  .wf-row:nth-child(odd) { background: #0f1117; }
  .wf-row:nth-child(even) { background: #12131b; }
  .wf-row:hover { background: #1a1b26; }

  .wf-label {
    width: 320px; flex-shrink: 0; display: flex; align-items: center;
    padding: 0 8px; overflow: hidden; border-right: 1px solid #2a2b36;
    position: relative;
  }
  .wf-label-text { white-space: nowrap; overflow: hidden; text-overflow: ellipsis; font-size: 11px; }
  .wf-label-svc { font-size: 8px; color: #565f89; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; position: absolute; bottom: 2px; left: 8px; }
  .wf-kind { position: absolute; right: 8px; top: 50%; transform: translateY(-50%); font-size: 8px; font-weight: 700; color: #3a3b46; }
  .wf-depth-dot { width: 4px; height: 4px; border-radius: 50%; background: #3a3b46; flex-shrink: 0; margin-right: 4px; }

  .wf-bars {
    flex: 1; position: relative; overflow: hidden;
  }
  .wf-bar {
    position: absolute; height: 10px; border-radius: 2px; min-width: 2px;
  }
  .wf-dur {
    position: absolute; font-size: 8px; font-weight: 600;
    white-space: nowrap; line-height: 10px;
  }
  .wf-delta {
    position: absolute; top: 50%; transform: translateY(-50%);
    font-size: 9px; font-weight: 700; white-space: nowrap;
  }

  .overlay {
    display: flex; justify-content: center; align-items: center;
    min-height: 300px;
    font-size: 14px; color: #888;
  }
  .overlay.error { color: #f7768e; }
  .spinner { animation: spin 1s linear infinite; display: inline-block; }
  @keyframes spin { to { transform: rotate(360deg); } }

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
  <span class="info"><a href="/api/v2/traces/diff/view{{if and .BaseID .NextID}}?base={{.BaseID}}&next={{.NextID}}{{end}}">tree view</a></span>
</div>

<div class="legend-bar">
  <span class="legend-item"><span class="legend-swatch" style="background:#f7768e"></span> base</span>
  <span class="legend-item"><span class="legend-swatch" style="background:#9ece6a"></span> next</span>
  <span class="legend-sep"></span>
  <span class="legend-item"><span class="legend-swatch" style="background:#f7768e;opacity:0.4"></span> removed (base-only)</span>
  <span class="legend-item"><span class="legend-swatch" style="background:#9ece6a;opacity:0.4"></span> added (next-only)</span>
  <span class="legend-item"><span class="legend-swatch" style="background:#565f89"></span> unchanged</span>
  <span class="legend-item"><span class="legend-swatch" style="background:#e0af68"></span> modified</span>
</div>

<div class="time-header" id="time-header" style="display:none">
  <div class="time-header-label"></div>
  <div class="time-header-axis" id="time-axis"></div>
</div>

<div id="rows"></div>
<div class="tooltip" id="tooltip"></div>

<script>
const DIFF_API = '/api/v2/traces/diff';
const LLM_ACCEPT = 'application/vnd.grafana.llm';
const INDENT_PX = 16;
const LABEL_W = 320;

const COLORS = {
  base: '#f7768e',
  next: '#9ece6a',
  removed: 'rgba(247,118,142,0.4)',
  added: 'rgba(158,206,106,0.4)',
  unchanged: '#565f89',
  modified: '#e0af68',
};

const STATUS_LABEL_COLOR = {
  'base-only': '#f7768e', 'next-only': '#9ece6a',
  'both': '#7aa2f7', 'modified': '#e0af68', 'unchanged': '#c0caf5',
};

document.addEventListener('DOMContentLoaded', () => {
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
  history.pushState(null, '', '?base=' + base + '&next=' + next);
  fetchAndRender(base, next);
  return false;
}

async function fetchAndRender(baseID, nextID) {
  const container = document.getElementById('rows');
  container.innerHTML = '<div class="overlay"><span class="spinner">&#9881;</span>&nbsp; Loading traces...</div>';

  try {
    const url = DIFF_API + '?base=' + encodeURIComponent(baseID) + '&next=' + encodeURIComponent(nextID);
    const resp = await fetch(url, { headers: { 'Accept': LLM_ACCEPT } });
    if (!resp.ok) {
      const body = await resp.text();
      throw new Error(resp.status + ': ' + body);
    }
    const data = await resp.json();
    renderWaterfall(data, container);
  } catch (err) {
    container.innerHTML = '<div class="overlay error">Error: ' + esc(err.message) + '</div>';
  }
}

function shortKind(k) { return (k || '').replace('SPAN_KIND_', ''); }

function buildRows(data) {
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
          startTime: span.startTimeUnixNano ? parseFloat(span.startTimeUnixNano) / 1e6 : 0,
          status: (span.attributes || {})['tempo.diff.status'] || 'unchanged',
          kind: shortKind(span.kind),
        });
      }
    }
  }

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
    const sortNodes = (nodes) => {
      nodes.sort((a, b) => (a.startTime || 0) - (b.startTime || 0) || b.duration - a.duration);
      nodes.forEach(n => sortNodes(n.children));
    };
    sortNodes(roots);
    return roots;
  }

  const baseTree = buildTree(base);
  const nextTree = buildTree(next);

  function matchKey(n) { return n.name + '\0' + n.kind; }

  function merge(baseNodes, nextNodes, depth) {
    const nextByKey = new Map();
    for (const n of nextNodes) {
      const k = matchKey(n);
      if (!nextByKey.has(k)) nextByKey.set(k, []);
      nextByKey.get(k).push(n);
    }
    const consumed = new Map();
    const seen = new Set();
    const rows = [];

    for (const bn of baseNodes) {
      const k = matchKey(bn);
      seen.add(k);
      const row = {
        name: bn.name, kind: bn.kind, service: bn.service,
        baseDur: bn.duration, nextDur: 0,
        baseStart: bn.startTime, nextStart: 0,
        status: 'base-only', depth: depth,
      };
      const nextList = nextByKey.get(k) || [];
      const idx = consumed.get(k) || 0;
      if (idx < nextList.length) {
        const nn = nextList[idx];
        consumed.set(k, idx + 1);
        row.nextDur = nn.duration;
        row.nextStart = nn.startTime;
        row.service = bn.service || nn.service;
        row.status = bn.status === 'unchanged' ? 'unchanged' : bn.status === 'modified' ? 'modified' : 'both';
        rows.push(row);
        rows.push(...merge(bn.children, nn.children, depth + 1));
      } else {
        rows.push(row);
        rows.push(...merge(bn.children, [], depth + 1));
      }
    }

    for (const nn of nextNodes) {
      const k = matchKey(nn);
      const nextList = nextByKey.get(k) || [];
      const idx = consumed.get(k) || 0;
      if (seen.has(k) && idx > 0) {
        consumed.set(k, idx - 1);
        continue;
      }
      rows.push({
        name: nn.name, kind: nn.kind, service: nn.service,
        baseDur: 0, nextDur: nn.duration,
        baseStart: 0, nextStart: nn.startTime,
        status: 'next-only', depth: depth,
      });
      rows.push(...merge([], nn.children, depth + 1));
    }
    return rows;
  }

  return merge(baseTree, nextTree, 0);
}

function renderWaterfall(data, container) {
  const tooltip = document.getElementById('tooltip');
  const rows = buildRows(data);

  if (!rows.length) {
    container.innerHTML = '<div class="overlay">No spans found in diff response.</div>';
    return;
  }

  // Compute time range
  let globalMinStart = Infinity, globalMaxEnd = 0;
  for (const r of rows) {
    if (r.baseStart > 0) {
      globalMinStart = Math.min(globalMinStart, r.baseStart);
      globalMaxEnd = Math.max(globalMaxEnd, r.baseStart + r.baseDur);
    }
    if (r.nextStart > 0) {
      globalMinStart = Math.min(globalMinStart, r.nextStart);
      globalMaxEnd = Math.max(globalMaxEnd, r.nextStart + r.nextDur);
    }
  }
  const hasStartTimes = globalMinStart < Infinity && globalMinStart > 0;
  const maxDur = Math.max(...rows.map(r => Math.max(r.baseDur, r.nextDur)));
  const totalDuration = hasStartTimes ? (globalMaxEnd - globalMinStart) : maxDur;

  // Time axis ticks
  const timeHeader = document.getElementById('time-header');
  const timeAxis = document.getElementById('time-axis');
  timeHeader.style.display = 'flex';
  timeAxis.innerHTML = '';
  const tickCount = Math.min(10, Math.max(4, Math.floor(window.innerWidth / 150)));
  for (let i = 0; i <= tickCount; i++) {
    const t = (totalDuration / tickCount) * i;
    const pct = (t / totalDuration) * 100;
    const tick = document.createElement('div');
    tick.className = 'time-tick';
    tick.style.left = pct + '%';
    tick.textContent = t.toFixed(1) + 'ms';
    timeAxis.appendChild(tick);
  }

  // Build rows
  container.innerHTML = '';
  const fragment = document.createDocumentFragment();

  rows.forEach((row, i) => {
    const rowEl = document.createElement('div');
    rowEl.className = 'wf-row';

    // Label section
    const label = document.createElement('div');
    label.className = 'wf-label';
    label.style.paddingLeft = (8 + row.depth * INDENT_PX) + 'px';

    if (row.depth > 0) {
      const dot = document.createElement('div');
      dot.className = 'wf-depth-dot';
      label.appendChild(dot);
    }

    const text = document.createElement('div');
    text.className = 'wf-label-text';
    text.style.color = STATUS_LABEL_COLOR[row.status] || '#c0caf5';
    if (row.depth === 0) text.style.fontWeight = '700';
    text.textContent = row.name;
    text.title = row.name;
    label.appendChild(text);

    if (row.service) {
      const svc = document.createElement('div');
      svc.className = 'wf-label-svc';
      svc.style.left = (8 + row.depth * INDENT_PX) + 'px';
      svc.textContent = row.service;
      label.appendChild(svc);
    }

    if (row.kind) {
      const kind = document.createElement('div');
      kind.className = 'wf-kind';
      kind.textContent = row.kind;
      label.appendChild(kind);
    }

    rowEl.appendChild(label);

    // Bars section
    const bars = document.createElement('div');
    bars.className = 'wf-bars';

    const barTopY = 5;
    const barBotY = 5 + 10 + 2;

    function addBar(startMs, durMs, topPx, color) {
      if (durMs <= 0) return;
      const leftPct = hasStartTimes
        ? ((startMs - globalMinStart) / totalDuration) * 100
        : 0;
      const widthPct = (durMs / totalDuration) * 100;
      const bar = document.createElement('div');
      bar.className = 'wf-bar';
      bar.style.left = leftPct + '%';
      bar.style.width = Math.max(0.15, widthPct) + '%';
      bar.style.top = topPx + 'px';
      bar.style.background = color;
      bars.appendChild(bar);

      // Duration label
      const dur = document.createElement('div');
      dur.className = 'wf-dur';
      dur.style.left = (leftPct + widthPct + 0.3) + '%';
      dur.style.top = topPx + 'px';
      dur.style.color = color;
      dur.textContent = durMs.toFixed(1) + 'ms';
      bars.appendChild(dur);
    }

    if (row.baseDur > 0) {
      const color = row.status === 'base-only' ? COLORS.removed : COLORS.base;
      addBar(row.baseStart, row.baseDur, barTopY, color);
    }
    if (row.nextDur > 0) {
      const color = row.status === 'next-only' ? COLORS.added : COLORS.next;
      addBar(row.nextStart, row.nextDur, barBotY, color);
    }

    // Delta annotation
    if (row.baseDur > 0 && row.nextDur > 0) {
      const delta = row.nextDur - row.baseDur;
      if (Math.abs(delta) > 0.5) {
        const pct = row.baseDur > 0 ? ((delta / row.baseDur) * 100).toFixed(0) : '0';
        const sign = delta > 0 ? '+' : '';
        const color = delta > 0 ? '#f7768e' : '#9ece6a';
        const deltaEl = document.createElement('div');
        deltaEl.className = 'wf-delta';
        deltaEl.style.color = color;
        deltaEl.style.right = '8px';
        deltaEl.textContent = sign + delta.toFixed(1) + 'ms (' + sign + pct + '%)';
        bars.appendChild(deltaEl);
      }
    }

    rowEl.appendChild(bars);

    // Tooltip
    rowEl.addEventListener('mouseenter', (e) => {
      let html = '<div class="tt-op">' + esc(row.name) + ' [' + row.kind + ']</div>';
      if (row.service) html += '<div class="tt-svc">' + esc(row.service) + '</div>';
      html += '<div style="margin-top:6px">';
      if (row.baseDur > 0) html += '<div class="tt-base">base: ' + row.baseDur.toFixed(2) + ' ms</div>';
      if (row.nextDur > 0) html += '<div class="tt-next">next: ' + row.nextDur.toFixed(2) + ' ms</div>';
      if (row.baseDur > 0 && row.nextDur > 0) {
        const d = row.nextDur - row.baseDur;
        const p = row.baseDur > 0 ? ((d / row.baseDur) * 100).toFixed(1) : '0';
        const cls = d > 0.5 ? 'tt-delta-pos' : d < -0.5 ? 'tt-delta-neg' : '';
        html += '<div class="' + cls + '">delta: ' + (d > 0 ? '+' : '') + d.toFixed(2) + 'ms (' + (d > 0 ? '+' : '') + p + '%)</div>';
      }
      html += '</div><div class="tt-svc">status: ' + row.status + '</div>';
      tooltip.innerHTML = html;
      tooltip.style.display = 'block';
    });
    rowEl.addEventListener('mousemove', (e) => {
      tooltip.style.left = (e.clientX + 16) + 'px';
      tooltip.style.top = (e.clientY + 16) + 'px';
    });
    rowEl.addEventListener('mouseleave', () => { tooltip.style.display = 'none'; });

    fragment.appendChild(rowEl);
  });

  container.appendChild(fragment);
}

function esc(s) {
  return s.replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;');
}
</script>
</body>
</html>`))

// RenderWaterfallPage writes the waterfall diff viewer HTML page.
func RenderWaterfallPage(w io.Writer, baseID, nextID string) error {
	if baseID == "" || nextID == "" {
		return waterfallLandingTemplate.Execute(w, nil)
	}
	return waterfallTemplate.Execute(w, waterfallData{BaseID: baseID, NextID: nextID})
}

type waterfallData struct {
	BaseID string
	NextID string
}

var waterfallLandingTemplate = template.Must(template.New("waterfall-landing").Parse(`<!DOCTYPE html>
<html lang="en"><head>
<meta charset="utf-8"/><title>Trace Diff - Waterfall</title>
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
  <h2>Trace Diff - Waterfall</h2>
  <form method="get">
    <label>Base Trace ID</label><input name="base" placeholder="Enter base trace ID" autofocus/>
    <label>Next Trace ID</label><input name="next" placeholder="Enter next trace ID"/>
    <br/><button type="submit">View Diff</button>
  </form>
</div>
</body></html>`))
