import type { WizardState } from "../../types";
import { type CommandBlock, otlpEditBlock, shellQuote } from "./shared";

export function linuxBlocks(state: WizardState): CommandBlock[] {
  const flags: string[] = [];
  if (state.destination === "honeycomb" && state.ingestKey) {
    flags.push(`--api-key=${shellQuote(state.ingestKey)}`);
  }
  if (state.serviceName.trim()) {
    flags.push(`--service-name=${shellQuote(state.serviceName.trim())}`);
  }
  if (state.deploymentEnvironment.trim()) {
    flags.push(
      `--deployment-env=${shellQuote(state.deploymentEnvironment.trim())}`,
    );
  }
  if (state.collect.has("obi_zero_code")) {
    flags.push("--with-obi");
  }
  const flagLine = flags.length ? ` \\\n    ${flags.join(" \\\n    ")}` : "";
  const blocks: CommandBlock[] = [
    {
      title: "1. Install",
      description:
        "One command auto-detects your distro (apt / dnf / yum / pacman / apk) and CPU arch, installs the matching package, seeds /etc/conduit/conduit.env, and starts the systemd unit.",
      body: `curl -fsSL https://raw.githubusercontent.com/conduit-obs/conduit-agent/main/scripts/install_linux.sh \\
  | sudo bash -s --${flagLine}`,
      lang: "bash",
    },
  ];
  if (state.destination === "otlp_generic") {
    blocks.push(otlpEditBlock(state, "/etc/conduit/conduit.yaml", blocks.length + 1));
  }
  blocks.push({
    title: blocks.length + 1 + ". Verify",
    description:
      "conduit doctor runs ~10 preflights (config syntax, output auth, port reachability, receiver permissions, kernel/eBPF if --with-obi) and returns non-zero if anything is wrong.",
    body: `sudo systemctl status conduit
sudo conduit doctor -c /etc/conduit/conduit.yaml
sudo journalctl -u conduit -n 50 --no-pager`,
    lang: "bash",
  });
  return blocks;
}
