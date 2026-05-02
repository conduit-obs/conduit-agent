# profiles/docker

The Docker profile (M4 + M9.A). Conduit's container deployment shape: the
agent runs as a peer container, accepts OTLP from other services on the
same docker network, and forwards to Honeycomb (or a customer gateway).

## Fragments shipped

| File | Concern | Loaded when |
|---|---|---|
| [`hostmetrics.yaml`](hostmetrics.yaml) | Per-host CPU / memory / disk / filesystem / network / paging / processes scraping, re-rooted at `/hostfs` so the operator's bind mount is the contract. Mirrors the k8s profile shape so dashboards keyed on `system.*` columns work identically across host / container / k8s. | `profile.mode: docker` and `profile.host_metrics` is unset or `true`. |

What this profile **does not** ship in V0:

- **`logs.yaml`**: container logs flow via OTLP from peer apps (every modern SDK can ship logs over OTLP). A future M9.E may add an on-host filelog scrape of `/var/lib/docker/containers/*/*.log` for operators who want to capture logs from instrumented-but-not-yet-OTLP apps; that recipe needs the same bind-mount + label-mapping work the k8s container-log fragment already does.

What the profile does outside the fragment loader:

1. **Bind OTLP receivers to `0.0.0.0`** so peer containers in the same compose / pod / network can reach the agent. Driven by `resolveOTLPBindAddress` in [`internal/expander/expander.go`](../../expander/expander.go); host-mode profiles stay on `127.0.0.1` so a stock `apt-get install` does not silently expose OTLP to the local network.
2. **Expose `health_check` on `0.0.0.0:13133`** for Docker / k8s liveness probes. That's universal — the `health_check` extension is wired into every Conduit deployment by [`internal/expander/templates/base.yaml.tmpl`](../../expander/templates/base.yaml.tmpl).

## Required bind mounts (the contract)

`hostmetrics.yaml` sets `root_path: /hostfs`, which means every scraper resolves `/proc`, `/sys`, and any filesystem path it walks under that prefix. The matching compose snippet is:

```yaml
services:
  conduit:
    pid: host                                # unlock processes scraper
    volumes:
      - /proc:/hostfs/proc:ro,rslave
      - /sys:/hostfs/sys:ro,rslave
      - /:/hostfs/:ro,rslave                 # filesystem scraper mountpoints
    environment:
      - HOST_PROC_MOUNTINFO=/hostfs/proc/self/mountinfo
```

`pid: host` is what the `processes` scraper needs — without it, the container's PID namespace masks every PID outside its own. `HOST_PROC_MOUNTINFO` tells the filesystem scraper which mountinfo file lists the host's mountpoints (rather than the container's).

Operators who want OTLP-only on docker (no host scraping) set `profile.host_metrics: false`; the bind mounts become irrelevant.

The reference compose example with the full bind-mount recipe is at [`deploy/docker/compose-linux-host.yaml`](../../../deploy/docker/compose-linux-host.yaml).

## Profile contract status

[`PROFILE_SPEC.md`](../PROFILE_SPEC.md) §1 ("Telemetry the profile MUST emit") status:

| Section | M9.A status |
|---|---|
| Resource attributes | `host.name`, `os.type`, etc. provided by `resourcedetectionprocessor` (universal). `container.id` / `container.name` come from incoming OTLP — peer apps' SDKs add them automatically. |
| Host metrics | **Done.** `system.cpu.{usage,utilization}`, `system.memory.{usage,utilization}`, `system.filesystem.{usage,utilization}`, `system.disk.io`, `system.network.io`, `system.load.*`, `system.paging.{usage,utilization}`, `system.processes.*` all emitted via the `hostmetrics.yaml` fragment when bind mounts are in place. |
| Logs | **Deferred to M9.E.** Peer apps push logs via OTLP. |

The dashboard quality bar (`PROFILE_SPEC.md` §3) ships at M9.D as [`dashboards/docker-host-overview.json`](../../../dashboards/docker-host-overview.json) — a docker-native opinionated board (keyed off `host.name` + the standard `system.*` metric vocabulary) that is intentionally a peer of the linux board, not a copy.

## See also

- [`deploy/docker/Dockerfile`](../../../deploy/docker/Dockerfile) — the multi-stage build that produces the V0 image.
- [`deploy/docker/conduit.yaml.default`](../../../deploy/docker/conduit.yaml.default) — the in-image default config that sets `profile.mode: docker`.
- [`deploy/docker/compose-linux-host.yaml`](../../../deploy/docker/compose-linux-host.yaml) — runnable example with the full host-bind-mount recipe.
- [`internal/expander/expander.go`](../../expander/expander.go) §`resolveOTLPBindAddress` for the bind-address rule.
