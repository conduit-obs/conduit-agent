# Contributing to Conduit

Thanks for your interest in Conduit. The notes below set expectations for contributing, with the hard rules called out first.

## Hard rules

These are non-negotiable. PRs that violate them are closed without further discussion.

### 1. Clean-room implementation

Conduit is a clean-room implementation as a matter of project discipline ([ADR-0013](docs/adr/adr-0013.md)). Patterns from other Apache-2.0 OpenTelemetry-agent projects may be borrowed conceptually with attribution; **no code is copied verbatim, even from compatibly-licensed sources.**

When a PR is influenced by code you read elsewhere, declare in the PR description which of the following applies:

| Type | Means | Acceptable? |
|---|---|---|
| **Inspiration** | You read the reference code and wrote yours from scratch with no clipboard contact | Yes |
| **Adaptation** | You copied a snippet and modified it | No — rewrite from scratch |
| **Verbatim copy** | You copied the code as-is | No — rewrite from scratch |

Reviewers actively look for verbatim copy. If asked, the answer must be yes/no with an honest description of any reference material.

### 2. Pure upstream OpenTelemetry components in V0

Conduit's V0 OCB manifest pulls only from `go.opentelemetry.io/collector` and `github.com/open-telemetry/opentelemetry-collector-contrib`. **Zero custom Conduit processors or receivers in V0.** This is locked in by [ADR-0004](docs/adr/adr-0004.md). If you find yourself wanting to write a custom processor, the right place is a `transformprocessor` block in `conduit.yaml` or an upstream-OTel-contrib feature request, not a Conduit-specific component.

### 3. Apache-2.0 license

Every source file must be compatible with [Apache-2.0](LICENSE). When introducing a new dependency, confirm its license is Apache-2.0, MIT, BSD, or another permissive license that works under Apache-2.0 redistribution.

### 4. No fallback modes, fake-data shims, or stubs of third-party services

Conduit talks to real systems (Honeycomb, OTLP gateways, Kubernetes APIs). Mock them in tests. Do not introduce dev/prod-affecting fallback code paths or fake-data generators outside test scope.

## How to set up a dev environment

```sh
git clone https://github.com/conduit-obs/conduit-agent
cd conduit-agent
make build       # builds ./bin/conduit
make test        # runs go test ./...
make lint        # runs golangci-lint
```

Required tooling:

- Go 1.23 or newer.
- `golangci-lint` (install via `go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest`).

`make install-ocb` downloads a pinned OpenTelemetry Collector Builder binary into `./bin/`. Required when you change [`builder-config.yaml`](builder-config.yaml) and need to regenerate the embedded collector.

## How to propose changes

1. Open an issue describing the change before opening a PR for anything more than a typo. We close large surprise PRs.
2. Keep PRs focused. One concept per PR.
3. Add tests for new behavior. Update goldens (`make update-goldens`) for any config-render changes.
4. Update docs in the same PR. Schema-drift bugs are a chronic OSS-project failure mode and we have a CI gate to prevent them.

## Architecture Decision Records (ADRs)

Architecturally significant changes get an ADR under [`docs/adr/`](docs/adr/). Use [`docs/adr/adr-template.md`](docs/adr/adr-template.md). Number sequentially. The 18 V0 baseline ADRs (`adr-0001.md` through `adr-0018.md`) are already committed; new ADRs start at `adr-0019.md`.

## Tests

- **Unit tests**: `go test ./...` (per-package coverage expected for new logic).
- **Linting**: `make lint`.
- **Vulnerability scan**: `govulncheck ./...`.

CI runs lint, tests, govulncheck, and CodeQL on every PR.

## Code style

- Follow `gofmt` and `goimports`. CI fails otherwise.
- Don't add narrative comments that restate what code already says. Comments should explain *why*, not *what*.
- Avoid files over 200–300 lines. Refactor at that point.

## Commits and PRs

- Commit messages: short subject, blank line, optional body. Reference issue numbers where applicable.
- Sign your commits if your platform supports it. Required for release tags.

## Code of conduct

A formal `CODE_OF_CONDUCT.md` is pending (Contributor Covenant 2.1 is the planned text). In the interim, the project follows the spirit of the Contributor Covenant: be respectful, assume good faith, and report any concerns to `security@conduit-obs.com`.

## Security

Do not file public issues for vulnerabilities. See [`SECURITY.md`](SECURITY.md) for the disclosure process.
