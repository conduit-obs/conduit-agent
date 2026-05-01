# internal/doctor

Populated in **M11**.

Structured-check registry behind the `conduit doctor` subcommand. Each check is a Go function paired with a `.tmpl` for human rendering, returns a `{status, message, fix_url, details}` shape, and has a stable `CDT0xxx` ID that maps to a docs anchor.

See:

- [`conduit-agent-plan/01-product-requirements.md`](../../conduit-agent-plan/01-product-requirements.md) §FR-8 for the full check list.
- [`conduit-agent-plan/03-technical-architecture-v0.md`](../../conduit-agent-plan/03-technical-architecture-v0.md) §"Diagnostics architecture".
- [`conduit-agent-plan/06-work-breakdown-structure.md`](../../conduit-agent-plan/06-work-breakdown-structure.md) STORY-12.1.
