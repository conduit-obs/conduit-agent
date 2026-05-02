# Deploy Conduit on AWS EC2

> **Audience**: a field engineer or platform engineer with a fresh AWS account and `aws configure` set up. Goal: telemetry from a Linux EC2 instance landing in Honeycomb within 30 minutes.

## What you'll deploy

A Conduit agent running as a systemd service on a Linux EC2 instance, scraping host metrics + journald + filelog and shipping OTLP/HTTP to Honeycomb. The Honeycomb ingest key lives in AWS Systems Manager Parameter Store as a `SecureString`; the EC2 instance reads it on boot via an attached IAM role (no API keys baked into AMIs or user-data scripts).

```text
                                    +-------------------------+
                                    | EC2 instance (Linux)    |
   +--------------------------+     |                         |
   | SSM Parameter Store      |     |  systemd: conduit       |
   |  /conduit/prod/api-key   |<----|  reads ${env:HONEYCOMB_ |
   |  (SecureString, KMS)     |     |    API_KEY} on start    |
   +--------------------------+     |                         |
                                    |  journald + hostmetrics |
                                    |  +--+-----+----------+  |
                                    +-----|-----|----------+--+
                                          v     v
                                  +--------------------+
                                  |  Honeycomb         |
                                  |  api.honeycomb.io  |
                                  +--------------------+
```

## Prerequisites

- AWS CLI v2 configured against an account where you can create IAM policies, EC2 instances, and SSM parameters.
- A Honeycomb ingest key (an `hcaik_…` token from `Honeycomb → Account → Team Settings → API Keys`). The EC2 region doesn't have to match the Honeycomb region, but US accounts should send to `api.honeycomb.io` (the default) and EU accounts to `api.eu1.honeycomb.io`.
- A VPC with a subnet that has outbound internet access on TCP/443 (NAT gateway, IGW, or PrivateLink to Honeycomb's VPC endpoint — that last option lands in M10's docs).

## Step 1 — Store the Honeycomb API key in Parameter Store

```bash
aws ssm put-parameter \
  --name '/conduit/prod/honeycomb-api-key' \
  --type SecureString \
  --value "$HONEYCOMB_API_KEY" \
  --description 'Honeycomb ingest key consumed by the conduit agent on EC2'
```

The default `alias/aws/ssm` KMS key is fine for non-regulated workloads. Customers with a KMS-CMK requirement add `--key-id alias/<cmk>` and adjust the IAM policy in Step 2 to grant `kms:Decrypt` against that CMK.

Cost note: standard SSM parameters under 4KB are free; the `Advanced` tier ($0.05/parameter/month) is only required if you want larger values or expirations.

## Step 2 — Create the IAM instance profile

The instance only needs two permissions: read the parameter, and (if using a CMK) decrypt with the encryption key. **No** `ec2:`, `s3:`, or `cloudwatch:` permissions are required — Conduit ships telemetry over OTLP/HTTPS to Honeycomb, not via AWS-native services.

`conduit-instance-policy.json`:

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Sid": "ReadHoneycombApiKeyParameter",
      "Effect": "Allow",
      "Action": "ssm:GetParameter",
      "Resource": "arn:aws:ssm:us-east-1:111122223333:parameter/conduit/prod/honeycomb-api-key"
    }
  ]
}
```

Replace the account ID + region. Then:

```bash
aws iam create-policy \
  --policy-name conduit-ec2-read-api-key \
  --policy-document file://conduit-instance-policy.json

aws iam create-role \
  --role-name conduit-ec2 \
  --assume-role-policy-document '{
    "Version":"2012-10-17",
    "Statement":[{"Effect":"Allow","Principal":{"Service":"ec2.amazonaws.com"},"Action":"sts:AssumeRole"}]
  }'

aws iam attach-role-policy \
  --role-name conduit-ec2 \
  --policy-arn arn:aws:iam::111122223333:policy/conduit-ec2-read-api-key

aws iam create-instance-profile --instance-profile-name conduit-ec2
aws iam add-role-to-instance-profile \
  --instance-profile-name conduit-ec2 \
  --role-name conduit-ec2
