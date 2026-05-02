# V0 launch checklist

The list of things that have to be true on the day Conduit V0 goes
public. Track each line with a real owner; "engineering" or "everyone"
means it'll slip.

This document is the *go/no-go* contract for cutting the first
public release tag. The process for cutting any subsequent release is
in [`runbook.md`](runbook.md); the upstream-pinning policy that
governs what we ship is in [`compatibility.md`](compatibility.md).

## T-2 weeks

### Source-of-truth gates

- [ ] All V0 milestones (M0 through M13) marked **done** in
      `conduit-agent-plan/04-milestone-plan.md`. M12 follow-ups
      gated on live infrastructure (AWS smoke automation,
      Authenticode signing, runtime doctor checks) explicitly
      listed as V1+ — not V0 blockers.
- [ ] The acceptance-criteria roll-up
      (`conduit-agent-plan/05-acceptance-criteria.md`) is fully
      checked off, **or** every unchecked item has a deferral note
      explaining the V0 → V1 boundary.
- [ ] All ADRs are marked Accepted or Superseded — no Accepted-soon /
      Draft ADRs in the merge queue.
- [ ] CHANGELOG entry for `v0.0.0` (or whatever the launch tag is)
      drafted from the runbook template.

### Documentation gates

- [ ] All four [`docs/getting-started/`](../getting-started/) guides
      (linux, docker, kubernetes, windows) have been walked
      end-to-end on a clean host by someone who didn't write them.
      Time-to-first-signal recorded for each.
- [ ] [`docs/reference/configuration.md`](../reference/configuration.md)
      has been diff-reviewed against `internal/config/types.go` —
      every field present, every default correct.
