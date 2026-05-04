import { useState } from "react";
import { StepCard, StepNav } from "../components/StepCard";
import { CodeBlock } from "../components/CodeBlock";
import {
  newHoneycombClient,
  type ImportError,
  type ImportProgress,
} from "../lib/honeycomb";
import { boardForPlatform } from "../lib/boards";
import type { WizardAction, WizardState } from "../types";

export function BoardStep({
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
  const board = boardForPlatform(state.platform);
  const [progress, setProgress] = useState<ImportProgress | null>(null);
  const [errorDetail, setErrorDetail] = useState<ImportError | null>(null);

  // If the user picked Generic OTLP back on step 3 there's nothing to
  // import — the boards we ship are Honeycomb-specific. Skip ahead.
  if (state.destination !== "honeycomb") {
    return (
      <StepCard
        eyebrow="Step 7 of 8"
        title="Board import is Honeycomb-only."
        intro="The starter boards we bundle are Honeycomb dashboards. Since you picked a generic OTLP destination, build your dashboards inside that backend instead."
      >
        <StepNav back={back} next={next} nextLabel="Wrap up →" />
      </StepCard>
    );
  }
  if (!board) {
    return (
      <StepCard
        eyebrow="Step 7 of 8"
        title="No starter board for this platform yet."
        intro="We'll add one in a future release. For now, head to the wrap-up — your install is complete."
      >
        <StepNav back={back} next={next} nextLabel="Wrap up →" />
      </StepCard>
    );
  }

  const start = async () => {
    setProgress({ stage: "resolving", current: 0, total: 0 });
    setErrorDetail(null);
    dispatch({ type: "SET_BOARD_RESULT", result: { kind: "running" } });
    try {
      const client = newHoneycombClient({
        configApiKey: state.configKey.trim(),
        team: state.honeycombTeam.trim() || "your-team",
        env: state.honeycombEnv.trim() || "your-env",
        apiHost: state.honeycombEndpoint,
      });
      const result = await client.importBoard(board, (p) => setProgress(p));
      dispatch({
        type: "SET_BOARD_RESULT",
        result: { kind: "ok", url: result.boardUrl },
      });
    } catch (e) {
      const ie = e as ImportError;
      setErrorDetail(ie);
      dispatch({
        type: "SET_BOARD_RESULT",
        result: { kind: "error", message: ie.message, cors: ie.cors },
      });
    }
  };

  return (
    <StepCard
      eyebrow="Step 7 of 8 (optional)"
      title="Import a starter dashboard?"
      intro={
        <>
          We'll use a <strong>separate</strong> Honeycomb Configuration
          API key to create the{" "}
          <code className="font-mono">{board.name}</code> board in your
          environment. This key is different from the ingest key you used
          earlier — it can rewrite boards, triggers, and SLOs, so we treat
          it carefully and never persist it.
        </>
      }
    >
      <div className="rounded-xl border border-slate-200 bg-white p-6 space-y-4">
        <h2 className="text-base font-semibold text-slate-900">
          Create a Configuration API key
        </h2>
        <ol className="space-y-2 text-sm text-slate-700 list-decimal list-inside">
          <li>
            Same <code>API Keys</code> page as before, on the team /
            environment you sent data to.
          </li>
          <li>
            Click <code>Create API Key</code>, name it{" "}
            <code>conduit-board-import</code>.
          </li>
          <li>
            Check <strong>Manage Boards</strong>. You can leave everything
            else off.
          </li>
          <li>
            Click Create. Copy the key — typically{" "}
            <code>hcxik_</code>-prefixed.
          </li>
        </ol>
      </div>

      <div className="rounded-xl border border-slate-200 bg-white p-6 space-y-4">
        <Field
          label="Configuration API key"
          hint="hcxik_… — used once to create the board, then forgotten."
          type="password"
          value={state.configKey}
          onChange={(v) =>
            dispatch({ type: "SET_FIELD", field: "configKey", value: v })
          }
        />
        <div className="grid grid-cols-2 gap-3">
          <Field
            label="Team slug"
            hint="from ui.honeycomb.io URL"
            value={state.honeycombTeam}
            onChange={(v) =>
              dispatch({ type: "SET_FIELD", field: "honeycombTeam", value: v })
            }
          />
          <Field
            label="Environment slug"
            hint="optional; used for the success URL"
            value={state.honeycombEnv}
            onChange={(v) =>
              dispatch({ type: "SET_FIELD", field: "honeycombEnv", value: v })
            }
          />
        </div>
        <button
          type="button"
          onClick={start}
          disabled={
            !state.configKey.trim() || progress?.stage === "resolving" ||
            progress?.stage === "creating-board"
          }
          className="w-full text-sm font-semibold bg-accent text-white px-5 py-3 rounded-md shadow-sm hover:opacity-95 disabled:opacity-40 disabled:cursor-not-allowed transition-opacity"
        >
          {state.boardResult.kind === "running"
            ? "Importing…"
            : `Import "${board.name}" into Honeycomb`}
        </button>
      </div>

      <ProgressView progress={progress} />
      <ResultView result={state.boardResult} errorDetail={errorDetail} />

      <StepNav
        back={back}
        next={next}
        nextLabel="Done →"
        secondary={{
          label: "Skip — I'll do this later",
          onClick: next,
        }}
      />
    </StepCard>
  );
}

function Field({
  label,
  hint,
  type = "text",
  value,
  onChange,
}: {
  label: string;
  hint?: string;
  type?: "text" | "password";
  value: string;
  onChange: (v: string) => void;
}) {
  return (
    <label className="block space-y-1">
      <span className="text-sm font-semibold text-slate-900">{label}</span>
      <input
        type={type}
        autoComplete="off"
        spellCheck={false}
        value={value}
        onChange={(e) => onChange(e.target.value)}
        className="w-full px-4 py-2.5 rounded-lg border border-slate-300 bg-white text-sm font-mono focus:outline-2 focus:outline-accent"
      />
      {hint ? <span className="text-xs text-slate-500">{hint}</span> : null}
    </label>
  );
}

function ProgressView({ progress }: { progress: ImportProgress | null }) {
  if (!progress) return null;
  if (progress.stage === "done") return null;
  return (
    <div className="rounded-lg border border-slate-200 bg-slate-50 p-4 text-sm text-slate-700">
      {progress.stage === "resolving"
        ? `Resolving query ${progress.current} of ${progress.total}…`
        : "Creating board…"}
    </div>
  );
}

function ResultView({
  result,
  errorDetail,
}: {
  result: WizardState["boardResult"];
  errorDetail: ImportError | null;
}) {
  if (result.kind === "ok") {
    return (
      <div className="rounded-lg border border-emerald-200 bg-emerald-50 p-5 space-y-2">
        <div className="text-sm font-semibold text-emerald-900">
          ✓ Board created.
        </div>
        <a
          href={result.url}
          target="_blank"
          rel="noreferrer"
          className="text-sm text-emerald-800 underline break-all"
        >
          {result.url}
        </a>
      </div>
    );
  }
  if (result.kind === "error") {
    return (
      <div className="space-y-3">
        <div className="rounded-lg border border-red-200 bg-red-50 p-5 space-y-2">
          <div className="text-sm font-semibold text-red-900">
            {result.cors
              ? "Browser couldn't reach Honeycomb directly."
              : "Honeycomb rejected the request."}
          </div>
          <div className="text-sm text-red-800 leading-relaxed">
            {result.message}
          </div>
        </div>
        {errorDetail ? (
          <div className="space-y-2">
            <div className="text-sm font-semibold text-slate-900">
              Run this from your terminal instead
            </div>
            <p className="text-sm text-slate-600 leading-relaxed">
              Same two API calls — POST queries, then POST the board.
              Requires <code>curl</code> and <code>jq</code>. The script is
              self-contained, no environment dependency on the wizard.
            </p>
            <CodeBlock body={errorDetail.curlScript} lang="bash" />
          </div>
        ) : null}
      </div>
    );
  }
  return null;
}
