package trace

import (
	"testing"

	"github.com/grafana/tempo/pkg/tempopb"
	v1_common "github.com/grafana/tempo/pkg/tempopb/common/v1"
	v1 "github.com/grafana/tempo/pkg/tempopb/trace/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func makeSpan(id []byte, name string, start, end uint64, attrs ...*v1_common.KeyValue) *v1.Span {
	return &v1.Span{
		SpanId:            id,
		Name:              name,
		Kind:              v1.Span_SPAN_KIND_SERVER,
		StartTimeUnixNano: start,
		EndTimeUnixNano:   end,
		Status:            &v1.Status{Code: v1.Status_STATUS_CODE_OK},
		Attributes:        attrs,
	}
}

func wrapTrace(spans ...*v1.Span) *tempopb.Trace {
	return &tempopb.Trace{
		ResourceSpans: []*v1.ResourceSpans{{
			ScopeSpans: []*v1.ScopeSpans{{
				Spans: spans,
			}},
		}},
	}
}

func getDiffStatus(s *v1.Span) string {
	for _, a := range s.Attributes {
		if a.Key == DiffAttrStatus {
			return a.Value.GetStringValue()
		}
	}
	return ""
}

func allSpans(t *tempopb.Trace) []*v1.Span {
	var out []*v1.Span
	for _, rs := range t.ResourceSpans {
		for _, ss := range rs.ScopeSpans {
			out = append(out, ss.Spans...)
		}
	}
	return out
}

func findSpanByID(spans []*v1.Span, id []byte) *v1.Span {
	for _, s := range spans {
		if string(s.SpanId) == string(id) {
			return s
		}
	}
	return nil
}

func TestDiffTraces_BothNil(t *testing.T) {
	result := DiffTraces(nil, nil)
	require.NotNil(t, result)
	assert.Empty(t, result.ResourceSpans)
}

func TestDiffTraces_BaseNil_AllAdded(t *testing.T) {
	s1 := makeSpan([]byte{0x01}, "span1", 100, 200)
	s2 := makeSpan([]byte{0x02}, "span2", 300, 400)
	next := wrapTrace(s1, s2)

	result := DiffTraces(nil, next)
	spans := allSpans(result)
	require.Len(t, spans, 2)
	for _, s := range spans {
		assert.Equal(t, DiffStatusAdded, getDiffStatus(s))
	}
}

func TestDiffTraces_NextNil_AllRemoved(t *testing.T) {
	s1 := makeSpan([]byte{0x01}, "span1", 100, 200)
	s2 := makeSpan([]byte{0x02}, "span2", 300, 400)
	base := wrapTrace(s1, s2)

	result := DiffTraces(base, nil)
	spans := allSpans(result)
	require.Len(t, spans, 2)
	for _, s := range spans {
		assert.Equal(t, DiffStatusRemoved, getDiffStatus(s))
	}
}

func TestDiffTraces_Unchanged(t *testing.T) {
	spanID := []byte{0x01}
	base := wrapTrace(makeSpan(spanID, "span1", 100, 200))
	next := wrapTrace(makeSpan(spanID, "span1", 100, 200))

	result := DiffTraces(base, next)
	spans := allSpans(result)
	require.Len(t, spans, 1)
	assert.Equal(t, DiffStatusUnchanged, getDiffStatus(spans[0]))
}

func TestDiffTraces_Modified_NameChanged(t *testing.T) {
	spanID := []byte{0x01}
	base := wrapTrace(makeSpan(spanID, "old-name", 100, 200))
	next := wrapTrace(makeSpan(spanID, "new-name", 100, 200))

	result := DiffTraces(base, next)
	spans := allSpans(result)
	require.Len(t, spans, 1)
	assert.Equal(t, DiffStatusModified, getDiffStatus(spans[0]))
	assert.Equal(t, "new-name", spans[0].Name) // next version is kept
}

func TestDiffTraces_Modified_DurationChanged(t *testing.T) {
	spanID := []byte{0x01}
	base := wrapTrace(makeSpan(spanID, "span1", 100, 200))
	next := wrapTrace(makeSpan(spanID, "span1", 100, 500))

	result := DiffTraces(base, next)
	spans := allSpans(result)
	require.Len(t, spans, 1)
	assert.Equal(t, DiffStatusModified, getDiffStatus(spans[0]))
}

func TestDiffTraces_Modified_AttributeChanged(t *testing.T) {
	spanID := []byte{0x01}
	attr1 := tempopb.MakeKeyValueStringPtr("http.method", "GET")
	attr2 := tempopb.MakeKeyValueStringPtr("http.method", "POST")

	base := wrapTrace(makeSpan(spanID, "span1", 100, 200, attr1))
	next := wrapTrace(makeSpan(spanID, "span1", 100, 200, attr2))

	result := DiffTraces(base, next)
	spans := allSpans(result)
	require.Len(t, spans, 1)
	assert.Equal(t, DiffStatusModified, getDiffStatus(spans[0]))
}

func TestDiffTraces_MixedStatuses(t *testing.T) {
	sharedID := []byte{0x01}
	baseOnlyID := []byte{0x02}
	nextOnlyID := []byte{0x03}
	modifiedID := []byte{0x04}

	base := wrapTrace(
		makeSpan(sharedID, "shared", 100, 200),
		makeSpan(baseOnlyID, "base-only", 300, 400),
		makeSpan(modifiedID, "will-change", 500, 600),
	)
	next := wrapTrace(
		makeSpan(sharedID, "shared", 100, 200),
		makeSpan(nextOnlyID, "next-only", 700, 800),
		makeSpan(modifiedID, "did-change", 500, 600),
	)

	result := DiffTraces(base, next)
	spans := allSpans(result)
	require.Len(t, spans, 4)

	shared := findSpanByID(spans, sharedID)
	require.NotNil(t, shared)
	assert.Equal(t, DiffStatusUnchanged, getDiffStatus(shared))

	baseOnly := findSpanByID(spans, baseOnlyID)
	require.NotNil(t, baseOnly)
	assert.Equal(t, DiffStatusRemoved, getDiffStatus(baseOnly))

	nextOnly := findSpanByID(spans, nextOnlyID)
	require.NotNil(t, nextOnly)
	assert.Equal(t, DiffStatusAdded, getDiffStatus(nextOnly))

	modified := findSpanByID(spans, modifiedID)
	require.NotNil(t, modified)
	assert.Equal(t, DiffStatusModified, getDiffStatus(modified))
}

func TestDiffTraces_NoOverlap(t *testing.T) {
	base := wrapTrace(makeSpan([]byte{0x01}, "base-span", 100, 200))
	next := wrapTrace(makeSpan([]byte{0x02}, "next-span", 300, 400))

	result := DiffTraces(base, next)
	spans := allSpans(result)
	require.Len(t, spans, 2)

	added := findSpanByID(spans, []byte{0x02})
	require.NotNil(t, added)
	assert.Equal(t, DiffStatusAdded, getDiffStatus(added))

	removed := findSpanByID(spans, []byte{0x01})
	require.NotNil(t, removed)
	assert.Equal(t, DiffStatusRemoved, getDiffStatus(removed))
}
