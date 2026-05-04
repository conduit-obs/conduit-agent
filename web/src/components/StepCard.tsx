import type { ReactNode } from "react";

export function StepCard({
  eyebrow,
  title,
  intro,
  children,
}: {
  eyebrow: string;
  title: string;
  intro?: ReactNode;
  children: ReactNode;
}) {
  return (
    <section className="space-y-6">
      <header className="space-y-2">
        <div className="text-xs font-semibold tracking-widest uppercase text-accent">
          {eyebrow}
        </div>
        <h1 className="text-3xl sm:text-4xl font-bold text-slate-900 leading-tight">
          {title}
        </h1>
        {intro ? (
          <p className="text-base text-slate-600 leading-relaxed max-w-2xl">
            {intro}
          </p>
        ) : null}
      </header>
      <div className="space-y-4">{children}</div>
    </section>
  );
}

export function StepNav({
  back,
  next,
  nextLabel,
  nextDisabled,
  secondary,
}: {
  back?: () => void;
  next?: () => void;
  nextLabel?: string;
  nextDisabled?: boolean;
  secondary?: { label: string; onClick: () => void };
}) {
  return (
    <div className="pt-8 mt-8 border-t border-slate-200 flex items-center justify-between gap-3">
      <div>
        {back ? (
          <button
            type="button"
            onClick={back}
            className="text-sm text-slate-600 hover:text-slate-900 px-3 py-2 rounded-md hover:bg-slate-100 transition-colors"
          >
            ← Back
          </button>
        ) : null}
      </div>
      <div className="flex items-center gap-2">
        {secondary ? (
          <button
            type="button"
            onClick={secondary.onClick}
            className="text-sm text-slate-700 hover:text-slate-900 px-4 py-2 rounded-md hover:bg-slate-100 transition-colors"
          >
            {secondary.label}
          </button>
        ) : null}
        {next ? (
          <button
            type="button"
            onClick={next}
            disabled={nextDisabled}
            className="text-sm font-semibold bg-accent text-white px-5 py-2 rounded-md shadow-sm hover:opacity-95 disabled:opacity-40 disabled:cursor-not-allowed transition-opacity focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent"
          >
            {nextLabel ?? "Continue →"}
          </button>
        ) : null}
      </div>
    </div>
  );
}
