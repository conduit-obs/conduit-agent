import { PLATFORM_DEFAULT_SERVICE_NAME, type WizardState } from "../../types";
import type { CommandBlock } from "./shared";

export function windowsBlocks(state: WizardState): CommandBlock[] {
  const blocks: CommandBlock[] = [
    {
      title: "1. Install the MSI",
      description:
        "Run install.ps1 from the latest release. It downloads the MSI, runs msiexec /qn, registers the Conduit Windows service, and starts it.",
      body: `iwr -useb https://raw.githubusercontent.com/conduit-obs/conduit-agent/main/deploy/windows/scripts/install.ps1 \`
  | iex
.\\install.ps1 \`
  -ApiKey "$env:HONEYCOMB_API_KEY" \`
  -DeploymentEnvironment "${state.deploymentEnvironment || "production"}"`,
      lang: "powershell",
    },
  ];
  if (state.serviceName.trim()) {
    blocks.push({
      title: "2. (Optional) Override service.name",
      description: `service.name defaults to "${PLATFORM_DEFAULT_SERVICE_NAME.windows}" via the profile-shaped fallback (ADR-0021). Override by editing conduit.yaml directly:`,
      body: `Add-Content "$env:ProgramData\\Conduit\\conduit.yaml" "service_name: ${state.serviceName.trim()}"
Restart-Service Conduit`,
      lang: "powershell",
    });
  }
  blocks.push({
    title: state.serviceName.trim() ? "3. Verify" : "2. Verify",
    description:
      "conduit doctor and the Windows service status give you a quick health view.",
    body: `Get-Service Conduit | Format-List Name, Status, StartType
& "$env:ProgramFiles\\Conduit\\conduit.exe" doctor \`
  -c "$env:ProgramData\\Conduit\\conduit.yaml"`,
    lang: "powershell",
  });
  return blocks;
}
