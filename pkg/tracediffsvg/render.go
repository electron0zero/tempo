package tracediffsvg

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"sort"
	"strings"

	svg "github.com/ajstarks/svgo"

	"github.com/grafana/tempo/modules/frontend/combiner"
)

// MergedNode represents a single operation that may appear in base, next, or both.
type MergedNode struct {
	Key      string
	Service  string
	Name     string
	Kind     string
	Depth    int
	BaseDur  float64
	NextDur  float64
	Status   string // "both", "base-only", "next-only", "unchanged", "modified"
	Children []*MergedNode

	// Layout fields set during positioning.
	X, Y    int
	LeafIdx int
}

type rawSpan struct {
	SpanID   string
	ParentID string
	Service  string
	Name     string
	Duration float64
	DiffStat string
	Kind     string
}

type rawNode struct {
	rawSpan
	Children []*rawNode
}

const (
	hexR       = 32
	hSpacing   = 260
	vSpacing   = 140
	treePadTop = 100
	treePadX   = 60
)

// RenderFromLLMJSON takes the LLM-format JSON diff response and writes an SVG tree to w.
func RenderFromLLMJSON(w io.Writer, llmJSON []byte, baseID, nextID string) error {
	var resp combiner.LLMTraceByIDResponse
	if err := json.Unmarshal(llmJSON, &resp); err != nil {
		return fmt.Errorf("failed to parse diff response: %w", err)
	}

	return RenderFromResponse(w, resp, baseID, nextID)
}

// RenderFromResponse takes a parsed LLM response and writes an SVG tree to w.
func RenderFromResponse(w io.Writer, resp combiner.LLMTraceByIDResponse, baseID, nextID string) error {
	allSpans := collectRawSpans(resp)
	baseSpans, nextSpans := splitByStatus(allSpans)

	baseRoots := buildRawTree(baseSpans)
	nextRoots := buildRawTree(nextSpans)

	roots := mergeTrees(baseRoots, nextRoots, 0)

	leafCounter := 0
	for _, r := range roots {
		assignPositions(r, 0, &leafCounter)
	}

	var allNodes []*MergedNode
	for _, r := range roots {
		collectAll(r, &allNodes)
	}

	return writeTreeSVG(w, roots, allNodes, baseID, nextID)
}

func assignPositions(n *MergedNode, depth int, leafIdx *int) {
	n.Depth = depth
	n.Y = treePadTop + depth*vSpacing

	if len(n.Children) == 0 {
		n.X = treePadX + (*leafIdx)*hSpacing
		n.LeafIdx = *leafIdx
		*leafIdx++
		return
	}

	for _, c := range n.Children {
		assignPositions(c, depth+1, leafIdx)
	}

	first := n.Children[0].X
	last := n.Children[len(n.Children)-1].X
	n.X = (first + last) / 2
}

func collectAll(n *MergedNode, out *[]*MergedNode) {
	*out = append(*out, n)
	for _, c := range n.Children {
		collectAll(c, out)
	}
}

