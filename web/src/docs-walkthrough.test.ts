import { describe, it, expect } from "vitest";
import {
  canonicalEmbed,
  findEmbeds,
  renderWalkthrough,
  walkthroughAssets,
  videoName,
  type Manifest,
} from "../../docs/src/plugins/walkthrough-embed.mjs";
import { scanDirectives } from "../../docs/scripts/lib/embed-scan.mjs";

// The docs embed of a walkthrough (#926): `::walkthrough{#id}` renders the
// recorder's manifest (walkthrough.json) as a poster, a video only when the mp4
// is present at build time, and every captioned still folded underneath. Which
// files the docs carry, and the HTML, are pure and pinned here.
const manifest = (extra: Partial<Manifest> = {}): Manifest => ({
  id: "sample-simulation",
  title: "Read a simulated component's status",
  description: "Open one to see where it is served.",
  steps: [
    { n: 1, kind: "goto", caption: "Open the Simulation page.", still: "step-01.png" },
    { n: 2, kind: "wait", caption: "Its status <says> where & how.", still: "step-02.png" },
  ],
  poster: "step-02.png",
  budget: { seconds: 12, bytes: 500000 },
  ...extra,
});

describe("walkthroughAssets", () => {
  it("lists the manifest and every step still, never the video or the poster copy", () => {
    expect(walkthroughAssets(manifest())).toEqual(["walkthrough.json", "step-01.png", "step-02.png"]);
  });

  it("refuses a still that is not a recorder still name", () => {
    const bad = manifest({
      steps: [{ n: 1, kind: "goto", caption: "x", still: "../../etc/passwd" }],
      poster: "../../etc/passwd",
    });
    expect(() => walkthroughAssets(bad)).toThrow(/step-NN\.png/);
  });

  it("refuses a poster that is not one of the step stills", () => {
    expect(() => walkthroughAssets(manifest({ poster: "step-09.png" }))).toThrow(/poster/);
  });

  it("refuses an id that cannot name a directory", () => {
    expect(() => walkthroughAssets(manifest({ id: "Bad/Id" }))).toThrow(/lowercase/);
  });

  it("refuses a step number that is not a positive integer", () => {
    const bad = manifest({
      steps: [{ n: '1" onerror="x' as unknown as number, kind: "goto", caption: "x", still: "step-01.png" }],
      poster: "step-01.png",
    });
    expect(() => walkthroughAssets(bad)).toThrow(/positive integer/);
  });

  it("carries the animated preview when the manifest records one", () => {
    expect(walkthroughAssets(manifest({ preview: "preview.webp" }))).toEqual(["walkthrough.json", "step-01.png", "step-02.png", "preview.webp"]);
  });

  it("refuses a preview that is not the recorder's preview name", () => {
    expect(() => walkthroughAssets(manifest({ preview: "../poster.png" }))).toThrow(/preview\.webp/);
  });

  it("refuses a manifest whose recording never passed the budget", () => {
    expect(() => walkthroughAssets(manifest({ budget: undefined }))).toThrow(/budget/);
  });
});

describe("videoName", () => {
  it("is the id with an mp4 extension, as the recorder writes it", () => {
    expect(videoName("sample-simulation")).toBe("sample-simulation.mp4");
  });
});

describe("renderWalkthrough", () => {
  it("renders the poster step and the captioned stills, with no video or link when the mp4 is absent", () => {
    const html = renderWalkthrough(manifest(), { video: false });
    expect(html).toContain('<img src="/walkthroughs/sample-simulation/step-02.png" alt="Read');
    expect(html).toContain('src="/walkthroughs/sample-simulation/step-01.png"');
    expect(html).not.toContain("poster.png");
    expect(html).toContain("<details>");
    expect(html).not.toContain(".mp4");
    expect(html).not.toContain("<video");
  });

  it("adds the video and its link only when the mp4 is present", () => {
    const html = renderWalkthrough(manifest(), { video: true });
    expect(html).toContain("<video");
    expect(html).toContain('href="/walkthroughs/sample-simulation/sample-simulation.mp4"');
    expect(html).toContain('poster="/walkthroughs/sample-simulation/step-02.png"');
  });

  it("shows the animated preview in place of the poster step when the manifest records one", () => {
    const html = renderWalkthrough(manifest({ preview: "preview.webp" }), { video: false });
    expect(html).toContain('<img src="/walkthroughs/sample-simulation/preview.webp" alt="Read a simulated component&#39;s status: animated preview" loading="lazy" />');
    expect(html).not.toContain(": poster");
    expect(html).toContain('src="/walkthroughs/sample-simulation/step-02.png" alt="Step 2: ');
  });

  it("escapes script text, so a caption cannot inject markup", () => {
    const html = renderWalkthrough(manifest(), { video: false });
    expect(html).toContain("Its status &lt;says&gt; where &amp; how.");
    expect(html).not.toContain("<says>");
  });

  it("carries the title and description, and numbers every step", () => {
    const html = renderWalkthrough(manifest(), { video: false });
    expect(html).toContain("Read a simulated component&#39;s status");
    expect(html).toContain("Open one to see where it is served.");
    expect(html).toContain("2 steps");
    expect(html).toContain('alt="Step 2: ');
    expect(html.match(/<li>/g)?.length).toBe(2);
  });

  it("omits the description when the script has none", () => {
    const html = renderWalkthrough(manifest({ description: undefined }), { video: false });
    expect(html).not.toContain("undefined");
  });
});

