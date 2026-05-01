# internal/config

Populated in **M2**.

This package holds the `AgentConfig` Go struct hierarchy (`AgentConfig`, `OutputConfig`, `ProfilesConfig`, `LogsConfig`, `MetricsConfig`, etc.) with `mapstructure` / `yaml` / `jsonschema` / `default` tags, plus the JSON Schema generator.

See [`docs/adr/adr-0017.md`](../../docs/adr/adr-0017.md) for the JSON Schema parity decision.
