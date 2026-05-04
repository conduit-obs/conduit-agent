import { StepCard, StepNav } from "../components/StepCard";

export function Welcome({ next }: { next: () => void }) {
  return (
    <StepCard
      eyebrow="Welcome"
      title="Get OpenTelemetry running on your host."
      intro="Conduit is a small, vendor-neutral OpenTelemetry Collector distribution. This walkthrough takes about 5 minutes — pick your platform, answer a couple of questions, and you'll get the exact commands to run."
    >
      <div className="grid sm:grid-cols-3 gap-3">
        <Bullet
          n="1"
          title="Tell us about your host"
          body="Linux, macOS, Windows, Docker, or Kubernetes."
        />
        <Bullet
          n="2"
          title="Pick what to collect"
          body="Host metrics, system logs, application traces."
        />
        <Bullet
          n="3"
          title="Run the commands"
          body="One-line install. Optional dashboard import at the end."
        />
      </div>
      <div className="rounded-lg border border-slate-200 bg-white p-5 text-sm text-slate-600">
        <strong className="text-slate-900">Heads up:</strong> nothing you
        type stays on this page. Refresh and you start over. Your API keys
        never touch our servers — they go directly from your browser to
        Honeycomb (or to the install script you'll run on your own host).
      </div>
      <StepNav next={next} nextLabel="Let's go →" />
    </StepCard>
  );
}

function Bullet({ n, title, body }: { n: string; title: string; body: string }) {
  return (
    <div className="rounded-lg border border-slate-200 bg-white p-4">
      <div className="text-xs font-bold text-accent">{n}</div>
      <div className="text-sm font-semibold text-slate-900 mt-1">{title}</div>
      <div className="text-xs text-slate-600 mt-1 leading-relaxed">{body}</div>
    </div>
  );
}
