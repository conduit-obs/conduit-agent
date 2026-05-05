# Local k8s — meminator + conduit + OBI

A reproducible local recipe for testing Conduit's zero-code application
instrumentation against the Honeycomb [meminator](https://github.com/jessitron/meminator-monorepo)
workshop app. Resolves [ADR-0020](../../docs/adr/adr-0020.md)'s "Open question:
build pipeline" for the local-development case; the public release pipeline is
unchanged and continues to ship a binary without OBI linked in until upstream
publishes pre-generated eBPF bindings.

The recipe runs four pieces in one kind cluster:

| Piece | Source | Purpose |
|---|---|---|
| kind cluster | upstream | Linux node with eBPF-capable kernel via Docker Desktop's VM |
| conduit-agent | local `conduit:obi` image | OBI receiver attaches to meminator processes, OTLP egress to Honeycomb |
| meminator | `~/dev/instruqt-hny-workshop/k8s/meminator/` | 5 services with all OTel instrumentation stripped — blank slate |
| traffic | `kubectl port-forward` + `curl` | Drives requests through the meminator service graph |

## Prerequisites

- Docker Desktop running (kind needs a Linux VM kernel ≥ 5.8 for OBI; Docker
  Desktop on recent macOS is fine).
- `kind`, `kubectl`, `helm`, `git`, `make` on PATH.
- The meminator workshop checkout at `~/dev/instruqt-hny-workshop/k8s/meminator/`
  (or set `MEMINATOR_DIR` when running `apply-meminator.sh`).
- A Honeycomb ingest API key. Export it as `HONEYCOMB_API_KEY` before
  `make obi-kind-deploy`. (Without it the agent still runs but OTLP egress
  fails — useful for confirming OBI's eBPF generation in isolation; see
  "Verify without Honeycomb" below.)

## One-time setup

```sh
# From the conduit-agent repo root.

# 1. Clone OBI at the pinned tag (see Makefile OBI_VERSION) and run upstream's
#    `make docker-generate` inside it to produce the eBPF Go bindings. Needs
#    Docker. ~2–4 min on first run; idempotent. Also injects
#    `replace go.opentelemetry.io/obi => ./third_party/obi` into go.mod
#    via `go mod edit` so subsequent Linux builds resolve OBI from the local
#    checkout (the upstream proxy v0.8.0 lacks pre-generated BPF bindings).
make obi-vendor

# 2. Cross-compile a Linux binary with OBI linked in (the //go:build linux
#    file at internal/collector/components_obi_linux.go is what pulls OBI
#    into the build) and bake it into the conduit:obi image.
make obi-image
```

When you're done, `make obi-clean` drops the replace directive from go.mod.
The third_party/obi/ checkout and the conduit:obi image are left in place
so the next iteration is fast; delete them manually to fully reset.

## Running the recipe

```sh
# 3. Bring up a single-node kind cluster (idempotent — reuses an existing
#    cluster named "conduit-smoke" if present).
make kind-up

# 4. Load the local conduit:obi image into the kind cluster's containerd.
make obi-kind-load

# 5. Helm-install the chart with OBI on. Honeycomb api key flows through
#    the env var; if HONEYCOMB_API_KEY is unset the chart still installs
#    but OTLP egress will 401.
export HONEYCOMB_API_KEY=hcaik_...
make obi-kind-deploy

# 6. Apply the meminator manifests (skipping otel-instrumentation.yaml,
#    which is the bindplane operator path — we don't want SDK
#    instrumentation interfering with the OBI test).
./tools/local-k8s/apply-meminator.sh

# 7. Drive traffic. In a second terminal:
kubectl --context kind-conduit-smoke -n meminator port-forward svc/web 10114:10114
# Then in the original terminal, hit the app a few times:
for i in $(seq 1 20); do
  curl -fsS http://localhost:10114/createPicture > /dev/null && echo "request $i ok"
  sleep 1
done
```

## Verify

In Honeycomb, open the `k8s-cluster` dataset (the profile-default
`service.name` per ADR-0021; matches `dashboards/k8s-cluster-overview.json`):

| Where to look | What you should see |
|---|---|
| **BubbleUp** on `instrumentation.scope = "obi"` | RED metrics + spans tagged with OBI's scope marker |
| **Service map** | `web → backend-for-frontend → {meminator, image-picker, phrase-picker}` |
| Spans with `host.name`, `k8s.pod.name`, `k8s.namespace.name = meminator` | k8sattributes processor enriched OBI's output |
| HTTP method / status / route attributes on every span | OBI's HTTP probe captured the request shape |

If you imported the k8s board via the wizard, the `Pod CPU` / `Pod Memory` /
`Log Volume` panels light up alongside the new `instrumentation.scope = "obi"`
spans.

## Verify without Honeycomb

To confirm OBI is generating data without an external dependency, swap the
agent's exporter to `debug` by editing the rendered ConfigMap in-place:

```sh
kubectl --context kind-conduit-smoke -n conduit edit cm conduit-conduit-agent
# Set output.mode: debug under the conduit.yaml block, save.
kubectl --context kind-conduit-smoke -n conduit rollout restart ds/conduit-conduit-agent
kubectl --context kind-conduit-smoke -n conduit logs -l app.kubernetes.io/name=conduit-agent --tail=200 -f
```

OBI's spans show up in the agent's stdout with `service.name=meminator` etc.
and `InstrumentationScope #0 obi`. If you don't see them: see Troubleshooting.

## Tear down

```sh
# Cluster only — preserves the OBI checkout + image for next time.
make kind-down

# Drop the OBI replace directive from go.mod when you're done with the
# session (committed go.mod has require but no replace). third_party/obi/
# and the conduit:obi image stay; delete them manually to reclaim disk.
make obi-clean
```

## Troubleshooting

### `obi-vendor` fails with "make docker-generate: docker not found"

OBI's eBPF bindings are produced by a containerized clang+libbpf toolchain
upstream ships in `third_party/obi/Dockerfile.generate`. Start Docker Desktop
and re-run.

### `make build` on Linux fails with "third_party/obi/ is missing"

ADR-0020 sub-decision 1 says every Linux conduit binary links OBI in.
`make build` enforces this by requiring `third_party/obi/` to exist when
`GOOS=linux`. Fix: `make obi-vendor` once. The CI workflow at
`.github/workflows/ci.yml` runs that on Linux runners automatically.

### Linux build fails with "undefined: BpfObjects" / "undefined: NetFlowId"

OBI v0.8.0's published Go module doesn't ship the pre-generated eBPF Go
bindings. Without `make obi-vendor` (which clones OBI source AND runs
`make docker-generate` inside it), the upstream proxy version is unusable
for an actual Linux build. macOS / Windows builds hide this because the
`//go:build linux` tag on `components_obi_linux.go` excludes the import.

### Conduit pod crash-loops with `failed to attach probe: operation not permitted`

Kind's nodes need to allow privileged containers (default: yes) and the
host kernel must support eBPF (default on Docker Desktop's recent VMs).
Check the agent pod's SecurityContext has the OBI capability set:

```sh
kubectl --context kind-conduit-smoke -n conduit get pod -l app.kubernetes.io/name=conduit-agent -o yaml \
  | yq '.items[0].spec.containers[0].securityContext'
```

Should include `SYS_ADMIN`, `BPF`, `PERFMON`, `SYS_PTRACE`, `DAC_READ_SEARCH`,
`NET_RAW`, `CHECKPOINT_RESTORE`. If missing, `obi.enabled=true` didn't make it
into the chart values — confirm `tools/local-k8s/values-obi.yaml` was passed.

### `conduit doctor` reports CDT0204

Means OBI's receiver factory isn't registered in the running binary. Most
common cause: the deployed image is `ghcr.io/conduit-obs/conduit-agent:<tag>`
(the public release, no OBI) instead of the locally-built `conduit:obi`.
Check `image:` in the values overlay and re-run `make obi-kind-load
obi-kind-deploy`.

### No OBI spans appear, even with traffic

OBI's discovery scans `/proc` for processes that match its instrumentation
patterns. With no `discovery.services` configured (the default), OBI tries to
instrument every process it can. Two reasons it might skip meminator:

1. **Kernel BTF unavailable.** Check `kubectl exec` into a conduit pod and
   `ls /sys/kernel/btf/vmlinux`. Missing on some kind base images.
2. **Process ran before agent attached.** Restart the meminator deployment
   so OBI sees the process come up:
   `kubectl --context kind-conduit-smoke -n meminator rollout restart deployment --all`.