```

If you want to manage Conduit via Session Manager (recommended over SSH), additionally attach the AWS-managed `AmazonSSMManagedInstanceCore` policy. That gives Session Manager + Patch Manager access; it does **not** grant the EC2 instance any extra Conduit-relevant permissions.

## Step 3 — Bootstrap the agent via user-data

Pick the version you want to pin to (e.g. `v0.0.1`); `latest` is documented but not the default for production launches.

`user-data.sh`:

```bash
#!/bin/bash
set -euo pipefail

CONDUIT_VERSION="0.0.1"
AWS_REGION="${AWS_REGION:-us-east-1}"

# Resolve the Honeycomb API key from SSM at boot time. The instance
# profile attached in Step 2 grants ssm:GetParameter against this exact
# parameter ARN.
HONEYCOMB_API_KEY="$(aws --region "$AWS_REGION" ssm get-parameter \
  --name '/conduit/prod/honeycomb-api-key' \
  --with-decryption \
  --query 'Parameter.Value' --output text)"

# Install Conduit. The script auto-detects deb / rpm and pulls the
# matching package from the GitHub release. A pinned version is used
# to avoid surprise updates during ASG rotations.
curl -fsSL "https://raw.githubusercontent.com/conduit-obs/conduit-agent/v${CONDUIT_VERSION}/scripts/install_linux.sh" \
  | bash -s -- \
      --version "$CONDUIT_VERSION" \
      --api-key "$HONEYCOMB_API_KEY" \
      --service-name "$(hostname)" \
      --deployment-environment "production"

# install_linux.sh writes /etc/conduit/conduit.env and starts the
# systemd unit. Override the auto-derived service.name here if your
# fleet has more useful naming (e.g. role tags from EC2 metadata).
systemctl restart conduit
```

Encode and ship:

```bash
INSTANCE_ID=$(aws ec2 run-instances \
  --image-id ami-0abcdef1234567890 \
  --instance-type t3.small \
  --subnet-id subnet-xxxx \
  --iam-instance-profile Name=conduit-ec2 \
  --user-data file://user-data.sh \
  --tag-specifications 'ResourceType=instance,Tags=[{Key=Role,Value=conduit-test}]' \
  --query 'Instances[0].InstanceId' --output text)
```

For Auto Scaling Groups, the same `user-data.sh` goes into the launch template — every new instance bootstraps idempotently.

## Step 4 — Verify

```bash
aws ssm start-session --target "$INSTANCE_ID"
# inside the instance:
sudo systemctl status conduit
sudo journalctl -u conduit -n 100 --no-pager
curl -fsS http://127.0.0.1:13133/ && echo "conduit health OK"
```

You should see:

- `systemctl status conduit`: active (running).
- Journal lines from the OTel Collector reporting `Everything is ready. Begin running and processing data.`.
- `:13133/` returning HTTP 200.
- Within ~5 minutes, the `system.cpu.utilization`, `system.memory.utilization`, `system.filesystem.utilization`, and `system.disk.io` columns light up in your Honeycomb dataset.

When `conduit doctor` lands (M11) the verification step becomes:

```bash
aws ssm send-command \
  --instance-ids "$INSTANCE_ID" \
  --document-name AWS-RunShellScript \
  --parameters 'commands=["conduit doctor --json"]'
```

…which surfaces the same checks as the manual verification (endpoint reachability, API-key validity, RED dimension projection, cardinality cap headroom) but as a structured JSON report.

## Common variations

### Use Secrets Manager instead of SSM Parameter Store

Swap the `aws ssm get-parameter` call:

```bash
HONEYCOMB_API_KEY="$(aws --region "$AWS_REGION" secretsmanager get-secret-value \
  --secret-id conduit/prod/honeycomb-api-key \
  --query 'SecretString' --output text)"
