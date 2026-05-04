// Bundle the checked-in board JSONs from the repo's top-level dashboards/
// directory at build time. Vite resolves the @dashboards alias from
// vite.config.ts. JSON imports are typed as `unknown` here and narrowed
// to Board at runtime — saves us from duplicating 400 lines of types.

import linuxBoardJson from "@dashboards/linux-host-overview.json";
import macosBoardJson from "@dashboards/macos-host-overview.json";
import windowsBoardJson from "@dashboards/windows-host-overview.json";
import dockerBoardJson from "@dashboards/docker-host-overview.json";
import k8sBoardJson from "@dashboards/k8s-cluster-overview.json";
import type { Platform } from "../types";

export type BoardPanel =
  | {
      type: "text";
      content: string;
      size?: { width: number; height: number };
    }
  | {
      type: "query";
      name: string;
      description?: string;
      chart_type?: string;
      display_style?: string;
      size?: { width: number; height: number };
      dataset: string;
      query_spec: unknown;
    };

export type Board = {
  format_version: number;
  name: string;
  description: string;
  tags: string[];
  preset_filters?: { column: string; alias: string }[];
  panels: BoardPanel[];
};

export const BOARD_BY_PLATFORM: Record<Platform, Board> = {
  linux: linuxBoardJson as Board,
  darwin: macosBoardJson as Board,
  windows: windowsBoardJson as Board,
  docker: dockerBoardJson as Board,
  k8s: k8sBoardJson as Board,
};

// boardForState returns the right board for the user's chosen platform.
// We currently ship one board per platform; that's enough for the V0
// "first 5 minutes" experience the wizard targets.
export function boardForPlatform(platform: Platform | null): Board | null {
  if (!platform) return null;
  return BOARD_BY_PLATFORM[platform];
}
