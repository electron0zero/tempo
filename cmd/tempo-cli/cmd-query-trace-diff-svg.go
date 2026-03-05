package main

import (
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"os"
	"sort"
	"strings"

	svg "github.com/ajstarks/svgo"

	"github.com/grafana/tempo/modules/frontend/combiner"
	"github.com/grafana/tempo/pkg/api"
	"github.com/grafana/tempo/pkg/httpclient"
)

type queryTraceDiffSVGCmd struct {
	APIEndpoint string `arg:"" help:"tempo api endpoint"`
	BaseTraceID string `arg:"" help:"base trace ID to compare from"`
	NextTraceID string `arg:"" help:"next trace ID to compare to"`

	Output string `short:"o" help:"output SVG file path" default:"trace-diff.svg"`
	OrgID  string `help:"optional orgID"`
}

// mergedNode represents a single operation that may appear in base, next, or both.
type mergedNode struct {
	Key      string
	Service  string
	Name     string
	Kind     string
	Depth    int
	BaseDur  float64
	NextDur  float64
	Status   string // "both", "base-only", "next-only", "unchanged", "modified"
	Children []*mergedNode

	// Layout fields set during positioning.
	X, Y    int
	LeafIdx int // sequential leaf index for x positioning
}

// rawSpan holds a flat span extracted from the LLM response.
type rawSpan struct {
	SpanID   string
	ParentID string
	Service  string
	Name     string
	Duration float64
	DiffStat string
	Kind     string
}

const (
	hexR       = 32   // hexagon "radius" (center to vertex)
	hexW       = 64   // hexagon width
	hexH       = 56   // hexagon height
	hSpacing   = 200  // horizontal spacing between leaf centers
	vSpacing   = 160  // vertical spacing between levels
	labelWidth = 180  // max label width below node
	treePadTop = 100  // top padding for title + legend
	treePadX   = 60   // left/right padding
)

func (cmd *queryTraceDiffSVGCmd) Run(_ *globalOptions) error {
	client := httpclient.New(cmd.APIEndpoint, cmd.OrgID)

	diffURL := cmd.APIEndpoint + api.PathTraceDiff + "?base=" + url.QueryEscape(cmd.BaseTraceID) + "&next=" + url.QueryEscape(cmd.NextTraceID)
	body, err := client.GetLLMFormat(diffURL)
	if err != nil {
		return err
	}

	var resp combiner.LLMTraceByIDResponse
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		return fmt.Errorf("failed to parse diff response: %w", err)
	}

	allSpans := collectRawSpans(resp)
	baseSpans, nextSpans := splitByStatus(allSpans)

	baseRoots := buildRawTree(baseSpans)
	nextRoots := buildRawTree(nextSpans)

	roots := mergeTrees(baseRoots, nextRoots, 0)

	// Assign x/y positions using a simple tree layout.
	leafCounter := 0
	for _, r := range roots {
		assignPositions(r, 0, &leafCounter)
	}

	// Collect all nodes for rendering.
	var allNodes []*mergedNode
	for _, r := range roots {
		collectAll(r, &allNodes)
	}

	if err := writeTreeSVG(cmd.Output, roots, allNodes, cmd.BaseTraceID, cmd.NextTraceID); err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "wrote %s (%d operations)\n", cmd.Output, len(allNodes))
	return nil
}

// assignPositions does a post-order traversal: leaves get sequential x, parents center over children.
func assignPositions(n *mergedNode, depth int, leafIdx *int) {
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

	// Center parent over children.
	first := n.Children[0].X
	last := n.Children[len(n.Children)-1].X
	n.X = (first + last) / 2
}

func collectAll(n *mergedNode, out *[]*mergedNode) {
	*out = append(*out, n)
	for _, c := range n.Children {
		collectAll(c, out)
	}
}

