package trace

import (
	"encoding/hex"
	"fmt"

	"github.com/grafana/tempo/pkg/tempopb"
	v1_common "github.com/grafana/tempo/pkg/tempopb/common/v1"
	v1 "github.com/grafana/tempo/pkg/tempopb/trace/v1"
)

const (
	DiffStatusAdded     = "added"
	DiffStatusRemoved   = "removed"
	DiffStatusUnchanged = "unchanged"
	DiffStatusModified  = "modified"

	DiffAttrStatus = "tempo.diff.status"
)

// DiffTraces compares base and next traces, returning a merged trace where every
// span is annotated with a tempo.diff.status attribute.
//
// Matching is by span ID:
//   - Spans only in next: "added"
//   - Spans only in base: "removed"
//   - Spans in both, identical: "unchanged"
//   - Spans in both, different: "modified"
func DiffTraces(base, next *tempopb.Trace) *tempopb.Trace {
	baseSpans := indexSpans(base)
	nextSpans := indexSpans(next)

	result := &tempopb.Trace{}

	// Walk next trace - annotate as added/unchanged/modified
	if next != nil {
		for _, rs := range next.ResourceSpans {
			for _, ss := range rs.ScopeSpans {
				for _, s := range ss.Spans {
					key := hex.EncodeToString(s.SpanId)
					if baseSpan, ok := baseSpans[key]; ok {
						if spansEqual(baseSpan, s) {
							addDiffAttr(s, DiffStatusUnchanged)
						} else {
							addDiffAttr(s, DiffStatusModified)
						}
					} else {
						addDiffAttr(s, DiffStatusAdded)
					}
				}
			}
		}
		result.ResourceSpans = append(result.ResourceSpans, next.ResourceSpans...)
	}

	// Walk base trace - collect removed spans
	var removedSpans []*v1.Span
	if base != nil {
		for _, rs := range base.ResourceSpans {
			for _, ss := range rs.ScopeSpans {
				for _, s := range ss.Spans {
					key := hex.EncodeToString(s.SpanId)
					if _, ok := nextSpans[key]; !ok {
						addDiffAttr(s, DiffStatusRemoved)
						removedSpans = append(removedSpans, s)
					}
				}
			}
		}
	}

	if len(removedSpans) > 0 {
		result.ResourceSpans = append(result.ResourceSpans, &v1.ResourceSpans{
			ScopeSpans: []*v1.ScopeSpans{{
				Spans: removedSpans,
			}},
		})
	}

	SortTrace(result)
	return result
}

// indexSpans builds a map of hex(spanID) -> *Span for all spans in the trace.
func indexSpans(t *tempopb.Trace) map[string]*v1.Span {
	m := make(map[string]*v1.Span)
	if t == nil {
		return m
	}
	for _, rs := range t.ResourceSpans {
		for _, ss := range rs.ScopeSpans {
			for _, s := range ss.Spans {
				m[hex.EncodeToString(s.SpanId)] = s
			}
		}
	}
	return m
}

// spansEqual compares two spans on their core fields (excluding attributes we inject).
func spansEqual(a, b *v1.Span) bool {
	if a.Name != b.Name {
		return false
	}
	if a.Kind != b.Kind {
		return false
	}
	if a.StartTimeUnixNano != b.StartTimeUnixNano {
		return false
	}
	if a.EndTimeUnixNano != b.EndTimeUnixNano {
		return false
	}
	if !statusEqual(a.Status, b.Status) {
		return false
	}
	if !attrsEqual(a.Attributes, b.Attributes) {
		return false
	}
	return true
}

func statusEqual(a, b *v1.Status) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return a.Code == b.Code && a.Message == b.Message
}

func attrsEqual(a, b []*v1_common.KeyValue) bool {
	if len(a) != len(b) {
		return false
	}
	am := make(map[string]string, len(a))
	for _, kv := range a {
		am[kv.Key] = anyValueString(kv.Value)
	}
	for _, kv := range b {
		if am[kv.Key] != anyValueString(kv.Value) {
			return false
		}
	}
	return true
}

func anyValueString(v *v1_common.AnyValue) string {
	if v == nil {
		return ""
	}
	switch val := v.Value.(type) {
	case *v1_common.AnyValue_StringValue:
		return "s:" + val.StringValue
	case *v1_common.AnyValue_IntValue:
		return fmt.Sprintf("i:%d", val.IntValue)
	case *v1_common.AnyValue_BoolValue:
		if val.BoolValue {
			return "b:true"
		}
		return "b:false"
	default:
		return "?"
	}
}

func addDiffAttr(s *v1.Span, status string) {
	s.Attributes = append(s.Attributes, tempopb.MakeKeyValueStringPtr(DiffAttrStatus, status))
}
