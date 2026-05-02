# Dashboards

Checked-in Honeycomb board definitions, in Conduit's vendor-neutral source
format. The CLI subcommand to apply these (`conduit board apply`) lands in
M11; until then this directory is the **source of truth** for the boards
the agent ships, and operators reuploading by hand can pass them straight
through `curl` to Honeycomb's `/1/boards` API.

## Files

| File | What it ships |
|---|---|
| [`macos-host-overview.json`](macos-host-overview.json) | Default per-host overview for the darwin profile: load avg, CPU/memory/filesystem **utilization** (percent-form), disk + network throughput, TCP connections, log volume by severity, top log templates by `process` + `normalized_message`. |
| [`linux-host-overview.json`](linux-host-overview.json) | Linux equivalent — shares most of darwin's narrative because Linux and darwin share host-telemetry shape, but diverges where Linux differs: the filesystem panel filters to block-device-backed mounts (excluding `tmpfs` / `cgroup` / `proc` / `sys` / `overlay`), the network panel excludes `lo`, and an extra Linux-only swap-utilization panel surfaces `system.paging.utilization`. |
| [`k8s-cluster-overview.json`](k8s-cluster-overview.json) | Default cluster-scoped overview for the k8s profile. Keyed off `k8s.pod.name` / `k8s.namespace.name` / `k8s.node.name` (never `host.name`). Narrative: Cluster shape (active-pod count by namespace; container-uptime restart proxy) → Compute pressure absolute (top pods by raw CPU cores / memory working set) → Compute pressure relative-to-limit (top pods by `k8s.pod.{cpu,memory}_limit_utilization` — which Conduit enables in [`internal/profiles/k8s/kubelet.yaml`](../internal/profiles/k8s/kubelet.yaml)) → Network → Filesystem → Logs (severity / namespace / top noisy pods / recent ERROR+WARN). |
| [`docker-host-overview.json`](docker-host-overview.json) | Default board for the docker profile (M9.D, deferred from M4). Two narratives in one place: the **compose host** underneath the agent (host metrics from `/hostfs`, broken down by `host.name`) and the **peer apps** (RED metrics from M8's `span_metrics` connector + logs broken down by `service.name`). Honest about its scope — V0 docker doesn't scrape per-container metrics; the host story is host-shaped and the per-app story is shaped by what your SDKs emit. Filesystem panel keys on `/var/lib/docker/*` mountpoints; network panel filters out `lo` + `docker0` + `br-*` + `veth*` so what remains is the host's actual NICs. |

Future platform boards land alongside their respective platform milestones — `windows-host-overview.json` at M6, `k8s-workload-overview.json` as a follow-up to M5.E once we have field signal on whether deployment-scoped vs pod-scoped is the more useful default. Each is its own opinionated, narrative-driven dashboard tailored to what's distinctive about that platform's telemetry, **not** a copy of the host-overview skeleton — a Datadog-quality experience comes from each platform feeling first-class, not from forced uniformity.

Every file in this directory must satisfy the contract in [`internal/profiles/PROFILE_SPEC.md`](../internal/profiles/PROFILE_SPEC.md): §1 fixes the *telemetry shape* (column names, severity defaults, resource attributes) so dashboards key off a stable vocabulary; §3 spells out the dashboard quality bar (narrative structure, platform-native primary key, viz shape matching the data, required pre-set filters and tags, first-five-minutes coverage) that PRs are reviewed against.

## File format (`format_version: 1`)

```jsonc
{
  "format_version": 1,
  "name": "...",
  "description": "...",
  "tags": ["k:v", ...],
  "preset_filters": [{ "column": "host.name", "alias": "Host" }],
  "panels": [
    {
      "type": "text",
      "size": { "width": 12, "height": 5 },
      "content": "markdown..."
    },
    {
      "type": "query",
      "name": "...",
      "description": "...",
      "chart_type": "line | stacked | bar | stat | ...",
      "display_style": "chart | table | combo",
      "size": { "width": 6, "height": 4 },
      "dataset": "metrics",
      "query_spec": { /* Honeycomb query spec — see run_query API */ }
    }
  ]
}
```

The schema is intentionally close to the Honeycomb create_board API shape:
the only Conduit-specific addition is `query_spec` per panel (Honeycomb's
API takes already-resolved `query_id`s; we keep the spec inline so the
file is self-contained and replayable into any environment without first
materializing query objects).

When `conduit board apply` ships, it will:

1. POST each panel's `query_spec` to `/1/queries/{dataset}` -> `query_id`.
2. POST the resolved board to `/1/boards`.

Until then, the JSON here is human-editable: change the regex / mountpoint
/ time_range in the `query_spec` and reapply by hand. PRs that change a
panel should change this file, **not** the live board (the live board
gets regenerated from this file).

## Auth

`conduit board apply` will read **`HONEYCOMB_CONFIG_API_KEY`** — a
*configuration* key from your team's API Keys page with the "Manage
Boards" permission. This is **distinct from** the ingest key
`HONEYCOMB_API_KEY` used by `output.honeycomb.api_key` in `conduit.yaml`:

- Ingest key (`hcaik_…`): writes telemetry, can't manage boards.
- Configuration key: can rewrite/delete every board, trigger, and SLO on
  the team. Treat it like a deploy credential.
