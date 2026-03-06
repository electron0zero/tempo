#!/usr/bin/env bash
# Test script for the trace diff endpoint.
# Assumes the docker-compose single-binary stack is running on localhost:3200
# with traces already ingested (k6-tracing).
#
# Usage:
#   ./scripts/test-trace-diff.sh                     # auto-discover traces
#   ./scripts/test-trace-diff.sh <baseID> <nextID>   # use specific trace IDs
#   TEMPO_URL=http://host:3200 ./scripts/test-trace-diff.sh

set -euox pipefail

TEMPO_URL="${TEMPO_URL:-http://localhost:3200}"
PASS=0
FAIL=0
SKIP=0

# Colors (disabled if not a terminal)
if [ -t 1 ]; then
  GREEN='\033[0;32m'
  RED='\033[0;31m'
  YELLOW='\033[0;33m'
  BOLD='\033[1m'
  RESET='\033[0m'
else
  GREEN='' RED='' YELLOW='' BOLD='' RESET=''
fi

pass() {
  ((PASS++))
  echo -e "${GREEN}PASS${RESET} $1"
}
fail() {
  ((FAIL++))
  echo -e "${RED}FAIL${RESET} $1: $2"
}
skip() {
  ((SKIP++))
  echo -e "${YELLOW}SKIP${RESET} $1: $2"
}
header() { echo -e "\n${BOLD}=== $1 ===${RESET}"; }

# ---------- Discover trace IDs ----------
header "Setup"

