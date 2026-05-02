# CDT0xxx — Conduit doctor check IDs

Every check `conduit doctor` runs has a stable identifier of the form
`CDT0xxx`. The codes are an operator contract — they don't change once
shipped, so dashboards, runbooks, and ticket templates can hard-link to
them. New checks get new codes; behavior changes get a new revision of
the same code with a `-vN` suffix only when the meaning shifts (rare).

This page is the **canonical fix doc** every doctor result links into.
Each section follows the same shape:

- **What it checks** — the literal observation the check makes.
- **Why it matters** — the failure mode the operator should avoid.
- **How to fix** — concrete commands that resolve the typical cause.
- **When to escalate** — the rare corner cases where the operator
  needs to read source code or open a ticket.

## Code map

| Code | Title | Severity | Source |
|---|---|---|---|
| `CDT0001` | `config.syntax` | Fail | [`config_checks.go`](../../internal/doctor/config_checks.go) |
| `CDT0101` | `output.endpoint_reachable` | Fail | [`output_checks.go`](../../internal/doctor/output_checks.go) |
| `CDT0102` | `output.auth` | Fail | [`output_checks.go`](../../internal/doctor/output_checks.go) |
| `CDT0103` | `output.tls_warning` | Warn | [`output_checks.go`](../../internal/doctor/output_checks.go) |
| `CDT0201` | `receiver.ports` | Fail | [`receiver_checks.go`](../../internal/doctor/receiver_checks.go) |
| `CDT0202` | `receiver.permissions` | Fail | [`receiver_checks.go`](../../internal/doctor/receiver_checks.go) |
| `CDT0403` | `version.compat` | Pass (informational) | [`system_checks.go`](../../internal/doctor/system_checks.go) |
| `CDT0501` | `config.cardinality_warnings` | Fail | [`config_checks.go`](../../internal/doctor/config_checks.go) |

Reserved-but-not-yet-implemented in V0:

| Code | Title | Status | Where it lands |
|---|---|---|---|
| `CDT0301` | `k8s.permissions` | Reserved | M11 follow-up — needs k8s API client |
| `CDT0401` | `queue.health` | Reserved | M11 follow-up — needs running collector |
| `CDT0402` | `memory.pressure` | Reserved | M11 follow-up — needs running collector |
| `CDT0510` | `cardinality.observed` | Reserved | M11 follow-up — needs telemetry feedback loop |

---

## CDT0001 — config-syntax

**What it checks**: `conduit.yaml` decodes into a valid `AgentConfig`,
and the schema validator (`internal/config.Validate`) accepts every
field. Parse errors and validation errors both surface here.

**Why it matters**: Every other check assumes a well-formed config.
When CDT0001 fails, the rest of the catalog skips — focus on this
first.

**How to fix**: read the message and fix the named field. The doctor
emits one Result per validation issue, so a config with three problems
prints three CDT0001 lines.

Common cases:

- `service_name: required; non-empty string` — set `service_name:` at
  the top of the file. Use `${env:CONDUIT_SERVICE_NAME}` if the value
  comes from a deployment system.
- `output.honeycomb.api_key: required; non-empty string` — set the
  key inline or via `${env:HONEYCOMB_API_KEY}`. The agent never logs
  the key; doctor only checks that *some* value is present.
- `output.persistent_queue.dir: must be an absolute path` — the queue
  directory has to be a real, writable, on-disk path. Defaults to
  `/var/lib/conduit/queue` on Linux and `%PROGRAMDATA%\Conduit\queue`
  on Windows.

