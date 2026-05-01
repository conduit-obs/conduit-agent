# profiles/docker

The Docker profile (M4). Conduit's container deployment shape: the agent
runs as a peer container, accepts OTLP from other services on the same
docker network, and forwards to Honeycomb (or a customer gateway).

## What this directory ships in V0

**Nothing.** No `hostmetrics.yaml`, no `logs.yaml`. M4 only needs the
docker profile to do two things, and both are handled outside the
fragment loader:

1. **Bind OTLP receivers to `0.0.0.0`** so peer containers in the same
   compose / pod / network can reach the agent. Driven by
   `resolveOTLPBindAddress` in [`internal/expander/expander.go`](../../expander/expander.go);
   host-mode profiles stay on `127.0.0.1` so a stock `apt-get install`
   does not silently expose OTLP to the local network.
2. **Expose `health_check` on `0.0.0.0:13133`** for Docker / k8s
   liveness probes. That's universal — the `health_check` extension is
   wired into every Conduit deployment by
   [`internal/expander/templates/base.yaml.tmpl`](../../expander/templates/base.yaml.tmpl).

Everything else flows through the OTLP receiver: peer apps push spans,
metrics, and logs via OTLP to `conduit:4317` / `conduit:4318`, Conduit
adds the resource shaping + redaction + transform/logs normalization
the base template performs, and exports.

## Why no host metrics by default

Scraping CPU / memory / disk from inside a container only gives you the
container's own view, not the host's. Getting host-level metrics from a
containerized agent requires bind-mounting `/proc`, `/sys`, and
optionally `/etc/passwd` from the host, plus telling `hostmetricsreceiver`
where they live (`root_path: /hostfs`). That's a deployment-time choice
the operator must opt into — V0 won't make it for them.

The opt-in path:

1. Use a custom `conduit.yaml` (mount it at `/etc/conduit/conduit.yaml`)
   that sets `profile.mode: linux`. The Linux fragment ships in this
   repo at [`linux/hostmetrics.yaml`](../linux/hostmetrics.yaml).
2. In your compose / pod spec, bind-mount the host paths and set
   `HOST_PROC=/hostfs/proc HOST_SYS=/hostfs/sys` (the upstream
   `hostmetricsreceiver` reads those env vars to retarget its scrapers).
3. Run the conduit container with `pid: host` if you want process
   metrics that aren't filtered to the container's PID namespace.

The full M4 docker docs in [`deploy/docker/README.md`](../../../deploy/docker/README.md)
walk through this. M9 will pick a default and ship a tested
`docker/hostmetrics.yaml`.

## Profile contract status

[`PROFILE_SPEC.md`](../PROFILE_SPEC.md) §1 ("Telemetry the profile MUST
emit") applies to docker too, with one platform-specific carve-out:

| Section | M4 status |
|---|---|
| Resource attributes | `host.name`, `os.type`, etc. provided by `resourcedetectionprocessor` (universal). `container.id` / `container.name` come from incoming OTLP — peer apps' SDKs add them automatically. |
| Host metrics | **Deferred to M9.** Docker profile ships zero metrics scrapers in V0; everything in the metrics pipeline arrives via OTLP from peer apps. |
| Logs | **Deferred to M9.** No filelog scraper for container logs in V0; peer apps send logs via OTLP. M9 will decide whether to add a host-level `/var/lib/docker/containers/*/*.log` filelog or rely on the docker daemon's OTLP log driver. |

The dashboard quality bar (`PROFILE_SPEC.md` §3) applies to docker once
the data lands: a `dashboards/docker-host-overview.json` is now an M9
deliverable, designed to be a docker-native opinionated board (keyed off
`container.name` / `container.image.name`, narrative organized around
container-fleet questions an operator actually has) — not a copy of the
host-overview skeleton. The deferral is recorded in the
[milestone plan](../../../conduit-agent-plan/04-milestone-plan.md) §M4
(scope) and §M9 (deliverables).

## See also

- [`deploy/docker/Dockerfile`](../../../deploy/docker/Dockerfile) — the
  multi-stage build that produces the V0 image.
- [`deploy/docker/conduit.yaml.default`](../../../deploy/docker/conduit.yaml.default)
  — the in-image default config that sets `profile.mode: docker`.
- [`deploy/docker/compose-linux-host.yaml`](../../../deploy/docker/compose-linux-host.yaml)
  — runnable example.
- [`internal/expander/expander.go`](../../expander/expander.go) §`resolveOTLPBindAddress`
  for the bind-address rule.
