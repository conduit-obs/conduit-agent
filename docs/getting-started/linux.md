# Getting started — Linux

**Time to first signal**: ~15 minutes on a fresh host. This guide
takes you from "I just provisioned an Ubuntu / RHEL / Amazon Linux
box" to "host metrics + system logs + traces are landing in
Honeycomb."

## What you'll have at the end

- A `conduit` systemd service running on the host.
- Host metrics (CPU, memory, filesystem, network) flowing to Honeycomb
  every 60 seconds.
- System logs (`journalctl` + `/var/log/messages` + `/var/log/syslog`)
  flowing to Honeycomb in near-real-time, with credentials redacted by
  default (M9.B).
- An OTLP receiver on `127.0.0.1:4317` (gRPC) and `127.0.0.1:4318`
  (HTTP) ready to accept traces / metrics / logs from your apps.
- `conduit doctor` reports green.

## Prerequisites (5 min)

| Item | Where to get it |
|---|---|
| A Linux host: Ubuntu 22.04+, RHEL 9+, Amazon Linux 2023, Debian 12+, Arch | Any cloud provider or local VM |
| `sudo` on the host | — |
| A Honeycomb ingest API key | [honeycomb.io](https://www.honeycomb.io) → Environment → API Keys → Create |
| `curl` (every distro ships it; double-check with `which curl`) | — |

If you'll be sending data from apps to the OTLP receiver, also have
your service's name, language, and SDK ready — but you can deploy
Conduit first and wire apps later.

## Step 1 — Install (5 min)

The one-liner installer downloads the right `.deb` / `.rpm` / `.apk`
for your distro and architecture, installs it, seeds
`/etc/conduit/conduit.env`, and `systemctl enable --now conduit`s:

```sh
curl -fsSL https://raw.githubusercontent.com/conduit-obs/conduit-agent/main/scripts/install_linux.sh \
  | sudo bash -s -- \
    --api-key="$HONEYCOMB_API_KEY" \
    --service-name=edge-gateway \
    --deployment-environment=production
```

That's it. The installer:

1. Detects your distro and architecture.
2. Installs the correct package via `apt-get`, `dnf`, `apk`, or
   `pacman`.
3. Creates the `conduit:conduit` system user (added to `adm` and
   `systemd-journal` so filelog can read `/var/log/syslog` and the
   journald receiver can read journal entries).
4. Writes `/etc/conduit/conduit.yaml` (the default config) and
   `/etc/conduit/conduit.env` (your API key + service name + deployment
   environment).
5. `systemctl enable --now conduit` so the service starts and survives
   reboots.

Re-running the installer is safe — it upgrades in place.

### Manual install

If your environment doesn't allow piping `curl` into `bash`, grab the
release artifact directly:

```sh
# Replace with the latest release tag.
VERSION=v0.x.y

# Pick the right package for your distro:
#   .deb  for Ubuntu / Debian
#   .rpm  for RHEL / Amazon Linux / Rocky / Fedora
#   .apk  for Alpine
#   .pkg.tar.zst  for Arch
curl -fsSLO "https://github.com/conduit-obs/conduit-agent/releases/download/${VERSION}/conduit_${VERSION#v}_linux_amd64.deb"
sudo dpkg -i "conduit_${VERSION#v}_linux_amd64.deb"

# Seed the env file:
sudo tee /etc/conduit/conduit.env > /dev/null <<EOF
HONEYCOMB_API_KEY=$HONEYCOMB_API_KEY
CONDUIT_SERVICE_NAME=edge-gateway
CONDUIT_DEPLOYMENT_ENVIRONMENT=production
EOF
sudo chown root:conduit /etc/conduit/conduit.env
sudo chmod 0640 /etc/conduit/conduit.env

sudo systemctl enable --now conduit
```

For the manual-install reference and packaging internals, see
[`deploy/linux/README.md`](../../deploy/linux/README.md).

## Step 2 — Verify (5 min)

Confirm the agent is healthy:

```sh
sudo systemctl status conduit
```

You should see `Active: active (running)`. If you see `failed`, jump
to [Troubleshooting](#troubleshooting) below.

Run the doctor:

```sh
sudo /usr/bin/conduit doctor -c /etc/conduit/conduit.yaml
```

You should see eight or so PASS lines and the summary
`0 failure(s), 0 warning(s), N passed, 0 skipped`. If anything fails,
each line carries a `CDT0xxx` code that links to
[`docs/troubleshooting/cdt-codes.md`](../troubleshooting/cdt-codes.md)
with the fix.

Tail the agent's own logs (it logs to journald on Linux):

```sh
sudo journalctl -u conduit -n 50 --no-pager
```

Look for lines like `Everything is ready. Begin running and processing
data.` from the embedded collector. The first batch of telemetry
ships within ~60 seconds.

## Step 3 — Confirm data in Honeycomb (5 min)

In Honeycomb, switch to the environment whose API key you used and
look for a new dataset named after your `service_name` (here:
`edge-gateway`). Within ~1 minute you should see:

| Where to look | What you'll see |
|---|---|
| **Datasets list** | A new entry: `edge-gateway` |
| **Query** → group by `host.name` | One row per host running Conduit |
| **Query** → metric: `system.cpu.utilization` | A time series of CPU usage |
| **Query** → group by `host.name`, attribute `severity_text` | Log-level distribution |

If your service name is generic ("default" / "unknown_service"), the
agent didn't pick up `CONDUIT_SERVICE_NAME` from `/etc/conduit/conduit.env`.
Re-check Step 1.

## Step 4 — Send traces from your app

The OTLP receiver is bound to `127.0.0.1:4317` (gRPC) and
`127.0.0.1:4318` (HTTP). Point your app's OTel SDK at
`http://127.0.0.1:4318` (HTTP/protobuf) or `127.0.0.1:4317` (gRPC) —
no headers needed (the agent injects the Honeycomb API key on egress).

Example with the Python OTel SDK:

```python
# requirements.txt
# opentelemetry-api
# opentelemetry-sdk
# opentelemetry-exporter-otlp-proto-http

from opentelemetry import trace
from opentelemetry.sdk.resources import Resource
from opentelemetry.sdk.trace import TracerProvider
from opentelemetry.sdk.trace.export import BatchSpanProcessor
from opentelemetry.exporter.otlp.proto.http.trace_exporter import OTLPSpanExporter

provider = TracerProvider(resource=Resource.create({"service.name": "checkout"}))
provider.add_span_processor(BatchSpanProcessor(
    OTLPSpanExporter(endpoint="http://127.0.0.1:4318/v1/traces")
))
trace.set_tracer_provider(provider)

with trace.get_tracer(__name__).start_as_current_span("hello"):
    print("sent a span")
```

Within ~10 seconds the span lands in Honeycomb's `checkout` dataset.
RED metrics (request count, error count, duration histogram) are
derived automatically — see [the architecture overview](../architecture/overview.md)
for how the `span_metrics` connector tees off the traces pipeline.

> **Optional: zero-code instrumentation for services without an OTel
> SDK.** Re-run the installer with `--with-obi` and add `obi: {
> enabled: true }` to `/etc/conduit/conduit.yaml` to capture HTTP /
> gRPC / database RED metrics and traces from every process on the
> host without code changes. See the [OBI guide](obi.md) for the
> full walkthrough.

## Step 5 — Switch output mode (optional)

Conduit supports three output modes:

- `output.mode: honeycomb` — direct to Honeycomb (the default).
- `output.mode: gateway` — to a customer-operated OTLP/gRPC gateway.
- `output.mode: otlp` — to any OTLP/HTTP destination (Datadog, Grafana
  Cloud, SigNoz, AWS ADOT, etc.).

Switching is one config field plus one restart:

```yaml
# /etc/conduit/conduit.yaml
output:
  mode: gateway
  gateway:
    endpoint: gateway.observability.svc:4317
```

```sh
sudo systemctl restart conduit
sudo /usr/bin/conduit doctor -c /etc/conduit/conduit.yaml
```

For the full output-mode contract — Refinery routing, persistent queue,
TLS-required-by-default, EU endpoints — see
[the configuration reference](../reference/configuration.md#output).

## Troubleshooting

### "active (running)" but `conduit doctor` fails

Read the CDT code. The most common findings:

- **CDT0001 config.syntax**: a typo in `/etc/conduit/conduit.yaml`.
  Run `sudo /usr/bin/conduit config --validate -c /etc/conduit/conduit.yaml`
  for a structured diff.
- **CDT0102 output.auth**: empty API key. Check
  `/etc/conduit/conduit.env` for `HONEYCOMB_API_KEY=…`.
- **CDT0101 output.endpoint_reachable**: TCP/TLS handshake failed.
  Walk the [CDT0101 fix doc](../troubleshooting/cdt-codes.md#cdt0101-output-endpoint-reachable)
  for DNS / TCP / TLS / corporate-CA debugging.
- **CDT0202 receiver.permissions**: filelog can't read a path. Add
  the agent user to `adm` (Debian/Ubuntu) or `systemd-journal`
  (RHEL): `sudo usermod -aG adm conduit && sudo systemctl restart conduit`.

### "failed" — service won't start

```sh
sudo journalctl -u conduit -n 200 --no-pager
```

The first ~50 lines after the most recent restart usually point at the
cause. The structured agent logs all carry a stable `CDT0xxx` code so
you can grep:

```sh
sudo journalctl -u conduit | grep -E 'CDT[0-9]{4}'
```

### No data in Honeycomb after 5 minutes

- Confirm the dataset name matches `CONDUIT_SERVICE_NAME` in the env
  file. If it doesn't, the env file isn't being loaded — check
  `sudo systemctl cat conduit | grep EnvironmentFile`.
- Confirm `output.endpoint_reachable` passes (see above).
- Check the agent's debug-exporter output for batches actually going
  out: `sudo journalctl -u conduit | grep TracesExporter | tail`.

## Next steps

- [**Configuration reference**](../reference/configuration.md) — the
  full `conduit.yaml` schema with examples.
- [**Architecture overview**](../architecture/overview.md) — what's
  actually running inside the agent.
- [**AWS deployment recipes**](../deploy/aws/README.md) — for EC2,
  ECS, EKS specifics.
- [**Troubleshooting index**](../troubleshooting/index.md) — every
  CDT code, every common failure mode.
