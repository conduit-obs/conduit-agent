# Deploy Conduit on AWS ECS

> **Audience**: a field engineer or platform engineer running services on ECS (Fargate or EC2 launch type) who wants OTLP-compatible apps to send telemetry through a per-task Conduit sidecar. Goal: telemetry from one task landing in Honeycomb within 30 minutes.

## What you'll deploy

A Conduit container running as a **sidecar** alongside your application container in the same ECS task. The application sends OTLP to `localhost:4317` (or `:4318`); Conduit batches, applies the standard processor chain (memory limiter → resource detection → transform/logs → batch), and forwards to Honeycomb.

```text
+----------------- ECS task ------------------+
|                                             |
|  +----------------+    +-----------------+  |
|  |  app           |    |  conduit        |  |
|  |  --container-- |    |  --container--  |  |     OTLP/HTTP
|  |                |    |                 |  |   +-------------+
|  |  OTLP exporter |--->| :4317 (gRPC)    |--+-->|  Honeycomb  |
|  |  -> 127.0.0.1  |    | :4318 (HTTP)    |  |   +-------------+
|  +----------------+    | :13133 (health) |  |
|                        +-----------------+  |
+---------------------------------------------+
```

**Why a sidecar** rather than a daemon task or a separate cluster service?

- **Locality**: OTLP traffic stays on the task's loopback — zero network cost, zero hops, no service-discovery story to maintain.
- **Scale-with-app**: the sidecar's resource limits scale linearly with the app's pod count. No "where does the central collector live and what's its capacity headroom" capacity-planning conversation.
- **Failure isolation**: a misconfigured app can't poison the collector for other apps; if the sidecar dies, only its task's telemetry stops.
- **Fargate-compatible**: no host-mount or DaemonSet concept on Fargate; the sidecar is the only model that works there.

The Conduit V0 ECS recipe **does not** ship a daemon-task pattern. The sidecar pattern is the default; if you have a specific reason to centralize (e.g. you want one-place tail sampling — wait for V2 + Refinery), the M10 docs cover the gateway flavor.

## Prerequisites

- AWS CLI v2 configured against an account where you can create IAM roles + ECS task definitions.
- An ECS cluster (Fargate or EC2 launch type — the recipe runs on both).
- An ECR registry or other public-pull permission for `ghcr.io/conduit-obs/conduit-agent` (Fargate's container runtime can pull from `ghcr.io` directly with no extra config).
- A Honeycomb ingest key stored in Secrets Manager:

  ```bash
  aws secretsmanager create-secret \
    --name 'conduit/prod/honeycomb-api-key' \
    --secret-string "$HONEYCOMB_API_KEY"
  ```

  Parameter Store is also fine (and cheaper); the snippet below uses Secrets Manager because ECS's `secrets:` block on container definitions has clean semantics for both stores. Using Parameter Store: replace `valueFrom: arn:...:secret:...` with the parameter ARN; ECS handles either transparently.

## Step 1 — IAM roles

ECS tasks need two roles:

- **Execution role** (`taskExecutionRoleArn`): ECS uses this to pull images, write CloudWatch logs, and resolve secrets at task start. Standard pattern: `AmazonECSTaskExecutionRolePolicy` + a customer-policy slice for the secrets the task references.
- **Task role** (`taskRoleArn`): the in-container processes assume this. Conduit doesn't need any AWS-side permissions in V0 — telemetry leaves over OTLP/HTTPS, not via AWS APIs — so the task role can be empty (or scoped to whatever your *application* needs).

`conduit-task-execution-policy.json`:

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Sid": "ResolveHoneycombApiKey",
      "Effect": "Allow",
      "Action": "secretsmanager:GetSecretValue",
      "Resource": "arn:aws:secretsmanager:us-east-1:111122223333:secret:conduit/prod/honeycomb-api-key-*"
    }
  ]
}
```

(The `-*` suffix is required: Secrets Manager appends a six-character suffix to every secret ARN, and the policy must match against the prefix to grant access regardless of revision.)

```bash
aws iam create-policy \
  --policy-name conduit-ecs-execution-secrets \
  --policy-document file://conduit-task-execution-policy.json

aws iam create-role \
  --role-name conduit-ecs-task-execution \
  --assume-role-policy-document '{
    "Version":"2012-10-17",
    "Statement":[{"Effect":"Allow","Principal":{"Service":"ecs-tasks.amazonaws.com"},"Action":"sts:AssumeRole"}]
  }'