**When to escalate**: a YAML error the parser surfaces ("mapping values
are not allowed in this context") usually points at indentation. If
the message is unintelligible, run `conduit config --validate -c
PATH` for the same diagnostic with a different framing — sometimes the
two messages catch different concerns.

---

## CDT0101 — output-endpoint-reachable

**What it checks**: a TCP+TLS handshake against whichever endpoint
`output.mode` selects. For `mode: honeycomb` it dials the
`honeycomb.endpoint` URL (default `https://api.honeycomb.io`); for
`mode: gateway` it dials the gateway endpoint as host:port; for
`mode: otlp` it dials the explicit endpoint URL.

**Why it matters**: this catches firewall + DNS + corporate-CA
problems before the operator has to wait for telemetry to pile up in
the embedded queue. AC-05.4 / AC-06.4 both call out this check by
name.

**How to fix**: walk the network layers in order.

1. **DNS**: `dig +short api.honeycomb.io` or `nslookup` against the
   endpoint host. No answer = a network-team conversation.
2. **TCP**: `nc -vz api.honeycomb.io 443` (or 4317 for gateway). A
   refused connection points at egress firewall rules.
3. **TLS**: `openssl s_client -connect api.honeycomb.io:443 -servername
   api.honeycomb.io`. A "verify error" line names the trust-store
   problem — typically a corporate MITM proxy whose CA isn't in
   `/etc/ssl/certs`.
4. **Refinery routing (M10.B)**: doctor does NOT dial the Refinery
   endpoint because it's typically reachable only from inside the
   customer's cluster. Validate Refinery reachability from a pod in
   the same namespace: `kubectl exec -n observability refinery-0 -- nc
   -vz refinery.observability.svc 4317`.

**When to escalate**: TLS verify failures that survive a fresh CA
bundle (`apt-get install ca-certificates`) usually mean the agent's
runtime trust store is being overridden by a `SSL_CERT_FILE`
environment variable. Check `systemctl show conduit | grep
Environment=` to see what's actually set in the service unit.

---

## CDT0102 — output-auth

**What it checks**: every required auth value (Honeycomb API key,
OTLP headers, gateway headers) is non-empty. The check accepts both
literal strings and `${env:NAME}` placeholders — the embedded
collector resolves env vars at startup, so doctor never dereferences
them itself.

**Why it matters**: an empty `api_key:` produces a confusing
`HTTP 400` from Honeycomb that looks like a transient ingest failure.
Catching it at config time is cheaper than debugging from the agent
logs.

**How to fix**:

- Empty `output.honeycomb.api_key`: set it to your ingest key, or use
  `${env:HONEYCOMB_API_KEY}` and populate the variable in the systemd
  unit / k8s secret / Windows registry per the deploy recipe.
- Empty headers in `output.otlp.headers`: vendor docs will tell you
  which header carries auth. Common patterns:
  - Datadog: `DD-API-KEY: ${env:DD_API_KEY}`
  - Grafana Cloud: `Authorization: Bearer ${env:GC_OTLP_TOKEN}`
  - Honeycomb (when in OTLP mode for a non-default endpoint):
    `x-honeycomb-team: ${env:HONEYCOMB_API_KEY}`

**Important**: doctor never logs the key value. The PASS message
includes only the value's length so operators can distinguish "set to
the empty string" from "set to a real key" without exposing the secret
in shared terminal output.

---

## CDT0103 — output-tls-warning

**What it checks**: the rendered config has `tls.insecure: true` on
any egress exporter (`output.otlp.insecure`, `output.gateway.insecure`,
`output.honeycomb.traces.via_refinery.insecure`).

**Why it matters**: AC-06.3 mandates a doctor warning on insecure TLS
overrides **even when the connection succeeds**, so the warning
travels with the install through every pre-prod review. Lab
deployments running against a `localhost:4317` collector are the
intended use; production deployments should never see this warning.

**How to fix**: drop the `insecure: true` line and confirm the
destination is reachable over TLS (CDT0101 still passes). If the
destination is a customer-operated gateway behind a private CA, mount
the CA bundle into the agent's trust store rather than disabling
verification:

```yaml
# systemd: /etc/conduit/conduit.env
SSL_CERT_FILE=/etc/conduit/internal-ca.pem
```

```yaml
# k8s: helm values.yaml
extraEnv:
  - name: SSL_CERT_FILE
    value: /var/run/conduit/ca/internal-ca.pem
extraVolumes:
  - name: internal-ca
    secret:
      secretName: internal-ca
extraVolumeMounts:
  - name: internal-ca
    mountPath: /var/run/conduit/ca
    readOnly: true
```

**When to escalate**: only escalate if the customer's security policy
forbids loading custom CAs. ADR-0009 documents the trade-offs.

---

## CDT0201 — receiver-ports

**What it checks**: the OTLP gRPC (4317) and OTLP HTTP (4318) ports
are free to bind on `127.0.0.1` (host profiles) or `0.0.0.0` (docker /
k8s / windows profiles). The check probes the same address the
expander would render so the test matches what the embedded collector
actually attempts.

**Why it matters**: AC-14.3 — when the OTLP port is taken, doctor
must surface the conflicting PID. The classic case is a stray
collector left over from a previous deploy that systemd never
cleaned up.

**How to fix**:

- Linux: `lsof -nP -i :4317` to confirm the holder, then
  `systemctl stop <unit>` (or `kill <pid>` for an orphaned process).
- macOS / non-Linux: `lsof -nP -i :4317` works too; doctor's PID
  resolution is Linux-only because it parses `/proc/net/tcp`.
- Windows: `Get-NetTCPConnection -LocalPort 4317` from an admin
  PowerShell.

**When to escalate**: a port conflict that survives every kill is
usually a different OTel-Collector distribution that the host
package-manager installed alongside Conduit. Pick one to keep.

---

## CDT0202 — receiver-permissions

**What it checks**: every filelog include path declared in the
rendered profile fragments is openable by the agent process. The
check expands globs and stats each match; missing files (no glob
match yet) are non-fatal; unreadable files (EACCES) always fail.

**Why it matters**: filelog silently skips files it can't read, so
log gaps caused by permission errors are invisible until someone
notices the dashboard is missing data. Catching this at config time
makes the failure mode "agent doesn't start clean" rather than "logs
are mysteriously incomplete".

**How to fix**: add the agent's user to the file's group.

- Debian/Ubuntu: `usermod -aG adm conduit` (the `/var/log/syslog`
  group). Restart the agent so the new GID takes effect.
- RHEL/CentOS: `usermod -aG systemd-journal conduit` for
  journald-derived files.
- Containers: ensure the Helm chart / Compose file mounts `/var/log`
  read-only with the right `securityContext.fsGroup` — see
  [docs/deploy/aws/eks.md](../deploy/aws/eks.md).

**When to escalate**: filelog paths that don't match any file (the
"no matches yet" PASS variant) are usually fine — files appear after
the next log rotation. Escalate only when you expect a file at a
specific path and the matching glob is empty.