```

…and replace the IAM policy's `ssm:GetParameter` with `secretsmanager:GetSecretValue` against the secret's ARN. Secrets Manager costs $0.40/secret/month, which adds up for a multi-environment fleet — Parameter Store is the V0 default for that reason.

### Honeycomb EU endpoint

Tell `install_linux.sh` to point to the EU endpoint:

```bash
curl -fsSL https://raw.githubusercontent.com/conduit-obs/conduit-agent/v${CONDUIT_VERSION}/scripts/install_linux.sh \
  | bash -s -- \
      --version "$CONDUIT_VERSION" \
      --api-key "$HONEYCOMB_API_KEY" \
      --honeycomb-endpoint "https://api.eu1.honeycomb.io" \
      --service-name "$(hostname)"
```

(The `--honeycomb-endpoint` flag lands in M10's output-mode work; until then, the EU endpoint is set by editing `/etc/conduit/conduit.yaml` after first install.)

### Air-gapped / VPC-restricted accounts

If the EC2 instance can't reach `api.honeycomb.io` directly:

1. Run a Conduit agent on a NAT-able edge instance and have it forward to Honeycomb (`output.mode: gateway` configured to point at a customer-operated OTLP gateway, with TLS terminating there).
2. Or, use Honeycomb's PrivateLink endpoint (M10 docs).
3. Mirror the GitHub release artifacts to a private S3 bucket and adjust `install_linux.sh` to fetch from S3. The bash script's `--mirror s3://…` knob lands in V1; until then, fork the script.

## Terraform

`main.tf`:

```hcl
resource "aws_ssm_parameter" "honeycomb_api_key" {
  name        = "/conduit/${var.environment}/honeycomb-api-key"
  type        = "SecureString"
  value       = var.honeycomb_api_key
  description = "Honeycomb ingest key consumed by the conduit agent."
}

resource "aws_iam_policy" "conduit_read_api_key" {
  name = "conduit-${var.environment}-read-api-key"

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Sid      = "ReadHoneycombApiKeyParameter"
      Effect   = "Allow"
      Action   = "ssm:GetParameter"
      Resource = aws_ssm_parameter.honeycomb_api_key.arn
    }]
  })
}

resource "aws_iam_role" "conduit_ec2" {
  name = "conduit-${var.environment}"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect    = "Allow"
      Principal = { Service = "ec2.amazonaws.com" }
      Action    = "sts:AssumeRole"
    }]
  })
}

resource "aws_iam_role_policy_attachment" "conduit_read_api_key" {
  role       = aws_iam_role.conduit_ec2.name
  policy_arn = aws_iam_policy.conduit_read_api_key.arn
}

resource "aws_iam_role_policy_attachment" "ssm_managed_instance_core" {
  role       = aws_iam_role.conduit_ec2.name
  policy_arn = "arn:aws:iam::aws:policy/AmazonSSMManagedInstanceCore"
}

resource "aws_iam_instance_profile" "conduit_ec2" {
  name = "conduit-${var.environment}"
  role = aws_iam_role.conduit_ec2.name
}

data "template_file" "user_data" {
  template = file("${path.module}/user-data.sh")
  vars = {
    conduit_version = var.conduit_version
    parameter_name  = aws_ssm_parameter.honeycomb_api_key.name
    aws_region      = var.aws_region
  }
}

resource "aws_launch_template" "conduit" {
  name_prefix   = "conduit-${var.environment}-"
  image_id      = var.ami_id
  instance_type = "t3.small"

  iam_instance_profile {
    name = aws_iam_instance_profile.conduit_ec2.name
  }

  user_data = base64encode(data.template_file.user_data.rendered)
}
```

The launch template plugs straight into an `aws_autoscaling_group`; every replacement instance bootstraps with the pinned version against the parameter resolved at boot.

## See also

- [`docs/deploy/aws/README.md`](README.md) — common conventions across the four AWS recipes.
- [`deploy/linux/README.md`](../../../deploy/linux/README.md) — what `install_linux.sh` does on the host (systemd unit shape, file layout, user/group).
- [`deploy/linux/scripts/`](../../../deploy/linux/scripts/) — the deb / rpm maintainer scripts the install runs.
- [`docs/adr/adr-0007.md`](../../adr/adr-0007.md) — `output.mode` rationale, including how `gateway` lets you funnel through a regional Honeycomb Collector if the instance has constrained egress.
