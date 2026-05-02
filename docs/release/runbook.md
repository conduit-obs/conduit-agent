# Conduit V0 — Release runbook

**Audience**: maintainers cutting a Conduit release.
**Scope**: the end-to-end "git tag → published artifacts → docs site update" workflow for V0.x. Pre-release rehearsals also follow this runbook so the first time we run it isn't on a real customer-facing tag.

This is a working document. Steps that fail in CI on the first real release become PRs against this file.

---

## 0 — Prereqs

| Item | Where it lives | Validated by |
|---|---|---|
| Maintainer commit access | GitHub repo permissions | `git push` works against `main` |
| `goreleaser` ≥ 1.25 | local install (`brew install goreleaser`) | `goreleaser --version` |
| `cosign` ≥ 2.2 | local install (`brew install cosign`) | `cosign version` |
| `gh` CLI authenticated | `gh auth status` | `gh release list` shows previous releases |
| `helm` ≥ 3.13 | local install | `helm version` |
| Honeycomb sandbox API key | maintainer keychain (`HONEYCOMB_SANDBOX_API_KEY`) | the AWS smoke tests can ingest test data |
| AWS sandbox credentials | maintainer's `~/.aws/credentials` (profile `conduit-sandbox`) | `aws --profile conduit-sandbox sts get-caller-identity` |

If any item is missing, **stop**: a partial release is worse than a delayed one.

---

## 1 — Pick the version

Conduit follows SemVer. For V0.x:

- **Patch bumps** (V0.0.x): bug fixes only. No schema changes; no new top-level config fields.
- **Minor bumps** (V0.x.0): new features. Schema additions allowed (new fields tolerated by older agents on read; rejected by older agents only when set non-default).
- **Pre-1.0 breaking changes**: bump minor and call it out in the changelog. Pre-1.0 we explicitly do not promise compatibility across minor bumps.

Decide the version in a maintainer issue before tagging:

```
Title: Release v0.x.y
Body:
  - Diff: <link to git compare since previous release>
  - Schema changes: yes / no — if yes, summarize
  - Upstream collector core bump: yes / no — if yes, list the new components
  - Risk areas: <bullet list>
  - Sign-off: <maintainer name>
```

The issue stays open through the release and is closed by the changelog PR.

---

## 2 — Pre-release checks (local)

Run these on the maintainer's laptop against the tip of `main`:

```sh
# 1. Tests + lint must be green.
make test
make lint
make vulncheck

# 2. Goldens must reflect the current renderer.
go test ./internal/expander -run TestExpand_Goldens
# If this fails, run `make update-goldens` and re-review the diff in
# its own commit BEFORE tagging — never sneak a golden update into
# the release commit.

# 3. Doctor docs anchor parity.
go test ./internal/doctor -run TestDocsAnchor

# 4. Schema reference + README parity (M13: when the generator lands).
# make check-readme

# 5. Snapshot release (no publish, no signing) to confirm goreleaser
#    config still validates against the current goreleaser version.
goreleaser release --snapshot --clean --skip=publish
```

If any step fails, **stop** and fix the cause on `main`. Do not tag a broken commit "and fix forward".

---

## 3 — Pre-release checks (CI)

| Gate | How to read it | Required for release |
|---|---|---|
| Latest `main` CI run | green | yes |
| Latest nightly AWS smoke | green or known cause | yes |
| `helm-publish` workflow on the test tag | succeeded with cosign signature | yes |
| `govulncheck` | no `HIGH` / `CRITICAL` | yes |
| Image scan | no `HIGH` / `CRITICAL` | yes |
| Output-mode matrix | green (Honeycomb / Refinery / gateway) | yes |
| Windows MSI smoke | green on the most recent `windows-latest` runner | yes (M12 follow-up) |

If a gate is yellow rather than green, document the reason in the maintainer issue and either fix or explicitly defer with a sign-off note.

---

## 4 — Cut the tag

Tags are the trigger for the publishing workflow. Once a tag is pushed, **everything is automated** through goreleaser; do not push artifacts manually.

```sh
# Confirm you're on the right SHA.
git checkout main
git pull --ff-only
git log --oneline -5

# Tag with a v-prefix per Go module conventions.
git tag -s -m "v0.x.y" v0.x.y       # signed tag; -s is required.
git push origin v0.x.y
```

The signed tag is the audit log; CI verifies the signature against the maintainer's GPG key as a sanity check before the publish job runs.

---

## 5 — Watch the publish workflow

The release workflow at `.github/workflows/release.yml` runs `goreleaser release` and produces:

