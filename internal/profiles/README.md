# internal/profiles

Embedded YAML fragments — chunks of upstream OTel Collector receiver config — that turn the OTLP-only base pipeline into a useful out-of-the-box agent on each supported platform. Loaded by [`profiles.go`](profiles.go); spliced into the rendered config by [`internal/expander`](../expander/).

Each fragment is plain upstream YAML rooted at the receiver level (no top-level `receivers:` key). Authors keep them honest by reading them; the loader only resolves which file to read for which (`platform`, `signal`) pairing.

## Authoring contract

Two rules every fragment file must follow:

1. **Top-level keys are receiver instance IDs.** `hostmetrics:`, `filelog/system:`, `journald:`. The expander reads the column-zero keys to populate the matching pipeline's `receivers:` list automatically — adding a new top-level key adds a new receiver to the pipeline.
2. **No top-level wrapping.** Don't wrap content in `receivers:`; the splicing target is already that block.

## Layout

| Subdirectory | Populated by | Contents |
|---|---|---|
| `linux/` | M3.A, M9 | `hostmetrics.yaml` (full scraper set), `logs.yaml` (filelog `/var/log/{syslog,messages,auth.log,secure}` + journald) |
| `darwin/` | M3.A | `hostmetrics.yaml` (macOS-safe scraper subset; no `paging` / `processes`), `logs.yaml` (filelog `/var/log/{system,install}.log`) |
| `windows/` | M6, M9 | Windows Event Log (Application + System), host metrics |
| `docker/` | M4 (bind only), M9 (host metrics) | M4 ships no fragment files: `profile.mode=docker` only flips OTLP receivers to `0.0.0.0` so peer containers can reach them, while `health_check` stays on `0.0.0.0:13133` from the base template. M9 will add `hostmetrics.yaml` once we land the bind-mount story for `/proc` and `/sys`. See [`docker/README.md`](docker/README.md) for the V0 contract. |
| `k8s/` | M5.B | `hostmetrics.yaml` (per-node CPU/memory/etc, expecting host bind mounts the chart provides in M5.C), `kubelet.yaml` (`kubeletstatsreceiver` against the local kubelet for per-node / per-pod / per-container CPU + memory), `logs.yaml` (`filelog/k8s` tailing `/var/log/pods/*/*/*.log` with the upstream container operator). The `k8sattributes` processor lives in the base template, gated by `profile.mode=k8s`, and runs on every pipeline so OTLP signals from instrumented apps get the same metadata enrichment. |
| `shared/` | reserved | cross-platform processor / connector defaults; not used in M3.A |

## Currently shipped signals

`profiles.SignalHostMetrics` (file `hostmetrics.yaml`), `profiles.SignalSystemLogs` (file `logs.yaml`), and `profiles.SignalKubelet` (file `kubelet.yaml`, k8s-only — host platforms have no analogue). Adding a new signal kind means: add a constant to `profiles.go`, document it here, and ship the matching `<platform>/<signal>.yaml` fragments.

## See also

- [`PROFILE_SPEC.md`](PROFILE_SPEC.md) — the cross-platform contract every new profile must satisfy (telemetry shape, repo deliverables, dashboard quality bar, PR checklist). Boards are tailored per platform, not forced into a shared skeleton.