if [ $# -ge 2 ]; then
  BASE="$1"
  NEXT="$2"
  echo "Using provided trace IDs: base=$BASE next=$NEXT"
else
  echo "Searching for traces at $TEMPO_URL ..."
  SEARCH_RESP=$(curl -sf "${TEMPO_URL}/api/search?limit=5" 2> /dev/null || true)
  if [ -z "$SEARCH_RESP" ]; then
    echo -e "${RED}ERROR${RESET}: Cannot reach Tempo at $TEMPO_URL or search returned empty."
    echo "Make sure the docker-compose stack is running and traces have been ingested."
    echo ""
    echo "Quick start:"
    echo "  make docker-tempo"
    echo "  cd example/docker-compose/single-binary && docker compose up -d"
    echo "  # wait 15-20 seconds, then re-run this script"
    exit 1
  fi

  TRACE_IDS=$(echo "$SEARCH_RESP" | jq -r '.traces[].traceID' 2> /dev/null)
  NUM_TRACES=$(echo "$TRACE_IDS" | wc -l | tr -d ' ')

  if [ "$NUM_TRACES" -lt 2 ]; then
    echo -e "${RED}ERROR${RESET}: Need at least 2 traces, found $NUM_TRACES."
    echo "Wait for k6-tracing to generate more traces and retry."
    exit 1
  fi

  BASE=$(echo "$TRACE_IDS" | sed -n '1p')
  NEXT=$(echo "$TRACE_IDS" | sed -n '2p')
  echo "Auto-discovered trace IDs: base=$BASE next=$NEXT"
fi

# ---------- Test 1: Diff two different traces ----------
header "Test 1: Diff two different traces"

RESP=$(curl -sf "${TEMPO_URL}/api/v2/traces/diff?base=${BASE}&next=${NEXT}" 2> /dev/null || true)
if [ -z "$RESP" ]; then
  fail "diff-two-traces" "request failed or returned empty"
else
  # Should have diff status attributes on every span
  STATUSES=$(echo "$RESP" | jq -r '[.trace.resourceSpans[].scopeSpans[].spans[] | .attributes[] | select(.key == "tempo.diff.status") | .value.stringValue]')
  COUNT=$(echo "$STATUSES" | jq 'length')

  if [ "$COUNT" -gt 0 ]; then
    pass "diff-two-traces: got $COUNT spans with diff status"
  else
    fail "diff-two-traces" "no spans have tempo.diff.status attribute"
  fi

  # With two different traces, we expect added and/or removed (no unchanged)
  HAS_ADDED=$(echo "$STATUSES" | jq 'map(select(. == "added")) | length')
  HAS_REMOVED=$(echo "$STATUSES" | jq 'map(select(. == "removed")) | length')
  HAS_UNCHANGED=$(echo "$STATUSES" | jq 'map(select(. == "unchanged")) | length')

  if [ "$HAS_ADDED" -gt 0 ] || [ "$HAS_REMOVED" -gt 0 ]; then
    pass "diff-two-traces: found added=$HAS_ADDED removed=$HAS_REMOVED (expected non-zero)"
  else
    fail "diff-two-traces" "expected added or removed spans, got added=$HAS_ADDED removed=$HAS_REMOVED"
  fi

  if [ "$HAS_UNCHANGED" -eq 0 ]; then
    pass "diff-two-traces: no unchanged spans (expected for different traces)"
  else
    # Not necessarily a failure - different traces could theoretically share span IDs
    echo -e "${YELLOW}NOTE${RESET} diff-two-traces: found $HAS_UNCHANGED unchanged spans (unusual for different traces)"
  fi

  SUMMARY=$(echo "$RESP" | jq '[.trace.resourceSpans[].scopeSpans[].spans[] | .attributes[] | select(.key == "tempo.diff.status") | .value.stringValue] | group_by(.) | map({status: .[0], count: length})')
  echo "  Status summary: $SUMMARY"
fi

# ---------- Test 2: Diff trace against itself ----------
header "Test 2: Self-diff (trace vs itself)"

RESP=$(curl -sf "${TEMPO_URL}/api/v2/traces/diff?base=${BASE}&next=${BASE}" 2> /dev/null || true)
if [ -z "$RESP" ]; then
  fail "self-diff" "request failed or returned empty"
else
  STATUSES=$(echo "$RESP" | jq -r '[.trace.resourceSpans[].scopeSpans[].spans[] | .attributes[] | select(.key == "tempo.diff.status") | .value.stringValue]')
  TOTAL=$(echo "$STATUSES" | jq 'length')
  UNCHANGED=$(echo "$STATUSES" | jq 'map(select(. == "unchanged")) | length')

  if [ "$TOTAL" -gt 0 ] && [ "$TOTAL" -eq "$UNCHANGED" ]; then
    pass "self-diff: all $TOTAL spans are unchanged"
  elif [ "$TOTAL" -eq 0 ]; then
    fail "self-diff" "no spans in response"
  else
    fail "self-diff" "expected all unchanged, got $UNCHANGED/$TOTAL"
    echo "  Statuses: $(echo "$STATUSES" | jq 'group_by(.) | map({status: .[0], count: length})')"
  fi
fi

# ---------- Test 3: Response structure ----------
header "Test 3: Response structure matches TraceByIDResponse"

RESP=$(curl -sf "${TEMPO_URL}/api/v2/traces/diff?base=${BASE}&next=${NEXT}" 2> /dev/null || true)
if [ -z "$RESP" ]; then
  fail "response-structure" "request failed"
else
  HAS_TRACE=$(echo "$RESP" | jq 'has("trace")')
  HAS_RS=$(echo "$RESP" | jq '.trace | has("resourceSpans")')

  if [ "$HAS_TRACE" = "true" ] && [ "$HAS_RS" = "true" ]; then
    pass "response-structure: has .trace.resourceSpans"
  else
    fail "response-structure" "missing expected fields (trace=$HAS_TRACE, resourceSpans=$HAS_RS)"
  fi
fi

# ---------- Test 4: LLM format ----------
header "Test 4: LLM format (Accept: application/vnd.grafana.llm)"

LLM_RESP=$(curl -sf -H 'Accept: application/vnd.grafana.llm' \
  "${TEMPO_URL}/api/v2/traces/diff?base=${BASE}&next=${NEXT}" 2> /dev/null || true)
if [ -z "$LLM_RESP" ]; then
  fail "llm-format" "request failed or returned empty"
else
  # LLM format: .trace.services[].scopes[].spans[] with flattened attributes and durationMs
  HAS_SERVICES=$(echo "$LLM_RESP" | jq '.trace | has("services")' 2> /dev/null || echo "false")
  if [ "$HAS_SERVICES" = "true" ]; then
    SPAN_COUNT=$(echo "$LLM_RESP" | jq '[.trace.services[].scopes[].spans[]] | length')
    FIRST_SPAN=$(echo "$LLM_RESP" | jq '.trace.services[0].scopes[0].spans[0]')
    HAS_DURATION=$(echo "$FIRST_SPAN" | jq 'has("durationMs")' 2> /dev/null || echo "false")
    HAS_DIFF_STATUS=$(echo "$LLM_RESP" | jq '[.trace.services[].scopes[].spans[] | .attributes["tempo.diff.status"] // empty] | length')

    if [ "$SPAN_COUNT" -gt 0 ]; then
      pass "llm-format: got $SPAN_COUNT spans"
    else
      fail "llm-format" "no spans in LLM response"
    fi

    if [ "$HAS_DURATION" = "true" ]; then
      pass "llm-format: spans have durationMs field"
    else
      fail "llm-format" "spans missing durationMs field"
    fi

    if [ "$HAS_DIFF_STATUS" -gt 0 ]; then
      pass "llm-format: $HAS_DIFF_STATUS spans have tempo.diff.status"
    else
      fail "llm-format" "no spans have tempo.diff.status in LLM format"
    fi
  else
    echo "  LLM response structure: $(echo "$LLM_RESP" | jq 'keys' 2> /dev/null || echo 'not JSON')"
    fail "llm-format" "unexpected response structure (missing .trace.services)"
  fi
fi

# ---------- Test 5: Error cases ----------
header "Test 5: Error handling"

# Missing base param
HTTP_CODE=$(curl -s -o /dev/null -w '%{http_code}' "${TEMPO_URL}/api/v2/traces/diff?next=${NEXT}")
if [ "$HTTP_CODE" = "400" ]; then
  pass "error-missing-base: got HTTP 400"
else
  fail "error-missing-base" "expected 400, got $HTTP_CODE"
fi

# Missing next param
HTTP_CODE=$(curl -s -o /dev/null -w '%{http_code}' "${TEMPO_URL}/api/v2/traces/diff?base=${BASE}")
if [ "$HTTP_CODE" = "400" ]; then
  pass "error-missing-next: got HTTP 400"
else
  fail "error-missing-next" "expected 400, got $HTTP_CODE"
fi

# Missing both params
HTTP_CODE=$(curl -s -o /dev/null -w '%{http_code}' "${TEMPO_URL}/api/v2/traces/diff")
if [ "$HTTP_CODE" = "400" ]; then
  pass "error-missing-both: got HTTP 400"
else
  fail "error-missing-both" "expected 400, got $HTTP_CODE"
fi

# Invalid trace ID
HTTP_CODE=$(curl -s -o /dev/null -w '%{http_code}' "${TEMPO_URL}/api/v2/traces/diff?base=not-a-trace-id&next=${NEXT}")
if [ "$HTTP_CODE" = "400" ]; then
  pass "error-invalid-id: got HTTP 400"
else
  fail "error-invalid-id" "expected 400, got $HTTP_CODE"
fi

# ---------- Test 6: CLI (if binary exists) ----------
header "Test 6: CLI (tempo-cli)"

SCRIPT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
CLI_BIN=""
# Check common locations for the tempo-cli binary
for candidate in \
  "./bin/tempo-cli" \
  "${SCRIPT_DIR}/bin/tempo-cli" \
  "./bin/darwin/tempo-cli-arm64" \
  "${SCRIPT_DIR}/bin/darwin/tempo-cli-arm64" \
  "./bin/linux/tempo-cli-amd64" \
  "${SCRIPT_DIR}/bin/linux/tempo-cli-amd64"; do
  if [ -x "$candidate" ]; then
    CLI_BIN="$candidate"
    break
  fi
done

if [ -n "$CLI_BIN" ] && [ -x "$CLI_BIN" ]; then
  CLI_OUT=$($CLI_BIN query api trace-diff "$TEMPO_URL" "$BASE" "$NEXT" 2> /dev/null || true)
  if [ -n "$CLI_OUT" ]; then
    CLI_HAS_TRACE=$(echo "$CLI_OUT" | jq 'has("trace")' 2> /dev/null || echo "false")
    if [ "$CLI_HAS_TRACE" = "true" ]; then
      CLI_SPAN_COUNT=$(echo "$CLI_OUT" | jq '[.trace.resourceSpans[].scopeSpans[].spans[]] | length')
      pass "cli-diff: got $CLI_SPAN_COUNT spans via CLI"
    else
      fail "cli-diff" "CLI output missing .trace field"
    fi
  else
    fail "cli-diff" "CLI returned empty output"
  fi
else
  skip "cli-diff" "tempo-cli binary not found (run 'make tempo-cli' first)"
fi

# ---------- Summary ----------
header "Summary"

TOTAL=$((PASS + FAIL + SKIP))
echo -e "Total: $TOTAL | ${GREEN}Passed: $PASS${RESET} | ${RED}Failed: $FAIL${RESET} | ${YELLOW}Skipped: $SKIP${RESET}"

if [ "$FAIL" -gt 0 ]; then
  exit 1
fi
