import { describe, it, expect } from "vitest";
import {
  renderWalkthrough,
  walkthroughAssets,
  videoName,
  type Manifest,
} from "../../docs/src/plugins/walkthrough-embed.mjs";

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
  ...extra,
});

describe("walkthroughAssets", () => {
  it("lists the manifest, the poster and every step still, never the video", () => {
    expect(walkthroughAssets(manifest())).toEqual([
      "walkthrough.json",
      "poster.png",
      "step-01.png",
      "step-02.png",
    ]);
  });

  it("refuses a still that is not a recorder still name", () => {
    const bad = manifest({
      steps: [{ n: 1, kind: "goto", caption: "x", still: "../../etc/passwd" }],
    });
    expect(() => walkthroughAssets(bad)).toThrow(/step-NN\.png/);
  });

  it("refuses an id that cannot name a directory", () => {
    expect(() => walkthroughAssets(manifest({ id: "Bad/Id" }))).toThrow(/lowercase/);
  });
});

describe("videoName", () => {
  it("is the id with an mp4 extension, as the recorder writes it", () => {
    expect(videoName("sample-simulation")).toBe("sample-simulation.mp4");
  });
});

describe("renderWalkthrough", () => {
  it("renders the poster and the captioned stills, with no video or link when the mp4 is absent", () => {
    const html = renderWalkthrough(manifest(), { video: false });
    expect(html).toContain('src="/walkthroughs/sample-simulation/poster.png"');
    expect(html).toContain('src="/walkthroughs/sample-simulation/step-01.png"');
    expect(html).toContain('src="/walkthroughs/sample-simulation/step-02.png"');
    expect(html).toContain("<details>");
    expect(html).not.toContain(".mp4");
    expect(html).not.toContain("<video");
  });

  it("adds the video and its link only when the mp4 is present", () => {
    const html = renderWalkthrough(manifest(), { video: true });
    expect(html).toContain("<video");
    expect(html).toContain('href="/walkthroughs/sample-simulation/sample-simulation.mp4"');
    expect(html).toContain('poster="/walkthroughs/sample-simulation/poster.png"');
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
    expect(html.match(/<li>/g)?.length).toBe(2);
  });

  it("omits the description when the script has none", () => {
    const html = renderWalkthrough(manifest({ description: undefined }), { video: false });
    expect(html).not.toContain("undefined");
  });
});
