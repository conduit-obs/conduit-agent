#!/usr/bin/env bash
# Apply the meminator workshop manifests into the kind cluster, excluding the
# OpenTelemetry Operator Instrumentation CRD (otel-instrumentation.yaml). For
# Conduit's OBI test we want zero application-side instrumentation — that's the
# whole point of zero-code: the agent's eBPF receiver attaches to the running
# meminator processes from the host side without touching their config.
#
# The meminator checkout lives at $MEMINATOR_DIR (default
# ~/dev/instruqt-hny-workshop/k8s/meminator). Override with
#   MEMINATOR_DIR=/path/to/k8s/meminator ./apply-meminator.sh
# if your workshop checkout is elsewhere.
#
# Sequence:
#   1. namespace.yaml           — creates `meminator` namespace.
#   2. {phrase,image}-picker     — bottom of the dependency graph (no deps).
#   3. meminator                 — calls the pickers.
#   4. backend-for-frontend      — calls meminator + pickers.
#   5. web                       — the user-facing service exposed via NodePort.
#
# Skipped:
#   - otel-instrumentation.yaml  — bindplane operator path; we use OBI from
#                                  conduit instead, so this CRD reference would
#                                  be a no-op (the OTel Operator isn't installed
#                                  in this cluster) and possibly confusing in
#                                  `kubectl get instrumentation`.
#
# Postconditions: meminator namespace + 5 deployments + 5 services exist; the
# `web` service is reachable on NodePort 30114 inside the kind cluster. Use
# `kubectl port-forward -n meminator svc/web 10114:10114` to drive traffic
# from the host (kind's NodePort isn't reachable from the macOS host without
# the port-forward bridge).

set -euo pipefail

MEMINATOR_DIR="${MEMINATOR_DIR:-$HOME/dev/instruqt-hny-workshop/k8s/meminator}"
KIND_CONTEXT="${KIND_CONTEXT:-kind-conduit-smoke}"

if [[ ! -d "$MEMINATOR_DIR" ]]; then
  echo "apply-meminator.sh: $MEMINATOR_DIR does not exist." >&2
  echo "Set MEMINATOR_DIR to the path of your meminator workshop's k8s/meminator/ folder." >&2
  exit 1
fi

# Ordered list. namespace first (everything else depends on it), then leaves
# of the call graph (pickers), then nodes that depend on leaves, then the
# entrypoint (web). Same ordering would also work bottom-up but this matches
# how kubectl rolls out readiness probes most reliably.
manifests=(
  namespace.yaml
  phrase-picker.yaml
  image-picker.yaml
  meminator.yaml
  backend-for-frontend.yaml
  web.yaml
)

echo "Applying meminator manifests to $KIND_CONTEXT (skipping otel-instrumentation.yaml)..."
for f in "${manifests[@]}"; do
  path="$MEMINATOR_DIR/$f"
  if [[ ! -f "$path" ]]; then
    echo "  warn: $path not found; skipping" >&2
    continue
  fi
  echo "  apply $f"
  kubectl --context "$KIND_CONTEXT" apply -f "$path"
done

echo
echo "Waiting for meminator deployments to roll out (timeout 180s)..."
for d in phrase-picker image-picker meminator backend-for-frontend web; do
  kubectl --context "$KIND_CONTEXT" -n meminator rollout status deployment/$d --timeout=180s || {
    echo "  $d did not become ready; describe + recent logs:" >&2
    kubectl --context "$KIND_CONTEXT" -n meminator describe deployment/$d || true
    kubectl --context "$KIND_CONTEXT" -n meminator logs --tail=50 -l "app=$d" || true
    exit 1
  }
done

echo
echo "meminator is up. Drive traffic with:"
echo "  kubectl --context $KIND_CONTEXT -n meminator port-forward svc/web 10114:10114"
echo "Then open http://localhost:10114 in your browser, or curl it:"
echo "  curl -fsS http://localhost:10114/createPicture"
