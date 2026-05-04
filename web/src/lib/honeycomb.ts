// Minimal Honeycomb Configuration API client. Used only by the optional
// "Import board" step. Operates in the browser with the operator's
// Configuration API key (NOT the ingest key — different permission set;
// see https://docs.honeycomb.io/get-started/configure/api-keys/).
//
// Two-call dance per board (the same shape conduit board apply will
// implement on the agent side once M11 lands):
//
//   1. POST /1/queries/{dataset}  — for each query panel; collect ids.
//   2. POST /1/boards             — with the resolved query_ids.
//
// CORS handling: Honeycomb's Configuration API does not advertise CORS
// for arbitrary origins. Browser fetches typically fail with a network
// error before we see an HTTP status. We surface that exact case to
// the UI so it can fall back to showing pre-filled curl commands.

import type { Board, BoardPanel } from "./boards";

export type ImportProgress =
  | { stage: "resolving"; current: number; total: number }
  | { stage: "creating-board" }
  | { stage: "done"; boardUrl: string };

export type ImportError = {
  message: string;
  cors: boolean;
  // When the failure was CORS, we hand back curl commands the operator
  // can paste into a terminal to do the same work themselves.
  curlScript: string;
};

export type HoneycombClient = {
  importBoard: (
    board: Board,
    onProgress?: (p: ImportProgress) => void,
  ) => Promise<ImportProgress & { stage: "done" }>;
};

export function newHoneycombClient(opts: {
  configApiKey: string;
  team: string;
  env: string;
  apiHost?: string;
}): HoneycombClient {
  const apiHost = opts.apiHost ?? "https://api.honeycomb.io";
  const headers = {
    "X-Honeycomb-Team": opts.configApiKey,
    "Content-Type": "application/json",
  };

  return {
    async importBoard(board, onProgress) {
      const queryPanels = board.panels.filter(
        (p): p is BoardPanel & { type: "query" } => p.type === "query",
      );
      const queryIds: { panel: BoardPanel & { type: "query" }; id: string }[] = [];

      for (let i = 0; i < queryPanels.length; i++) {
        const panel = queryPanels[i];
        onProgress?.({ stage: "resolving", current: i + 1, total: queryPanels.length });

        let res: Response;
        try {
          res = await fetch(`${apiHost}/1/queries/${encodeURIComponent(panel.dataset)}`, {
            method: "POST",
            headers,
            body: JSON.stringify(toWireQuerySpec(panel.query_spec)),
          });
        } catch (e) {
          // Network error before we got a response. In a browser this
          // is overwhelmingly CORS or DNS / offline. We bubble up as
          // CORS so the UI can show curl-fallback.
          throw {
            message: `Browser couldn't reach ${apiHost}: ${(e as Error).message}. This is almost always CORS — Honeycomb's Configuration API doesn't allow browser-origin POSTs. Use the curl snippet below from a terminal.`,
            cors: true,
            curlScript: buildCurlScript(opts, board, apiHost),
          } as ImportError;
        }
        if (!res.ok) {
          const text = await res.text().catch(() => "");
          throw {
            message: `POST /1/queries/${panel.dataset} failed: ${res.status} ${res.statusText} ${text}`,
            cors: false,
            curlScript: buildCurlScript(opts, board, apiHost),
          } as ImportError;
        }
        const json = (await res.json()) as { id: string };
        queryIds.push({ panel, id: json.id });
      }

      onProgress?.({ stage: "creating-board" });
      // Resolve the board: replace each query panel's query_spec with
      // the matching query_id; keep markdown panels as-is.
      const resolvedBoard = {
        name: board.name,
        description: board.description,
        tags: board.tags,
        column_layout: "multi",
        panels: board.panels.map((p) => {
          if (p.type === "text") {
            return {
              type: "text_panel",
              text_panel: { body: p.content },
              ...(p.size ? { layout: { width: p.size.width, height: p.size.height } } : {}),
            };
          }
          const match = queryIds.find((q) => q.panel === p);
          if (!match) throw new Error(`unmatched query panel ${p.name}`);
          return {
            type: "query",
            query_id: match.id,
            ...(p.name ? { query_annotation_id: match.id } : {}),
            ...(p.size ? { layout: { width: p.size.width, height: p.size.height } } : {}),
          };
        }),
      };

      let res: Response;
      try {
        res = await fetch(`${apiHost}/1/boards`, {
          method: "POST",
          headers,
          body: JSON.stringify(resolvedBoard),
        });
      } catch (e) {
        throw {
          message: `Browser couldn't reach ${apiHost}: ${(e as Error).message}.`,
          cors: true,
          curlScript: buildCurlScript(opts, board, apiHost),
        } as ImportError;
      }
      if (!res.ok) {
        const text = await res.text().catch(() => "");
        throw {
          message: `POST /1/boards failed: ${res.status} ${res.statusText} ${text}`,
          cors: false,
          curlScript: buildCurlScript(opts, board, apiHost),
        } as ImportError;
      }
      const created = (await res.json()) as { id: string; links?: { board_url?: string } };
      const boardUrl =
        created.links?.board_url ??
        `https://ui.honeycomb.io/${opts.team}/environments/${opts.env}/boards/${created.id}`;

      const done = { stage: "done" as const, boardUrl };
      onProgress?.(done);
      return done;
    },
  };
}

