# internal/expander

Expands a parsed `conduit.yaml` into the upstream OpenTelemetry Collector configuration that the embedded collector loads via the standard inline `yaml:` URI resolver.

## Two entry points

| Func | Returns | Used by |
|---|---|---|
| `Expand` / `ExpandWithWarnings` | The single rendered base YAML — always-on collector pieces + active platform fragments + selected exporter. | Tests, debugging tools, anywhere you want one self-contained YAML doc. |
| `ExpandConfigs` / `ExpandConfigsWithWarnings` | A slice of YAML config sources. One element when `cfg.Overrides` is empty (the base render); two elements when it's set (base, then the user's overrides marshaled back to YAML). | `cmd/run` (passes each as a `yaml:...` collector URI; the collector deep-merges per its standard multi-config resolver), `cmd/preview` (joins with `---\n# overrides...\n` for human inspection). |

The split exists because the overrides escape hatch from [ADR-0012](../../docs/adr/adr-0012.md) is implemented as a *second config source* to the embedded collector — Conduit never deep-merges in Go. The collector's resolver merges maps by key (overrides win where they overlap) and replaces lists wholesale, matching what `otelcol --config base.yaml --config overrides.yaml` would do at the upstream level.

`Expand` returning the base only (no overrides) keeps the contract simple for the dozens of expander tests asserting fragment shape; `ExpandConfigs` is the production path.
