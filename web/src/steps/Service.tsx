import { StepCard, StepNav } from "../components/StepCard";
import {
  PLATFORM_DEFAULT_SERVICE_NAME,
  type WizardAction,
  type WizardState,
} from "../types";

export function ServiceStep({
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
  const fallback = state.platform
    ? PLATFORM_DEFAULT_SERVICE_NAME[state.platform]
    : "linux-host";

  return (
    <StepCard
      eyebrow="Step 5 of 8"
      title="Identify this fleet."
      intro={
        <>
          Conduit tags every signal with{" "}
          <code>service.name</code> and{" "}
          <code>deployment.environment</code>. Keep the defaults for one
          dataset per platform (recommended for your first install) or
          override below.
        </>
      }
    >
      <div className="rounded-xl border border-slate-200 bg-white p-6 space-y-5">
        <div className="space-y-2">
          <label
            htmlFor="serviceName"
            className="text-sm font-semibold text-slate-900"
          >
            service.name
          </label>
          <input
            id="serviceName"
            type="text"
            placeholder={fallback}
            value={state.serviceName}
            onChange={(e) =>
              dispatch({
                type: "SET_FIELD",
                field: "serviceName",
                value: e.target.value,
              })
            }
            className="w-full px-4 py-3 rounded-lg border border-slate-300 bg-white text-sm font-mono focus:outline-2 focus:outline-accent"
          />
          <p className="text-xs text-slate-500 leading-relaxed">
            Leave blank to use{" "}
            <code className="font-mono">{fallback}</code> — the
            profile-shaped default for your platform (per ADR-0021). When
            unset, the agent applies it via{" "}
            <code>action: insert</code>, so any{" "}
            <code>service.name</code> on forwarded application traffic is
            preserved.
          </p>
        </div>

        <div className="space-y-2">
          <label
            htmlFor="depEnv"
            className="text-sm font-semibold text-slate-900"
          >
            deployment.environment
          </label>
          <input
            id="depEnv"
            type="text"
            placeholder="production"
            value={state.deploymentEnvironment}
            onChange={(e) =>
              dispatch({
                type: "SET_FIELD",
                field: "deploymentEnvironment",
                value: e.target.value,
              })
            }
            className="w-full px-4 py-3 rounded-lg border border-slate-300 bg-white text-sm font-mono focus:outline-2 focus:outline-accent"
          />
          <p className="text-xs text-slate-500">
            Stamped on every signal via <code>action: upsert</code>. Common
            values: <code>production</code>, <code>staging</code>,{" "}
            <code>dev</code>.
          </p>
        </div>
      </div>

      <StepNav back={back} next={next} />
    </StepCard>
  );
}
