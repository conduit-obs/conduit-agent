// Wizard state shape. One source of truth — every step component reads /
// updates this through useReducer in App.tsx. The state is intentionally
// plain serializable JSON so that, if we ever decide to persist progress
// across reloads, dropping in localStorage is a one-line change.

export type Platform = "linux" | "darwin" | "windows" | "docker" | "k8s";

export type CollectKind =
  | "host_metrics"
  | "system_logs"
  | "otlp_app_traces"
  | "obi_zero_code";

export type Destination = "honeycomb" | "otlp_generic";

export type WizardStep =
  | "welcome"
  | "platform"
  | "collect"
  | "destination"
  | "apikey"
  | "service"
  | "install"
  | "board"
  | "done";

export type WizardState = {
  step: WizardStep;
  platform: Platform | null;
  // The "what to collect" multi-select. host_metrics + system_logs are
  // on by default for every platform Conduit ships fragments for; the
  // user can deselect. otlp_app_traces is on by default everywhere
  // (it's the always-on receiver). obi_zero_code is off by default
  // and gated to Linux + Kubernetes (per ADR-0020 sub-decision 4).
  collect: Set<CollectKind>;
  destination: Destination;
  // Honeycomb ingest key (hcaik_*); only populated when destination =
  // honeycomb. We never persist this — if the user reloads they re-enter.
  ingestKey: string;
  honeycombEndpoint: string;
  // Generic OTLP destination fields — used when destination = otlp_generic.
  otlpEndpoint: string;
  otlpHeaders: { name: string; value: string }[];
  // service.name override. When empty, the agent uses the profile-shaped
  // default per ADR-0021 (linux-host, macos-host, windows-host,
  // docker-host, k8s-cluster).
  serviceName: string;
  deploymentEnvironment: string;
  // Optional board-import flow on the last step.
  configKey: string;
  honeycombTeam: string;
  honeycombEnv: string;
  // Result of the optional board-import attempt (success URL, or an
  // error + curl-fallback details).
  boardResult:
    | { kind: "idle" }
    | { kind: "running" }
    | { kind: "ok"; url: string; skipped: { name: string; reason: string }[] }
    | { kind: "error"; message: string; cors: boolean };
};

export const initialState: WizardState = {
  step: "welcome",
  platform: null,
  collect: new Set<CollectKind>([
    "host_metrics",
    "system_logs",
    "otlp_app_traces",
  ]),
  destination: "honeycomb",
  ingestKey: "",
  honeycombEndpoint: "https://api.honeycomb.io",
  otlpEndpoint: "",
  otlpHeaders: [],
  serviceName: "",
  deploymentEnvironment: "production",
  configKey: "",
  honeycombTeam: "",
  honeycombEnv: "",
  boardResult: { kind: "idle" },
};

export type WizardAction =
  | { type: "GO"; step: WizardStep }
  | { type: "SET_PLATFORM"; platform: Platform }
  | { type: "TOGGLE_COLLECT"; kind: CollectKind }
  | { type: "SET_DESTINATION"; destination: Destination }
  | { type: "SET_FIELD"; field: keyof WizardState; value: string }
  | {
      type: "SET_OTLP_HEADERS";
      headers: { name: string; value: string }[];
    }
  | { type: "SET_BOARD_RESULT"; result: WizardState["boardResult"] };

export function wizardReducer(
  state: WizardState,
  action: WizardAction,
): WizardState {
  switch (action.type) {
    case "GO":
      return { ...state, step: action.step };
    case "SET_PLATFORM":
      return applyPlatformSideEffects({ ...state, platform: action.platform });
    case "TOGGLE_COLLECT": {
      const collect = new Set(state.collect);
      if (collect.has(action.kind)) {
        collect.delete(action.kind);
      } else {
        collect.add(action.kind);
      }
      return { ...state, collect };
    }
    case "SET_DESTINATION":
      return { ...state, destination: action.destination };
    case "SET_FIELD":
      return { ...state, [action.field]: action.value };
    case "SET_OTLP_HEADERS":
      return { ...state, otlpHeaders: action.headers };
    case "SET_BOARD_RESULT":
      return { ...state, boardResult: action.result };
  }
}

// Platform-driven defaults. Switching to docker or k8s pre-deselects
// host_metrics on platforms where it isn't part of the default install
// path (docker without /hostfs bind-mounts, k8s where kubelet is used
// instead). Switching back to linux/darwin/windows turns it back on.
function applyPlatformSideEffects(state: WizardState): WizardState {
  const collect = new Set(state.collect);
  if (state.platform === "docker") {
    collect.delete("host_metrics");
    collect.delete("system_logs");
    collect.delete("obi_zero_code");
  } else if (state.platform === "k8s") {
    // k8s replaces "host metrics" with kubelet stats — operationally the
    // same offering, but we don't expose kubelet as a separate toggle.
    collect.add("host_metrics");
    collect.add("system_logs");
  } else {
    // linux / darwin / windows: defaults restored.
    collect.add("host_metrics");
    collect.add("system_logs");
    if (state.platform !== "linux") {
      // OBI is gated to linux + k8s; the k8s branch above already
      // bypasses this, so platforms that fall here (darwin, windows)
      // can't have it on.
      collect.delete("obi_zero_code");
    }
  }
  return { ...state, collect };
}

export const PLATFORM_DEFAULT_SERVICE_NAME: Record<Platform, string> = {
  linux: "linux-host",
  darwin: "macos-host",
  windows: "windows-host",
  docker: "docker-host",
  k8s: "k8s-cluster",
};

export function defaultServiceNameFor(state: WizardState): string {
  if (state.serviceName.trim()) return state.serviceName.trim();
  if (!state.platform) return "";
  return PLATFORM_DEFAULT_SERVICE_NAME[state.platform];
}

// ORDER controls the progress bar and the next/back navigation. Steps
// can short-circuit forward (e.g. "Skip board import") but never branch
// — the flow stays linear so the experience is predictable.
export const STEP_ORDER: WizardStep[] = [
  "welcome",
  "platform",
  "collect",
  "destination",
  "apikey",
  "service",
  "install",
  "board",
  "done",
];

export function stepIndex(step: WizardStep): number {
  return STEP_ORDER.indexOf(step);
}
