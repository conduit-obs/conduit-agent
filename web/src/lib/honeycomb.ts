// Minimal Honeycomb Configuration API client. Used only by the optional
// "Import board" step. Operates in the browser with the operator's
// Configuration API key (NOT the ingest key — different permission set;
// see https://docs.honeycomb.io/get-started/configure/api-keys/).
//
// Three-call dance per board (the same shape conduit board apply will
// implement on the agent side once M11 lands):
//
//   1. POST /1/queries/{dataset}            — for each query panel; collect query_ids.
//   2. POST /1/query_annotations/{dataset}  — name + description for each query.
//   3. POST /1/boards                       — flexible board referencing both ids.
//
// Honeycomb's Board v3 schema (see api-docs.honeycomb.io) makes
// query_annotation_id required on every QueryPanel, so step 2 is not
// optional even though our bundled JSONs predate that schema and store
// the panel's name/description inline.
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

  // hcFetch wraps every Honeycomb API call so the CORS-vs-HTTP-error
  // distinction is handled in one place. Throws on network failure (CORS
  // is the dominant cause from a browser); returns the Response on any
  // HTTP status so callers can branch on status (skip vs hard fail).
  const hcFetch = async (path: string, body: unknown): Promise<Response> => {
    try {
      return await fetch(`${apiHost}${path}`, {
        method: "POST",
        headers,
        body: JSON.stringify(body),
      });
    } catch (e) {
      throw {
        message: `Browser couldn't reach ${apiHost}: ${(e as Error).message}. This is almost always CORS — Honeycomb's Configuration API doesn't allow browser-origin POSTs. Use the curl snippet below from a terminal.`,
        cors: true,
        curlScript: buildCurlScript(opts, board, apiHost),
      } as ImportError;
    }
  };

  // hardFailOnAuth is called from per-panel loops where individual
  // 422s are recoverable (skip the panel) but auth failures are not.
  const hardFailOnAuth = (
    res: Response,
    text: string,
    where: string,
  ): void => {
    if (res.status === 401 || res.status === 403) {
      throw {
        message: `Honeycomb rejected the API key on ${where}: ${res.status} ${res.statusText} ${text}. Confirm the key has the "Manage Boards" permission and matches the team/env you sent data to.`,
        cors: false,
        curlScript: buildCurlScript(opts, board, apiHost),
      } as ImportError;
    }
  };

  // Bound to the closure for buildCurlScript / hardFail to reach.
  let board: Board = null as unknown as Board;

  return {
    async importBoard(b, onProgress) {
      board = b;
      const queryPanels = board.panels.filter(
        (p): p is BoardPanel & { type: "query" } => p.type === "query",
      );

      // Step 1: create the query for each panel; collect query_ids.
      // Panel-level validation failures (missing column, etc.) are
      // captured into `skipped` so we can still ship the board with
      // whatever resolved.
      type Resolved = {
        panel: BoardPanel & { type: "query" };
        queryId: string;
        annotationId: string;
      };
      const resolved: Resolved[] = [];
      const pendingAnnotation: { panel: BoardPanel & { type: "query" }; queryId: string }[] = [];
      const skipped: SkippedPanel[] = [];

      for (let i = 0; i < queryPanels.length; i++) {
        const panel = queryPanels[i];
        onProgress?.({ stage: "resolving", current: i + 1, total: queryPanels.length });

        const res = await hcFetch(
          `/1/queries/${encodeURIComponent(panel.dataset)}`,
          toWireQuerySpec(panel.query_spec),
        );
        if (!res.ok) {
          const text = await res.text().catch(() => "");
          hardFailOnAuth(res, text, `POST /1/queries/${panel.dataset}`);
          skipped.push({ name: panel.name, reason: summarizeApiError(res.status, text) });
          continue;
        }
        const { id } = (await res.json()) as { id: string };
        pendingAnnotation.push({ panel, queryId: id });
      }

      if (pendingAnnotation.length === 0 && skipped.length > 0) {
        throw {
          message: `All ${skipped.length} query panel(s) were rejected by Honeycomb. Most often this means the agent hasn't sent any data yet (no columns exist in the dataset), or the dataset slug in the board doesn't match what your agent is writing to. First reason: ${skipped[0].reason}`,
          cors: false,
          curlScript: buildCurlScript(opts, board, apiHost),
        } as ImportError;
      }

      // Step 2: create a query annotation per resolved query. Honeycomb's
      // Board v3 schema requires query_annotation_id on every QueryPanel
      // — we use the panel's own name + description from the bundled
      // JSON so the saved annotation matches what the operator sees in
      // the UI.
      for (const pa of pendingAnnotation) {
        const res = await hcFetch(
          `/1/query_annotations/${encodeURIComponent(pa.panel.dataset)}`,
          {
            name: pa.panel.name,
            description: pa.panel.description ?? "",
            query_id: pa.queryId,
          },
        );
        if (!res.ok) {
          const text = await res.text().catch(() => "");
          hardFailOnAuth(res, text, `POST /1/query_annotations/${pa.panel.dataset}`);
          // Annotation failure → skip the panel; the query will be left
          // orphaned in Honeycomb but boards aren't billed and orphan
          // queries get garbage-collected. Better than failing the whole
          // import on one bad annotation save.
          skipped.push({
            name: pa.panel.name,
            reason: `annotation save: ${summarizeApiError(res.status, text)}`,
          });
          continue;
        }
        const { id } = (await res.json()) as { id: string };
        resolved.push({ panel: pa.panel, queryId: pa.queryId, annotationId: id });
      }

      // Step 3: build + POST the board. layout_generation: "auto" lets
      // Honeycomb arrange the panels — we drop the per-panel size hints
      // from the bundled JSON because manual layout requires explicit
      // x/y coordinates that we'd have to compute. Auto-layout is good
      // enough for V0; the operator can drag panels around in the UI.
      onProgress?.({ stage: "creating-board" });
      const resolvedBoard = {
        name: board.name,
        description: board.description,
        type: "flexible" as const,
        layout_generation: "auto" as const,
        tags: toWireTags(board.tags),
        panels: board.panels.flatMap<Record<string, unknown>>((p) => {
          if (p.type === "text") {
            return [{ type: "text", text_panel: { content: p.content } }];
          }
          const match = resolved.find((r) => r.panel === p);
          if (!match) return [];
          return [
            {
              type: "query",
              query_panel: {
                query_id: match.queryId,
                query_annotation_id: match.annotationId,
              },
            },
          ];
        }),
      };

      const res = await hcFetch(`/1/boards`, resolvedBoard);
      if (!res.ok) {
        const text = await res.text().catch(() => "");
        hardFailOnAuth(res, text, `POST /1/boards`);
        throw {
          message: `POST /1/boards failed: ${summarizeApiError(res.status, text)}`,
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
  const queryPanels = board.panels.filter((p) => p.type === "query") as Array<
    BoardPanel & { type: "query" }
  >;
  const lines: string[] = [
    `# Import "${board.name}" into Honeycomb. Run from any terminal with`,
    `# curl + jq installed. Treat HONEYCOMB_CONFIG_API_KEY as a deploy`,
    `# credential — it can rewrite every board, trigger, and SLO on the team.`,
    "",
    "set -euo pipefail",
    `export HONEYCOMB_CONFIG_API_KEY=${shellQuote(opts.configApiKey)}`,
    `export HONEYCOMB_API=${shellQuote(apiHost)}`,
    "",
    "tmp=$(mktemp -d) && trap 'rm -rf $tmp' EXIT",
    "auth=(-H \"X-Honeycomb-Team: $HONEYCOMB_CONFIG_API_KEY\" -H 'Content-Type: application/json')",
    "",
    "# 1. POST /1/queries — one per query panel; collect query_ids.",
  ];
  queryPanels.forEach((p, i) => {
    lines.push(
      `cat > "$tmp/q${i}.json" <<'JSON'`,
      JSON.stringify(toWireQuerySpec(p.query_spec), null, 2),
      "JSON",
      `qid${i}=$(curl -fsS "\${auth[@]}" -d "@$tmp/q${i}.json" \\`,
      `  "$HONEYCOMB_API/1/queries/${encodeURIComponent(p.dataset)}" | jq -r .id)`,
      `echo "  query  ${p.name.replace(/"/g, '\\"')} -> $qid${i}"`,
      "",
    );
  });
  lines.push("# 2. POST /1/query_annotations — required by Board v3 schema.");
  queryPanels.forEach((p, i) => {
    const annotation = {
      name: p.name,
      description: p.description ?? "",
      query_id: `__QID_${i}__`,
    };
    lines.push(
      `cat > "$tmp/a${i}.json" <<JSON`,
      JSON.stringify(annotation, null, 2).replace(`"__QID_${i}__"`, `"$qid${i}"`),
      "JSON",
      `aid${i}=$(curl -fsS "\${auth[@]}" -d "@$tmp/a${i}.json" \\`,
      `  "$HONEYCOMB_API/1/query_annotations/${encodeURIComponent(p.dataset)}" | jq -r .id)`,
      `echo "  annot  ${p.name.replace(/"/g, '\\"')} -> $aid${i}"`,
      "",
    );
  });
  lines.push("# 3. POST /1/boards referencing the query_ids + annotation_ids.");
  lines.push("cat > \"$tmp/board.json\" <<'JSON'");
  lines.push(JSON.stringify(boardManifestPlaceholder(board), null, 2));
  lines.push("JSON");
  queryPanels.forEach((_, i) => {
    lines.push(
      `sed -i.bak "s/__QID_${i}__/$qid${i}/g; s/__AID_${i}__/$aid${i}/g" "$tmp/board.json"`,
    );
  });
  lines.push(
    'rm -f "$tmp/board.json.bak"',
    "",
    'curl -fsS "${auth[@]}" -d @"$tmp/board.json" \\',
    '  "$HONEYCOMB_API/1/boards" | jq .',
  );
  return lines.join("\n");
}

// boardManifestPlaceholder mirrors the in-browser resolvedBoard shape
// but with __QID_n__ / __AID_n__ placeholders that the curl script
// substitutes via sed once each id is known. The shape is the v3
// Honeycomb Board schema (type: flexible, layout_generation: auto,
// nested query_panel / text_panel objects).
function boardManifestPlaceholder(board: Board): unknown {
  let qIdx = 0;
  return {
    name: board.name,
    description: board.description,
    type: "flexible",
    layout_generation: "auto",
    tags: toWireTags(board.tags),
    panels: board.panels.map((p) => {
      if (p.type === "text") {
        return { type: "text", text_panel: { content: p.content } };
      }
      const i = qIdx++;
      return {
        type: "query",
        query_panel: {
          query_id: `__QID_${i}__`,
          query_annotation_id: `__AID_${i}__`,
        },
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
