import type { WizardState } from "../../types";
import {
  type CommandBlock,
  conduitYaml,
  honeycombEnvBlock,
} from "./shared";

export function darwinBlocks(state: WizardState): CommandBlock[] {
  // macOS isn't packaged in V0; download the tarball and put the binary
  // on PATH. Service supervision is intentionally manual — most macOS
  // operators will run it under launchd via their own preferred shape.
  const arch = "$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/')";
  const env = honeycombEnvBlock(state);
  return [
    {
      title: "1. Download the tarball",
      description:
        "macOS doesn't have a packaged installer in V0. Grab the tarball from the latest release; the binary is statically-linked and self-contained.",
      body: `VERSION=$(curl -fsSL https://api.github.com/repos/conduit-obs/conduit-agent/releases/latest \\
  | grep tag_name | head -1 | cut -d'"' -f4)
curl -fsSLO "https://github.com/conduit-obs/conduit-agent/releases/download/\${VERSION}/conduit_\${VERSION#v}_darwin_${arch}.tar.gz"
tar -xzf conduit_\${VERSION#v}_darwin_${arch}.tar.gz
sudo mv conduit /usr/local/bin/`,
      lang: "bash",
    },
    {
      title: "2. Create the config directory and write conduit.yaml",
      description:
        "macOS has no default config path; we put it under /usr/local/etc to match Homebrew convention. Adjust to taste.",
      body: `sudo mkdir -p /usr/local/etc/conduit
sudo tee /usr/local/etc/conduit/conduit.yaml > /dev/null <<'YAML'
${conduitYaml(state)}
YAML`,
      lang: "bash",
    },
    {
      title: "3. Run",
      description:
        "Foreground for now. Wrap in a launchd plist if you want it to start on boot.",
      body: `${env}
conduit run -c /usr/local/etc/conduit/conduit.yaml`,
      lang: "bash",
    },
  ];
}
