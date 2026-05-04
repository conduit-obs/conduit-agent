import { StepCard, StepNav } from "../components/StepCard";
import { CheckCard } from "../components/OptionCard";
import type { CollectKind, WizardAction, WizardState } from "../types";

export function CollectStep({
  state,
  dispatch,
  back,
  next,
}: {
  state: WizardState;
  dispatch: (a: WizardAction) => void;
  back: () => void;
  next: () => void;
}) {
  const isLinux = state.platform === "linux";
  const isK8s = state.platform === "k8s";
  const isDocker = state.platform === "docker";

  const items: Array<{
    id: CollectKind;
    title: string;
    description: string;
    badge?: string;
    disabled?: boolean;
  }> = [
    {
      id: "host_metrics",
      title: isK8s ? "Node + kubelet metrics" : "Host metrics",
      description: isK8s
        ? "kubeletstats receiver + per-node hostmetrics. CPU, memory, disk, network for every node + container."
        : "hostmetrics receiver: CPU, memory, disk, network, filesystem, load. Sub-1% CPU at default 60s scrape.",
      disabled: isDocker,
    },
    {
      id: "system_logs",
      title: isK8s ? "Pod + node logs" : "System logs",
      description: isK8s
        ? "filelog/k8s tails /var/log/pods. k8sattributes processor adds pod, namespace, container metadata."
        : "filelog + journald (Linux) / Event Log (Windows) / unified.log (macOS). Auto-detects what's available.",
      disabled: isDocker,
    },
    {
      id: "otlp_app_traces",
      title: "OTLP from your apps",
      description:
        "Receivers on 127.0.0.1:4317 (gRPC) and :4318 (HTTP). Point your application's OTel SDK here and Conduit forwards everything to your destination.",
    },
    {
      id: "obi_zero_code",
      title: "OBI — zero-code application traces",
      description:
        "OpenTelemetry eBPF Instrumentation auto-traces HTTP, gRPC, and database calls without touching your app code. Adds ~30 MB binary size and requires CAP_BPF + a 5.8+ kernel. Off by default.",
      badge: "Linux / k8s only",
      disabled: !isLinux && !isK8s,
    },
  ];

  return (
    <StepCard
      eyebrow="Step 2 of 8"
      title="What do you want to collect?"
      intro="The defaults are sensible — most users keep them as-is. Toggle off anything you don't need; the smaller the agent's footprint, the better."
    >
      <div className="space-y-3">
        {items.map((it) => (
          <CheckCard
            key={it.id}
            selected={state.collect.has(it.id)}
            onToggle={() => dispatch({ type: "TOGGLE_COLLECT", kind: it.id })}
            title={it.title}
            description={it.description}
            badge={it.badge}
            disabled={it.disabled}
          />
        ))}
      </div>
      <StepNav back={back} next={next} />
    </StepCard>
  );
}
