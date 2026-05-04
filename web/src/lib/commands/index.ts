// Public entry point for the install-command generator. Step components
// import generateInstallCommands; per-platform generators live alongside
// each other so the diff for a new platform is a single-file addition.

import type { WizardState } from "../../types";
import { darwinBlocks } from "./darwin";
import { dockerBlocks } from "./docker";
import { k8sBlocks } from "./k8s";
import { linuxBlocks } from "./linux";
import { windowsBlocks } from "./windows";
import type { CommandBlock } from "./shared";

export type { CommandBlock } from "./shared";

export function generateInstallCommands(state: WizardState): CommandBlock[] {
  switch (state.platform) {
    case "linux":
      return linuxBlocks(state);
    case "darwin":
      return darwinBlocks(state);
    case "windows":
      return windowsBlocks(state);
    case "docker":
      return dockerBlocks(state);
    case "k8s":
      return k8sBlocks(state);
    default:
      return [];
  }
}