describe("findEmbeds", () => {
  it("finds every rendered embed in built HTML, by id, verbatim", () => {
    const a = renderWalkthrough(manifest(), { video: false });
    const b = renderWalkthrough(manifest({ id: "other" }), { video: true });
    const page = `<main><p>x</p>${a}<p>y</p>${b}</main>`;
    expect(findEmbeds(page)).toEqual([
      { id: "sample-simulation", html: a },
      { id: "other", html: b },
    ]);
  });

  it("finds nothing on a page with no walkthrough", () => {
    expect(findEmbeds('<figure class="screenshot"></figure>')).toEqual([]);
  });
});

// The built page carries the embed after Astro's HTML serializer, which writes
// the same markup differently (no self-closing slash, hex entities); the
// post-build check compares the two in a canonical form, so a serialization
// difference passes and a content difference fails.
describe("canonicalEmbed", () => {
  const rendered = renderWalkthrough(manifest(), { video: false });
  const serialized = rendered
    .replace(/ \/>/g, ">")
    .replace(/&#39;/g, "&#x27;")
    .replace(/&amp;/g, "&#x26;")
    .replace(/&lt;/g, "&#x3C;");

  it("treats a serializer's rewrite of the same embed as the same", () => {
    expect(serialized).not.toBe(rendered);
    expect(canonicalEmbed(serialized)).toBe(canonicalEmbed(rendered));
  });

  it("tells a changed caption apart", () => {
    const stale = renderWalkthrough(
      manifest({ steps: [{ n: 1, kind: "goto", caption: "Old words.", still: "step-01.png" }, manifest().steps[1]] }),
      { video: false },
    );
    expect(canonicalEmbed(stale)).not.toBe(canonicalEmbed(rendered));
  });

  it("tells a preview added apart", () => {
    expect(canonicalEmbed(renderWalkthrough(manifest({ preview: "preview.webp" }), { video: false }))).not.toBe(canonicalEmbed(rendered));
  });

  it("tells a video added apart", () => {
    expect(canonicalEmbed(renderWalkthrough(manifest(), { video: true }))).not.toBe(canonicalEmbed(rendered));
  });
});

// The scanner the pre-build gate (check-embeds.mjs) uses for both directives:
// only a leaf directive alone on its line embeds; a text directive, a
// container, a quote in code or an HTML comment does not.
describe.each(["screenshot", "walkthrough"])("scanDirectives(%s)", (name) => {
  const scan = (text: string) => scanDirectives(text.split("\n"), name);

  it("finds a leaf directive alone on its line, with its line number", () => {
    expect(scan(`intro\n\n::${name}{#a}\n`)).toEqual([{ line: 3, id: "a" }]);
  });

  it("reads the id of a directive that also carries attributes", () => {
    expect(scan(`::${name}{#a title="y"}`)).toEqual([{ line: 1, id: "a" }]);
  });

  it("reports a leaf directive with no id", () => {
    expect(scan(`::${name}`)).toEqual([{ line: 1, id: null }]);
    expect(scan(`::${name}{title="y"}`)).toEqual([{ line: 1, id: null }]);
  });

  it("ignores an inline text directive in prose", () => {
    expect(scan(`see :${name}{#a} here, or ::${name}{#a} mid-line`)).toEqual([]);
  });

  it("ignores a container directive", () => {
    expect(scan(`:::${name}{#a}\nbody\n:::`)).toEqual([]);
  });

  it("ignores one inside an HTML comment, single or multi-line", () => {
    expect(scan(`<!-- ::${name}{#a} -->`)).toEqual([]);
    expect(scan(`<!--\n::${name}{#a}\n-->\n::${name}{#b}`)).toEqual([{ line: 4, id: "b" }]);
  });

  it("ignores one in a fenced code block or an inline span", () => {
    expect(scan("```markdown\n::" + name + "{#a}\n```")).toEqual([]);
    expect(scan("write `::" + name + "{#a}` to embed")).toEqual([]);
  });

  it("skips the frontmatter", () => {
    expect(scan(`---\ntitle: x\n---\n::${name}{#a}`)).toEqual([{ line: 4, id: "a" }]);
  });
});
