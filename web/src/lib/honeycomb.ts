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

export type SkippedPanel = {
  name: string;
  reason: string;
};

export type ImportProgress =
  | { stage: "resolving"; current: number; total: number }
  | { stage: "creating-board" }
  | { stage: "done"; boardUrl: string; skipped: SkippedPanel[] };

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
      const skipped: SkippedPanel[] = [];

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
          // Auth / permission failures fail the whole import — there's
          // no point continuing if the key can't talk to the API at
          // all. Anything else (overwhelmingly 422 validation: column
          // missing, dataset missing, derived column unresolved) is
          // treated as a per-panel skip: we capture the reason and
          // build the board with whatever else resolved. That keeps a
          // single board JSON workable across hosts where some metrics
          // legitimately don't exist (no swap, no NIC bonding, etc.).
          if (res.status === 401 || res.status === 403) {
            throw {
              message: `Honeycomb rejected the API key on POST /1/queries/${panel.dataset}: ${res.status} ${res.statusText} ${text}. Confirm the key has the "Manage Boards" permission and matches the team/env you sent data to.`,
              cors: false,
              curlScript: buildCurlScript(opts, board, apiHost),
            } as ImportError;
          }
          skipped.push({
            name: panel.name,
            reason: summarizeApiError(res.status, text),
          });
          continue;
        }
        const json = (await res.json()) as { id: string };
        queryIds.push({ panel, id: json.id });
      }

      // If literally every query failed validation, the board would be
      // empty — bail with a more useful error than "you got an empty
      // board." Most likely cause: wrong dataset slugs, or the agent
      // hasn't sent any data yet so no columns exist at all.
      if (queryIds.length === 0 && skipped.length > 0) {
        throw {
          message: `All ${skipped.length} query panel(s) were rejected by Honeycomb. Most often this means the agent hasn't sent any data yet (no columns exist in the dataset), or the dataset slug in the board doesn't match what your agent is writing to. First reason: ${skipped[0].reason}`,
          cors: false,
          curlScript: buildCurlScript(opts, board, apiHost),
        } as ImportError;
      }

      onProgress?.({ stage: "creating-board" });
      // Resolve the board: replace each query panel's query_spec with
      // the matching query_id; keep markdown panels as-is. Panels that
      // were skipped above (validation failure, missing column, etc.)
      // are dropped from the rendered board entirely — Honeycomb has no
      // "placeholder panel" concept, and an empty panel would be more
      // confusing than a missing one.
      const resolvedBoard = {
        name: board.name,
        description: board.description,
        tags: toWireTags(board.tags),
        column_layout: "multi",
        panels: board.panels.flatMap<Record<string, unknown>>((p) => {
          if (p.type === "text") {
            return [
              {
                type: "text_panel",
                text_panel: { body: p.content },
                ...(p.size ? { layout: { width: p.size.width, height: p.size.height } } : {}),
              },
            ];
          }
          const match = queryIds.find((q) => q.panel === p);
          if (!match) return [];
          return [
            {
              type: "query",
              query_id: match.id,
              ...(p.name ? { query_annotation_id: match.id } : {}),
              ...(p.size ? { layout: { width: p.size.width, height: p.size.height } } : {}),
            },
          ];
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

      const done = { stage: "done" as const, boardUrl, skipped };
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
    tags: toWireTags(board.tags),
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

// toWireTags translates the human-editable "key:value" tag strings stored
// in dashboards/*.json (per dashboards/README.md's "tags": ["k:v", ...]
// convention) into the Tag object form Honeycomb's Board API requires:
//   [{ key: "agent", value: "conduit" }, ...]
// Honeycomb's schema is strict — keys are lowercase letters only (max 32)
// and values are alphanumeric + "/" / "-" (max 128). The bundled JSONs
// already conform; we just need the type-shape change here. Tags without
// a ":" are dropped silently rather than mangled — the checked-in boards
// don't produce any, and a missing tag is far better than a 422.
function toWireTags(
  tags: string[] | undefined,
): { key: string; value: string }[] {
  if (!tags) return [];
  return tags.flatMap((t) => {
    const i = t.indexOf(":");
    if (i <= 0 || i === t.length - 1) return [];
    return [{ key: t.slice(0, i), value: t.slice(i + 1) }];
  });
}

// summarizeApiError pulls the most-useful sentence out of a Honeycomb
// 4xx body so the wizard can show "missing column 'system.paging.utilization'"
// instead of the full RFC-7807 problem document. Honeycomb's validation
// errors come back as
//   { type_detail: [{ code, description }, ...], error: "..." }
// We pick the first type_detail.description if present, then fall back
// to .error, then to the raw body.
function summarizeApiError(status: number, body: string): string {
  try {
    const j = JSON.parse(body) as {
      type_detail?: { code?: string; description?: string }[];
      error?: string;
    };
    const detail = j.type_detail?.[0]?.description;
    if (detail) return `${status}: ${detail}`;
    if (j.error) return `${status}: ${j.error}`;
  } catch {
    // not JSON; fall through
  }
  return `${status}: ${body.slice(0, 200) || "(empty body)"}`;
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
