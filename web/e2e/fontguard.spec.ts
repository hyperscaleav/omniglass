import { readFileSync } from "node:fs";
import { test, expect } from "@playwright/test";
import { assertFontsRendered, REQUIRED_FACES, watchFontRequests } from "./fontguard.mjs";

// The browser half of the capture's font guard (#775). The pure decision is unit
// tested (web/src/fontguard.test.ts); what needs a real browser is the probe: that
// what the page reports about document.fonts actually distinguishes a page
// rendering in the console's typefaces from one rendering in fallback metrics.
//
// That distinction is the whole fix on the capture side. Before it, a shot whose
// faces had not arrived was written as though it were real, and the zero-tolerance
// freshness gate reported it three steps downstream as UI drift on a PR that
// changed no UI, which is how main went red at 3b16b325.
//
// Synthetic markup, no server: the same reason web/e2e/shotmask.spec.ts uses it,
// so the failure mode can be produced on demand rather than waited for.

// A real vendored face, inlined so the "healthy" case loads actual font bytes
// rather than a stub the browser might reject. Playwright runs with cwd=web.
const face = (path: string) =>
  `data:font/woff2;base64,${readFileSync(path).toString("base64")}`;
const PLEX = face("public/fonts/ibm-plex-sans-latin-400.woff2");
const MONO = face("public/fonts/jetbrains-mono-latin-400.woff2");

// The console declares nine faces; the guard asks that all nine are declared and
// that each family rendered from at least one of its own files. `src` decides
// which of those two properties this page satisfies.
const markup = (src: (f: { family: string }) => string) => `
<style>
${REQUIRED_FACES.map(
  (f) => `@font-face {
  font-family: '${f.family}';
  font-style: ${f.style};
  font-weight: ${f.weight};
  font-display: block;
  src: url('${src(f)}') format('woff2');
}`,
).join("\n")}
</style>
<p style="font-family: 'IBM Plex Sans'">Sydney Boardroom 17C</p>
<p style="font-family: 'JetBrains Mono'">og-7f3a-2c11</p>`;

test("passes a page rendering in the console's own typefaces", async ({ page }) => {
  const failed = watchFontRequests(page);
  await page.setContent(markup((f) => (f.family === "IBM Plex Sans" ? PLEX : MONO)));
  await expect(assertFontsRendered(page, "healthy", failed)).resolves.toBeUndefined();
});

test("refuses a page whose faces never arrived", async ({ page }) => {
  const failed = watchFontRequests(page);
  // A path that cannot resolve: the shape of a font that 404s, a CDN that did not
  // answer in time, or a build that dropped the files.
  await page.setContent(markup((f) => `/absent/${f.family.replace(/\s+/g, "-")}.woff2`));
  await expect(assertFontsRendered(page, "starved", failed)).rejects.toThrow(
    /refusing to write it/,
  );
  // And it says which family painted wrong, not just that something is off.
  await expect(assertFontsRendered(page, "starved", failed)).rejects.toThrow(
    /IBM Plex Sans/,
  );
});

test("refuses a page that declares no faces at all", async ({ page }) => {
  // document.fonts.check() answers true here (nothing matched, so nothing is
  // unloaded), which is why declaredness is asserted separately: a console that
  // lost its stylesheet must not read as a clean capture.
  await page.setContent("<p>Sydney Boardroom 17C</p>");
  await expect(assertFontsRendered(page, "bare", [])).rejects.toThrow(
    /no @font-face declares IBM Plex Sans 400/,
  );
});
