# Conduit Quickstart — `web/`

A stateless, single-page React wizard that walks a new operator through:

1. Picking a host platform (Linux / macOS / Windows / Docker / Kubernetes).
2. Choosing what to collect (host metrics, system logs, OTLP from apps, OBI zero-code instrumentation).
3. Picking a destination (Honeycomb fast-path or generic OTLP).
4. Generating an ingest API key on Honeycomb.
5. Customising `service.name` / `deployment.environment` (with profile-shaped defaults from ADR-0021).
6. Showing the exact install commands to run, with one-click copy.
7. Optionally importing a starter Honeycomb dashboard via the Configuration API.
8. Pointing at next steps and troubleshooting docs.

The page is **stateless** — refresh and you start over. No DB, no session, no telemetry. API keys never leave the browser; they go directly from the user's input into the install command (which they then run on their own host) or, for board import, into a direct `fetch()` call to `api.honeycomb.io` from their browser.

## Stack

- React 19 + TypeScript 6
- Vite 8 (static build output)
- Tailwind 4 via the official `@tailwindcss/vite` plugin
- No router (single-page wizard, internal state only)
- No state management library (`useReducer` is plenty)

Build output is ~85 KB gzipped, hostable as static files on GitHub Pages, Cloudflare Pages, S3, Netlify, Vercel, or any static file server.

## Develop

```bash
cd web
npm install
npm run dev
```

Vite serves on `http://localhost:5173`. Edits to step files hot-reload.

## Build

```bash
npm run build       # outputs to web/dist/
npm run preview     # serves the production bundle locally on :4173
```

For deployment under a sub-path (e.g. GitHub Pages on a project repo):

```bash
VITE_BASE=/conduit-agent/ npm run build
```

## Deploy

The site is fully static. Three reference deployments:

| Host | How |
| ---- | --- |
| GitHub Pages | `web/dist/` → `gh-pages` branch via the workflow at `.github/workflows/quickstart.yml`. |
| Cloudflare Pages | Set build command `cd web && npm install && npm run build`, output `web/dist`. |
| S3 + CloudFront | `aws s3 sync web/dist/ s3://your-bucket/` then invalidate `/*`. |

### Honeycomb API CORS

The wizard's last step calls `POST /1/queries/{dataset}` and `POST /1/boards` directly from the user's browser. Honeycomb's Configuration API does **not** advertise CORS for arbitrary origins, so the direct call will typically fail with a network error. The wizard catches this and renders a copy-pasteable `curl` script the user can run from any terminal — same two API calls, fully self-contained.

If a future Honeycomb release enables CORS for `api.honeycomb.io`, the direct call starts working with no code change here.

## Where the dashboards come from

Board JSONs are imported at build time from `../dashboards/` via the `@dashboards` alias in `vite.config.ts`. When dashboards in the top-level `dashboards/` directory change, the site needs a rebuild for the new boards to be available in the import flow. CI does this on every push to `main`.

## File budget

Per repo convention, no source file exceeds ~250 lines. If a step grows past that, split it into helper components in the same file or a new file under `src/components/` — don't reach for shared state libraries.

```text
src/
├── App.tsx               # wizard orchestrator
├── main.tsx              # React mount point
├── types.ts              # WizardState + reducer + helpers
├── index.css             # Tailwind + design tokens
├── components/
│   ├── Shell.tsx         # header, progress rail, footer
│   ├── StepCard.tsx      # eyebrow + title + nav primitives
│   ├── CodeBlock.tsx     # copy-button code blocks
│   └── OptionCard.tsx    # radio + checkbox cards
├── lib/
│   ├── commands.ts       # install command generator
│   ├── honeycomb.ts      # Configuration API client + curl fallback
│   └── boards.ts         # @dashboards/*.json import + per-platform mapping
└── steps/
    ├── Welcome.tsx
    ├── Platform.tsx
    ├── Collect.tsx
    ├── Destination.tsx
    ├── ApiKey.tsx
    ├── Service.tsx
    ├── Install.tsx
    ├── Board.tsx
    └── Done.tsx
```
