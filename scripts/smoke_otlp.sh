#!/usr/bin/env bash
# smoke_otlp.sh — the shared "binary boots + real OTLP roundtrip" smoke.
#
# Starts a conduit agent on a deterministic OTLP-only (profile.mode=none)
# config, sends synthetic spans through it with `conduit send-test-data`,
# and asserts the agent's debug exporter logged the received trace. Used by
# the host-smoke matrix (Linux / macOS / Windows-via-Git-Bash) and the
# package-install foreground smoke in .github/workflows/integration.yml, and
# locally via `make smoke-host`.
#
# Usage:
#   scripts/smoke_otlp.sh [path-to-conduit-binary]
#
# The binary defaults to $CONDUIT_BIN, then ./bin/conduit. Override the
# endpoint with SMOKE_TARGET (default 127.0.0.1:4318). A dummy Honeycomb key
# is injected so the agent boots in honeycomb output mode without a real
# tenant — the export fails auth upstream, but the debug exporter still logs
# every received batch, which is what we assert on (same pattern as the kind
# smoke in the Makefile).
set -euo pipefail

BIN="${1:-${CONDUIT_BIN:-./bin/conduit}}"
TARGET="${SMOKE_TARGET:-127.0.0.1:4318}"
HOST="${TARGET%%:*}"
PORT="${TARGET##*:}"

if [ ! -x "$BIN" ] && ! command -v "$BIN" >/dev/null 2>&1; then
    echo "smoke_otlp: conduit binary not found or not executable: $BIN" >&2
    exit 1
fi

WORKDIR="$(mktemp -d)"
LOG="$WORKDIR/agent.log"
CFG="$WORKDIR/conduit.yaml"

cleanup() {
    if [ -n "${AGENT_PID:-}" ] && kill -0 "$AGENT_PID" 2>/dev/null; then
        kill "$AGENT_PID" 2>/dev/null || true
        wait "$AGENT_PID" 2>/dev/null || true
    fi
    rm -rf "$WORKDIR"
}
trap cleanup EXIT

# profile.mode=none keeps the agent to OTLP-in / debug+honeycomb-out only:
# no host-metrics, journald, or filelog receivers that would be flaky in a
# container or on a CI runner. A literal dummy key avoids needing env wiring.
cat > "$CFG" <<'YAML'
service_name: smoke-host
deployment_environment: ci
output:
  mode: honeycomb
  honeycomb:
    api_key: smoke-dummy-key
profile:
  mode: none
YAML

echo "smoke_otlp: starting agent ($BIN) on $TARGET"
"$BIN" run -c "$CFG" > "$LOG" 2>&1 &
AGENT_PID=$!

# Wait for the OTLP/HTTP listener to accept connections, failing fast if the
# agent crashed on boot. /dev/tcp is a bash builtin available on the Linux,
# macOS, and Git-Bash-on-Windows runners we drive this from.
ready=0
for _ in $(seq 1 60); do
    if ! kill -0 "$AGENT_PID" 2>/dev/null; then
        echo "smoke_otlp: agent exited before opening $TARGET" >&2
        cat "$LOG" >&2
        exit 1
    fi
    if (exec 3<>"/dev/tcp/${HOST}/${PORT}") 2>/dev/null; then
        exec 3>&- 3<&- || true
        ready=1
        break
    fi
    sleep 0.5
done
if [ "$ready" -ne 1 ]; then
    echo "smoke_otlp: OTLP endpoint $TARGET never opened" >&2
    cat "$LOG" >&2
    exit 1
fi

echo "smoke_otlp: sending synthetic spans"
"$BIN" send-test-data --target "$TARGET" --duration 2s --rate 5 --profile red

# Give the agent a moment to flush the debug exporter, then assert it logged
# the received trace. "TracesExporter" is the debug exporter's per-batch log
# line at the default (basic) verbosity.
found=0
for _ in $(seq 1 20); do
    if grep -qE "TracesExporter|conduit-send-test-data" "$LOG"; then
        found=1
        break
    fi
    sleep 0.5
done
if [ "$found" -ne 1 ]; then
    echo "smoke_otlp: agent never logged a received trace" >&2
    echo "---- agent log ----" >&2
    cat "$LOG" >&2
    exit 1
fi

echo "smoke_otlp: ok ($TARGET)"
