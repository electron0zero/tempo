package main

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"sort"
	"text/tabwriter"

	"github.com/grafana/tempo/modules/frontend/combiner"
	"github.com/grafana/tempo/pkg/api"
	"github.com/grafana/tempo/pkg/httpclient"
)

type queryTraceDiffSummaryCmd struct {
	APIEndpoint string `arg:"" help:"tempo api endpoint"`
	BaseTraceID string `arg:"" help:"base trace ID to compare from"`
	NextTraceID string `arg:"" help:"next trace ID to compare to"`

	TopK  int    `name:"top" help:"number of results per list" default:"10"`
	OrgID string `help:"optional orgID"`
}

type spanEntry struct {
	Service  string
	Name     string
	Duration float64
	Status   string
	Kind     string
}

func (cmd *queryTraceDiffSummaryCmd) Run(_ *globalOptions) error {
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

	spans := extractSpans(resp)

	var added, removed, modified []spanEntry
	for _, s := range spans {
		switch s.Status {
		case "added":
			added = append(added, s)
		case "removed":
			removed = append(removed, s)
		case "modified":
			modified = append(modified, s)
		}
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)

	// 1. Duration changes for modified spans
	if len(modified) > 0 {
		fmt.Fprintf(w, "\n--- Modified spans (duration changed) ---\n")
		fmt.Fprintf(w, "SERVICE\tOPERATION\tDURATION (ms)\tKIND\n")
		sortByDuration(modified)
		for i, s := range modified {
			if i >= cmd.TopK {
				break
			}
			fmt.Fprintf(w, "%s\t%s\t%.2f\t%s\n", s.Service, s.Name, s.Duration, s.Kind)
		}
		w.Flush()
	}

	// 2. Duration delta by operation name (added vs removed)
	durationByOp := buildDurationDelta(added, removed)
	if len(durationByOp) > 0 {
		fmt.Fprintf(w, "\n--- Top duration changes by operation name ---\n")
		fmt.Fprintf(w, "OPERATION\tBASE AVG (ms)\tNEXT AVG (ms)\tDELTA (ms)\n")
		sort.Slice(durationByOp, func(i, j int) bool {
			return abs(durationByOp[i].delta) > abs(durationByOp[j].delta)
		})
		for i, d := range durationByOp {
			if i >= cmd.TopK {
				break
			}
			fmt.Fprintf(w, "%s\t%.2f\t%.2f\t%+.2f\n", d.name, d.baseAvg, d.nextAvg, d.delta)
		}
		w.Flush()
	}

	// 3. Slowest removed operations (from base trace)
	if len(removed) > 0 {
		fmt.Fprintf(w, "\n--- Slowest removed operations (base trace) ---\n")
		fmt.Fprintf(w, "SERVICE\tOPERATION\tDURATION (ms)\tKIND\n")
		sortByDuration(removed)
		for i, s := range removed {
			if i >= cmd.TopK {
				break
			}
			fmt.Fprintf(w, "%s\t%s\t%.2f\t%s\n", s.Service, s.Name, s.Duration, s.Kind)
		}
		w.Flush()
	}

	// 4. Slowest added operations (from next trace)
	if len(added) > 0 {
		fmt.Fprintf(w, "\n--- Slowest added operations (next trace) ---\n")
		fmt.Fprintf(w, "SERVICE\tOPERATION\tDURATION (ms)\tKIND\n")
		sortByDuration(added)
		for i, s := range added {
			if i >= cmd.TopK {
				break
			}
			fmt.Fprintf(w, "%s\t%s\t%.2f\t%s\n", s.Service, s.Name, s.Duration, s.Kind)
		}
		w.Flush()
	}

	// Summary
	fmt.Fprintf(os.Stdout, "\nSummary: %d added, %d removed, %d modified\n", len(added), len(removed), len(modified))

	return nil
}

func extractSpans(resp combiner.LLMTraceByIDResponse) []spanEntry {
	var spans []spanEntry
	for _, svc := range resp.Trace.Services {
		for _, scope := range svc.Scopes {
			for _, span := range scope.Spans {
				status, _ := span.Attributes["tempo.diff.status"].(string)
				spans = append(spans, spanEntry{
					Service:  svc.ServiceName,
					Name:     span.Name,
					Duration: span.DurationMs,
					Status:   status,
					Kind:     span.Kind,
				})
			}
		}
	}
	return spans
}

func sortByDuration(spans []spanEntry) {
	sort.Slice(spans, func(i, j int) bool {
		return spans[i].Duration > spans[j].Duration
	})
}

type durationDelta struct {
	name    string
	baseAvg float64
	nextAvg float64
	delta   float64
}

func buildDurationDelta(added, removed []spanEntry) []durationDelta {
	type stats struct {
		totalDur float64
		count    int
	}

	removedByName := make(map[string]stats)
	for _, s := range removed {
		st := removedByName[s.Name]
		st.totalDur += s.Duration
		st.count++
		removedByName[s.Name] = st
	}

	addedByName := make(map[string]stats)
	for _, s := range added {
		st := addedByName[s.Name]
		st.totalDur += s.Duration
		st.count++
		addedByName[s.Name] = st
	}

	// Only include operations that appear in both
	var deltas []durationDelta
	for name, baseSt := range removedByName {
		nextSt, ok := addedByName[name]
		if !ok {
			continue
		}
		baseAvg := baseSt.totalDur / float64(baseSt.count)
		nextAvg := nextSt.totalDur / float64(nextSt.count)
		deltas = append(deltas, durationDelta{
			name:    name,
			baseAvg: baseAvg,
			nextAvg: nextAvg,
			delta:   nextAvg - baseAvg,
		})
	}
	return deltas
}

func abs(f float64) float64 {
	if f < 0 {
		return -f
	}
	return f
}
