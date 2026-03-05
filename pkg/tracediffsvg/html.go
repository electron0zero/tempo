package tracediffsvg

import (
	"bytes"
	"fmt"
	"io"
	"text/template"

	"github.com/grafana/tempo/modules/frontend/combiner"
)

var htmlTemplate = template.Must(template.New("diff-view").Parse(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8"/>
<meta name="viewport" content="width=device-width, initial-scale=1"/>
<title>Trace Diff: {{.BaseID}} vs {{.NextID}}</title>
<style>
  * { margin: 0; padding: 0; box-sizing: border-box; }
  body {
    font-family: 'SF Mono','Menlo','Monaco','Consolas',monospace;
    background: #f0f2f5;
    color: #1a1a2e;
  }
  .toolbar {
    position: fixed; top: 0; left: 0; right: 0; z-index: 100;
    background: #1a1a2e; color: #eee; padding: 10px 20px;
    display: flex; align-items: center; gap: 12px;
    box-shadow: 0 2px 8px rgba(0,0,0,0.2);
    font-size: 13px;
  }
  .toolbar label { color: #aaa; font-size: 11px; }
  .toolbar input {
    background: #2a2a3e; border: 1px solid #444; color: #eee;
    padding: 4px 8px; border-radius: 4px; font-family: inherit;
    font-size: 12px; width: 290px;
  }
  .toolbar button {
    background: #5b6abf; color: #fff; border: none;
    padding: 6px 16px; border-radius: 4px; cursor: pointer;
    font-family: inherit; font-size: 12px; font-weight: 600;
  }
  .toolbar button:hover { background: #4a59ae; }
  .toolbar .zoom-controls { margin-left: auto; display: flex; gap: 6px; }
  .toolbar .zoom-controls button {
    background: #333; padding: 4px 10px; font-size: 14px;
  }
  .toolbar .zoom-controls button:hover { background: #444; }
  .svg-container {
    margin-top: 52px; overflow: auto; width: 100%; height: calc(100vh - 52px);
    cursor: grab; position: relative;
  }
  .svg-container:active { cursor: grabbing; }
  .svg-wrapper {
    transform-origin: 0 0;
    display: inline-block;
    min-width: 100%;
    min-height: 100%;
  }
  /* Tooltip */
  .tooltip {
    display: none; position: fixed; z-index: 200;
    background: #1a1a2e; color: #eee; padding: 10px 14px;
    border-radius: 6px; font-size: 11px; line-height: 1.6;
    box-shadow: 0 4px 12px rgba(0,0,0,0.3);
    pointer-events: none; max-width: 400px;
  }
</style>
</head>
<body>
<div class="toolbar">
  <form id="diff-form" method="get" style="display:flex;align-items:center;gap:8px;">
    <label>base</label>
    <input name="base" value="{{.BaseID}}" placeholder="base trace ID"/>
    <label>next</label>
    <input name="next" value="{{.NextID}}" placeholder="next trace ID"/>
    <button type="submit">Diff</button>
  </form>
  <div class="zoom-controls">
    <button id="zoom-in" title="Zoom in">+</button>
    <button id="zoom-out" title="Zoom out">-</button>
    <button id="zoom-reset" title="Reset zoom">1:1</button>
  </div>
</div>
<div class="svg-container" id="container">
  <div class="svg-wrapper" id="wrapper">
    {{.SVGContent}}
  </div>
</div>
<div class="tooltip" id="tooltip"></div>
<script>
(function() {
  // Pan and zoom
  const container = document.getElementById('container');
  const wrapper = document.getElementById('wrapper');
  let scale = 1, panX = 0, panY = 0;
  let isPanning = false, startX = 0, startY = 0;

  function applyTransform() {
    wrapper.style.transform = 'translate(' + panX + 'px,' + panY + 'px) scale(' + scale + ')';
  }

  container.addEventListener('wheel', function(e) {
    e.preventDefault();
    const delta = e.deltaY > 0 ? -0.1 : 0.1;
    scale = Math.max(0.1, Math.min(5, scale + delta));
    applyTransform();
  }, {passive: false});

  container.addEventListener('mousedown', function(e) {
    isPanning = true;
    startX = e.clientX - panX;
    startY = e.clientY - panY;
  });
  window.addEventListener('mousemove', function(e) {
    if (!isPanning) return;
    panX = e.clientX - startX;
    panY = e.clientY - startY;
    applyTransform();
  });
  window.addEventListener('mouseup', function() { isPanning = false; });

  document.getElementById('zoom-in').onclick = function() {
    scale = Math.min(5, scale + 0.2); applyTransform();
  };
  document.getElementById('zoom-out').onclick = function() {
    scale = Math.max(0.1, scale - 0.2); applyTransform();
  };
  document.getElementById('zoom-reset').onclick = function() {
    scale = 1; panX = 0; panY = 0; applyTransform();
  };

  // Tooltips on hexagon polygons
  const tooltip = document.getElementById('tooltip');
  document.querySelectorAll('svg polygon[style*="filter"]').forEach(function(hex) {
    hex.style.cursor = 'pointer';
    hex.addEventListener('mouseenter', function(e) {
      // Collect sibling text elements (labels rendered after the polygon)
      const texts = [];
      let sib = hex.nextElementSibling;
      while (sib && sib.tagName === 'text') {
        texts.push(sib.textContent);
        sib = sib.nextElementSibling;
      }
      if (texts.length > 0) {
        tooltip.innerHTML = texts.join('<br/>');
        tooltip.style.display = 'block';
      }
    });
    hex.addEventListener('mousemove', function(e) {
      tooltip.style.left = (e.clientX + 14) + 'px';
      tooltip.style.top = (e.clientY + 14) + 'px';
    });
    hex.addEventListener('mouseleave', function() {
      tooltip.style.display = 'none';
    });
  });
})();
</script>
</body>
</html>`))

type htmlData struct {
	BaseID     string
	NextID     string
	SVGContent string
}

// RenderHTML writes a full HTML page with the embedded SVG diff tree.
func RenderHTML(w io.Writer, resp combiner.LLMTraceByIDResponse, baseID, nextID string) error {
	var svgBuf bytes.Buffer
	if err := RenderFromResponse(&svgBuf, resp, baseID, nextID); err != nil {
		return fmt.Errorf("failed to render SVG: %w", err)
	}

	return htmlTemplate.Execute(w, htmlData{
		BaseID:     baseID,
		NextID:     nextID,
		SVGContent: svgBuf.String(),
	})
}
