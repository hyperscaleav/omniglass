import { defineConfig } from "vite";
import solid from "vite-plugin-solid";
import tailwindcss from "@tailwindcss/vite";

// The console is served by the Go binary under /web (api.go nests it there;
// Solid Router's base matches). base makes Vite emit asset URLs as /web/assets/*
// so they resolve both embedded and under `npm run dev`. The build writes into
// the Go module's embed target (internal/webui/dist), which spa_embed.go embeds
// under the `web` build tag. In dev, server.proxy forwards API calls to a
// locally-running `omniglass server`, so the frontend loop needs no rebuild.
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
