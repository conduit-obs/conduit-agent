import { useReducer, useEffect } from "react";
import { Shell } from "./components/Shell";
import { Welcome } from "./steps/Welcome";
import { PlatformStep } from "./steps/Platform";
import { CollectStep } from "./steps/Collect";
import { DestinationStep } from "./steps/Destination";
import { ApiKeyStep } from "./steps/ApiKey";
import { ServiceStep } from "./steps/Service";
import { InstallStep } from "./steps/Install";
import { BoardStep } from "./steps/Board";
import { DoneStep } from "./steps/Done";
import {
  initialState,
  STEP_ORDER,
  stepIndex,
  wizardReducer,
  type WizardStep,
} from "./types";

export function App() {
  const [state, dispatch] = useReducer(wizardReducer, initialState);

  // Smooth-scroll to the top whenever the step changes — keeps the
  // experience guided instead of jumping the viewport mid-page.
  useEffect(() => {
    window.scrollTo({ top: 0, behavior: "smooth" });
  }, [state.step]);

  const goto = (step: WizardStep) => dispatch({ type: "GO", step });
  const next = () => {
    const idx = stepIndex(state.step);
    const nextStep = STEP_ORDER[Math.min(idx + 1, STEP_ORDER.length - 1)];
    goto(nextStep);
  };
  const back = () => {
    const idx = stepIndex(state.step);
    const prev = STEP_ORDER[Math.max(idx - 1, 0)];
    goto(prev);
  };
  const reset = () => goto("welcome");

  return (
    <Shell step={state.step}>
      {state.step === "welcome" && <Welcome next={next} />}
      {state.step === "platform" && (
        <PlatformStep state={state} dispatch={dispatch} back={back} next={next} />
      )}
      {state.step === "collect" && (
        <CollectStep state={state} dispatch={dispatch} back={back} next={next} />
      )}
      {state.step === "destination" && (
        <DestinationStep
          state={state}
          dispatch={dispatch}
          back={back}
          next={next}
        />
      )}
      {state.step === "apikey" && (
        <ApiKeyStep state={state} dispatch={dispatch} back={back} next={next} />
      )}
      {state.step === "service" && (
        <ServiceStep state={state} dispatch={dispatch} back={back} next={next} />
      )}
      {state.step === "install" && (
        <InstallStep state={state} back={back} next={next} />
      )}
      {state.step === "board" && (
        <BoardStep state={state} dispatch={dispatch} back={back} next={next} />
      )}
      {state.step === "done" && (
        <DoneStep state={state} back={back} reset={reset} />
      )}
    </Shell>
  );
}
