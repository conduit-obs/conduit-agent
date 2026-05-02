# Getting started — Docker

**Time to first signal**: ~10 minutes. This guide takes you from
"`docker` is installed" to "host metrics + peer-container traces are
landing in Honeycomb."

## What you'll have at the end

- A `conduit` container running on the Docker host.
- Host metrics (CPU, memory, filesystem, network) reading the host's
  `/proc` + `/sys` via bind mounts and shipping to Honeycomb.
- An OTLP receiver bound to `0.0.0.0:4317` (gRPC) and `0.0.0.0:4318`
  (HTTP) inside the container, published to `127.0.0.1` on the host.
- Peer containers on the same Docker network can send OTLP to
  `conduit:4317` / `conduit:4318` by service name.
- `conduit doctor` reports green inside the container.

## Prerequisites (2 min)

| Item | Where to get it |
|---|---|
| Docker Engine 20.10+ | [docs.docker.com](https://docs.docker.com/engine/install/) |
| Docker Compose v2 | shipped with modern Docker Desktop / `docker compose` plugin |
| A Honeycomb ingest API key | [honeycomb.io](https://www.honeycomb.io) → API Keys |

The recipe below uses the `compose-linux-host.yaml` file shipped in
the repo. It bind-mounts `/proc`, `/sys`, and `/` into the container
so the `hostmetrics` receiver can read the **host's** state instead
of the container's. Without those mounts you'd see container
metrics, not host metrics.

## Step 1 — Pull the image (1 min)

```sh
docker pull ghcr.io/conduit-obs/conduit-agent:latest
```

The image is `gcr.io/distroless/static-debian12:nonroot` (UID 65532,
no shell) with the conduit binary statically linked. It's multi-arch
— Docker pulls `amd64` on x86_64 hosts and `arm64` on Graviton /
Apple Silicon.

For a development image built from source:

```sh
git clone https://github.com/conduit-obs/conduit-agent
cd conduit-agent
docker build -t ghcr.io/conduit-obs/conduit-agent:dev -f deploy/docker/Dockerfile .
```

## Step 2 — Run with compose (5 min)

The `deploy/docker/compose-linux-host.yaml` file is the reference
recipe — it sets `pid: host` and bind-mounts `/proc`, `/sys`, `/`
into `/hostfs` so the `hostmetrics` receiver sees the host's process
tree:

```sh
git clone https://github.com/conduit-obs/conduit-agent
cd conduit-agent
HONEYCOMB_API_KEY=hcaik_xxx \
CONDUIT_SERVICE_NAME=docker-host \
CONDUIT_DEPLOYMENT_ENVIRONMENT=production \
  docker compose -f deploy/docker/compose-linux-host.yaml up -d
```

The compose file:

- Reads `HONEYCOMB_API_KEY` / `CONDUIT_SERVICE_NAME` /
  `CONDUIT_DEPLOYMENT_ENVIRONMENT` from your shell.
- Publishes OTLP `4317` and `4318` to `127.0.0.1` on the host
  (LAN-wide ingest is an opt-in — change the bind address on the
  `ports:` mappings to `0.0.0.0` if you want it).
- Mounts the host's filesystem into the container at `/hostfs` so the
  Docker profile fragment ([`internal/profiles/docker/hostmetrics.yaml`](../../internal/profiles/docker/hostmetrics.yaml))
  can do `root_path: /hostfs` and read the right `/proc`, `/sys`,
  filesystem usage, etc.

For the in-image default config, see
[`deploy/docker/conduit.yaml.default`](../../deploy/docker/conduit.yaml.default)
— it sets `profile.mode: docker` so the expander binds OTLP to
`0.0.0.0`.

### Standalone `docker run`

If you don't want compose:

```sh
docker run -d \
  --name conduit \
  --pid=host \
  --restart=unless-stopped \
  -e HONEYCOMB_API_KEY="$HONEYCOMB_API_KEY" \
  -e CONDUIT_SERVICE_NAME=docker-host \
  -e CONDUIT_DEPLOYMENT_ENVIRONMENT=production \
  -p 127.0.0.1:4317:4317 \
  -p 127.0.0.1:4318:4318 \
  -v /proc:/hostfs/proc:ro \
  -v /sys:/hostfs/sys:ro \
  -v /:/hostfs:ro \
  ghcr.io/conduit-obs/conduit-agent:latest
```

The `--pid=host` flag is what makes the `processes` scraper see the
host's processes instead of just the container's PID 1.

## Step 3 — Verify (2 min)

Confirm the container is healthy:

```sh
docker ps --filter name=conduit
docker logs --tail 50 conduit
```

You should see the embedded collector's "Everything is ready. Begin
running and processing data." line. The image's `HEALTHCHECK` hits
the `health_check` extension on `:13133/`, so `STATUS` should report
`healthy`.

Run the doctor inside the container:

```sh
docker exec conduit /conduit doctor -c /etc/conduit/conduit.yaml
```

You should see a clean run. The `receiver.permissions` check is a
no-op in the docker profile (no filelog fragments), and
`receiver.ports` confirms `0.0.0.0:4317` / `:4318` are bound.

## Step 4 — Confirm data in Honeycomb (1 min)

In Honeycomb, open the `docker-host` dataset (or whatever
`CONDUIT_SERVICE_NAME` you set):

| Where to look | What you'll see |
|---|---|
| **Datasets list** | A new entry: `docker-host` |
| **Query** → metric: `system.cpu.utilization`, group by `host.name` | One row per Docker host |
| **Query** → metric: `system.filesystem.utilization`, filter `mountpoint != /hostfs/proc` | Real filesystem-usage time series |

Within ~1 minute. If nothing shows up, see
[Troubleshooting](#troubleshooting) below.

## Step 5 — Send traces from peer containers

Run any container on the same Docker network and point its OTel SDK
at `http://conduit:4318` (HTTP) or `conduit:4317` (gRPC):

```sh
docker network connect conduit_default <your-app-container>
```

Then in your app:

```yaml
# environment for an OTel-instrumented container
OTEL_EXPORTER_OTLP_ENDPOINT: http://conduit:4318
OTEL_RESOURCE_ATTRIBUTES: service.name=checkout,deployment.environment=production
```

The agent forwards everything to Honeycomb with the API key already
attached — peer apps never see the key.

## Troubleshooting

### Host metrics show containers, not the host

Most common cause: `--pid=host` not set, or the bind mounts are
missing. The `hostmetrics` receiver's `processes` scraper uses
`/proc` to enumerate processes; without `--pid=host`, that's the
container's PID namespace.

Re-create with the compose file or the explicit `docker run`
recipe above; both set `pid: host` and the right mounts.

### `conduit doctor` reports `output.endpoint_reachable` failure

The container has its own networking stack — confirm it can reach
Honeycomb:

```sh
docker exec conduit /conduit doctor --check output -c /etc/conduit/conduit.yaml
```

If TCP fails, your Docker host's egress is firewalled. If TLS fails,
the issue is usually a corporate MITM proxy whose CA isn't in the
container's trust bundle. The distroless base ships standard
Mozilla CAs; for internal CAs, mount your bundle and set
`SSL_CERT_FILE`:

```sh
docker run -d \
  ... \
  -v /etc/ssl/certs/internal-ca.pem:/etc/ssl/certs/internal-ca.pem:ro \
  -e SSL_CERT_FILE=/etc/ssl/certs/internal-ca.pem \
  ghcr.io/conduit-obs/conduit-agent:latest
```

### Container restarts in a loop

```sh
docker logs conduit | tail -100
```

If you see `validation error`, your env vars aren't being substituted
into the in-image config. The default config references
`${env:HONEYCOMB_API_KEY}` — confirm the env var is set in the
container with `docker exec conduit env | grep HONEYCOMB`.

## Next steps

- [**Kubernetes**](kubernetes.md) — for fleet deployments.
- [**AWS ECS recipe**](../deploy/aws/ecs.md) — sidecar pattern for
  Fargate / ECS-on-EC2.
- [**Configuration reference**](../reference/configuration.md) — the
  full `conduit.yaml` schema.
- [**Troubleshooting index**](../troubleshooting/index.md) — every
  CDT code with fix steps.
