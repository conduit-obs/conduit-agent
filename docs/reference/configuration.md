# Configuration reference — `conduit.yaml`

This is the complete schema for the Conduit agent's configuration file.
The schema is deliberately small — every field documented here is
either required, has a sensible default, or is the documented escape
hatch (`overrides:`) for advanced cases.

The source of truth is [`internal/config/types.go`](../../internal/config/types.go);
this page is hand-curated to stay in lock step. If something here
looks out of date, [open an issue](https://github.com/conduit-obs/conduit-agent/issues).

## File location

| Platform | Path |
|---|---|
| Linux (deb / rpm / apk) | `/etc/conduit/conduit.yaml` |
| Docker (in-image default) | `/etc/conduit/conduit.yaml` |
| Kubernetes (Helm) | rendered into a ConfigMap; mounted at `/etc/conduit/conduit.yaml` |
| Windows (MSI) | `%PROGRAMDATA%\Conduit\conduit.yaml` |
| Anywhere | pass `--config /path/to/conduit.yaml` to `conduit run` / `conduit doctor` / `conduit preview` |

## Top-level shape

```yaml
service_name: edge-gateway              # required
deployment_environment: production      # required

output:                                 # required, see "output" below
  mode: honeycomb
  honeycomb:
    api_key: ${env:HONEYCOMB_API_KEY}

profile:                                # optional
  mode: auto
  host_metrics: true
  system_logs: true

metrics:                                # optional
  red:
    enabled: true
    span_dimensions: []
    extra_resource_dimensions: []
    cardinality_limit: 5000

overrides: {}                           # optional, escape hatch
```

Every field is parsed in **strict** mode — unknown keys produce a
validation error (`CDT0001 config.syntax`). This is intentional: the
schema is small enough to memorize, and we want typos to fail fast
rather than silently no-op.

## `service_name` (required, string)

Populates the `service.name` resource attribute on every emitted
signal. This is the dimension Honeycomb uses to name the dataset, so
it should be a stable, human-readable identifier for the workload.

Example: `checkout`, `edge-gateway`, `cluster-prod`.

May reference an env var: `service_name: ${env:CONDUIT_SERVICE_NAME}`.
The deb / rpm / Helm / MSI installs all use this pattern by default.

## `deployment_environment` (required, string)

Populates the `deployment.environment` resource attribute. The OTel
spec recommends one of `development`, `staging`, `production`, but
operators are free to choose any string.

May reference an env var: `${env:CONDUIT_DEPLOYMENT_ENVIRONMENT}`.

## `output` (required, object)

Declares where the agent ships data. Exactly one of the nested
output blocks (`honeycomb` / `otlp` / `gateway`) must be populated,
and it must match `mode`.

### `output.mode` (required, enum)

| Value | When to use |
|---|---|
| `honeycomb` | Direct to Honeycomb's OTLP/HTTP ingest. The `x-honeycomb-team` header is pre-wired. The default for the shipped install scripts. |
| `otlp` | Generic OTLP/HTTP egress. Use for any vendor not yet wrapped as a named preset (Datadog, Grafana Cloud, SigNoz Cloud, AWS ADOT, in-cluster collectors, etc.). |
| `gateway` | OTLP/gRPC egress to a customer-operated gateway collector. The mental model is "fan out / aggregate at a gateway tier", not "send directly to a vendor". |

### `output.honeycomb` (when `mode: honeycomb`)

```yaml
output:
  mode: honeycomb
  honeycomb:
    api_key: ${env:HONEYCOMB_API_KEY}      # required
    endpoint: https://api.honeycomb.io     # optional, default shown
    traces:                                # optional, M10.B
      via_refinery:
        endpoint: https://refinery.observability.svc:4317
        insecure: false
```

| Field | Default | Notes |
|---|---|---|
| `api_key` | (required) | Honeycomb ingest key. May reference `${env:NAME}`. |
| `endpoint` | `https://api.honeycomb.io` | Set to `https://api.eu1.honeycomb.io` for EU tenants. |
| `traces.via_refinery.endpoint` | — | If set, traces route through Refinery; metrics + logs continue direct to Honeycomb. |
| `traces.via_refinery.insecure` | `false` | Skip TLS verification on the Refinery hop. Lab-only override; flagged by `conduit doctor` (CDT0103) even on success. |

### `output.otlp` (when `mode: otlp`)

```yaml
output:
  mode: otlp
  otlp:
    endpoint: https://otlp.us5.datadoghq.com   # required
    headers:
      DD-API-KEY: ${env:DD_API_KEY}            # required by the destination
    compression: gzip                          # optional, default shown
    insecure: false                            # optional, default shown
```

| Field | Default | Notes |
|---|---|---|
| `endpoint` | (required) | OTLP/HTTP base URL. Conduit appends `/v1/traces`, `/v1/metrics`, `/v1/logs` per the upstream `otlphttp` exporter convention. |
| `headers` | empty | Auth / routing headers required by the destination. May reference env vars. |
| `compression` | `gzip` | Set to `none` only if the destination explicitly rejects compressed payloads. |
| `insecure` | `false` | Skip TLS verification. Lab-only; flagged by `conduit doctor` (CDT0103). |

### `output.gateway` (when `mode: gateway`)

```yaml
output:
  mode: gateway
  gateway:
    endpoint: gateway.observability.svc:4317   # required
    headers:
      x-tenant-id: prod-edge                   # optional
    insecure: false                            # optional, default shown
```

| Field | Default | Notes |
|---|---|---|
| `endpoint` | (required) | OTLP/gRPC URL of the gateway. |
| `headers` | empty | Gateway-specific auth/routing. May reference env vars. |
| `insecure` | `false` | Skip TLS verification. Lab-only; flagged by `conduit doctor` (CDT0103). |

### `output.persistent_queue` (optional)

When enabled, OTLP batches that fail to deliver (network blip, 5xx)
are persisted to disk and replayed on agent restart instead of
being lost.

```yaml
output:
  persistent_queue:
    enabled: true
    dir: /var/lib/conduit/queue   # optional, default shown for Linux
```

| Field | Default | Notes |
|---|---|---|
| `enabled` | `false` | Off by default — the dir must be writable, persist across restarts, and not be on a tmpfs. |
| `dir` | `/var/lib/conduit/queue` | Validation rejects relative paths and known-tmpfs roots (`/tmp`, `/dev/shm`). On Windows the MSI sets `%PROGRAMDATA%\Conduit\queue`. |

The deb / rpm / MSI / Helm install all create the default directory
with the right ownership. On ephemeral container hosts (ECS Fargate)
there's no useful persistent dir; leave this off.

## `profile` (optional, object)

Selects which platform-default fragment set the expander layers on
top of the always-on OTLP receiver. When the block is omitted
entirely, defaults are applied as if you wrote
`{mode: auto, host_metrics: true, system_logs: true}`.

```yaml
profile:
  mode: auto              # auto | linux | darwin | docker | k8s | windows | none
  host_metrics: true      # default: true unless mode=none
  system_logs: true       # default: true unless mode=none
```

### `profile.mode`

| Value | What loads |
|---|---|
| `auto` | Detects `runtime.GOOS` at expansion time and picks `linux` / `darwin` / `windows`. Falls back to `none` with a warning when no fragment set is available. The default. |
| `linux` | hostmetrics + filelog (`/var/log/syslog`, `/var/log/messages`) + journald receiver. |
| `darwin` | hostmetrics + filelog + console-log fragments. |
| `docker` | OTLP bound to `0.0.0.0`; no hostmetrics fragment by default (the docker getting-started uses bind mounts and `--pid=host` to scrape the host's `/proc`). |
| `k8s` | OTLP bound to `0.0.0.0`; kubeletstats + filelog/k8s + k8sattributes. The Helm chart wires up the rest (RBAC, ServiceAccount, host bind mounts). |
| `windows` | hostmetrics with the Windows scrapers + windowseventlogreceiver (Application + System channels; Security is opt-in via `overrides:`). |
| `none` | OTLP-only; no platform defaults. Useful for sidecars that should only forward what apps explicitly send them. |

### `profile.host_metrics` (bool, optional)

Toggles the platform's hostmetrics fragment. `null` (omitted) means
"use the default for the resolved mode" — `true` for linux / darwin /
windows, `false` for none / docker. Set explicitly to override.

### `profile.system_logs` (bool, optional)

Toggles the platform's system-log fragment (filelog, journald,
windowseventlog). Same default rule as `host_metrics`.

## `metrics` (optional, object)

Configures Conduit's derived-metrics behavior. V0 ships exactly one
nested block (RED).

### `metrics.red`

Tunes the `span_metrics` connector that tees RED metrics
(request count, error count, duration histogram) off the traces
pipeline. Lives **before** any sampling step so derived metrics see
100% of traffic even if you tail-sample downstream.

```yaml
metrics:
  red:
    enabled: true                      # default: true
    span_dimensions:                   # appended to defaults
      - my.tenant_safe_attr
    extra_resource_dimensions:         # appended to defaults
      - my.region
    cardinality_limit: 5000            # default: 5000
```

#### Always-on default span dimensions

`deployment.environment`, `http.route`, `http.method`,
`http.status_code`, `rpc.system`, `rpc.service`, `rpc.method`,
`messaging.system`, `messaging.operation`.

(The connector also adds its built-in `service.name`, `span.name`,
`span.kind`, `status.code` regardless.)

#### Always-on default resource dimensions

`service.name`, `deployment.environment`, `k8s.namespace.name`,
`cloud.region`, `team`.

#### Cardinality denylist

These attribute names are **rejected at validation time** if added
to `span_dimensions` or `extra_resource_dimensions`:

| Name | Why it's rejected |
|---|---|
| `trace_id`, `span_id`, `request_id` | Per-request unique. |
| `user.id`, `customer_id`, `tenant_id` | Cardinality scales with user / customer / tenant count. |
| `url.full`, `http.url` | Includes query string + fragment. |
| `http.path`, `http.target` | Usually contains IDs. Use `http.route` (templated form) instead. |

The validator surfaces denylist hits as `CDT0501` warnings — see
[`docs/troubleshooting/cdt-codes.md`](../troubleshooting/cdt-codes.md#cdt0501-config-cardinality-warnings)
for the long-form explanation.

#### `cardinality_limit` (default 5000)

Total unique dimension-value combinations the connector tracks.
Excess combinations are dropped into a single overflow series tagged
`otel.metric.overflow="true"`. Maps directly to the upstream
`aggregation_cardinality_limit`.

## `overrides` (optional, map)

The documented escape hatch for advanced users who need OTel
Collector knobs Conduit hasn't surfaced as first-class fields. Any
key under `overrides` is spliced verbatim into the rendered
collector configuration as a second config source — the embedded
collector deep-merges base + overrides at startup, with overrides
winning where they overlap (and lists replacing rather than
concatenating, matching upstream multi-config semantics).

Heavy reliance on `overrides:` is a signal the schema is missing a
first-class knob. We review usage patterns at retro time and decide
whether to promote them to typed fields. See
[ADR-0012](../adr/adr-0012.md) for the design and review cadence.

### Example: bumping kubeletstats collection interval

```yaml
overrides:
  receivers:
    kubeletstats:
      collection_interval: 15s
```

### Example: adding the redaction processor to the logs pipeline

Because lists replace (not concatenate), you must restate the full
pipeline order:

```yaml
overrides:
  processors:
    redaction:
      allow_all_keys: true
      blocked_values:
        - '(?i)password=\S+'
  service:
    pipelines:
      logs:
        processors:
          - memory_limiter
          - resourcedetection
          - k8sattributes
          - resource
          - transform/logs
          - redaction
          - batch
```

`conduit doctor` warns when `overrides:` is non-empty (CDT0xxx
reserved) so operators know they're off the supported path.

## Environment-variable substitution

Any string field may reference an environment variable using OTel's
standard `${env:NAME}` syntax. The embedded collector resolves these
at startup. Common uses:

```yaml
service_name: ${env:CONDUIT_SERVICE_NAME}
deployment_environment: ${env:CONDUIT_DEPLOYMENT_ENVIRONMENT}
output:
  honeycomb:
    api_key: ${env:HONEYCOMB_API_KEY}
```

API keys should **always** come from env vars, not be inlined in
`conduit.yaml`. The `CDT0102 output.auth` doctor check passes the
env-placeholder form; it never logs the resolved value.

## CLI reference

The agent binary exposes three subcommands you'll touch day-to-day:

| Command | Purpose |
|---|---|
| `conduit run` | Run the embedded collector. Used by the systemd unit / Windows service / container `CMD`. |
| `conduit doctor` | Run the diagnostic checks. See [`docs/troubleshooting/cdt-codes.md`](../troubleshooting/cdt-codes.md) for what each check means. Add `--json` for machine-readable output. |
| `conduit preview` | Render `conduit.yaml` to the upstream collector YAML it produces, without starting the collector. Useful for code-review and for the [`golden-file regression suite`](../../internal/expander/testdata/goldens/README.md). |
| `conduit config --validate` | Run the config parser only and report problems. Equivalent to the `CDT0001` portion of `conduit doctor`. |

All commands accept `-c / --config <path>`. When omitted, they look
in the platform default location (table at the top of this page).

## Validation behavior

`conduit run` and `conduit doctor` both load the config through the
same parser. Validation errors carry a stable `CDT0001` code and a
field-path pointer:

```
CDT0001 config.syntax: output: when mode=otlp, output.otlp.endpoint is required
```

`conduit doctor` will continue running its other checks even when
`CDT0001` fails — you get all the structured signal in one pass
instead of having to fix and re-run.

## Schema stability

- **Additive changes** (a new optional field) are non-breaking and
  ship in any release.
- **Removing or renaming** a field is breaking and goes through ADR
  review (ADR-0014).
- The `overrides:` escape hatch is **not** stable — fields that work
  there today may need restating after collector upstream upgrades.
  See [`docs/release/compatibility.md`](../release/compatibility.md)
  for the support policy.
