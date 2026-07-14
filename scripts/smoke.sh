#!/usr/bin/env bash
# End-to-end smoke test for twinget: builds the CLI and the twin demo
# backends, runs them on ephemeral loopback ports, and asserts on real
# CLI output and exit codes. No external network, idempotent, finishes
# in seconds.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
WORKDIR="$(mktemp -d)"
DEMO_PID=""
cleanup() {
  [ -n "$DEMO_PID" ] && kill "$DEMO_PID" 2>/dev/null || true
  rm -rf "$WORKDIR"
}
trap cleanup EXIT

fail() {
  echo "SMOKE FAIL: $*" >&2
  exit 1
}

BIN="$WORKDIR/twinget"
DEMO="$WORKDIR/demo-backends"

echo "1. build the CLI and the demo backends"
(cd "$ROOT" && go build -o "$BIN" ./cmd/twinget) || fail "go build twinget failed"
(cd "$ROOT" && go build -o "$DEMO" ./examples/demo-backends) || fail "go build demo failed"

echo "2. version matches the manifest"
"$BIN" version | grep -qx "twinget 0.1.0" || fail "version mismatch"

echo "3. start the twin backends on ephemeral loopback ports"
"$DEMO" > "$WORKDIR/demo.out" &
DEMO_PID=$!
for _ in $(seq 1 50); do
  [ "$(wc -l < "$WORKDIR/demo.out")" -ge 2 ] && break
  sleep 0.1
done
A="$(sed -n 's/^A=//p' "$WORKDIR/demo.out")"
B="$(sed -n 's/^B=//p' "$WORKDIR/demo.out")"
[ -n "$A" ] && [ -n "$B" ] || fail "demo backends did not announce URLs"

echo "4. unfiltered diff finds the planted /api/users regressions (exit 1)"
set +e
OUT="$("$BIN" diff --a "$A" --b "$B" /api/users)"
CODE=$?
set -e
[ "$CODE" -eq 1 ] || fail "diff should exit 1, got $CODE"
echo "$OUT" | grep -q '\$\.users\[0\]\.role' || fail "role regression missing"
echo "$OUT" | grep -q '\$\.users\[1\]\.email' || fail "dropped email missing"
echo "$OUT" | grep -q '\$\.total.*type' || fail "type regression missing"

echo "5. noise filters mask timestamps, ids and volatile headers"
echo "$OUT" | grep -q '\$\.request_id' || fail "request_id should be a raw diff"
set +e
OUT="$("$BIN" diff --a "$A" --b "$B" --ignore-timestamps --ignore-ids /api/users)"
set -e
echo "$OUT" | grep -q '\$\.request_id' && fail "--ignore-ids did not mask request_id"
echo "$OUT" | grep -q 'ignored as noise' || fail "ignored count missing"

echo "6. filters reach full parity on the health endpoint (exit 0)"
"$BIN" diff --a "$A" --b "$B" \
  --ignore-timestamps --ignore '$.uptime_s' --ignore-header content-type \
  /api/health | grep -q 'result: PARITY' || fail "health should reach parity"

echo "7. status regression is caught (200 vs 404)"
set +e
OUT="$("$BIN" diff --a "$A" --b "$B" /api/orders/42)"
CODE=$?
set -e
[ "$CODE" -eq 1 ] || fail "status diff should exit 1"
echo "$OUT" | grep -q 'a: 200  b: 404' || fail "status line missing"

echo "8. JSON output is a stable machine-readable envelope"
"$BIN" diff --a "$A" --b "$B" --format json /api/users > "$WORKDIR/out.json" || true
grep -q '"tool": "twinget"' "$WORKDIR/out.json" || fail "json envelope missing"
grep -q '"schema_version": 1' "$WORKDIR/out.json" || fail "schema version missing"

echo "9. batch mode sweeps the example request list"
set +e
OUT="$("$BIN" batch --a "$A" --b "$B" \
  --ignore-timestamps --ignore-ids --ignore-header content-type \
  --ignore '$.uptime_s' \
  "$ROOT/examples/requests.txt")"
CODE=$?
set -e
[ "$CODE" -eq 1 ] || fail "batch should exit 1 (regressions remain)"
echo "$OUT" | grep -q '4 requests: 1 parity, 3 diff — FAIL' \
  || fail "batch summary missing"

echo "10. usage errors exit 2, transport failures exit 3"
set +e
"$BIN" diff --a "$A" --b "$B" --format yaml /x >/dev/null 2>&1
[ $? -eq 2 ] || fail "bad --format should exit 2"
"$BIN" diff --a "$A" --b "http://127.0.0.1:1" /x >/dev/null 2>&1
[ $? -eq 3 ] || fail "unreachable backend should exit 3"
set -e

echo "SMOKE OK"
