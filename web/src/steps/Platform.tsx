import { StepCard, StepNav } from "../components/StepCard";
import { OptionCard } from "../components/OptionCard";
import type { Platform, WizardAction, WizardState } from "../types";

const OPTIONS: Array<{
  id: Platform;
  title: string;
  description: string;
  icon: string;
  badge?: string;
}> = [
  {
    id: "linux",
    title: "Linux host",
    description:
      "systemd-managed agent with apt / dnf / yum / pacman package install. Defaults: hostmetrics + journald/syslog + OTLP receiver on :4317/:4318.",
    icon: "🐧",
  },
  {
    id: "k8s",
    title: "Kubernetes",
    description:
      "Helm chart deploys a DaemonSet across all nodes. kubeletstats + filelog/k8s + k8sattributes are pre-wired.",
    icon: "⎈",
  },
  {
    id: "docker",
    title: "Docker host",
    description:
      "docker compose up. The compose file mounts /proc, /sys, and / into /hostfs so host metrics work from inside the container.",
    icon: "🐳",
  },
  {
    id: "darwin",
    title: "macOS",
    description:
      "Tarball install — no packaged installer in V0. You drop the binary on PATH and run it under launchd or a terminal.",
    icon: "",
    badge: "manual",
  },
  {
    id: "windows",
    title: "Windows",
    description:
      "MSI installer registers a Windows service. Defaults: ETW collector, perfcounters, OTLP on :4317/:4318.",
    icon: "🪟",
  },
];

export function PlatformStep({
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
  return (
    <StepCard
      eyebrow="Step 1 of 8"
      title="Where will Conduit run?"
      intro="Pick the host you want to install on. The rest of the wizard tailors itself to your choice — we'll only show options that exist on that platform."
    >
      <div className="space-y-3">
        {OPTIONS.map((opt) => (
          <OptionCard
            key={opt.id}
            selected={state.platform === opt.id}
            onSelect={() => dispatch({ type: "SET_PLATFORM", platform: opt.id })}
            title={opt.title}
            description={opt.description}
            badge={opt.badge}
            icon={
              <span className="text-xl" aria-hidden>
                {opt.icon}
              </span>
            }
          />
        ))}
      </div>
      <StepNav back={back} next={next} nextDisabled={!state.platform} />
    </StepCard>
  );
}