- [ ] [`docs/troubleshooting/cdt-codes.md`](../troubleshooting/cdt-codes.md)
      has a heading per implemented `CDT0xxx` code (the
      `TestDocsAnchorParity` test enforces this; verify the test is
      green in the launch tag's CI run).
- [ ] [`docs/architecture/overview.md`](../architecture/overview.md)
      reviewed by an outside engineer for "would this make sense to
      me cold?" — feedback incorporated.
- [ ] [`docs/demo/script.md`](../demo/script.md) rehearsed end-to-
      end at least twice. Wall-clock time under 30 minutes.

### Release engineering gates

- [ ] `make release-snapshot` produces every artifact (deb, rpm,
      apk, pacman, docker image, helm chart, MSI, bare tarballs) on
      both `linux/amd64` and `linux/arm64`.
- [ ] [`scripts/install_linux.sh`](../../scripts/install_linux.sh)
      has been smoke-tested on Ubuntu 22.04, Ubuntu 24.04, Debian 12,
      RHEL 9, Amazon Linux 2023, Alpine 3.18, and Arch — `latest`.
- [ ] The release workflow (`.github/workflows/release.yml`) has
      been dry-run on a release candidate tag (`vX.Y.Z-rc1`) and
      every job is green.
- [ ] Cosign keyless OIDC signing of container images and helm
      charts verified post-build:

      ```sh
      cosign verify ghcr.io/conduit-obs/conduit-agent:vX.Y.Z-rc1 \
        --certificate-identity-regexp 'https://github.com/conduit-obs/.*' \
        --certificate-oidc-issuer https://token.actions.githubusercontent.com
      ```

- [ ] SBOMs generated for every artifact (Syft) and attached to the
      release.
- [ ] Vulnerability scan (`make vulncheck`) clean against the latest
      commit on `main`.
- [ ] **Windows MSI Authenticode signing**: either resolved (M12
      follow-up complete, MSI signed) **or** documented as known
      limitation in the launch announcement so SmartScreen warning
      isn't a surprise.

### Repo + project hygiene

- [ ] `LICENSE` file present and correct (Apache 2.0 unless decided
      otherwise — see ADR-0019 if there is one).
- [ ] `CODEOWNERS` covers `internal/`, `cmd/`, `deploy/`, `docs/`,
      `.github/`.
- [ ] `SECURITY.md` published with a real reporting address. Set up
      `security@<domain>` mailbox **before** the announcement.
- [ ] `CONTRIBUTING.md` covers DCO / sign-off, build setup, the
      golden-file workflow (`make update-goldens`), how to add a
      doctor check, and how to add an ADR.
- [ ] Issue and PR templates created. PR template references the
      change-checklist (lint, test, docs, ADR if surface area
      changed).
- [ ] Repo description and topics on GitHub match what we'll claim
      publicly.

## T-1 week

### Pre-launch dry-run

- [ ] Cut a release-candidate tag (`vX.Y.Z-rc1`). Walk the runbook
      end-to-end; record any deviations.
- [ ] Run [`docs/demo/script.md`](../demo/script.md) against the rc
      build (not against `main`). Adjust the script for any new
      surface area introduced after the script was written.
- [ ] Smoke install on every supported platform from the rc release
      assets (deb, rpm, MSI, helm chart, container image). Time
      each install. Log any "I had to look something up" moments —
      those are docs gaps to plug.
- [ ] Run `conduit doctor --json` against each install, save the
      output. This is the baseline for monitoring after launch.

### Comms gates

- [ ] Launch announcement post drafted, reviewed by:
      - Engineering lead (technical accuracy).
      - PM (positioning).
      - Legal / compliance (license claims, "vendor agnostic"
        wording, CVE process).
- [ ] Hacker News / Lobsters / Reddit / r/devops / r/sre comment
      drafts prepared. (Don't actually post them yet.)
- [ ] Social copy queued (X, LinkedIn, Mastodon, Bluesky).
- [ ] Honeycomb internal go-to-market materials briefed: who knows
      what, when, and what they should and shouldn't say.
- [ ] At least three external reviewers (people not on the project)
      have seen the announcement post and the getting-started
      guides. Feedback incorporated.

### Operational gates

- [ ] `security@<domain>` mailbox monitored, with an on-call rotation
      defined for the first 48 hours.
- [ ] GitHub Issues triaged regularly: an "issue intake" rotation
      defined for the first two weeks.
- [ ] CI uptime monitored: if `main` is red for >1 hour during
      launch week, paged. (We'll get a flood of issues — a red
      pipeline tells the wrong story to drive-by visitors.)
- [ ] A status page (or at minimum a public README badge) reflects
      the most recent CI run.

### Telemetry / feedback

- [ ] Post-install opt-in telemetry (usage / version) — either
      shipped and clearly disclosed in `conduit.yaml` defaults, or
      explicitly **not** shipped (V0 default). State the choice in
      the announcement.
- [ ] Issue templates ask the questions the on-call needs to triage
      fast: OS, install method, `conduit doctor --json` output,
      redacted `conduit.yaml`.

## T-day (launch day)

### Sequence

1. **08:00 local** — final CI green check on `main`. If red, postpone.
2. **09:00** — cut the release tag, push to GitHub. Watch the
   `release.yml` workflow:
   - goreleaser build (~5 min)
   - container image push + cosign sign (~3 min)
   - helm chart push + cosign sign (~2 min)
   - SBOM upload (~1 min)
3. **09:15** — verify artifacts:
   - Pull a deb from the release URL, install on a test VM.
   - Pull the helm chart, install on a test cluster.
   - Verify cosign signatures on the container image.
4. **09:30** — flip the repo from private to public (if it wasn't
   already), or remove the "internal preview" banner.
5. **10:00** — publish the announcement post. Post links to:
   - HN / Lobsters / Reddit / Mastodon / Bluesky / X / LinkedIn.
   - Honeycomb's blog / community Slack.
6. **10:30** — start the on-call rotation watching:
   - GitHub Issues / Discussions
   - `security@<domain>`
   - Hacker News thread (most-asked-question signal)
   - Twitter / Bluesky mentions

### Fire-watch checklist

- [ ] On-call has the runbook (`docs/release/runbook.md`) open in
      one tab.
- [ ] On-call has the ["common-issues"](../troubleshooting/common-issues.md)
      and ["cdt-codes"](../troubleshooting/cdt-codes.md) tabs open.
- [ ] CI is green throughout launch day. If a community PR breaks
      it, revert + re-merge after launch.
- [ ] No new PRs merged during launch day except hotfixes signed off
      by engineering lead.

## T+1 day (the day after)

- [ ] Triage the overnight issue queue. Tag with milestones; nothing
      is `now` unless it's a CVE or "agent crashes on install."
- [ ] Read every comment on every social channel. Capture themes —
      what people loved, what confused them — into a launch
      retrospective doc.
- [ ] Author a "Day 1 patch" if any CDT codes are misfiring or any
      install path is broken on a platform we claimed to support.
- [ ] Update the demo script with anything that landed differently
      live than in rehearsal.

## T+1 week

- [ ] Launch retrospective with everyone who shipped V0. Standard
      "what went well / what didn't / what to change" with concrete
      owners and dates.
- [ ] Decide V1 milestones. The deferred-from-V0 items
      (M11/M12 follow-ups: AWS smoke, signed MSI, runtime doctor
      checks, k8s permissions check) are the obvious starting point.
      Prioritize against actual user feedback from the launch.
- [ ] Schedule the V0.1 release (probably 2-3 weeks out) with the
      bugs filed during launch week as the changelog.
- [ ] Ensure the public CHANGELOG is up to date.

## Definition of "launch is done"

We declare V0 successfully launched when:

- The release artifacts are public and downloadable.
- The four getting-started guides verifiably work on the four
  platforms.
- The launch announcement is published.
- An external user (not on the project) has gone from zero to
  "data in Honeycomb" using only the public docs.
- No critical bugs are open against the launch build.

Anything else is V1+.
