import type { WizardState } from "../../types";
import { type CommandBlock, shellQuote } from "./shared";

export function k8sBlocks(state: WizardState): CommandBlock[] {
  const sets: string[] = [];
  if (state.destination === "honeycomb" && state.ingestKey) {
    sets.push(`--set honeycomb.apiKey=${shellQuote(state.ingestKey)}`);
  }
  if (state.deploymentEnvironment.trim()) {
    sets.push(
      `--set conduit.deploymentEnvironment=${shellQuote(state.deploymentEnvironment.trim())}`,
    );
  }
  if (state.serviceName.trim()) {
    sets.push(
      `--set conduit.serviceName=${shellQuote(state.serviceName.trim())}`,
    );
  }
  if (state.collect.has("obi_zero_code")) {
    sets.push("--set obi.enabled=true");
  }
  const setLine = sets.length ? ` \\\n  ${sets.join(" \\\n  ")}` : "";
  return [
    {
      title: "1. Install with Helm",
      description:
        "The chart is published to GHCR as an OCI artifact (cosign-signed). It deploys a DaemonSet running one agent pod per node with kubeletstats + filelog/k8s + k8sattributes pre-wired.",
      body: `helm install conduit \\
  oci://ghcr.io/conduit-obs/charts/conduit-agent \\
  --version 0.0.x \\
  --namespace conduit --create-namespace${setLine}

kubectl -n conduit rollout status ds/conduit-conduit-agent --timeout=120s`,
      lang: "bash",
    },
    {
      title: "2. Verify",
      description: "Run the doctor inside one of the daemonset pods.",
      body: `kubectl -n conduit logs -l app.kubernetes.io/name=conduit-agent --tail=200
kubectl -n conduit exec -i ds/conduit-conduit-agent -- \\
  conduit doctor -c /etc/conduit/conduit.yaml`,
      lang: "bash",
    },
  ];
}