func writeTreeSVG(w io.Writer, roots []*MergedNode, allNodes []*MergedNode, baseID, nextID string) error {
	maxX, maxY := 0, 0
	for _, n := range allNodes {
		if n.X > maxX {
			maxX = n.X
		}
		if n.Y > maxY {
			maxY = n.Y
		}
	}
	totalW := maxX + hSpacing + treePadX
	totalH := maxY + vSpacing + 40

	titleText := fmt.Sprintf("Trace Diff:  base=%s  vs  next=%s", baseID, nextID)
	titleW := treePadX + len(titleText)*8 + treePadX
	if titleW > totalW {
		totalW = titleW
	}

	canvas := svg.New(w)
	canvas.Start(totalW, totalH)

	// Defs
	canvas.Def()
	fmt.Fprintf(w, `<marker id="arrowhead" markerWidth="10" markerHeight="7" refX="10" refY="3.5" orient="auto">
      <path d="M 0 0 L 10 3.5 L 0 7 z" fill="#999"/>
    </marker>`)
	fmt.Fprintf(w, `<filter id="shadow" x="-10%%" y="-10%%" width="130%%" height="130%%">
      <feDropShadow dx="1" dy="2" stdDeviation="2" flood-opacity="0.15"/>
    </filter>`)
	canvas.DefEnd()

	canvas.Style("text/css", `
		.mono { font-family: 'SF Mono','Menlo','Monaco','Consolas',monospace; }
		.title { font-size: 14px; font-weight: bold; fill: #1a1a2e; }
		.op-name { font-size: 11px; font-weight: 600; fill: #1a1a2e; }
		.svc-name { font-size: 10px; fill: #666; }
		.dur-base { font-size: 10px; fill: #e74c3c; font-weight: bold; }
		.dur-next { font-size: 10px; fill: #27ae60; font-weight: bold; }
		.dur-delta { font-size: 10px; font-weight: bold; }
		.kind-label { font-size: 9px; text-anchor: middle; font-weight: bold; }
		.legend-text { font-size: 11px; fill: #555; }
		.edge { fill: none; stroke: #bbb; stroke-width: 1.5; }
	`)

	canvas.Rect(0, 0, totalW, totalH, "fill:#fafbfc")
	canvas.Text(treePadX, 28, titleText, `class="mono title"`)
	drawLegend(canvas, treePadX, 46)

	// Edges
	for _, n := range allNodes {
		for _, c := range n.Children {
			fromX, fromY := n.X, n.Y+hexR+2
			toX, toY := c.X, c.Y-hexR-4
			midY := (fromY + toY) / 2
			pathD := fmt.Sprintf("M %d %d C %d %d %d %d %d %d",
				fromX, fromY, fromX, midY, toX, midY, toX, toY)
			canvas.Path(pathD, `class="edge" marker-end="url(#arrowhead)"`)
		}
	}

	// Nodes
	for _, n := range allNodes {
		drawHexNode(canvas, n)
	}

	canvas.End()
	return nil
}

func drawLegend(canvas *svg.SVG, x, y int) {
	items := []struct {
		fill, stroke, label string
	}{
		{"#fdf0f0", "#e74c3c", "base-only (removed)"},
		{"#f0fdf4", "#27ae60", "next-only (added)"},
		{"#eef2ff", "#5b6abf", "both (overlaid)"},
		{"#fef9e7", "#f0a500", "modified"},
		{"#f0f0f0", "#999", "unchanged"},
	}
	lx := x
	for _, it := range items {
		drawMiniHex(canvas, lx+10, y+7, 8, it.fill, it.stroke)
		canvas.Text(lx+24, y+12, it.label, `class="mono legend-text"`)
		lx += len(it.label)*7 + 42
	}
}

func drawMiniHex(canvas *svg.SVG, cx, cy, r int, fill, stroke string) {
	xs, ys := hexPoints(cx, cy, r)
	canvas.Polygon(xs, ys, fmt.Sprintf("fill:%s;stroke:%s;stroke-width:1.5", fill, stroke))
}

