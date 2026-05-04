import type { ReactNode } from "react";
import { STEP_ORDER, stepIndex, type WizardStep } from "../types";

const STEP_LABELS: Record<WizardStep, string> = {
  welcome: "Welcome",
  platform: "Platform",
  collect: "Collect",
  destination: "Destination",
  apikey: "API key",
  service: "Identity",
  install: "Install",
  board: "Board",
  done: "Done",
};

export function Shell({
  step,
  children,
}: {
  step: WizardStep;
  children: ReactNode;
}) {
  return (
    <div className="min-h-screen flex flex-col">
      <Header />
      <ProgressRail step={step} />
      <main className="flex-1 w-full max-w-3xl mx-auto px-6 sm:px-8 py-10">
        {children}
      </main>
      <Footer />
    </div>
  );
}

function Header() {
  return (
    <header className="border-b border-slate-200 bg-white">
      <div className="max-w-5xl mx-auto px-6 sm:px-8 py-5 flex items-center justify-between">
        <div className="flex items-center gap-3">
          <Logo />
          <div>
            <div className="text-base font-semibold text-slate-900 leading-tight">
              Conduit Quickstart
            </div>
            <div className="text-xs text-slate-500 leading-tight">
              OpenTelemetry on your host in five minutes
            </div>
          </div>
        </div>
        <a
          href="https://github.com/conduit-obs/conduit-agent"
          target="_blank"
          rel="noreferrer"
          className="text-xs text-slate-500 hover:text-slate-900 transition-colors"
        >
          GitHub →
        </a>
      </div>
    </header>
  );
}

function Footer() {
  return (
    <footer className="border-t border-slate-200 bg-white mt-16">
      <div className="max-w-5xl mx-auto px-6 sm:px-8 py-6 flex flex-col sm:flex-row items-start sm:items-center justify-between gap-3 text-xs text-slate-500">
        <div>
          Conduit is an Apache-2.0 OpenTelemetry Collector distribution.
          This wizard is stateless — refresh the page to start over.
        </div>
        <div className="flex gap-4">
          <a
            href="https://github.com/conduit-obs/conduit-agent/tree/main/docs/getting-started"
            target="_blank"
            rel="noreferrer"
            className="hover:text-slate-900"
          >
            Full docs
          </a>
          <a
            href="https://github.com/conduit-obs/conduit-agent/blob/main/docs/troubleshooting/cdt-codes.md"
            target="_blank"
            rel="noreferrer"
            className="hover:text-slate-900"
          >
            Troubleshooting
          </a>
        </div>
      </div>
    </footer>
  );
}

function ProgressRail({ step }: { step: WizardStep }) {
  const idx = stepIndex(step);
  const total = STEP_ORDER.length;
  return (
    <div className="border-b border-slate-200 bg-white sticky top-0 z-10">
      <div className="max-w-5xl mx-auto px-6 sm:px-8 py-4">
        <div className="flex items-center gap-2">
          <div className="flex-1 h-1.5 bg-slate-100 rounded-full overflow-hidden">
            <div
              className="h-full bg-accent transition-all duration-300 ease-out"
              style={{ width: `${((idx + 1) / total) * 100}%` }}
            />
          </div>
          <div className="text-xs text-slate-500 font-medium tabular-nums">
            {idx + 1} / {total}
          </div>
        </div>
        <div className="hidden sm:flex items-center gap-1 mt-3 text-xs text-slate-400 overflow-x-auto whitespace-nowrap">
          {STEP_ORDER.map((s, i) => (
            <span
              key={s}
              className={
                i === idx
                  ? "text-accent font-semibold"
                  : i < idx
                    ? "text-slate-700"
                    : "text-slate-400"
              }
            >
              {STEP_LABELS[s]}
              {i < STEP_ORDER.length - 1 ? (
                <span className="mx-2 text-slate-300">›</span>
              ) : null}
            </span>
          ))}
        </div>
      </div>
    </div>
  );
}

function Logo() {
  // Simple stacked-bars mark — three rounded rects representing the
  // three pipelines (traces / metrics / logs) the agent unifies.
  return (
    <svg
      viewBox="0 0 32 32"
      width={28}
      height={28}
      className="text-accent"
      aria-hidden
    >
      <rect x="4" y="6" width="24" height="4" rx="2" fill="currentColor" />
      <rect x="4" y="14" width="24" height="4" rx="2" fill="currentColor" />
      <rect x="4" y="22" width="24" height="4" rx="2" fill="currentColor" />
    </svg>
  );
}
