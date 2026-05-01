# Security Policy

## Reporting a vulnerability

Please **do not** file public GitHub issues for security vulnerabilities. Email `security@conduit-obs.com` with a description of the issue, the version of Conduit affected, and reproduction steps if available.

We will acknowledge your report within **3 business days**.

## Disclosure process

We follow a 90-day coordinated disclosure norm with severity-driven exceptions:

| Severity | Conduit response target |
|---|---|
| Critical (CVSS ≥ 9.0) | Patch released within 7 days; advisory published |
| High (7.0 ≤ CVSS < 9.0) | Patch released within 14 days |
| Medium / Low | Bundled into next regular release |

For CVEs in the upstream OpenTelemetry Collector that are pinned by Conduit, we follow upstream's CVE response and ship a Conduit PATCH within 7 days that bumps to the upstream PATCH.

## Supported versions

While Conduit is in pre-alpha (`v0.0.x`, M1 skeleton), there is no commitment to backports. From `v0.1.0` onwards (M12 release hardening), security patches will be backported to the most recent MINOR per the release-and-support model documented at launch.

## Scope

In scope:

- The `conduit` binary and its CLI subcommands.
- The `conduit.yaml` schema and config expander (when implemented in M2).
- Conduit-published packaging (deb, rpm, MSI, container image, Helm chart) when those land in M3–M6.
- Conduit-published install scripts (`install_linux.sh`, `install.ps1`).

Out of scope:

- Vulnerabilities in upstream OpenTelemetry Collector or contrib components — please report those upstream at `https://github.com/open-telemetry/opentelemetry-collector` or `https://github.com/open-telemetry/opentelemetry-collector-contrib`. We will track the upstream advisory and release a Conduit PATCH per the schedule above.
- Vulnerabilities in Honeycomb's ingest, product, or APIs — report to Honeycomb's security team directly.
- Vulnerabilities in customer-deployed Conduit instances that arise from configuration choices documented as advanced (`overrides:`, `output.gateway.insecure: true`, etc.).

## Signing

Release artifacts will be signed starting with the M12 release-hardening milestone:

- Container image: `cosign`-signed.
- MSI: Authenticode-signed.
- `.deb` / `.rpm`: GPG-signed.
- Helm chart: Helm chart provenance.
- All artifacts ship with attached SBOMs.

Verification instructions will be published with the first signed release.

## Acknowledgements

Researchers who report valid vulnerabilities are credited in the corresponding security advisory unless they request anonymity.
