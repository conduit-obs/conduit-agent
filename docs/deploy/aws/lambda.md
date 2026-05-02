# AWS Lambda — use the upstream OTel Lambda Layer

> **TL;DR**: Conduit V0 deliberately does not ship a Lambda layer. For Lambda telemetry, use the upstream [OpenTelemetry Lambda Layer](https://github.com/open-telemetry/opentelemetry-lambda) directly. This document explains why, what to deploy instead, and what would change in V1+.

## Why no Conduit Lambda layer in V0?

The V0 mission is "OTel Collector distribution that turns `apt-get install` into observable telemetry on Linux / Windows / Docker / Kubernetes." Lambda is a different runtime model:

- **Lifetime**: a Lambda execution environment runs for at most 15 minutes; cold starts re-launch the layer's collector. Conduit's M9 redaction patterns and M8 RED metrics make the most sense over the lifetime of a long-running agent that sees thousands of spans per second; in a Lambda Function with 50 invocations per minute the metric-tee story is dominated by Lambda's own `aws.lambda.duration` / `Init Duration` measurements, not by Conduit-side RED rollups.
- **Constraints**: Lambda layers are size-capped at 250MB unzipped; the Conduit binary (with the embedded collector + every contrib component listed in [`builder-config.yaml`](../../../builder-config.yaml)) is dozens of MB. A Conduit Lambda layer would have to ship a stripped-down component set with different defaults — different enough that you'd be deploying **a different agent** with the Conduit name. That's a footgun.
- **Existing solution**: the upstream OpenTelemetry Lambda project ships a well-maintained, properly-sized layer with the right components for Lambda. The Conduit team's effort is better spent improving how Conduit interoperates with that layer (see "Where Conduit fits in" below) than competing with it.

Concretely: **a customer who asks "where's Conduit on Lambda?" should be told "use the upstream OTel Lambda layer; it's actively maintained and shaped for the runtime."** Field-engineer-shaped honesty wins more conversations than half-shipped solutions.

## What to deploy instead

The upstream OTel Lambda project publishes pre-built layer ARNs for every region; the README has the current list:

- **OTel Lambda layer (auto-instrumentation)**: [`open-telemetry/opentelemetry-lambda`](https://github.com/open-telemetry/opentelemetry-lambda). Adds Java / Node / Python / .NET auto-instrumentation; bundles a small OTel Collector configured via `OTEL_LAMBDA_COLLECTOR_CONFIG`.
- **AWS Distro for OpenTelemetry (ADOT) Lambda layer**: [`aws-observability/aws-otel-lambda`](https://github.com/aws-observability/aws-otel-lambda). The same upstream collector, packaged through AWS's distribution channel.

Both layers ship the OTel Collector internally; the only Conduit-relevant question is "what does the layer's collector forward to?". The answer is **the same Honeycomb endpoint your other workloads send to**, configured via the layer's environment variables.

A minimal Lambda environment configured for Honeycomb:

```text
AWS_LAMBDA_EXEC_WRAPPER          /opt/otel-instrument
OPENTELEMETRY_COLLECTOR_CONFIG_FILE  /var/task/collector.yaml
HONEYCOMB_API_KEY                hcaik_…              # via Secrets Manager / Parameter Store
OTEL_SERVICE_NAME                my-lambda-fn
OTEL_RESOURCE_ATTRIBUTES         deployment.environment=production
```

`/var/task/collector.yaml` (bundled with your function code):

```yaml
receivers:
  otlp:
    protocols:
      grpc:
        endpoint: localhost:4317
      http:
        endpoint: localhost:4318

processors:
  batch:
    timeout: 200ms
    send_batch_size: 32

exporters:
  otlphttp/honeycomb:
    endpoint: https://api.honeycomb.io
    headers:
      x-honeycomb-team: ${env:HONEYCOMB_API_KEY}

service:
  pipelines:
    traces:
      receivers:  [otlp]
      processors: [batch]
      exporters:  [otlphttp/honeycomb]
    logs:
      receivers:  [otlp]
      processors: [batch]
      exporters:  [otlphttp/honeycomb]
```

This config is intentionally close to what Conduit's expander generates for `output.mode: honeycomb` — the same exporter, the same header pattern, the same batch sizing. Customers who later expand to non-Lambda workloads can lift the same `output.mode: honeycomb` shape into `conduit.yaml` and the egress contract is identical.

The Honeycomb docs maintain a [Lambda-specific walkthrough](https://docs.honeycomb.io/getting-data-in/aws/aws-lambda/) that's the authoritative how-to on layer ARN selection, IAM policy, and cold-start instrumentation choices.

## Where Conduit fits in

Even though Conduit isn't *on* the Lambda function, it's still relevant in three places:

1. **Aggregating traffic**: Lambda spans + logs flow over OTLP/HTTP to Honeycomb. If you also run Conduit on EC2 / ECS / EKS, the Lambda telemetry lands in the same dataset under the same `service.name` convention — and queries that span "Lambda function and the EC2 service that calls it" Just Work because both agents emit the same OTel resource shape.
2. **Refinery in front of Honeycomb (V1)**: when Refinery integration ships in M10, Lambda functions can be configured to send through a regional Refinery cluster (running on Conduit-friendly EC2 / EKS) for tail-based sampling. The Lambda layer points at the Refinery endpoint; Refinery applies the same sampling rules across Lambda + EC2 + EKS traffic; Honeycomb sees a coherent sampled stream.
3. **Egress through a Conduit gateway**: a customer with strict Lambda egress controls can point the layer's collector config at a Conduit running as a gateway in the same VPC (`output.mode: gateway` from the Lambda's perspective, with TLS terminating at the gateway). Lambda-to-Conduit-to-Honeycomb keeps the API key in one place (the gateway) and the Lambda environment carries no Honeycomb credentials.

Patterns 2 and 3 are the actual Conduit-on-Lambda story. Both are V0-shippable today using the upstream layer + a Conduit deployment elsewhere.

## What might change in V1+

- **A first-class Conduit Lambda layer** is plausible if field engineers report consistent friction shaping the upstream layer's config to match Conduit's defaults (e.g. customers want the M9.B redaction patterns applied to Lambda log records). The bar is "it does meaningfully more than the upstream layer with Honeycomb-shaped defaults," not "we want the brand on it."
- **Lambda-aware Refinery sampling rules** as part of M10 / V1, so the Refinery + Conduit pairing handles the cold-start vs warm-invocation duration distribution sensibly without per-team tuning.
- **A Conduit Lambda Extension** (different mechanism than a layer; runs alongside the function process and forwards telemetry) — viable if customer demand grows past what the upstream layer covers. V2 territory at the earliest.

If you're hitting the limits of the upstream layer + Honeycomb today, the [`docs/adr/`](../../adr/) directory and the conduit-agent issue tracker are the right places to land that signal — concrete customer cases drive whether a Conduit Lambda artifact moves up the roadmap.

## See also

- [`docs/deploy/aws/README.md`](README.md) — the other three AWS recipes (EC2 / ECS / EKS), where Conduit *does* run.
- [`docs/deploy/aws/ec2.md`](ec2.md) — the gateway-on-EC2 deployment pattern that pairs with Lambda's upstream layer for the "Lambda → Conduit gateway → Honeycomb" pattern.
- [`docs/adr/adr-0007.md`](../../adr/adr-0007.md) — `output.mode: gateway` rationale.
- [`open-telemetry/opentelemetry-lambda`](https://github.com/open-telemetry/opentelemetry-lambda) — the upstream layer this page recommends.
