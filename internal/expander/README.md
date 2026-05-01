# internal/expander

Populated in **M2**.

This package expands a parsed `conduit.yaml` into the upstream OpenTelemetry Collector configuration that the embedded collector loads via the standard inline `yaml:` URI resolver.

See:

- [`conduit-agent-plan/03-technical-architecture-v0.md`](../../conduit-agent-plan/03-technical-architecture-v0.md) §"Expansion model".
- [`docs/adr/adr-0012.md`](../../docs/adr/adr-0012.md) for the `overrides:` escape-hatch decision.
- [`conduit-agent-plan/06-work-breakdown-structure.md`](../../conduit-agent-plan/06-work-breakdown-structure.md) STORY-3.2.