func writeTreeSVG(path string, roots []*mergedNode, allNodes []*mergedNode, baseID, nextID string) error {
	// Compute canvas bounds.
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

	// Ensure title fits.
	titleText := fmt.Sprintf("Trace Diff:  base=%s  vs  next=%s", baseID, nextID)
	titleW := treePadX + len(titleText)*8 + treePadX
	if titleW > totalW {
		totalW = titleW
	}

	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer f.Close()

	canvas := svg.New(f)
	canvas.Start(totalW, totalH)

	// Defs for arrow markers and drop shadow.
	canvas.Def()
	canvas.Marker("arrowhead", 10, 7, 10, 7, `orient="auto"`)
	canvas.Path("M 0 0 L 10 3.5 L 0 7 z", "fill:#999")
	canvas.MarkerEnd()
	fmt.Fprintf(f, `<filter id="shadow" x="-10%%" y="-10%%" width="130%%" height="130%%">
      <feDropShadow dx="1" dy="2" stdDeviation="2" flood-opacity="0.15"/>
    </filter>`)
	canvas.DefEnd()

	canvas.Style("text/css", `
		.mono { font-family: 'SF Mono','Menlo','Monaco','Consolas',monospace; }
		.title { font-size: 14px; font-weight: bold; fill: #1a1a2e; }
		.op-name { font-size: 11px; font-weight: 600; fill: #1a1a2e; text-anchor: middle; }
		.svc-name { font-size: 10px; fill: #666; text-anchor: middle; }
		.dur-base { font-size: 10px; fill: #e74c3c; text-anchor: middle; font-weight: bold; }
		.dur-next { font-size: 10px; fill: #27ae60; text-anchor: middle; font-weight: bold; }
		.dur-delta { font-size: 10px; text-anchor: middle; font-weight: bold; }
		.kind-label { font-size: 9px; text-anchor: middle; font-weight: bold; }
		.legend-text { font-size: 11px; fill: #555; }
		.edge { fill: none; stroke: #bbb; stroke-width: 1.5; }
	`)

	// Background
	canvas.Rect(0, 0, totalW, totalH, "fill:#fafbfc")

	// Title
	canvas.Text(treePadX, 28, titleText, `class="mono title"`)

	// Legend
	drawLegend(canvas, treePadX, 46)

	// Draw edges first (behind nodes).
	for _, n := range allNodes {
		for _, c := range n.Children {
			// Curved edge from parent bottom to child top.
			fromX, fromY := n.X, n.Y+hexR
			toX, toY := c.X, c.Y-hexR
			midY := (fromY + toY) / 2
			pathD := fmt.Sprintf("M %d %d C %d %d %d %d %d %d",
				fromX, fromY, fromX, midY, toX, midY, toX, toY)
			canvas.Path(pathD, `class="edge" marker-end="url(#arrowhead)"`)
		}
	}

	// Draw nodes.
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

// drawHexNode draws a hexagon node with labels.
func drawHexNode(canvas *svg.SVG, n *mergedNode) {
	cx, cy := n.X, n.Y
	fill, stroke := hexColors(n.Status)

	// Hexagon shape with drop shadow.
	xs, ys := hexPoints(cx, cy, hexR)
	canvas.Polygon(xs, ys, fmt.Sprintf("fill:%s;stroke:%s;stroke-width:2;filter:url(#shadow)", fill, stroke))

	// Kind label inside hexagon.
	kindLabel := n.Kind
	if len(kindLabel) > 8 {
		kindLabel = kindLabel[:3]
	}
	canvas.Text(cx, cy+4, kindLabel, `class="mono kind-label"`, fmt.Sprintf(`fill="%s"`, kindTextColor(n.Status)))

	// Operation name below hexagon.
	opLabel := n.Name
	if len(opLabel) > 24 {
		opLabel = opLabel[:21] + "..."
	}
	canvas.Text(cx, cy+hexR+16, escapeXML(opLabel), `class="mono op-name"`)

	// Service name.
	if n.Service != "" {
		svcLabel := n.Service
		if len(svcLabel) > 24 {
			svcLabel = svcLabel[:21] + "..."
		}
		canvas.Text(cx, cy+hexR+30, escapeXML(svcLabel), `class="mono svc-name"`)
	}

	// Duration labels.
	durY := cy + hexR + 44
	if n.BaseDur > 0 && n.NextDur > 0 {
		// Both: show base / next / delta
		canvas.Text(cx, durY, fmt.Sprintf("%.1fms", n.BaseDur), `class="mono dur-base"`)
		canvas.Text(cx, durY+14, fmt.Sprintf("%.1fms", n.NextDur), `class="mono dur-next"`)
		delta := n.NextDur - n.BaseDur
		deltaStr, deltaColor := formatDelta(delta)
		canvas.Text(cx, durY+28, deltaStr, `class="mono dur-delta"`, fmt.Sprintf(`fill="%s"`, deltaColor))
	} else if n.BaseDur > 0 {
		canvas.Text(cx, durY, fmt.Sprintf("%.1fms", n.BaseDur), `class="mono dur-base"`)
	} else if n.NextDur > 0 {
		canvas.Text(cx, durY, fmt.Sprintf("%.1fms", n.NextDur), `class="mono dur-next"`)
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

// hexPoints returns the 6 vertices of a flat-top hexagon centered at (cx, cy).
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

// --- Data extraction and tree merging (unchanged logic) ---

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

type rawNode struct {
	rawSpan
	Children []*rawNode
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

func mergeTrees(baseNodes, nextNodes []*rawNode, depth int) []*mergedNode {
	baseByKey := groupByKey(baseNodes)
	nextByKey := groupByKey(nextNodes)

	nextConsumed := make(map[string]int)
	seen := make(map[string]bool)
	var result []*mergedNode

	for _, bn := range baseNodes {
		key := matchKey(bn.Name, bn.Kind)
		seen[key] = true

		mn := &mergedNode{
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
				mn.Status = overlayStatus(bn.Duration, nn.Duration, bn.DiffStat)
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
		mn := &mergedNode{
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

func overlayStatus(baseDur, nextDur float64, diffStat string) string {
	if diffStat == "unchanged" {
		return "unchanged"
	}
	if diffStat == "modified" {
		return "modified"
	}
	return "both"
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
