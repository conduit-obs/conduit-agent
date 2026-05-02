# Conduit documentation

The complete documentation set for the Conduit agent. Skim the
sections below; each links into the canonical doc for that topic.

## Get going

- [**Getting started — Linux**](getting-started/linux.md) — 15
  minutes from a fresh Ubuntu / RHEL / Amazon Linux box to data in
  Honeycomb.
- [**Getting started — Docker**](getting-started/docker.md) — the
  same agent running as a container, with the bind-mount + `--pid=host`
  recipe for real host metrics.
- [**Getting started — Kubernetes**](getting-started/kubernetes.md) —
  the Helm chart, `k8sattributes` enrichment, RBAC, and the
  per-namespace agent Service.
- [**Getting started — Windows**](getting-started/windows.md) —
  unattended PowerShell install, Windows Service registration, and
  the Windows Event Log integration.

## Reference

- [**Configuration reference**](reference/configuration.md) — the
  complete `conduit.yaml` schema with every field, default, and
  validation rule.
- [**Architecture overview**](architecture/overview.md) — what runs
  inside the agent, how the expander composes upstream collector
  YAML from `conduit.yaml`, and what each pipeline component does.

## Operate

- [**Troubleshooting index**](troubleshooting/index.md) — the
  symptoms-to-CDT-codes cheat sheet and the first-response command
  list.
- [**Common issues**](troubleshooting/common-issues.md) — symptom-
  driven walkthroughs ("no data in Honeycomb", "agent won't start",
  "high memory").
- [**CDT0xxx codes**](troubleshooting/cdt-codes.md) — the canonical
  fix doc for every check `conduit doctor` runs.

## Deploy on AWS

Single-purpose recipes per AWS shape. See
[`docs/deploy/aws/`](deploy/aws/README.md):

- [`ec2.md`](deploy/aws/ec2.md) — systemd on a Linux EC2 instance,
  IAM instance profile, SSM Parameter Store, Terraform module.
- [`ecs.md`](deploy/aws/ecs.md) — sidecar pattern for Fargate /
  ECS-on-EC2, with `dependsOn: HEALTHY` and Secrets Manager.
- [`eks.md`](deploy/aws/eks.md) — Helm chart with IRSA via
  `eksctl create iamserviceaccount`.
- [`lambda.md`](deploy/aws/lambda.md) — explicit non-deliverable
  with the rationale and where Conduit *does* still fit.

## Architecture decisions

The full ADR set under [`docs/adr/`](adr/) — every decision that
locks V0's shape, with alternatives and consequences. Read in order
for the build doctrine.

## Release engineering

- [**Runbook**](release/runbook.md) — end-to-end "git tag →
  published artifacts → docs site" workflow.
- [**Compatibility matrix**](release/compatibility.md) — Conduit ↔
  upstream OTel Collector core version policy.
- [**Launch checklist**](release/launch-checklist.md) — the go/no-go
  contract for cutting V0.

## Demo

- [**Demo script**](demo/script.md) — the rehearsable 30-minute
  walkthrough that takes a brand-new viewer from "what is this" to
  "I see how to operate it".
