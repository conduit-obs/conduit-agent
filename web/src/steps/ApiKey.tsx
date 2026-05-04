import { useMemo } from "react";
import { StepCard, StepNav } from "../components/StepCard";
import type { WizardAction, WizardState } from "../types";

export function ApiKeyStep({
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
  if (state.destination === "honeycomb") {
    return (
      <HoneycombApiKey
        state={state}
        dispatch={dispatch}
        back={back}
        next={next}
      />
    );
  }
  return (
    <GenericOtlp state={state} dispatch={dispatch} back={back} next={next} />
  );
}

function HoneycombApiKey({
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
  const validation = useMemo(() => validateIngestKey(state.ingestKey), [
    state.ingestKey,
  ]);
  return (
    <StepCard
      eyebrow="Step 4 of 8"
      title="Grab a Honeycomb ingest API key."
      intro="The agent uses an ingest key — narrow scope, send-events permission only. (We'll ask for a separate Configuration key at the very end if you want to import a dashboard.)"
    >
      <div className="rounded-xl border border-slate-200 bg-white p-6 space-y-4">
        <h2 className="text-base font-semibold text-slate-900">
          How to create one
        </h2>
        <ol className="space-y-3 text-sm text-slate-700 leading-relaxed list-decimal list-inside">
          <li>
            Open{" "}
            <a
              href="https://ui.honeycomb.io/teams"
              target="_blank"
              rel="noreferrer"
              className="text-accent font-medium hover:underline"
            >
              ui.honeycomb.io
            </a>
            , pick the team and environment you want to send data to.
          </li>
          <li>
            Click <code>Environment Settings → API Keys → Create API Key</code>.
          </li>
          <li>
            Name it something like <code>conduit-ingest</code> and check
            only <strong>Send Events</strong>. Leave the rest off.
          </li>
          <li>Click Create. Copy the key — it starts with <code>hcaik_</code>.</li>
        </ol>
      </div>

      <label className="block space-y-2">
        <span className="text-sm font-semibold text-slate-900">
          Paste your ingest key
        </span>
        <input
          type="password"
          autoComplete="off"
          spellCheck={false}
          placeholder="hcaik_01abc..."
          value={state.ingestKey}
          onChange={(e) =>
            dispatch({
              type: "SET_FIELD",
              field: "ingestKey",
              value: e.target.value,
            })
          }
          className="w-full px-4 py-3 rounded-lg border border-slate-300 bg-white text-sm font-mono focus:outline-2 focus:outline-accent focus:border-transparent"
        />
        <span className="text-xs text-slate-500">
          {validation.kind === "empty" ? (
            "We never store this. It travels straight from your browser to the install command we render below."
          ) : validation.kind === "warn" ? (
            <span className="text-amber-700">{validation.message}</span>
          ) : (
            <span className="text-emerald-700">
              ✓ Looks like a Honeycomb ingest key.
            </span>
          )}
        </span>
      </label>

      <details className="rounded-lg border border-slate-200 bg-slate-50 p-4 text-sm text-slate-700">
        <summary className="cursor-pointer font-semibold text-slate-900">
          Sending to Honeycomb EU?
        </summary>
        <div className="mt-3 space-y-2">
          <p>
            Switch the API endpoint to <code>https://api.eu1.honeycomb.io</code>.
          </p>
          <input
            type="text"
            value={state.honeycombEndpoint}
            onChange={(e) =>
              dispatch({
                type: "SET_FIELD",
                field: "honeycombEndpoint",
                value: e.target.value,
              })
            }
            className="w-full px-3 py-2 rounded-lg border border-slate-300 bg-white text-sm font-mono"
          />
        </div>
      </details>

      <StepNav back={back} next={next} nextDisabled={!state.ingestKey.trim()} />
    </StepCard>
  );
}

function GenericOtlp({
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
  const setHeader = (i: number, field: "name" | "value", v: string) => {
    const next = [...state.otlpHeaders];
    next[i] = { ...next[i], [field]: v };
    dispatch({ type: "SET_OTLP_HEADERS", headers: next });
  };
  const addHeader = () =>
    dispatch({
      type: "SET_OTLP_HEADERS",
      headers: [...state.otlpHeaders, { name: "", value: "" }],
    });
  const rmHeader = (i: number) =>
    dispatch({
      type: "SET_OTLP_HEADERS",
      headers: state.otlpHeaders.filter((_, j) => j !== i),
    });

  return (
    <StepCard
      eyebrow="Step 4 of 8"
      title="OTLP destination details"
      intro="Tell us the OTLP/HTTPS endpoint and any auth headers your destination requires. Conduit talks OTLP/gRPC by default; pass http(s) URLs to use OTLP/HTTP."
    >
      <label className="block space-y-2">
        <span className="text-sm font-semibold text-slate-900">
          OTLP endpoint
        </span>
        <input
          type="text"
          autoComplete="off"
          spellCheck={false}
          placeholder="https://otlp.example.com:4318"
          value={state.otlpEndpoint}
          onChange={(e) =>
            dispatch({
              type: "SET_FIELD",
              field: "otlpEndpoint",
              value: e.target.value,
            })
          }
          className="w-full px-4 py-3 rounded-lg border border-slate-300 bg-white text-sm font-mono focus:outline-2 focus:outline-accent"
        />
      </label>

      <div className="space-y-2">
        <span className="text-sm font-semibold text-slate-900">Headers</span>
        {state.otlpHeaders.map((h, i) => (
          <div key={i} className="flex gap-2">
            <input
              type="text"
              placeholder="Header name"
              value={h.name}
              onChange={(e) => setHeader(i, "name", e.target.value)}
              className="flex-1 px-3 py-2 rounded-lg border border-slate-300 text-sm font-mono"
            />
            <input
              type="text"
              placeholder="Value (e.g. Bearer xxx)"
              value={h.value}
              onChange={(e) => setHeader(i, "value", e.target.value)}
              className="flex-[2] px-3 py-2 rounded-lg border border-slate-300 text-sm font-mono"
            />
            <button
              type="button"
              onClick={() => rmHeader(i)}
              className="px-3 text-slate-500 hover:text-red-600"
              aria-label="Remove header"
            >
              ✕
            </button>
          </div>
        ))}
        <button
          type="button"
          onClick={addHeader}
          className="text-sm text-accent font-medium hover:underline"
        >
          + Add header
        </button>
      </div>

      <StepNav
        back={back}
        next={next}
        nextDisabled={!state.otlpEndpoint.trim()}
      />
    </StepCard>
  );
}

function validateIngestKey(
  k: string,
): { kind: "empty" } | { kind: "warn"; message: string } | { kind: "ok" } {
  const t = k.trim();
  if (!t) return { kind: "empty" };
  if (!t.startsWith("hcaik_")) {
    return {
      kind: "warn",
      message:
        "Honeycomb ingest keys start with hcaik_. If yours starts with hcxik_ that's a Configuration key — use it later for board import, not here.",
    };
  }
  if (t.length < 20) return { kind: "warn", message: "That looks too short to be a real key." };
  return { kind: "ok" };
}