func drawHexNode(canvas *svg.SVG, n *MergedNode) {
	cx, cy := n.X, n.Y
	fill, stroke := hexColors(n.Status)

	xs, ys := hexPoints(cx, cy, hexR)
	canvas.Polygon(xs, ys, fmt.Sprintf("fill:%s;stroke:%s;stroke-width:2;filter:url(#shadow)", fill, stroke))

	kindLabel := n.Kind
	if len(kindLabel) > 8 {
		kindLabel = kindLabel[:3]
	}
	canvas.Text(cx, cy+4, kindLabel, `class="mono kind-label"`, fmt.Sprintf(`fill="%s"`, kindTextColor(n.Status)))

	lx := cx + hexR + 10
	ly := cy - 18

	opLabel := n.Name
	if len(opLabel) > 30 {
		opLabel = opLabel[:27] + "..."
	}
	canvas.Text(lx, ly, escapeXML(opLabel), `class="mono op-name"`)
	ly += 14

	if n.Service != "" {
		svcLabel := n.Service
		if len(svcLabel) > 30 {
			svcLabel = svcLabel[:27] + "..."
		}
		canvas.Text(lx, ly, escapeXML(svcLabel), `class="mono svc-name"`)
		ly += 14
	}

	if n.BaseDur > 0 && n.NextDur > 0 {
		canvas.Text(lx, ly, fmt.Sprintf("base: %.1fms", n.BaseDur), `class="mono dur-base"`)
		ly += 13
		canvas.Text(lx, ly, fmt.Sprintf("next: %.1fms", n.NextDur), `class="mono dur-next"`)
		ly += 13
		delta := n.NextDur - n.BaseDur
		deltaStr, deltaColor := formatDelta(delta)
		canvas.Text(lx, ly, deltaStr, `class="mono dur-delta"`, fmt.Sprintf(`fill="%s"`, deltaColor))
	} else if n.BaseDur > 0 {
		canvas.Text(lx, ly, fmt.Sprintf("base: %.1fms", n.BaseDur), `class="mono dur-base"`)
	} else if n.NextDur > 0 {
		canvas.Text(lx, ly, fmt.Sprintf("next: %.1fms", n.NextDur), `class="mono dur-next"`)
	}
}

func formatDelta(delta float64) (string, string) {
	if delta > 0.5 {
		return fmt.Sprintf("+%.1fms", delta), "#e74c3c"
	}
	if delta < -0.5 {
		return fmt.Sprintf("%.1fms", delta), "#27ae60"
	}
	return "~0ms", "#999"
}

func hexPoints(cx, cy, r int) ([]int, []int) {
	xs := make([]int, 6)
	ys := make([]int, 6)
	for i := 0; i < 6; i++ {
		angle := math.Pi/6 + float64(i)*math.Pi/3
		xs[i] = cx + int(float64(r)*math.Cos(angle))
		ys[i] = cy + int(float64(r)*math.Sin(angle))
	}
	return xs, ys
}

func hexColors(status string) (fill, stroke string) {
	switch status {
	case "base-only":
		return "#fdf0f0", "#e74c3c"
	case "next-only":
		return "#f0fdf4", "#27ae60"
	case "both":
		return "#eef2ff", "#5b6abf"
	case "modified":
		return "#fef9e7", "#f0a500"
	case "unchanged":
		return "#f0f0f0", "#999"
	default:
		return "#f8f9fa", "#ccc"
	}
}

func kindTextColor(status string) string {
	switch status {
	case "base-only":
		return "#c0392b"
	case "next-only":
		return "#1e8449"
	case "both":
		return "#3949ab"
	case "modified":
		return "#d4890a"
	default:
		return "#666"
	}
}

// --- Data extraction and tree merging ---

func collectRawSpans(resp combiner.LLMTraceByIDResponse) []rawSpan {
	var spans []rawSpan
	for _, svc := range resp.Trace.Services {
		for _, scope := range svc.Scopes {
			for _, span := range scope.Spans {
				status, _ := span.Attributes["tempo.diff.status"].(string)
				spans = append(spans, rawSpan{
					SpanID:   span.SpanID,
					ParentID: span.ParentSpanID,
					Service:  svc.ServiceName,
					Name:     span.Name,
					Duration: span.DurationMs,
					DiffStat: status,
					Kind:     shortKind(span.Kind),
				})
			}
		}
	}
	return spans
}

func splitByStatus(spans []rawSpan) (base, next []rawSpan) {
	for _, s := range spans {
		switch s.DiffStat {
		case "removed":
			base = append(base, s)
		case "added":
			next = append(next, s)
		case "unchanged", "modified":
			base = append(base, s)
			next = append(next, s)
		}
	}
	return
}

