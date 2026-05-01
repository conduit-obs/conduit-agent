# internal/config

Populated in **M2**.

This package holds the `AgentConfig` Go struct hierarchy (`AgentConfig`, `OutputConfig`, `ProfilesConfig`, `LogsConfig`, `MetricsConfig`, etc.) with `mapstructure` / `yaml` / `jsonschema` / `default` tags, plus the JSON Schema generator.

See:

- [`conduit-agent-plan/03-technical-architecture-v0.md`](../../conduit-agent-plan/03-technical-architecture-v0.md) §"Config model" for the struct shape.
- [`docs/adr/adr-0017.md`](../../docs/adr/adr-0017.md) for the JSON Schema parity decision.
- [`conduit-agent-plan/06-work-breakdown-structure.md`](../../conduit-agent-plan/06-work-breakdown-structure.md) STORY-3.1 for the M2 task list.
