# internal/doctor

Populated in **M11**.

Structured-check registry behind the `conduit doctor` subcommand. Each check is a Go function paired with a `.tmpl` for human rendering, returns a `{status, message, fix_url, details}` shape, and has a stable `CDT0xxx` ID that maps to a docs anchor.