// buildCurlScript renders the same two-step dance as a shell script the
// operator can paste into their terminal. We write each query body to a
// temp file (jq emits cleanly, posix-portable) and pipe through curl,
// then assemble the board.
function buildCurlScript(
  opts: { configApiKey: string; team: string; env: string },
  board: Board,
  apiHost: string,
): string {
  const lines = [
    `# Import "${board.name}" into Honeycomb. Run from any terminal with`,
    `# curl + jq installed. Treat HONEYCOMB_CONFIG_API_KEY as a deploy`,
    `# credential — it can rewrite every board, trigger, and SLO on the team.`,
    "",
    `export HONEYCOMB_CONFIG_API_KEY=${shellQuote(opts.configApiKey)}`,
    `export HONEYCOMB_API=${shellQuote(apiHost)}`,
    "",
    "tmp=$(mktemp -d) && trap 'rm -rf $tmp' EXIT",
    "",
    "# 1. Resolve every query panel into a query_id.",
  ];
  const queryPanels = board.panels.filter((p) => p.type === "query") as Array<
    BoardPanel & { type: "query" }
  >;
  queryPanels.forEach((p, i) => {
    lines.push(
      `cat > "$tmp/q${i}.json" <<'JSON'`,
      JSON.stringify(toWireQuerySpec(p.query_spec), null, 2),
      "JSON",
      `qid${i}=$(curl -fsS -H "X-Honeycomb-Team: $HONEYCOMB_CONFIG_API_KEY" \\`,
      `  -H 'Content-Type: application/json' \\`,
      `  -d "@$tmp/q${i}.json" \\`,
      `  "$HONEYCOMB_API/1/queries/${encodeURIComponent(p.dataset)}" | jq -r .id)`,
      `echo "  resolved ${p.name.replace(/"/g, '\\"')} -> $qid${i}"`,
      "",
    );
  });
  lines.push("# 2. Build the board referencing those query_ids.");
  lines.push("cat > \"$tmp/board.json\" <<'JSON'");
  lines.push(JSON.stringify(boardManifestPlaceholder(board), null, 2));
  lines.push("JSON");
  // Substitute placeholder query_ids
  queryPanels.forEach((_, i) => {
    lines.push(
      `sed -i.bak "s/__QID_${i}__/$qid${i}/g" "$tmp/board.json" && rm -f "$tmp/board.json.bak"`,
    );
  });
  lines.push(
    "",
    'curl -fsS -H "X-Honeycomb-Team: $HONEYCOMB_CONFIG_API_KEY" \\',
    "  -H 'Content-Type: application/json' \\",
    '  -d @"$tmp/board.json" \\',
    '  "$HONEYCOMB_API/1/boards" | jq .',
  );
  return lines.join("\n");
}

function boardManifestPlaceholder(board: Board): unknown {
  let qIdx = 0;
  return {
    name: board.name,
    description: board.description,
    tags: board.tags,
    column_layout: "multi",
    panels: board.panels.map((p) => {
      if (p.type === "text") {
        return {
          type: "text_panel",
          text_panel: { body: p.content },
          ...(p.size ? { layout: { width: p.size.width, height: p.size.height } } : {}),
        };
      }
      const placeholder = `__QID_${qIdx++}__`;
      return {
        type: "query",
        query_id: placeholder,
        ...(p.size ? { layout: { width: p.size.width, height: p.size.height } } : {}),
      };
    }),
  };
}

function shellQuote(value: string): string {
  return `'${value.replace(/'/g, "'\\''")}'`;
}

// toWireQuerySpec normalizes a query_spec from the human-editable form
// stored in dashboards/*.json into the shape Honeycomb's Configuration
// API actually accepts. The boards live as the human form per
// dashboards/README.md ("change the regex / mountpoint / time_range in
// the query_spec and reapply by hand"), so the boundary translation
// happens here, not in the JSON files.
//
// Currently only one normalization needed: time_range strings like
// "24h" / "1h" / "7d" → integer seconds. Honeycomb's API rejects the
// string form with 422 "incorrect type for field
// InnerQuerySpec.QuerySpec.time_range".
function toWireQuerySpec(spec: unknown): unknown {
  if (!spec || typeof spec !== "object") return spec;
  const out: Record<string, unknown> = { ...(spec as Record<string, unknown>) };
  if (typeof out.time_range === "string") {
    const seconds = parseDurationSeconds(out.time_range);
    if (seconds != null) out.time_range = seconds;
  }
  return out;
}

// parseDurationSeconds accepts "10s", "5m", "1h", "24h", "7d", "2w" and
// the bare integer seconds form. Returns null if it can't parse, in
// which case toWireQuerySpec leaves the value alone and the caller sees
// Honeycomb's original 422. Honeycomb caps time_range at 1209600s (14d)
// — we don't enforce that client-side, just translate.
function parseDurationSeconds(s: string): number | null {
  const m = /^(\d+)\s*([smhdw])$/.exec(s.trim());
  if (!m) {
    const n = Number(s);
    return Number.isFinite(n) && n > 0 ? Math.trunc(n) : null;
  }
  const n = Number(m[1]);
  const unit = m[2];
  const mul: Record<string, number> = {
    s: 1,
    m: 60,
    h: 3600,
    d: 86400,
    w: 604800,
  };
  return n * mul[unit];
}
