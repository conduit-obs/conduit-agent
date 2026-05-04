import type { WizardState } from "../../types";
import { type CommandBlock, shellQuote } from "./shared";

export function dockerBlocks(state: WizardState): CommandBlock[] {
  const envExports: string[] = [];
  if (state.destination === "honeycomb" && state.ingestKey) {
    envExports.push(`HONEYCOMB_API_KEY=${shellQuote(state.ingestKey)}`);
  }
  if (state.deploymentEnvironment.trim()) {
    envExports.push(
      `CONDUIT_DEPLOYMENT_ENVIRONMENT=${shellQuote(state.deploymentEnvironment.trim())}`,
    );
  }
  return [
    {
      title: "1. Run with Docker Compose",
      description:
        "The compose file mounts /proc, /sys, and / into /hostfs so the docker profile can scrape host metrics. OTLP is published on 127.0.0.1:4317 and :4318 by default — change the port mappings to 0.0.0.0 if peer apps live on other hosts.",
      body: `git clone https://github.com/conduit-obs/conduit-agent
cd conduit-agent
${envExports.join(" \\\n")} \\
  docker compose -f deploy/docker/compose-linux-host.yaml up -d`,
      lang: "bash",
    },
    {
      title: "2. Verify",
      description: "Check the health-check extension and tail logs.",
      body: `docker ps --filter name=conduit
docker exec conduit /conduit doctor -c /etc/conduit/conduit.yaml
docker logs --tail 50 conduit`,
      lang: "bash",
    },
  ];
}
