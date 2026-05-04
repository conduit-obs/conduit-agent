import {
  defaultServiceNameFor,
  type WizardState,
} from "../../types";

export type CommandBlock = {
  // Heading rendered above the block (e.g. "1. Install").
  title: string;
  // Single-line lead paragraph above the code, optional.
  description?: string;
  // The actual command(s); newline-separated for multi-line.
  body: string;
  // Lang hint for syntax styling. We treat all our snippets as bash
  // except the docker run / yaml ones.
  lang: "bash" | "yaml" | "powershell";
};

// shellQuote wraps a string in single quotes safely. Used for any user
// input that ends up on a shell command line (api keys, service names,
// endpoints) so a stray quote in their input can't break the script.
export function shellQuote(value: string): string {
  return `'${value.replace(/'/g, "'\\''")}'`;
}

// honeycombEnvBlock renders the export lines a non-installer flow (macOS)
// needs in the same shell where conduit run is started.
export function honeycombEnvBlock(state: WizardState): string {
  const lines: string[] = [];
  if (state.destination === "honeycomb" && state.ingestKey) {
    lines.push(`export HONEYCOMB_API_KEY=${shellQuote(state.ingestKey)}`);
  }
  if (state.deploymentEnvironment.trim()) {
    lines.push(
      `export CONDUIT_DEPLOYMENT_ENVIRONMENT=${shellQuote(state.deploymentEnvironment.trim())}`,
    );
  }
  return lines.join("\n");
}

// otlpEditBlock instructs the operator to swap the output: block in the
// shipped conduit.yaml when they picked Generic OTLP. We keep the snippet
// short and pointed: replace this sub-block, restart, done.
export function otlpEditBlock(state: WizardState, path: string, stepNum: number): CommandBlock {
  const headers = state.otlpHeaders.filter((h) => h.name && h.value);
  const headerYaml = headers.length
    ? "        headers:\n" +
      headers
        .map((h) => `          ${h.name}: ${shellQuote(h.value)}`)
        .join("\n") +
      "\n"
    : "";
  const yaml = `output:
  mode: otlp
  otlp:
    endpoint: ${shellQuote(state.otlpEndpoint || "https://otlp.example.com")}
${headerYaml}`;
  return {
    title: `${stepNum}. Switch output to your generic OTLP destination`,
    description: `Edit ${path} and replace the output: block with the snippet below. Then restart conduit.`,
    body: yaml,
    lang: "yaml",
  };
}

// conduitYaml renders a minimal, self-contained conduit.yaml the operator
// can drop into /etc/conduit/conduit.yaml on macOS or anywhere else
// without an installer. Mirrors deploy/linux/conduit.yaml.default but
// inlined with the user's choices.
export function conduitYaml(state: WizardState): string {
  const sn = defaultServiceNameFor(state);
  const lines = [
    `service_name: ${sn || "conduit"}`,
    `deployment_environment: ${state.deploymentEnvironment || "production"}`,
    "",
  ];
  if (state.destination === "honeycomb") {
    lines.push(
      "output:",
      "  mode: honeycomb",
      "  honeycomb:",
      "    api_key: ${env:HONEYCOMB_API_KEY}",
      `    endpoint: ${state.honeycombEndpoint}`,
    );
  } else {
    lines.push(
      "output:",
      "  mode: otlp",
      "  otlp:",
      `    endpoint: ${state.otlpEndpoint}`,
    );
    const hs = state.otlpHeaders.filter((h) => h.name && h.value);
    if (hs.length) {
      lines.push("    headers:");
      hs.forEach((h) => lines.push(`      ${h.name}: ${shellQuote(h.value)}`));
    }
  }
  if (state.platform) {
    lines.push("", "profile:", `  mode: ${state.platform}`);
  }
  return lines.join("\n");
}