---

## CDT0403 — version-compat

**What it checks**: an informational PASS that prints the conduit
version, the embedded otelcol-core version, and the runtime
GOOS/GOARCH. Today there's only one upstream-core version per
Conduit release; the check exists as a stable anchor for the day a
support window matters.

**Why it matters**: when an operator opens a support ticket, this
check's output is the first thing the field engineer asks for. Having
it in the doctor report by default removes a round-trip.

**How to fix**: nothing to fix at V0 — the check is informational.
Future Conduit releases that span more than one upstream-core version
will introduce a min/max compatibility band; the check graduates to a
warn/fail at that point.

---

## CDT0501 — config-cardinality-warnings

**What it checks**: any RED dimension on the documented denylist —
`trace_id`, `span_id`, `request_id`, `user.id`, `customer_id`,
`tenant_id`, `url.full`, `http.url`, `http.path`, `http.target`. These
are blocked at parse time by the schema validator; CDT0501 surfaces
the same finding through doctor for symmetric tooling.

**Why it matters**: adding any of those attributes to
`metrics.red.span_dimensions` or
`metrics.red.extra_resource_dimensions` would tip the span_metrics
connector into per-request cardinality and blow out the dimension
budget at the destination. ADR-0006 documents the allowlist + denylist
model.

**How to fix**: pick a different dimension. The default set
(service.name, deployment.environment, http.{route,method,status_code},
rpc.{system,service,method}, messaging.{system,operation}) is what
Datadog / Honeycomb / Grafana Cloud users get on a service map without
lifting a finger. If you need tenant breakdowns for query-time
filtering, attach the tenant attribute as a span attribute (not a
dimension) and slice on it at read time.

**When to escalate**: if you need a denylisted attribute on a metric,
the right answer is usually "stop using a metric for this" — query
the underlying span data at read time instead. See ADR-0006 for the
broader cardinality model.

---

## Reading the JSON output

`conduit doctor --json` emits a single envelope with the following
shape (sorted by `id` so consumers can `jq` deterministically):

```json
{
  "generator": "conduit doctor",
  "generated": "2026-05-02T05:04:51Z",
  "config_path": "/etc/conduit/conduit.yaml",
  "results": [
    {
      "id": "CDT0001",
      "title": "config.syntax",
      "severity": "pass",
      "message": "conduit.yaml at /etc/conduit/conduit.yaml parses cleanly and passes schema validation.",
      "docs_url": "https://github.com/conduit-obs/conduit-agent/blob/main/docs/troubleshooting/cdt-codes.md#cdt0001-config-syntax"
    }
  ]
}
```

Useful jq snippets:

```bash
# Bail out of CI if any check failed:
conduit doctor --json | jq -e '[.results[] | select(.severity == "fail")] | length == 0'

# Dump just the failing check titles + messages:
conduit doctor --json | jq -r '.results[] | select(.severity == "fail") | "\(.title): \(.message)"'

# Extract the docs URLs for everything that's not PASS, for a runbook
# template:
conduit doctor --json | jq -r '.results[] | select(.severity != "pass") | "- \(.id) \(.title): \(.docs_url)"'
```

`severity` is one of `pass`, `skip`, `warn`, `fail`. The exit code is
non-zero **only** when at least one result has `severity: "fail"` —
warnings and skips never block.
