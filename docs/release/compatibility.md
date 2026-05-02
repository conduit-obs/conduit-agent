# Conduit ↔ OpenTelemetry Collector compatibility matrix

Each Conduit release embeds a specific upstream OpenTelemetry Collector
core version (and the matching contrib pin from `builder-config.yaml`).
This page maps Conduit releases to the embedded versions so operators
auditing a deployment can answer "what otelcol-core am I actually
running" without grepping the Conduit binary.

## Active V0 releases

| Conduit version | otelcol-core | Builder (OCB) | Components | Status | Released |
|---|---|---|---|---|---|
| `v0.0.0-dev` | `v0.151.0` | `v0.151.0` | see [`builder-config.yaml`](../../builder-config.yaml) | pre-alpha | unreleased |

The `Components` column points at the canonical receiver / processor /
exporter / connector / extension list for the embedded distribution.
Any release-blocking divergence from the listed components (added,
removed, or pinned to a different upstream tag than the otelcol-core
metaversion) is called out in the release notes.

## Support window

Per [`08-release-and-support-model.md`](../../conduit-agent-plan/08-release-and-support-model.md):

- **Latest minor**: full support (bug fixes, security patches, otelcol
  bumps as upstream releases land).
- **Previous minor**: security-only patches for 6 months after the
  next minor ships.
- **Older minors**: best-effort. We won't refuse a PR for a 2-minor-
  back fix, but we won't drive one ourselves either.

## How upstream version is chosen

1. **OCB pin**: `Makefile` and `builder-config.yaml` agree on the
   single source of truth. Both are bumped together; CI fails the
   release pipeline if they diverge.
2. **Component pins**: every component in `builder-config.yaml`
   carries an explicit version (no `@latest`). The default for V0 is
   "match the otelcol-core metaversion"; deviations earn a comment
   in the manifest explaining why (e.g. "stick on contrib v0.150.0
   for the windowseventlogreceiver fix in PR #1234, drop on next core
   bump").
3. **Test gate**: a release with a different otelcol-core version
   than the previous release runs the full output-mode matrix (see
   [`07-testing-and-conformance-plan.md`](../../conduit-agent-plan/07-testing-and-conformance-plan.md))
   against the new core before the maintainer signs the tag.

## Reading this matrix from the agent

`conduit doctor` surfaces the embedded version in the `CDT0403`
(`version.compat`) check:

```
[PASS] version.compat — conduit v0.x.y embeds otelcol-core v0.M.N on
       linux/amd64; in the supported window for this build. (CDT0403)
```

Scripts can extract the JSON form:

```bash
conduit doctor --json | jq '.results[] | select(.id=="CDT0403") | .message'
```

The string is stable enough to parse — when the format changes (e.g.
when the matrix grows a min/max compatibility band), the change ships
under a new check ID rather than a breaking message reformat.

## Cross-references

- [`Makefile`](../../Makefile) — the `OCB_VERSION` and `VERSION` knobs.
- [`builder-config.yaml`](../../builder-config.yaml) — the actual
  upstream component pin list (single source of truth).
- [`docs/release/runbook.md`](runbook.md) §1 — the version-selection
  policy that drives entries in this table.
