---
title: "Slice 3b: the operator console"
date: 2026-06-29
refs: []
form: bullet
---

- **Slice 3b: the operator console.** The last stage of the [generation pipeline](/contributing/api-first/)
  and the first browser surface: a SolidJS SPA (Vite, daisyUI 5 on Tailwind 4, `@solidjs/router`,
  `@tanstack/solid-query`, `openapi-fetch`) `go:embed`'d into the binary and served under `/web`. `make gen`
  now also emits the **typed SPA client** (`openapi-typescript` to `web/src/api/schema.gen.ts`), so the
  console cannot drift from the API. The embed uses the build-tag-with-fallback pattern (`internal/webui`): a
  bare `go build`/`go test` needs no `dist/` and serves a build-the-console placeholder, while `make build-web`
  runs the Vite build and compiles with `-tags web` to embed the real shell. The visual system is the
  "Omniglass Console" design ported faithfully (the `theme.css` tokens and component classes, the nav rail,
  top bar, density/theme tweaks, and Home). **Locations is the first live view**: list, detail, and create
  through the typed client, gated by a bearer-token login and the AuthGuard; the other nav sections render
  honest stubs until their backends land. Proven by `internal/webui` unit tests (the SPA-fallback routing
  against a fake FS, and the real embed under `-tags web`), an API mount test (`/web` serves the shell and the
  bare `/web` redirects), and a vitest suite over the locations data layer. Deliberate thin cuts: only
  locations is wired live; styling is daisyUI (two brand themes via the plugin) with Kobalte deferred until the
  first interactive widget needs it; auth is bearer-token paste, not a full login. Deferred: the mock-data screens
  (alarms, systems, components, dashboards) as their backends land, HTTP/2 (h2c) and the SSE live relay, and
  full auth UX.
