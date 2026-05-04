import { useMemo } from "react";
import { StepCard, StepNav } from "../components/StepCard";
import { CodeBlock } from "../components/CodeBlock";
import { generateInstallCommands } from "../lib/commands";
import type { WizardState } from "../types";

export function InstallStep({
  state,
  back,
  next,
}: {
  state: WizardState;
  back: () => void;
  next: () => void;
}) {
  const blocks = useMemo(() => generateInstallCommands(state), [state]);

  return (
    <StepCard
      eyebrow="Step 6 of 8"
      title="Run these commands."
      intro="We've assembled exactly what to run on your host. Each block has a one-click copy. After the install, the verify step proves data is flowing."
    >
      <Summary state={state} />

      {blocks.map((b, i) => (
        <div key={i} className="space-y-2">
          <div>
            <h3 className="text-base font-semibold text-slate-900">
              {b.title}
            </h3>
            {b.description ? (
              <p className="text-sm text-slate-600 mt-1 leading-relaxed">
                {b.description}
              </p>
            ) : null}
          </div>
          <CodeBlock body={b.body} lang={b.lang} />
        </div>
      ))}

      <div className="rounded-lg border border-emerald-200 bg-emerald-50 p-5 text-sm text-emerald-900">
        <strong className="font-semibold">Once the verify step is green</strong>{" "}
        — meaning <code>conduit doctor</code> exits 0 and you're seeing
        events in your destination — come back here. The next step is
        optional: importing a starter dashboard into Honeycomb.
      </div>

      <StepNav
        back={back}
        next={next}
        nextLabel={
          state.destination === "honeycomb"
            ? "Import a board →"
            : "Skip to wrap-up →"
        }
      />
    </StepCard>
  );
}

function Summary({ state }: { state: WizardState }) {
  const lines: { label: string; value: string }[] = [
    { label: "Platform", value: state.platform ?? "—" },
    {
      label: "Collecting",
      value: Array.from(state.collect)
        .map((c) =>
          c
            .split("_")
            .map((w) => w.charAt(0).toUpperCase() + w.slice(1))
            .join(" "),
        )
        .join(", "),
    },
    {
      label: "Destination",
      value:
        state.destination === "honeycomb"
          ? `Honeycomb (${state.honeycombEndpoint})`
          : `OTLP → ${state.otlpEndpoint || "(not set)"}`,
    },
    {
      label: "Identity",
      value: `${state.serviceName || "(profile default)"} / ${state.deploymentEnvironment || "production"}`,
    },
  ];
  return (
    <div className="rounded-lg border border-slate-200 bg-slate-50 p-4">
      <div className="text-xs font-semibold uppercase tracking-wider text-slate-500 mb-2">
        Your config
      </div>
      <dl className="grid sm:grid-cols-2 gap-x-6 gap-y-1.5 text-sm">
        {lines.map((l) => (
          <div key={l.label} className="flex gap-3">
            <dt className="text-slate-500 min-w-24">{l.label}</dt>
            <dd className="text-slate-900 font-medium break-all">{l.value}</dd>
          </div>
        ))}
      </dl>
    </div>
  );
}
