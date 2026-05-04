import { StepCard, StepNav } from "../components/StepCard";
import { OptionCard } from "../components/OptionCard";
import type { WizardAction, WizardState } from "../types";

export function DestinationStep({
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
      eyebrow="Step 3 of 8"
      title="Where should the data go?"
      intro="Conduit can ship to any OTLP backend. We have a fast path for Honeycomb because that's our default; everything else is a few extra lines of YAML."
    >
      <div className="space-y-3">
        <OptionCard
          selected={state.destination === "honeycomb"}
          onSelect={() =>
            dispatch({ type: "SET_DESTINATION", destination: "honeycomb" })
          }
          title="Honeycomb"
          description="One-flag setup. Optional bonus: import a pre-built dashboard at the last step."
          icon={<span className="text-xl">🍯</span>}
        />
        <OptionCard
          selected={state.destination === "otlp_generic"}
          onSelect={() =>
            dispatch({ type: "SET_DESTINATION", destination: "otlp_generic" })
          }
          title="Generic OTLP endpoint"
          description="For Datadog OTLP, Grafana Cloud, New Relic, self-hosted Tempo / Loki / Mimir, or your own collector. You'll provide endpoint + headers."
          icon={<span className="text-xl">📡</span>}
        />
      </div>
      <StepNav back={back} next={next} />
    </StepCard>
  );
}
