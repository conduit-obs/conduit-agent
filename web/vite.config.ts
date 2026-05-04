import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
import path from "node:path";

// The wizard is a single-page static app. We bundle the dashboard JSONs
// from the repo's top-level dashboards/ directory at build time so the
// site can offer one-click board imports without an unreliable
// cross-origin fetch from raw.githubusercontent.com at runtime.
export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: {
      "@dashboards": path.resolve(__dirname, "../dashboards"),
    },
  },
  // Built-in base of "/" assumes the site is served from a domain root
  // (Cloudflare Pages, custom domain, gh-pages with a CNAME). Hosters
  // serving from a subpath override this via the BASE env var, e.g.
  // VITE_BASE=/conduit-quickstart/ npm run build for GitHub Pages on
  // a project repo.
  base: process.env.VITE_BASE ?? "/",
});
