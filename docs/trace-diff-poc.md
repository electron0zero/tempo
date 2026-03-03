# Trace Diff Endpoint - POC

Compare two traces by ID and get a merged trace where every span is annotated with its diff status.

## Endpoint

```
GET /api/v2/traces/diff?base={traceID}&next={traceID}
```

Returns the same `TraceByIDResponse` format as `/api/v2/traces/{traceID}`. Supports JSON, protobuf, and LLM (`application/vnd.grafana.llm`) response formats via the `Accept` header. Each span gets a `tempo.diff.status` attribute:

| Status | Meaning |
|---|---|
| `added` | Span only exists in `next` trace |
| `removed` | Span only exists in `base` trace |
| `unchanged` | Span exists in both, all fields identical |
| `modified` | Span exists in both, some fields differ |

Spans are matched by their span ID.

## Prerequisites

- Docker and Docker Compose
- Go 1.23+ (for building)
- `make`, `curl`, `jq`

## Steps

### 1. Build the Tempo Docker image

From the repo root:

```bash
make docker-tempo
```

This compiles the `tempo` binary for linux and builds the `grafana/tempo:latest` docker image.

### 2. Start the local stack

```bash
cd example/docker-compose/single-binary
docker compose up -d
```

This starts:
- **Tempo** (single-binary mode) on port 3200
- **Redpanda** (Kafka-compatible broker)
- **MinIO** (S3-compatible object store)
- **k6-tracing** (synthetic trace generator)
- **Alloy** (OTLP collector)
- **Grafana** on port 3000
- **Prometheus** on port 9090

### 3. Wait for traces to be ingested

k6-tracing starts generating synthetic traces immediately. Wait ~15-20 seconds for traces to flow through the pipeline.

Verify traces are available:

```bash
curl -s 'http://localhost:3200/api/search?limit=5' | jq '.traces[:5] | .[].traceID'
```

Expected output - a list of trace IDs:

```
"2dffbb31c7f77ec9472147e563b055c2"
"65c3fb6975b9286a1dd9f7464e3492"
"f29a13ed4f5db33f204c16f05de2c3b"
...
```

If you get an empty result, wait a few more seconds and retry.

### 4. Test the diff endpoint

Pick any two trace IDs from step 3 and substitute them below.

**Diff two different traces** (expect all `added` and `removed`):

```bash
BASE=<traceID1>
NEXT=<traceID2>

curl -s "http://localhost:3200/api/v2/traces/diff?base=${BASE}&next=${NEXT}" | jq '
  [.trace.resourceSpans[].scopeSpans[].spans[]
   | .attributes[] | select(.key == "tempo.diff.status")
   | .value.stringValue]
  | group_by(.) | map({status: .[0], count: length})'
```

Expected output (counts will vary):

```json
[
  { "status": "added", "count": 2 },
  { "status": "removed", "count": 7 }
]
```

**Diff a trace against itself** (expect all `unchanged`):

```bash
TRACE=<traceID1>

curl -s "http://localhost:3200/api/v2/traces/diff?base=${TRACE}&next=${TRACE}" | jq '
  [.trace.resourceSpans[].scopeSpans[].spans[]
   | .attributes[] | select(.key == "tempo.diff.status")
   | .value.stringValue]
  | group_by(.) | map({status: .[0], count: length})'
```

Expected output:

```json
[
  { "status": "unchanged", "count": 7 }
]
```

**View full diff response** (raw JSON):

```bash
curl -s "http://localhost:3200/api/v2/traces/diff?base=${BASE}&next=${NEXT}" | jq .
```

**LLM format** (simplified JSON for AI/LLM consumption):

```bash
curl -s -H 'Accept: application/vnd.grafana.llm' \
  "http://localhost:3200/api/v2/traces/diff?base=${BASE}&next=${NEXT}" | jq .
```

The LLM format flattens attributes, converts IDs to hex strings, and adds computed `durationMs` to each span. The `tempo.diff.status` attribute appears in each span's `attributes` map.

### 5. Test via CLI

Build the CLI:

```bash
make tempo-cli
```

Run a diff:

```bash
./bin/tempo-cli query api trace-diff http://localhost:3200 <baseTraceID> <nextTraceID>
```

Optional `--org-id` flag for multi-tenant setups:

```bash
./bin/tempo-cli query api trace-diff --org-id=my-tenant http://localhost:3200 <baseTraceID> <nextTraceID>
```

Output is the full `TraceByIDResponse` as JSON, same as `query api trace-id`.

### 6. Test via MCP

The MCP server (when enabled via `query_frontend.mcp_server.enabled: true`) exposes a `diff-traces` tool.

**Tool parameters:**
- `base_trace_id` (required) - base trace ID to compare from
- `next_trace_id` (required) - next trace ID to compare to

The MCP tool returns the diff in LLM format automatically. You can test it via any MCP client connected to `http://localhost:3200/api/mcp`.

### 7. Run unit tests

From the repo root:

```bash
go test ./pkg/model/trace/... -run TestDiff -v
```

Expected: all 9 test cases pass.

### 8. Tear down

```bash
cd example/docker-compose/single-binary
docker compose down -v
```

## Troubleshooting

**"invalid UUID length: 0" error**: The Tempo container is running an old image. Rebuild with `make docker-tempo` and recreate the container:

```bash
cd example/docker-compose/single-binary
docker compose up -d --force-recreate tempo
```

**Empty search results**: k6-tracing needs time to generate traces. Wait 15-20 seconds after starting the stack. Check Tempo logs for errors:

```bash
docker logs single-binary-tempo-1 2>&1 | tail -20
```

**"querier not initialized" error**: In rare cases the querier module hasn't started yet. Retry after a few seconds.

## Files changed

| File | Description |
|---|---|
| `pkg/api/http.go` | `PathTraceDiff` constant, `ParseTraceDiffRequest` function |
| `pkg/model/trace/diff.go` | `DiffTraces` - core diff algorithm |
| `pkg/model/trace/diff_test.go` | Unit tests for diff logic |
| `modules/querier/http.go` | `TraceDiffHandler` HTTP handler, LLM format in response writer |
| `modules/frontend/combiner/llm_marshaler.go` | Exported `MarshalResponseToLLM` for reuse |
| `modules/frontend/frontend.go` | `TraceDiffHandler` field on `QueryFrontend` |
| `modules/frontend/mcp.go` | `diff-traces` MCP tool registration |
| `modules/frontend/mcp_tools.go` | `handleDiffTraces` MCP handler |
| `cmd/tempo/app/modules.go` | Route registration at querier and frontend levels |
| `pkg/httpclient/client.go` | `QueryTraceDiff` HTTP client method |
| `cmd/tempo-cli/cmd-query-trace-diff.go` | CLI `query api trace-diff` command |
| `cmd/tempo-cli/main.go` | CLI command registration |