aws iam attach-role-policy \
  --role-name conduit-ecs-task-execution \
  --policy-arn arn:aws:iam::aws:policy/service-role/AmazonECSTaskExecutionRolePolicy

aws iam attach-role-policy \
  --role-name conduit-ecs-task-execution \
  --policy-arn arn:aws:iam::111122223333:policy/conduit-ecs-execution-secrets
```

For `taskRoleArn`, reuse whatever your application already needs; Conduit itself has no requirement.

## Step 2 — Task definition

Two containers in the same task: your app and the Conduit sidecar. The app sends OTLP to `127.0.0.1` because the containers share a loopback in the task's network namespace (true for both Fargate's `awsvpc` mode and EC2 `awsvpc`).

`conduit-task-definition.json`:

```json
{
  "family": "myapp-with-conduit",
  "networkMode": "awsvpc",
  "requiresCompatibilities": ["FARGATE"],
  "cpu": "512",
  "memory": "1024",

  "executionRoleArn": "arn:aws:iam::111122223333:role/conduit-ecs-task-execution",
  "taskRoleArn":      "arn:aws:iam::111122223333:role/myapp-task-role",

  "containerDefinitions": [
    {
      "name": "app",
      "image": "111122223333.dkr.ecr.us-east-1.amazonaws.com/myapp:1.2.3",
      "essential": true,
      "environment": [
        { "name": "OTEL_EXPORTER_OTLP_ENDPOINT", "value": "http://127.0.0.1:4318" },
        { "name": "OTEL_SERVICE_NAME",            "value": "myapp" },
        { "name": "OTEL_RESOURCE_ATTRIBUTES",     "value": "deployment.environment=production" }
      ],
      "dependsOn": [
        {
          "containerName": "conduit",
          "condition": "HEALTHY"
        }
      ],
      "logConfiguration": {
        "logDriver": "awslogs",
        "options": {
          "awslogs-group": "/ecs/myapp",
          "awslogs-region": "us-east-1",
          "awslogs-stream-prefix": "myapp"
        }
      }
    },
    {
      "name": "conduit",
      "image": "ghcr.io/conduit-obs/conduit-agent:0.0.1",
      "essential": true,

      "portMappings": [
        { "containerPort": 4317, "protocol": "tcp" },
        { "containerPort": 4318, "protocol": "tcp" },
        { "containerPort": 13133, "protocol": "tcp" }
      ],

      "environment": [
        { "name": "CONDUIT_SERVICE_NAME",            "value": "myapp-collector" },
        { "name": "CONDUIT_DEPLOYMENT_ENVIRONMENT",  "value": "production" }
      ],

      "secrets": [
        {
          "name": "HONEYCOMB_API_KEY",
          "valueFrom": "arn:aws:secretsmanager:us-east-1:111122223333:secret:conduit/prod/honeycomb-api-key"
        }
      ],

      "healthCheck": {
        "command": ["CMD", "wget", "--quiet", "--spider", "http://127.0.0.1:13133/"],
        "interval": 10,
        "timeout":  5,
        "retries":  3,
        "startPeriod": 15
      },

      "logConfiguration": {
        "logDriver": "awslogs",
        "options": {
          "awslogs-group": "/ecs/conduit",
          "awslogs-region": "us-east-1",
          "awslogs-stream-prefix": "conduit"
        }
      }
    }
  ]
}
```

Three things to call out:

1. **`dependsOn` on the app container**: the app waits for Conduit to report `HEALTHY` (via the health-check endpoint at `:13133/`) before starting. This prevents the app from emitting telemetry into a connection-refused void during the first ~10 seconds of task startup.
2. **`HONEYCOMB_API_KEY` via `secrets`**: ECS resolves the secret at task-start time and injects it as an environment variable. The Conduit conduit.yaml inside the image references it as `${env:HONEYCOMB_API_KEY}`. The execution role (Step 1) grants the `secretsmanager:GetSecretValue` permission.
3. **No persistent volume**: Conduit V0 doesn't write anything that needs to survive task replacement. The persistent OTLP queue (filestorage) lands in M10; until then, OTLP failures are dropped after the in-memory retry budget. For ECS workloads that's typically fine — failed batches are re-emitted by the SDK on the next push.

Register and run:

```bash
aws ecs register-task-definition --cli-input-json file://conduit-task-definition.json

