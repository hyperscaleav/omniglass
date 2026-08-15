import { defineConfig } from "vite";
import solid from "vite-plugin-solid";
import tailwindcss from "@tailwindcss/vite";

// The console is served by the Go binary under /web (api.go nests it there;
// Solid Router's base matches). base makes Vite emit asset URLs as /web/assets/*
// so they resolve both embedded and under `npm run dev`. The build writes into
// the Go module's embed target (internal/webui/dist), which spa_embed.go embeds
// under the `web` build tag. In dev, server.proxy forwards API calls to a
// locally-running `omniglass server`, so the frontend loop needs no rebuild.
//
// The vendored typefaces (public/fonts/, #775) are referenced from src/fonts.css by
// their served path, `/web/fonts/...`, base included, the same way index.html
// references the favicon. Vite logs one "didn't resolve at build time, it will
// remain unchanged to be resolved at runtime" line per file for that, which is the
// intended outcome rather than a warning to fix: public/ is copied verbatim and the
// URL is already the final one. Dropping the base to `/fonts/...` silences the
// lines and builds identical output, but then `npm run dev` 404s every face,
// because the dev server serves public/ only under the base.
export default defineConfig({
  base: "/web/",
  plugins: [solid(), tailwindcss()],
  build: {
    outDir: "../internal/webui/dist",
    emptyOutDir: true,
  },
  server: {
    proxy: {
      "/api": "http://localhost:8080",
    },
  },
});
