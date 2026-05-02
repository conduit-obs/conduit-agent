# Expander golden-file matrix

The `*_test.go` harness in `internal/expander/goldens_test.go` walks
each subdirectory here, treats `conduit.yaml` as the input, runs it
through the production `Expand()` pipeline, and diffs the rendered
upstream OTel Collector YAML against `expected.yaml`.

This is the second-line defense against silent renderer regressions
([07-testing-and-conformance-plan.md] §Layer 2). Unit tests catch
"the template no longer compiles"; goldens catch "the output
changed in a way the unit tests didn't notice because they only
asserted structural shape, not exact byte equality."

## Updating goldens

When an intentional change to the renderer or a profile fragment
shifts the rendered YAML:

```sh
go test ./internal/expander -run TestExpand_Goldens -update
```

That rewrites every `expected.yaml` from the current renderer.
Inspect the diff in `git status`, confirm it matches the change
you intended, and commit the new goldens alongside the source
change.

## Adding a new case

1. Make a new directory: `mkdir testdata/goldens/<NN>-<short-slug>/`
   where `NN` is a two-digit ordinal that keeps the case list
   readable in the test runner output.
2. Drop a `conduit.yaml` into it. Keep it minimal — the canonical
   shape is "what the user would actually write", not a
   testdata-only kitchen sink.
3. Run `go test ./internal/expander -run TestExpand_Goldens -update`
   to seed `expected.yaml`.
4. Commit both files. Reviewers can read `expected.yaml` directly
   to see what the new case produces.

## Slice list (V0 matrix)

| # | Name | What it proves |
|---|---|---|
| 01 | `honeycomb-linux` | Most common Linux install — hostmetrics + filelog/system + journald + RED on by default |
| 02 | `honeycomb-via-refinery` | US-07 journey — traces through `otlp/refinery` (gRPC), metrics + logs through `otlphttp/honeycomb` |
| 03 | `gateway-tls-required` | US-06 journey — explicit `tls: { insecure: false }` on the gateway exporter (AC-06.3) |
| 04 | `otlp-vendor-headers` | Generic OTLP/HTTP — `Authorization` and `DD-API-KEY` style headers + compression rendering |
| 05 | `persistent-queue` | M10.A — `file_storage` extension + `service.extensions: [health_check, file_storage]` + per-exporter `sending_queue.storage` |
| 06 | `k8s-default` | Most common k8s install — kubeletstats + filelog/k8s + k8sattributes + 0.0.0.0 OTLP bind |
| 07 | `docker-default` | Compose / sidecar install — docker hostmetrics fragment with `root_path: /hostfs` + 0.0.0.0 OTLP bind |
| 08 | `windows-default` | M6 path — windows hostmetrics + windowseventlog/{application,system} + 127.0.0.1 OTLP bind |
| 09 | `darwin-default` | Dev/local laptop install — macOS hostmetrics + macOS-side filelog/system + 127.0.0.1 OTLP bind |
| 10 | `no-profile-red-off` | Minimal-renderable — smallest possible output for regression-baseline diffs |

The matrix is intentionally hand-curated rather than exhaustive
(the full `platform × output × RED × queue` cross-product would be
~96 cases and most add no signal). New cases earn their place by
proving a contract that the existing 10 don't already cover.