aws ecs run-task \
  --cluster my-cluster \
  --task-definition myapp-with-conduit \
  --launch-type FARGATE \
  --network-configuration 'awsvpcConfiguration={subnets=[subnet-xxxx],securityGroups=[sg-xxxx],assignPublicIp=ENABLED}'
```

For long-running services, point an `aws ecs create-service` at the same task definition.

## Step 3 — Verify

```bash
TASK_ARN=$(aws ecs list-tasks --cluster my-cluster --family myapp-with-conduit --query 'taskArns[0]' --output text)

# Check both containers' status
aws ecs describe-tasks --cluster my-cluster --tasks "$TASK_ARN" \
  --query 'tasks[0].containers[].{name:name,status:lastStatus,health:healthStatus}'

# Tail the conduit logs from CloudWatch
aws logs tail /ecs/conduit --follow --since 5m

# Exec into the conduit sidecar to run the health probe (M11 conduit doctor lands here):
aws ecs execute-command \
  --cluster my-cluster --task "$TASK_ARN" \
  --container conduit --command "wget --quiet -O- http://127.0.0.1:13133/" --interactive
```

You should see:

- Both containers `RUNNING`, conduit's `healthStatus=HEALTHY`.
- CloudWatch logs from conduit reporting `Everything is ready. Begin running and processing data.`.
- Within ~5 minutes, your app's traces / metrics / logs visible in the configured Honeycomb dataset, with `service.name=myapp` (or whatever the OTel SDK sets).

`aws ecs execute-command` requires the cluster to be set up with ECS Exec — see [the AWS docs](https://docs.aws.amazon.com/AmazonECS/latest/developerguide/ecs-exec.html) for the one-time setup of the SSM-managed-agent + IAM permissions.

## Common variations

### Use Parameter Store instead of Secrets Manager

```json
"secrets": [
  {
    "name": "HONEYCOMB_API_KEY",
    "valueFrom": "arn:aws:ssm:us-east-1:111122223333:parameter/conduit/prod/honeycomb-api-key"
  }
]
```

…and replace the IAM policy with:

```json
{
  "Sid": "ResolveHoneycombApiKey",
  "Effect": "Allow",
  "Action": "ssm:GetParameters",
  "Resource": "arn:aws:ssm:us-east-1:111122223333:parameter/conduit/prod/honeycomb-api-key"
}
```

(Note: ECS uses `ssm:GetParameters` plural for the secrets-injection flow, not `ssm:GetParameter` like the EC2 user-data path.)

### Override the in-image conduit.yaml

The default in-image config (`/etc/conduit/conduit.yaml`) sets `profile.mode: docker`, which binds OTLP receivers to `0.0.0.0:4317` / `:4318` so the sidecar is reachable on the task's loopback. To override (e.g. add an `overrides:` block, switch output endpoints, set custom resource attributes), bake your own image:

```dockerfile
FROM ghcr.io/conduit-obs/conduit-agent:0.0.1
COPY conduit.yaml /etc/conduit/conduit.yaml
```

…and reference your image in the task definition. **Don't** mount a config file from a host path on Fargate — Fargate doesn't expose a persistent host filesystem; bind-mounted configs only work on EC2 launch type.

### Honeycomb EU endpoint

Override the endpoint in your custom image's `conduit.yaml`:

```yaml
output:
  mode: honeycomb
  honeycomb:
    api_key: ${env:HONEYCOMB_API_KEY}
    endpoint: https://api.eu1.honeycomb.io
```

(The `endpoint` knob lands in M10's output-mode work.)

### Multiple apps sharing one Conduit

Don't. The whole point of the sidecar pattern is per-task isolation. If you have N apps in the same task, that's a single Conduit sidecar serving N peer containers — that's still one task, still one sidecar, that's fine. If you have N apps in N tasks, each task has its own Conduit. The "centralize them on a daemon task" anti-pattern is what we explicitly chose not to ship at V0.

## See also

- [`docs/deploy/aws/README.md`](README.md) — common conventions across the four AWS recipes.
- [`deploy/docker/README.md`](../../../deploy/docker/README.md) — what the `ghcr.io/conduit-obs/conduit-agent` image contains and how its default config drives the sidecar shape.
- [`internal/profiles/docker/README.md`](../../../internal/profiles/docker/README.md) — what `profile.mode: docker` loads (no host metrics by default in the sidecar shape — Conduit on ECS Fargate has no /hostfs to scrape).
- [`docs/adr/adr-0007.md`](../../adr/adr-0007.md) — `output.mode` rationale.
