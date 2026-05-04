import { StepCard, StepNav } from "../components/StepCard";
import type { WizardState } from "../types";

export function DoneStep({
  state,
  back,
  reset,
}: {
  state: WizardState;
  back: () => void;
  reset: () => void;
}) {
  const honeycomb = state.destination === "honeycomb";
  return (
    <StepCard
      eyebrow="All done"
      title="You're collecting telemetry."
      intro="Below are the three things every Conduit operator ends up needing within the first week. Bookmark them now."
    >
      <NextSteps honeycomb={honeycomb} />

      <div className="rounded-xl border border-slate-200 bg-white p-6 space-y-3">
        <h3 className="text-base font-semibold text-slate-900">
          When something goes wrong
        </h3>
        <p className="text-sm text-slate-600 leading-relaxed">
          Conduit emits structured diagnostics with stable{" "}
          <code>CDT####</code> codes. Search the troubleshooting index for
          any code you see in <code>conduit doctor</code> output or the
          agent logs.
        </p>
        <div className="flex flex-wrap gap-2">
          <Pill href="https://github.com/conduit-obs/conduit-agent/blob/main/docs/troubleshooting/cdt-codes.md">
            CDT code reference
          </Pill>
          <Pill href="https://github.com/conduit-obs/conduit-agent/issues">
            Report a bug
          </Pill>
          <Pill href="https://github.com/conduit-obs/conduit-agent/discussions">
            Ask a question
          </Pill>
        </div>
      </div>

      <StepNav
        back={back}
        secondary={{ label: "Start over", onClick: reset }}
      />
    </StepCard>
  );
}

function NextSteps({ honeycomb }: { honeycomb: boolean }) {
  return (
    <div className="grid sm:grid-cols-3 gap-3">
      <NextCard
        title="Tune the config"
        body="Edit conduit.yaml to add filters, redact PII, or wire in a custom processor. Re-run conduit doctor after every change."
        href="https://github.com/conduit-obs/conduit-agent/blob/main/docs/configuration/conduit-yaml.md"
      />
      <NextCard
        title="Instrument your apps"
        body="Point your OTel SDK at 127.0.0.1:4317 (gRPC) or :4318 (HTTP). Conduit handles batching, retry, and resource enrichment."
        href="https://opentelemetry.io/docs/languages/"
      />
      <NextCard
        title={honeycomb ? "Build a SLO" : "Build a dashboard"}
        body={
          honeycomb
            ? "Pick the most important user journey, draw a query that captures it, save it as an SLO."
            : "Use your destination's dashboarding to plot the metrics and traces you just started collecting."
        }
        href={
          honeycomb
            ? "https://docs.honeycomb.io/working-with-your-data/slos/"
            : "https://opentelemetry.io/ecosystem/vendors/"
        }
      />
    </div>
  );
}

function NextCard({
  title,
  body,
  href,
}: {
  title: string;
  body: string;
  href: string;
}) {
  return (
    <a
      href={href}
      target="_blank"
      rel="noreferrer"
      className="block rounded-xl border border-slate-200 bg-white p-5 hover:border-accent hover:shadow-sm transition-all"
    >
      <div className="text-sm font-semibold text-slate-900">{title}</div>
      <div className="text-xs text-slate-600 mt-2 leading-relaxed">{body}</div>
      <div className="text-xs text-accent font-medium mt-3">Open →</div>
    </a>
  );
}

function Pill({ href, children }: { href: string; children: React.ReactNode }) {
  return (
    <a
      href={href}
      target="_blank"
      rel="noreferrer"
      className="text-xs font-medium text-slate-700 px-3 py-1.5 rounded-full border border-slate-200 hover:border-accent hover:text-accent transition-colors"
    >
      {children}
    </a>
  );
}
