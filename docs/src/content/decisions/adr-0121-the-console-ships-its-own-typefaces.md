---
title: "The console ships its own typefaces"
date: 2026-08-15
status: "Accepted"
summary: "The console serves IBM Plex Sans and JetBrains Mono from its own origin (vendored under `web/public/fonts/`, embedded in the binary, declared by a generated `web/src/fonts.css`) instead of linking a font CDN: rendering correctly stops depending on reaching a third party, which is the deployment this product targets and was also the cause of a docs capture writing fallback-font PNGs the zero-tolerance freshness gate reported as UI drift. Every script the CDN served is vendored (54 files, 1,005,028 bytes) so no operator string renders differently than before; `font-display: block` replaces `swap`; the capture now aborts rather than photograph a fallback render; `DOCS_SHOTS_PROXY` retires"
---

- **Date:** 2026-08-15 | **Status:** Accepted | **Pages:** [design system](/contributing/design-system/),
  [UI](/architecture/ui/)
- **Decision:** The console serves both of its typefaces, **IBM Plex Sans** (UI) and **JetBrains
  Mono** (data), from its own origin. The woff2 files are vendored under `web/public/fonts/`, which
  Vite copies verbatim into the build `internal/webui/spa_embed.go` embeds, so the faces travel
  inside the single binary and are served by it; `web/src/fonts.css` declares them and is generated
  by `web/scripts/vendor-fonts.mjs` from the same request the console used to make. `index.html`
  links no stylesheet and preconnects to no host off this origin, and a guard test
  (`web/src/font-hosting-guard.test.ts`) fails the suite if one comes back.
  Three sub-decisions ride with it. **Every script the CDN served is vendored**, not a latin subset:
  latin, latin-ext, cyrillic, cyrillic-ext, greek and vietnamese, 54 files and 1,005,028 bytes, so no
  operator-supplied string (a location name, a label, a principal name) renders differently than it
  did before, and `unicode-range` keeps the per-script split so a latin page still fetches only the
  latin file. A codepoint no block covers falls through to the next family in `--og-font-ui` /
  `--og-font-data` and renders in the platform face, which is what already happened for every script
  these families do not carry. **`font-display: block` replaces `swap`**: same-origin and embedded,
  the block period costs nothing measurable, and block is the only value that cannot paint fallback
  metrics (`optional` can keep them for the life of the page). **The capture refuses to photograph a
  fallback render**: `web/e2e/fontguard.mjs` asserts per shot that every face is declared and that
  both families rendered from their own files, aborting otherwise.
- **Context:** The console linked `fonts.googleapis.com` with `display=swap`, so it painted in
  fallback metrics until a third-party fetch completed. Two costs, one cause. An estate on a closed
  campus or AV network, the deployment this product targets, never completes that fetch and renders
  the console in the wrong face permanently, having also disclosed each viewer to a third party. And
  the docs capture opens a new browser context per shot, so every shot re-raced the CDN: on
  `3b16b325` two of ten shots lost, were written anyway, and failed the zero-tolerance freshness gate
  as UI drift on a PR that changed no UI, turning main red. The measurement that named it: the two
  failing shots differed in the same 3,806 and 456 pixels of the shared sidebar, and the differing
  pixels were 20,955 greyscale glyph edges against 1,405 colourful, which is text metrics rather than
  content (#775).
  Waiting on `document.fonts.ready` before the shutter was the cheaper fix and was rejected: it
  greens the gate while leaving the console unable to render correctly where it is deployed, which is
  the more expensive half of the same defect. `DOCS_SHOTS_PROXY`, the egress-proxy escape hatch that
  existed for capture hosts that could not reach the CDN, retires with the dependency it worked
  around (it had been inert since the capture moved into a container that never received it).
  The screenshots recaptured **byte-identical**, which is the check on the vendoring: the files
  served are the same faces at the same version the browser had been fetching, so this changed no
  pixel of the console.
