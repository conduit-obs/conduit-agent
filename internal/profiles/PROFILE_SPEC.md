# Platform profile contract

Doctrine for every new platform profile we add to Conduit (linux, darwin,
windows, docker, k8s, ...). The goal is a Datadog-quality first-five-minutes
experience on every supported platform, achieved by:

1. holding every profile to **the same telemetry shape** (§1) so dashboards
   and runbooks key off a stable column vocabulary, and
2. giving every platform **its own opinionated, narrative-driven dashboard**
   (§3) tailored to what makes that platform's telemetry distinctive — a
   k8s board is keyed off pods and namespaces, a docker board off
   containers and compose services, a Windows board off Event Log
   channels. We do **not** force a shared panel skeleton across boards;
   that produces lowest-common-denominator dashboards that nobody loves.

A profile is **shippable** only when it satisfies all four sections below.
Anything missing blocks the matching milestone (M3/M4/M5/M6/M9).

## 1. Telemetry the profile MUST emit

These columns are what every Conduit dashboard, doctor check, and
troubleshooting playbook keys off. If a platform genuinely cannot emit one
(e.g. macOS has no journald), it is documented as omitted in
`<platform>/README.md` and the platform's dashboard substitutes a
platform-native equivalent in the same panel slot.

### Resource attributes (every signal)

Wired by `resourcedetectionprocessor` in
[`templates/base.yaml.tmpl`](../expander/templates/base.yaml.tmpl) — already
universal, listed here so platform PRs know not to re-derive them.

- `host.name`, `host.id`, `host.arch`, `host.ip`, `host.mac`
- `os.type`, `os.description`
- Plus any platform-specific identity keys (e.g. `k8s.node.name` for k8s,
  `container.id` for docker) added in the platform's own resource detector.

### Host metrics (`hostmetrics.yaml`)

Every platform profile enables the `*.utilization` percent-form variants
upstream ships disabled — they're free (computed alongside the byte-level
metrics) and they're what dashboards plot.

| Metric | Required on |
|---|---|
| `system.cpu.utilization` | every platform |
| `system.memory.utilization` | every platform |
| `system.filesystem.utilization` | every platform with a queryable filesystem |
| `system.paging.utilization` | linux, windows (omitted on darwin: privilege-sensitive) |
| `system.disk.io`, `system.network.io`, `system.network.connections`, `system.cpu.load_average.{1m,5m,15m}` | every platform that has them |

### Logs (`logs.yaml`)

Filelog or platform-native sources with at minimum the parsed columns
below — these are what the `Top Log Templates` panel groups by, and what
keeps a multi-host fleet from drowning in unique lines.

| Column | Source |
|---|---|
| `severity_text`, `severity_number` | filelog severity_parser, or default to INFO via the always-on `transform/logs` block in the base template |
| `process` | regex_parser inside the platform's filelog operators |
| `pid` | regex_parser, when available in the source format |
| `message` | regex_parser; the parsed body, distinct from the raw `body` |
| `normalized_message` | always-on, computed by `transform/logs` from `message` |

`body` is preserved verbatim — `normalized_message` is the masked-template
sibling, never a replacement.

## 2. Repository deliverables per platform

Every platform profile PR ships **all** of:

| Path | Purpose |
|---|---|
| `internal/profiles/<platform>/hostmetrics.yaml` | scraper config, with `*.utilization` enabled |
| `internal/profiles/<platform>/logs.yaml` (or platform-native equivalent) | log sources + regex_parser operators producing the columns above |
| `internal/profiles/<platform>/README.md` | what the profile emits, what it intentionally omits, and the privilege model the install path expects |
| `dashboards/<platform>-<scope>-overview.json` | matching default board (see §3). `<scope>` reflects the platform's primary axis: `host` for linux/darwin/windows, `cluster` for k8s, `host` for docker (when M9 lands) — the file name conveys what the board summarizes. |
| `internal/expander/expander_test.go` additions | `TestExpand_<Platform>Profile_*` covering every receiver added by the profile and pinning the column names the dashboard depends on |

## 3. Dashboard quality criteria

Every platform ships its own opinionated default board under
`dashboards/<platform>-*.json`. Boards do **not** share a fixed panel
skeleton — they share a quality bar. Each board must satisfy every
criterion below, and PRs are reviewed against this list rather than
against a panel-by-panel mirror of the darwin board.

### A. Narrative structure

A new operator should be able to read the board top-to-bottom and answer
"is this thing healthy?" without writing a query. That means:

- **Title and a short overview text panel** that names the platform, links
  back to the profile fragment, and says one sentence about the pre-set
  filters.
- **Section dividers** (text panels) when the board exceeds ~6 query
  panels, so the operator can scan headers ("Compute", "Storage", "Logs",
  "Top sources") rather than blocks of identical-looking charts.
- **Highest-signal panel first.** What's the one number you'd glance at
  first on this platform? Load average for a Linux host. Pod restart count
  for a k8s cluster. OTLP bytes/sec for a docker sidecar. Lead with it.

### B. Platform-native primary key

Every panel that aggregates is broken down by the key the operator
already thinks in for that platform. Don't fake `host.name` on a docker
board.

| Platform | Primary key | Secondary breakdowns to lean on |
|---|---|---|
| linux, darwin, windows | `host.name` | `service.name`, scraper-native attributes (`device`, `mountpoint`, `state`) |
| docker | `container.name` | `container.image.name`, `service.name`, host's `host.name` for multi-host docker fleets |
| k8s | `k8s.pod.name` | `k8s.namespace.name`, `k8s.node.name`, `k8s.deployment.name`, `service.name` |

### C. Visualization shape matches the data

- **Time series (line)** for trended values: utilization, throughput,
  latency, request rate, error rate.
- **Top-list / bar chart** for "which N is hottest": top processes by log
  volume, top pods by CPU, top containers by network egress.
- **Heatmap** for distributions where it matters (request latency,
  message size); not for utilization metrics.
- **Single value / gauge** for the highest-signal headline number when
  it stands alone (e.g. cluster-wide pod restart count in the last 1h).
- **Log stream** when the panel is meant to be read as events, not
  counted as a metric.

A 10-panel board of identical line charts is a failure of this rule even
if every column is correct.

### D. Pre-set filters and tags

Every board declares the **two or three filters an operator most often
investigates with on that platform**, set up so flipping one of them
narrows every panel at once.

| Platform | Required pre-set filters |
|---|---|
| linux, darwin, windows | `host.name`, `service.name` |
| docker | `container.name`, `service.name`, optionally `host.name` for multi-host fleets |
| k8s | `k8s.namespace.name`, `k8s.pod.name`, `service.name` |

Boards also carry `tags: ["conduit", "profile-default", "<platform>"]`
so operators can find them in the board list and so `conduit board apply`
(M11) can identify Conduit-managed boards.

### E. First-five-minutes coverage

Without writing a query, a new operator should be able to answer at
least these from the board alone:

1. Is the platform healthy right now? (the headline panel)
2. Which entity (`host.name` / `k8s.pod.name` / `container.name`) is
   hottest by the platform's most common saturation axis?
3. What's the recent error / warning rate? (severity-aware log panel)
4. What's the top noisy log template, so I know what's flooding me?

Cross-platform host boards (linux, darwin, windows) tend to converge on
similar panel structure because they share telemetry shape — that's
fine, and even encouraged where it doesn't force lowest-common-
denominator design. Platforms with distinctive telemetry (docker, k8s)
should diverge wherever the divergence answers a real operator question
that the host pattern would obscure.

## 4. PR checklist

Reviewers reject profile PRs that miss any of these:

- [ ] Every required metric / log column above is emitted by the rendered
      pipeline (or explicitly documented as platform-omitted).
- [ ] Every column the dashboard JSON references is in §1 or in the
      platform's README as a justified extension.
- [ ] `internal/expander/expander_test.go` asserts the receiver names and
      key column names — break the rendered YAML, break the test.
- [ ] `dashboards/<platform>-*.json` validates as JSON and its
      `query_spec`s parse against the same schema the macOS board uses.
- [ ] The board satisfies §3 A–E (narrative structure, platform-native
      primary key, viz shape matches data, required pre-set filters and
      tags, first-five-minutes coverage). Reviewers explicitly call out
      §3.E in review: "what can I learn from this board without writing
      a query?".
- [ ] `internal/profiles/<platform>/README.md` lists what's omitted and
      why, with a link to the upstream OTel issue or design doc that
      captures the privilege / portability constraint.

## See also

- The macOS reference profile: [`internal/profiles/darwin/`](darwin/).
- The macOS reference dashboard: [`dashboards/macos-host-overview.json`](../../dashboards/macos-host-overview.json).
- The base template that wires the always-on processors:
  [`internal/expander/templates/base.yaml.tmpl`](../expander/templates/base.yaml.tmpl).
- M9 in [`04-milestone-plan.md`](../../conduit-agent-plan/04-milestone-plan.md)
  for the cross-platform rollout.
