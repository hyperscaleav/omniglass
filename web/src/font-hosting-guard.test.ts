import { existsSync, readFileSync, statSync } from "node:fs";
import { describe, it, expect } from "vitest";

// Guard: the console renders with typefaces it ships, never with typefaces it
// fetches from a third party at runtime (#775).
//
// The console used to load IBM Plex Sans and JetBrains Mono from
// fonts.googleapis.com with `display=swap`. Two costs fell out of that. An estate
// on a closed campus or AV network renders the console in fallback metrics, which
// is the deployment this product targets; and the docs capture, which opens a new
// browser context per shot, re-fetched the faces for every shot, so a context that
// lost the race wrote a fallback-font PNG that the zero-tolerance freshness gate
// then reported as UI drift on a PR that changed no UI.
//
// Both are the same defect: rendering correctly depended on a network fetch. These
// assertions pin the fix structurally, so a future edit that reaches for a CDN
// stylesheet fails here rather than in a screenshot diff six commits later.
const indexHtml = readFileSync("index.html", "utf8");
const appCss = readFileSync("src/app.css", "utf8");
const fontsCss = existsSync("src/fonts.css") ? readFileSync("src/fonts.css", "utf8") : "";

// The nine faces the console asks for: the four IBM Plex Sans weights plus its
// italic (used for empty-state and hint copy), and the four JetBrains Mono weights
// the data/ID/count vocabulary uses. This list is the contract between app.css's
// --og-font-ui / --og-font-data, the vendored files, and the capture's own guard.
const REQUIRED_FACES = [
  { family: "IBM Plex Sans", weight: "400", style: "normal" },
  { family: "IBM Plex Sans", weight: "400", style: "italic" },
  { family: "IBM Plex Sans", weight: "500", style: "normal" },
  { family: "IBM Plex Sans", weight: "600", style: "normal" },
  { family: "IBM Plex Sans", weight: "700", style: "normal" },
  { family: "JetBrains Mono", weight: "400", style: "normal" },
  { family: "JetBrains Mono", weight: "500", style: "normal" },
  { family: "JetBrains Mono", weight: "600", style: "normal" },
  { family: "JetBrains Mono", weight: "700", style: "normal" },
];

// One @font-face block, split on the closing brace so a block's descriptors stay
// together. Cheap and exact enough for a generated stylesheet.
function faceBlocks(css: string) {
  return [...css.matchAll(/@font-face\s*\{([^}]*)\}/g)].map((m) => m[1]);
}
const descriptor = (block: string, name: string) =>
  new RegExp(`${name}:\\s*([^;]+);`).exec(block)?.[1].trim() ?? "";

describe("the console ships its own typefaces", () => {
  it("loads no stylesheet, and preconnects to no host, off this origin", () => {
    const remote = [...indexHtml.matchAll(/<link[^>]*>/g)]
      .map((m) => m[0])
      .filter((tag) => /https?:\/\//.test(tag));
    expect(remote, `remote <link> in index.html:\n${remote.join("\n")}`).toEqual([]);
  });

  it("names no font CDN anywhere in the console source", () => {
    const sources: Record<string, string> = {
      "index.html": indexHtml,
      "src/app.css": appCss,
      "src/fonts.css": fontsCss,
    };
    const offenders = Object.entries(sources)
      .filter(([, src]) => /fonts\.(googleapis|gstatic)\.com/.test(src))
      .map(([path]) => path);
    expect(offenders).toEqual([]);
  });

  it("declares every face the type system asks for", () => {
    const blocks = faceBlocks(fontsCss);
    const declared = new Set(
      blocks.map((b) =>
        [
          descriptor(b, "font-family").replace(/['"]/g, ""),
          descriptor(b, "font-weight"),
          descriptor(b, "font-style"),
        ].join("/"),
      ),
    );
    const missing = REQUIRED_FACES.filter(
      (f) => !declared.has(`${f.family}/${f.weight}/${f.style}`),
    ).map((f) => `${f.family} ${f.weight} ${f.style}`);
    expect(missing, `undeclared faces:\n${missing.join("\n")}`).toEqual([]);
  });

  it("serves every face from this origin, from a file that exists", () => {
    const blocks = faceBlocks(fontsCss);
    expect(blocks.length).toBeGreaterThan(0);
    const offenders: string[] = [];
    for (const block of blocks) {
      const url = /url\(["']?([^"')]+)["']?\)/.exec(block)?.[1] ?? "";
      if (!url.startsWith("/web/fonts/")) {
        offenders.push(`off-origin or unexpected src: ${url}`);
        continue;
      }
      // public/ is copied verbatim into the Vite output, which is what
      // internal/webui/spa_embed.go compiles into the binary, so a file that is
      // not here is a 404 in the shipped console.
      const path = `public/${url.slice("/web/".length)}`;
      if (!existsSync(path)) offenders.push(`missing file: ${path}`);
    }
    expect(offenders, `\n${offenders.join("\n")}\n`).toEqual([]);
  });

  it("blocks rather than swaps, so no render can show fallback metrics", () => {
    // The file is same-origin and embedded in the same binary that served the
    // document, so the block period costs nothing measurable, and `block` removes
    // the flash of fallback text that `swap` keeps by definition. `optional` would
    // be worse than the bug: it lets the browser keep the fallback for the page's
    // whole life if the face misses its budget.
    const displays = faceBlocks(fontsCss).map((b) => descriptor(b, "font-display"));
    expect(new Set(displays)).toEqual(new Set(["block"]));
  });

  it("ships the OFL license text beside the faces it covers", () => {
    for (const path of [
      "public/fonts/ibm-plex-sans-OFL.txt",
      "public/fonts/jetbrains-mono-OFL.txt",
    ]) {
      expect(existsSync(path), `missing ${path}`).toBe(true);
      expect(readFileSync(path, "utf8")).toContain("SIL OPEN FONT LICENSE Version 1.1");
    }
  });

  it("keeps the vendored payload declared, so its size cannot creep unnoticed", () => {
    // Every byte here lands in the shipped binary (all:dist). The ceiling is not a
    // performance budget, it is a review trigger: adding a weight or a script is a
    // deliberate decision, so it should have to move this number.
    const bytes = faceBlocks(fontsCss)
      .map((b) => /url\(["']?([^"')]+)["']?\)/.exec(b)?.[1] ?? "")
      .filter((u) => u.startsWith("/web/fonts/"))
      .reduce((sum, u) => sum + statSync(`public/${u.slice("/web/".length)}`).size, 0);
    expect(bytes).toBeLessThan(1_100_000);
  });

  it("wires the faces into the stylesheet the console actually loads", () => {
    expect(appCss).toMatch(/@import\s+["']\.\/fonts\.css["']/);
  });
});
