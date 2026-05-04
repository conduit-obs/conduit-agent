import type { ReactNode } from "react";

// Selection-card primitive used by single-select (radio) steps like
// Platform and Destination. Visual: large click target with a left
// indicator dot, title, description, optional badge.
export function OptionCard({
  selected,
  onSelect,
  title,
  description,
  badge,
  icon,
}: {
  selected: boolean;
  onSelect: () => void;
  title: string;
  description: ReactNode;
  badge?: string;
  icon?: ReactNode;
}) {
  return (
    <button
      type="button"
      role="radio"
      aria-checked={selected}
      onClick={onSelect}
      className={`w-full text-left p-5 rounded-xl border-2 transition-all ${
        selected
          ? "border-accent bg-indigo-50/40 shadow-sm"
          : "border-slate-200 bg-white hover:border-slate-300 hover:bg-slate-50"
      }`}
    >
      <div className="flex items-start gap-4">
        <div
          className={`mt-1 flex-shrink-0 w-5 h-5 rounded-full border-2 transition-colors ${
            selected ? "border-accent" : "border-slate-300"
          }`}
        >
          {selected ? (
            <div className="w-full h-full rounded-full bg-accent scale-50" />
          ) : null}
        </div>
        {icon ? (
          <div className="flex-shrink-0 w-10 h-10 rounded-lg bg-slate-100 grid place-items-center text-slate-700">
            {icon}
          </div>
        ) : null}
        <div className="flex-1 min-w-0">
          <div className="flex items-center gap-2">
            <div className="text-base font-semibold text-slate-900">{title}</div>
            {badge ? (
              <span className="text-[10px] uppercase tracking-wider font-bold px-2 py-0.5 rounded bg-slate-200 text-slate-700">
                {badge}
              </span>
            ) : null}
          </div>
          <div className="text-sm text-slate-600 mt-1 leading-relaxed">
            {description}
          </div>
        </div>
      </div>
    </button>
  );
}

// Multi-select variant — same visual but the indicator is a checkbox.
export function CheckCard({
  selected,
  onToggle,
  title,
  description,
  badge,
  disabled,
}: {
  selected: boolean;
  onToggle: () => void;
  title: string;
  description: ReactNode;
  badge?: string;
  disabled?: boolean;
}) {
  return (
    <button
      type="button"
      role="checkbox"
      aria-checked={selected}
      aria-disabled={disabled}
      onClick={() => {
        if (!disabled) onToggle();
      }}
      className={`w-full text-left p-5 rounded-xl border-2 transition-all ${
        disabled
          ? "border-slate-200 bg-slate-50 opacity-60 cursor-not-allowed"
          : selected
            ? "border-accent bg-indigo-50/40 shadow-sm"
            : "border-slate-200 bg-white hover:border-slate-300 hover:bg-slate-50"
      }`}
    >
      <div className="flex items-start gap-4">
        <div
          className={`mt-0.5 flex-shrink-0 w-5 h-5 rounded border-2 transition-colors ${
            selected ? "border-accent bg-accent" : "border-slate-300 bg-white"
          }`}
        >
          {selected ? (
            <svg
              viewBox="0 0 12 12"
              fill="none"
              className="w-full h-full text-white"
            >
              <path
                d="M3 6.5L5 8.5L9 4"
                stroke="currentColor"
                strokeWidth="2"
                strokeLinecap="round"
                strokeLinejoin="round"
              />
            </svg>
          ) : null}
        </div>
        <div className="flex-1 min-w-0">
          <div className="flex items-center gap-2">
            <div className="text-base font-semibold text-slate-900">{title}</div>
            {badge ? (
              <span className="text-[10px] uppercase tracking-wider font-bold px-2 py-0.5 rounded bg-slate-200 text-slate-700">
                {badge}
              </span>
            ) : null}
          </div>
          <div className="text-sm text-slate-600 mt-1 leading-relaxed">
            {description}
          </div>
        </div>
      </div>
    </button>
  );
}