func buildRawTree(spans []rawSpan) []*rawNode {
	byID := make(map[string]*rawNode, len(spans))
	for i := range spans {
		byID[spans[i].SpanID] = &rawNode{rawSpan: spans[i]}
	}
	var roots []*rawNode
	for _, n := range byID {
		if parent, ok := byID[n.ParentID]; ok {
			parent.Children = append(parent.Children, n)
		} else {
			roots = append(roots, n)
		}
	}
	var sortChildren func(nodes []*rawNode)
	sortChildren = func(nodes []*rawNode) {
		sort.Slice(nodes, func(i, j int) bool {
			return nodes[i].Duration > nodes[j].Duration
		})
		for _, n := range nodes {
			sortChildren(n.Children)
		}
	}
	sortChildren(roots)
	sort.Slice(roots, func(i, j int) bool {
		return roots[i].Duration > roots[j].Duration
	})
	return roots
}

func matchKey(name, kind string) string {
	return name + "\x00" + kind
}

func mergeTrees(baseNodes, nextNodes []*rawNode, depth int) []*MergedNode {
	baseByKey := groupByKey(baseNodes)
	nextByKey := groupByKey(nextNodes)

	nextConsumed := make(map[string]int)
	seen := make(map[string]bool)
	var result []*MergedNode

	for _, bn := range baseNodes {
		key := matchKey(bn.Name, bn.Kind)
		seen[key] = true

		mn := &MergedNode{
			Key:     key,
			Service: coalesce(bn.Service),
			Name:    bn.Name,
			Kind:    bn.Kind,
			Depth:   depth,
			BaseDur: bn.Duration,
		}

		var nextChildren []*rawNode
		if nextList, ok := nextByKey[key]; ok {
			idx := nextConsumed[key]
			if idx < len(nextList) {
				nn := nextList[idx]
				nextConsumed[key] = idx + 1
				mn.NextDur = nn.Duration
				mn.Service = coalesce(bn.Service, nn.Service)
				mn.Status = overlayStatus(bn.DiffStat)
				nextChildren = nn.Children
			} else {
				mn.Status = "base-only"
			}
		} else {
			mn.Status = "base-only"
		}

		mn.Children = mergeTrees(bn.Children, nextChildren, depth+1)
		result = append(result, mn)
	}

	for _, nn := range nextNodes {
		key := matchKey(nn.Name, nn.Kind)
		nextList := nextByKey[key]
		consumed := nextConsumed[key]
		if seen[key] && consumed > 0 {
			consumed--
			nextConsumed[key] = consumed
			continue
		}
		mn := &MergedNode{
			Key:     key,
			Service: coalesce(nn.Service),
			Name:    nn.Name,
			Kind:    nn.Kind,
			Depth:   depth,
			NextDur: nn.Duration,
			Status:  "next-only",
		}
		if seen[key] && len(nextList) > len(baseByKey[key]) {
			mn.Status = "next-only"
		}
		mn.Children = mergeTrees(nil, nn.Children, depth+1)
		result = append(result, mn)
	}

	return result
}

func groupByKey(nodes []*rawNode) map[string][]*rawNode {
	m := make(map[string][]*rawNode)
	for _, n := range nodes {
		key := matchKey(n.Name, n.Kind)
		m[key] = append(m[key], n)
	}
	return m
}

func coalesce(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func overlayStatus(diffStat string) string {
	switch diffStat {
	case "unchanged":
		return "unchanged"
	case "modified":
		return "modified"
	default:
		return "both"
	}
}

func shortKind(kind string) string {
	switch kind {
	case "SPAN_KIND_SERVER":
		return "SERVER"
	case "SPAN_KIND_CLIENT":
		return "CLIENT"
	case "SPAN_KIND_PRODUCER":
		return "PRODUCER"
	case "SPAN_KIND_CONSUMER":
		return "CONSUMER"
	case "SPAN_KIND_INTERNAL":
		return "INTERNAL"
	default:
		return kind
	}
}

func escapeXML(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, "'", "&apos;")
	s = strings.ReplaceAll(s, "\"", "&quot;")
	return s
}
