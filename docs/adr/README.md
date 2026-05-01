# Architecture Decision Records

This directory holds Conduit's Architecture Decision Records (ADRs). The format follows the lightweight Nygard style, customized to the template at [`adr-template.md`](adr-template.md).

## Status flow

Each ADR has one of these statuses:

- **Proposed** — under discussion; not yet binding.
- **Accepted** — locked in; engineering should rely on this.
- **Deprecated** — no longer the active guidance; explanation in the ADR.
- **Superseded by ADR-NNNN** — replaced by a later ADR.

## V0 baseline (`adr-0001` … `adr-0018`)

The 18 V0 baseline ADRs are committed alongside the M1 skeleton. They lock in the foundational decisions Conduit's V0 architecture rests on.

| ADR | Title |
|---|---|
| [0001](adr-0001.md) | V0 is an adoption bridge, not a control plane |
| [0002](adr-0002.md) | V0 is agent-only; gateway is a destination, not a Conduit role |
| [0003](adr-0003.md) | Build via OpenTelemetry Collector Builder |
| [0004](adr-0004.md) | Pure upstream OTel components in V0; zero custom processors / receivers |
| [0005](adr-0005.md) | Generate RED metrics before any sampling step |
| [0006](adr-0006.md) | Allowlist-based RED dimension model with denylist enforcement |
| [0007](adr-0007.md) | Honeycomb-optimized defaults, vendor-neutral schema |
| [0008](adr-0008.md) | `output:` block uses a single `output.mode` field with mode-specific sub-blocks |
| [0009](adr-0009.md) | TLS required by default for `output.mode: gateway` |
| [0010](adr-0010.md) | Refinery is a sub-field of Honeycomb, not its own output mode |
| [0011](adr-0011.md) | `conduit` CLI shape |
| [0012](adr-0012.md) | `conduit.yaml` expands to upstream collector YAML; `overrides:` is the only escape hatch |
| [0013](adr-0013.md) | Apache-2.0 license; clean-room implementation; Observe is reference only |
| [0014](adr-0014.md) | Monthly MINOR cadence pinned to upstream OTel Collector MINOR |
| [0015](adr-0015.md) | Field-engineer-promisable support boundaries |
| [0016](adr-0016.md) | `memorylimiter` first, `batch` last, persistent queue on, redaction on |
| [0017](adr-0017.md) | JSON Schema generated from Go struct tags; CI enforces parity |
| [0018](adr-0018.md) | Demo script is a first-class V0 deliverable |

## Resolved during build (`adr-0019` …)

ADRs in this section were resolved during V0 build, often after a tracked
open question matured into a decision. Once Accepted, ADRs are immutable.

| ADR | Title |
|---|---|
| [0019](adr-0019.md) | Container image published to ghcr.io/conduit-obs/conduit-agent |

## Adding a new ADR

1. Copy [`adr-template.md`](adr-template.md) to `adr-NNNN.md` with the next sequential number.
2. Fill in the metadata, context, decision, alternatives, consequences, and references.
3. Set status to **Proposed** during review; flip to **Accepted** when the change merges.
4. Add a row to the table above.
5. Cross-reference the ADR from any code, ADR, or planning doc it touches.

ADRs are immutable once Accepted, except for the status field. To change a decision, write a new ADR that supersedes the old one.
