# Common issues

Symptom-driven guide. Each section starts with what you're observing,
points at the most likely cause, and walks the fix. For a code-driven
view, see [`cdt-codes.md`](cdt-codes.md). For a quick command index,
see [`index.md`](index.md).

## "I don't see any data in Honeycomb"

Five things to check, in order. Stop at the first one that points
at the cause.

### 1. Is the agent actually running?

| Platform | Command |
|---|---|
| Linux | `sudo systemctl status conduit` (expect `active (running)`) |
| Docker | `docker ps --filter name=conduit` (expect `Up X minutes (healthy)`) |
| Kubernetes | `kubectl -n conduit get pods` (expect every pod `Running`) |
| Windows | `Get-Service Conduit` (expect `Status: Running`) |

If it's not running, that's the issue — jump to ["Agent won't start"](#agent-wont-start)
below.

### 2. Does `conduit doctor` pass?

```sh
sudo /usr/bin/conduit doctor -c /etc/conduit/conduit.yaml
```

The doctor's output exit code is non-zero if any check failed. Each
failed line has a `CDT0xxx` code linking to
[`cdt-codes.md`](cdt-codes.md) with the exact fix. The most common
cases for "no data":

- **CDT0102 output.auth**: empty / wrong API key. Check the env
  file (Linux: `/etc/conduit/conduit.env`; Docker: env on the
  container; k8s: the Secret + ConfigMap; Windows: the per-service
  registry `Environment`).
- **CDT0101 output.endpoint_reachable**: TCP/TLS handshake failed.
  Walk the [CDT0101 fix doc](cdt-codes.md#cdt0101--output-endpoint-reachable);
  it covers DNS, TCP, TLS, and corporate-CA cases.

### 3. Are batches actually leaving the agent?

```sh
# Linux
sudo journalctl -u conduit | grep -E 'TracesExporter|MetricsExporter|LogsExporter' | tail
# Docker
docker logs conduit | grep -E 'TracesExporter|MetricsExporter|LogsExporter' | tail
# Kubernetes
kubectl -n conduit logs -l app.kubernetes.io/name=conduit-agent --tail=200 | grep Exporter
```

You should see lines like `{"items": 42}` showing batches with
non-zero counts going out. If counts are zero, the receivers aren't
producing anything — jump to ["No metrics" / "No traces" / "No logs"](#no-metrics--no-traces--no-logs)
below.

### 4. Are you looking at the right environment / dataset?

In Honeycomb, the dataset name comes from `service_name` (the OTel
SDK's `service.name` attribute, or `conduit.serviceName` in the Helm
chart). If your `conduit.yaml` has
`service_name: ${env:CONDUIT_SERVICE_NAME}` and the env var isn't
set, the dataset will be named `unknown_service` or `default` — a
common surprise after a bad install.

```sh
# Linux: confirm the env file is being loaded
sudo systemctl cat conduit | grep EnvironmentFile
sudo cat /etc/conduit/conduit.env

# k8s: confirm the Secret was rendered
kubectl -n conduit get secret conduit-conduit-agent -o jsonpath='{.data}' | jq
```

### 5. Are you in the right Honeycomb environment?

Honeycomb has multiple "environments" per team, each with its own
API key. If you copied a key from the wrong environment, the data
lands somewhere — just not where you're looking. Compare the API
key in `conduit.env` (or the registry / Secret) with the key shown
in the Honeycomb UI under the dataset.

## Agent won't start

### Linux

```sh
sudo journalctl -u conduit -n 200 --no-pager
```

The first ~50 lines after the most recent restart point at the
cause. The most common patterns:

| Error | Likely cause | Fix |
|---|---|---|
| `validation error: …` | malformed `conduit.yaml` | Run `conduit config --validate -c /etc/conduit/conduit.yaml` |
| `bind: address already in use` | another collector is on 4317 / 4318 | `sudo /usr/bin/conduit doctor --check receiver -c /etc/conduit/conduit.yaml` will print the conflicting PID (Linux) |
| `Failed to load config: open /etc/conduit/conduit.yaml: permission denied` | mode/owner drift | `sudo chown root:conduit /etc/conduit/conduit.yaml && sudo chmod 0640 /etc/conduit/conduit.yaml` |
| `permission denied` against `/var/log/...` | filelog can't read the path | `sudo usermod -aG adm conduit && sudo systemctl restart conduit` |

### Docker

```sh
docker logs --tail 100 conduit
```

Same patterns as Linux. The most-common-extra is the agent failing
because the env vars aren't substituted into the in-image config:
`docker exec conduit env | grep HONEYCOMB` should show the key.

### Kubernetes

```sh
POD=$(kubectl -n conduit get pod -l app.kubernetes.io/name=conduit-agent -o name | head -1)
kubectl -n conduit describe $POD                # Events at the bottom
kubectl -n conduit logs $POD --previous --tail=200
```

If `Events` shows `CrashLoopBackOff`, the previous pod's logs hold
the cause. If it's hanging in `ContainerCreating` past 30 seconds,
the most common reasons are RBAC drift (the ClusterRoleBinding got
deleted) or a host-bind-mount path that doesn't exist (older nodes).

### Windows

```powershell
Get-EventLog -LogName Application -Source "Conduit" -Newest 20 |
  Format-List TimeGenerated, EntryType, Message
```

The service writes its own startup / shutdown events here. Most
common: the per-service registry `Environment` wasn't written by
the install script (re-run the script with `-ApiKey "..."`), or
SmartScreen blocked an unsigned MSI install.

## No metrics / no traces / no logs

### Host metrics missing

The hostmetrics receiver depends on the resolved profile. If you
see no `system.*` metrics:

```sh
sudo /usr/bin/conduit preview -c /etc/conduit/conduit.yaml | grep -A20 hostmetrics
```

If the section is missing, check `profile.host_metrics` (default
`true` unless `profile.mode: none`). On Docker, host metrics need
the bind-mount + `--pid=host` recipe in
[`docs/getting-started/docker.md`](../getting-started/docker.md).

### Container logs missing (k8s)

The k8s profile mounts `/var/log/pods/`. If logs are missing:

```sh
kubectl -n conduit exec deploy/conduit-conduit-agent -- ls /var/log/pods/ | head
```

Empty result → the bind-mount isn't reaching the host's pod-log
directory. Re-install the chart with default `daemonset.hostPaths`.

### Traces from app pods missing

Confirm the app pod can reach the agent's Service:

```sh
kubectl -n <your-app-ns> exec deploy/<your-app> -- sh -c \
  'getent hosts conduit-conduit-agent.conduit.svc; nc -vz conduit-conduit-agent.conduit.svc 4318'
```

If `getent` fails, your CoreDNS isn't resolving the service. If
`nc` fails, you have a NetworkPolicy blocking egress — see the
[Kubernetes guide's NetworkPolicy section](../getting-started/kubernetes.md#app-pods-get-connection-refused-on-conduit-agentconduitsvc4318).

## High memory / OOMKilled

`memory_limiter` is the first processor in every pipeline; it
backpressures the receivers when memory grows. If the agent is
still OOMKilled despite that, the most common causes are:

1. **Persistent queue dir is on tmpfs**. Validation rejects `/tmp`
   and `/dev/shm`, but other tmpfs mounts slip through. Check
   `df -T /var/lib/conduit/queue` (or wherever your `dir:` points)
   for the FS type.
2. **A non-default RED dimension is high-cardinality**. The
   `cardinality_limit` (default 5000) caps the connector but the
   in-flight working set during overflow can still be hot. Look at
   the `otel.metric.overflow="true"` series in Honeycomb — if it's
   non-zero, you have a cardinality problem.
3. **A receiver is unbounded** — kubeletstats with a huge cluster,
   filelog without a `start_at: end`. These are runtime issues,
   not config issues; the M11 follow-up `CDT0402 memory.pressure`
   check will surface them once it ships.

For now, the workaround is the
[`overrides:`](../reference/configuration.md#overrides-optional-map)
escape hatch — bump `memory_limiter`'s `limit_mib` (default 1500
MiB on amd64) for high-traffic deployments:

```yaml
overrides:
  processors:
    memory_limiter:
      limit_mib: 4096
      spike_limit_mib: 512
```

## "Why is `conduit preview` output different from `service.collector.yaml`?"

`conduit preview` always renders fresh from `conduit.yaml` — it
doesn't read the cached collector YAML the embedded collector built
at startup. If they differ:

1. The agent hasn't been restarted since the last `conduit.yaml`
   edit. `sudo systemctl restart conduit` (or equivalent).
2. You're on different binaries. Confirm with
   `conduit doctor --check version` — `CDT0403` reports the
   binary's collector-core version. If two hosts show different
   versions, one's lagging on its package upgrade.

## Persistent queue replays old data after a restart

This is by design: the on-disk queue is the whole reason
`output.persistent_queue.enabled: true` exists. If you're seeing
duplicated traces or stale timestamps after a deploy, the queue is
draining what was buffered. It catches up within a few minutes on a
healthy egress.

If you genuinely want to drop the queue (e.g. you rotated the API
key and want to start fresh):

```sh
sudo systemctl stop conduit
sudo rm -rf /var/lib/conduit/queue/*
sudo systemctl start conduit
```

## "I added a field to `overrides:` and now the doctor warns"

Expected. Heavy reliance on `overrides:` signals the schema is
missing a first-class knob; the warning prompts you to either
restate that the trade-off is intentional or open an issue asking
us to promote the knob. See
[ADR-0012](../adr/adr-0012.md) for the review cadence.
