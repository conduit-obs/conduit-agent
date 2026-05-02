# AWS deployment recipes (M7)

Field-engineer-grade walkthroughs for getting Conduit running in the four AWS shapes that cover ~95% of telemetry-collection conversations:

| Recipe | What it deploys | Workload shape | Page |
|---|---|---|---|
| **EC2** | Conduit as a systemd unit on a Linux EC2 instance | One-agent-per-host, including auto-scaling groups | [`ec2.md`](ec2.md) |
| **ECS** | Conduit as a sidecar container in an ECS task (Fargate or EC2 launch type) | Per-task agent, OTLP from app on `localhost` | [`ecs.md`](ecs.md) |
| **EKS** | Conduit's Helm chart on EKS with IRSA for credentials | DaemonSet per node, IRSA-bound IAM | [`eks.md`](eks.md) |
| **Lambda** | (explicit non-deliverable) Use the upstream OTel Lambda Layer | Function-scoped tracing | [`lambda.md`](lambda.md) |

Each recipe is **end-to-end runnable on a fresh AWS account**: spin up the prerequisites, paste the snippet, and you should see telemetry land in your Honeycomb sandbox within ~5 minutes. The field-success bar is "30 minutes from `aws configure` to a populated dashboard", which is what M7's acceptance criteria measure.

## Common conventions

All four recipes share a few moving parts:

- **API-key storage**: AWS Systems Manager Parameter Store (`SecureString` parameter under `/conduit/<env>/honeycomb-api-key`) is the recommended default — it's free for standard parameters, KMS-encrypted at rest, and every AWS workload runtime (EC2 user-data, ECS task secrets, EKS IRSA-bound roles) can read it without extra integration. Each recipe shows the IAM policy slice that grants `ssm:GetParameter` (and `kms:Decrypt` when a customer CMK is in play). Secrets Manager works too — there's a one-line swap noted in every recipe — but Parameter Store wins on cost for plain ingest keys.
- **Conduit version pinning**: every recipe pins to a release tag (e.g. `v0.0.1`) rather than `latest`. AWS-side blast radius for "the agent updated underneath us during a deploy" is bigger than on a developer laptop, so pinned-version bootstraps are the default and "auto-upgrade to the latest GA" is documented as a deliberate opt-in.
- **`conduit doctor` validation**: every recipe ends with a `conduit doctor` invocation (`aws ssm send-command` for EC2, `aws ecs execute-command` for ECS, `kubectl exec` for EKS). `conduit doctor` lands in M11; until then the recipes use the equivalent manual `curl http://127.0.0.1:13133/` health probe and a log tail showing the OTel Collector's startup messages.
- **Region awareness**: every recipe defaults to `us-east-1` because Honeycomb's free-tier endpoint lives there (`api.honeycomb.io`). For Honeycomb EU customers, switch to `api.eu1.honeycomb.io` via the `output.honeycomb.endpoint` knob (M10) — every recipe shows where to set it.

## What about other clouds?

GCP and Azure recipes are V1 work. The Conduit-side configuration is identical (a `conduit.yaml` referencing `${env:HONEYCOMB_API_KEY}`); the cloud-side wiring is materially different (GCP uses Secret Manager + Workload Identity; Azure uses Key Vault + Managed Identity), and rather than ship half-baked recipes for every cloud at V0 we picked the one with the most existing customer demand and committed to doing it well. Field engineers have used [`ec2.md`](ec2.md) as a template for ad-hoc GCE / Azure VM walkthroughs in customer engagements; the structure transfers cleanly.
