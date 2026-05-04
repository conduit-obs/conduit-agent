# Getting started

Pick the path that matches where you'll run Conduit. Each guide is
self-contained — no cross-reading needed — and gets you to "data is
landing in Honeycomb" in the time noted.

| Platform | Time | Guide |
|---|---|---|
| Linux (Ubuntu / RHEL / Amazon Linux / Debian / Alpine / Arch) | 15 min | [`linux.md`](linux.md) |
| Docker (single host) | 10 min | [`docker.md`](docker.md) |
| Kubernetes (any cluster) | 20 min | [`kubernetes.md`](kubernetes.md) |
| Windows Server / Windows 11 | 15 min | [`windows.md`](windows.md) |

After the platform install, optionally:

| Add-on | Time | Guide |
|---|---|---|
| **OBI — zero-code app instrumentation** (Linux only) | 10 min | [`obi.md`](obi.md) |

## What you'll need regardless

- A Honeycomb ingest API key (any plan, including the free tier).
  Get one from your Honeycomb env's "API keys" tab.
- About 30 minutes the first time. Subsequent installs are
  ~10 minutes once you know what you're doing.

## What every install gives you

- The OTLP receiver on `:4317` (gRPC) and `:4318` (HTTP) ready for
  your apps. Bound to `127.0.0.1` for host installs and `0.0.0.0`
  for Docker / Kubernetes (where peer containers / pods need to
  reach it).
- Platform-default profile fragments (host metrics + system logs)
  loaded based on the host OS, with auto-detection. Set
  `profile.mode: none` if you want OTLP-only.
- The `conduit doctor` diagnostic command — every install path
  includes a verification step that runs the full check catalog and
  exits non-zero on any failure, so install scripts can gate on it.
- RED metrics from spans (request count, error count, duration
  histogram) tee'd off the traces pipeline before any sampling
  step, so derived metrics see 100% of traffic.

## After the install

- [**Configuration reference**](../reference/configuration.md) — the
  complete `conduit.yaml` schema.
- [**Architecture overview**](../architecture/overview.md) — what's
  actually running inside the agent.
- [**Troubleshooting index**](../troubleshooting/index.md) — the
  CDT-codes cheat sheet and the symptom-driven walkthroughs.