| Artifact | Signed by | Verification |
|---|---|---|
| Linux deb / rpm / apk / archlinux / tar.gz | maintainer GPG key + cosign keyless | `cosign verify-blob` and `dpkg-sig --verify` (deb), `rpm --checksig` (rpm) |
| macOS zip | cosign keyless | `cosign verify-blob` |
| Windows MSI | Authenticode (DigiCert / sigstore — M12 follow-up) | `signtool verify /pa /v conduit.msi` |
| Multi-arch container image at `ghcr.io/conduit-obs/conduit-agent:vX.Y.Z` | cosign keyless | `cosign verify ghcr.io/...` |
| Helm chart at `oci://ghcr.io/conduit-obs/charts/conduit-agent:X.Y.Z` | cosign keyless | `cosign verify-blob` |
| Source tarball | maintainer GPG key | `gpg --verify` |
| SBOM (CycloneDX) | cosign attestation | `cosign verify-attestation` |

Open `gh run watch` against the tag's workflow and stay until it finishes. **If a step fails, the release stays in "draft" state**; fix the failure (or revert the tag if the cause is in the source) and re-run only the failed step.

---

## 6 — Post-release smoke

Before marking the release public:

```sh
# 1. Pull the published image and run it.
docker run --rm ghcr.io/conduit-obs/conduit-agent:v0.x.y --version

# 2. Verify the container signature.
cosign verify ghcr.io/conduit-obs/conduit-agent:v0.x.y \
  --certificate-identity-regexp 'https://github.com/conduit-obs/.*' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com

# 3. Pull the helm chart, render, install on kind.
helm pull oci://ghcr.io/conduit-obs/charts/conduit-agent --version 0.x.y
helm template ./conduit-agent-0.x.y.tgz \
  --set conduit.serviceName=release-smoke \
  --set honeycomb.apiKey=dummy >/dev/null

# 4. Run the AWS smoke recipes against the new release.
#    (Currently manual; nightly automation lands at M12 follow-up.)
```

If any post-smoke step fails, **mark the release as broken**: edit the GitHub release notes to add a `**WITHDRAWN**` banner and open a fix issue. Do not delete the tag; deletion would leave install-script users with broken integrity checks.

---

## 7 — Publish the release notes

Convert the maintainer issue from §1 into the GitHub release body. Required sections:

```markdown
## What's new
<bulleted list, customer-facing wording, no internal jargon>

## Schema changes
<list, or "none" if no changes>. Include before/after YAML snippets for
non-trivial additions.

## Upstream OpenTelemetry Collector core
<otelcol-core version + a one-liner per added/removed component>.

## Breaking changes
<list, or "none">. For each breaking change, include the migration path.

## Acknowledgments
<contributors since the previous release>

## Verification
- Container image: ghcr.io/conduit-obs/conduit-agent:v0.x.y (cosign keyless)
- Helm chart: oci://ghcr.io/conduit-obs/charts/conduit-agent:0.x.y (cosign keyless)
- Linux packages: signed with the maintainer GPG key (fingerprint <FPR>)
- Windows MSI: Authenticode-signed (publisher: Conduit Observability LLC)

See [the release runbook](../docs/release/runbook.md#6--post-release-smoke)
for verification commands.
```

Click "Set as latest release" once §6's smoke steps all pass.

---

## 8 — Update the docs site

| Doc | Update | Tooling |
|---|---|---|
| Compatibility matrix | new row for the released version | `docs/release/compatibility.md` |
| Getting Started | bump the install one-liner version pin | `README.md` + the `docs/installing/*` family |
| AWS recipes | bump pinned version in user-data templates | `docs/deploy/aws/*.md` |

These updates land as a single "post-release docs bump for v0.x.y" PR within 24 hours of the release.

---

## 9 — When something goes wrong

### A signing step fails mid-release

If the cosign / GPG / Authenticode signing step fails, the workflow leaves a partial release artifact in the dist staging area. **Do not** retry from a fresh tag — re-run the workflow against the existing tag (the goreleaser `--clean` flag re-cleans dist on its own).

### A published artifact has a critical bug

1. Edit the GitHub release notes to add a `**WITHDRAWN**` banner with a link to the tracking issue.
2. Open a yank PR against `main` documenting the issue, then immediately cut a `v0.x.(y+1)` patch.
3. The withdrawn version stays available (deletion would break installer integrity); the docs site's "current version" pointer moves forward.

### A maintainer GPG key rotation

1. Add the new key to the GitHub Action secret + the SECURITY.md doc.
2. Cut a docs-only patch release that regenerates the install scripts with the new key fingerprint.
3. Old releases stay signed with the old key — that's why we publish full key history in `docs/release/key-history.md` (M13 follow-up).

---

## Cross-references

- [`07-testing-and-conformance-plan.md`](../../conduit-agent-plan/07-testing-and-conformance-plan.md) §Release gates lists the gates this runbook enforces.
- [`08-release-and-support-model.md`](../../conduit-agent-plan/08-release-and-support-model.md) defines the SemVer + support window contract the version selection in §1 follows.
- [`docs/release/compatibility.md`](compatibility.md) is the published Conduit ↔ otelcol-core matrix.
- [`docs/release/security.md`](security.md) (M13 follow-up) documents the signing-key rotation policy.
