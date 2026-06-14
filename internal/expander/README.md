# internal/expander

Expands a parsed `conduit.yaml` into the upstream OpenTelemetry Collector configuration that the embedded collector loads via the standard inline `yaml:` URI resolver.

## Two entry points

| Func | Returns | Used by |
|---|---|---|
| `Expand` / `ExpandWithWarnings` | The single rendered base YAML — always-on collector pieces + active platform fragments + selected exporter. | Tests, debugging tools, anywhere you want one self-contained YAML doc. |
| `ExpandConfigs` / `ExpandConfigsWithWarnings` | A slice of YAML config sources. One element when `cfg.Overrides` is empty (the base render); two elements when it's set (base, then the user's overrides marshaled back to YAML). | `cmd/run` (passes each as a `yaml:...` collector URI; the collector deep-merges per its standard multi-config resolver), `cmd/preview` (joins with `---\n# overrides...\n` for human inspection). |

The split exists because the overrides escape hatch from [ADR-0012](../../docs/adr/adr-0012.md) is implemented as a *second config source* to the embedded collector — Conduit never deep-merges in Go. The collector's resolver merges maps by key (overrides win where they overlap) and replaces lists wholesale, matching what `otelcol --config base.yaml --config overrides.yaml` would do at the upstream level.

`Expand` returning the base only (no overrides) keeps the contract simple for the dozens of expander tests asserting fragment shape; `ExpandConfigs` is the production path.

## RED metrics from spans (M8)

`applyREDView` (in `expander.go`) renders the `span_metrics` connector when `cfg.Metrics.RED.REDEnabled()` is true. It is called *after* `pipelineProcessorIDs` so the derivation pipeline sees the final receiver / processor shape:

- Main traces pipeline: unchanged egress (`[<egress>, debug]`). It ships **every** span to the destination — RED no longer tees the connector here, so non-request spans are never dropped from the trace stream.
- Dedicated `traces/red` pipeline (ADR-0022): shares the main traces receiver set, runs `REDTraceProcessors` (the main traces processors with `filter/red_requests` spliced in before `batch`), and exports to `span_metrics` only. The `filter/red_requests` processor keeps only `Server` / `Consumer` spans so the connector counts request-like spans only.
- Metrics pipeline: appends `span_metrics` to `MetricReceivers` so the connector's emitted metrics flow through the standard processor chain to the egress exporter.
- Logs pipeline: untouched.

The connector ID is `span_metrics` (snake_case) — the canonical upstream name as of v0.151. The legacy `spanmetrics` alias still works at v0.151 but emits a startup warning, which we don't want to ship.

Both `filter/red_requests` and the `traces/red` pipeline are gated on `REDEnabled`, so a RED-off config (or `obi.replace_span_metrics_connector: true`) renders neither.

Defaults — span dimensions, resource dimensions, histogram buckets, cardinality limit — live in `internal/config/types.go` (`REDDefault*`, `DefaultREDCardinalityLimit`) so the schema package owns the policy and the expander just renders. The default span dimensions carry both stable (semconv 1.23+) and legacy HTTP method / status-code names. Adding a new default dimension is a single-file change there.

The denylist (`REDDimensionDenylist`) is enforced at `Validate` time, not in the expander — the expander assumes a clean config. This keeps the rendering path a pure function of `*config.AgentConfig` with no validation branching.
