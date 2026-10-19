import { describe, it, expect } from "vitest";
import {
  WALKTHROUGH_MARKER,
  commentBody,
  rawUrl,
  selectWalkthroughs,
  stillsDir,
  type Manifest,
} from "../e2e/walkthroughpr.mjs";

// Walkthroughs as PR evidence (#925): the run:walkthrough workflow records the
// PR's walkthroughs, commits their poster and stills under
// .github/screenshots/<pr>/walkthroughs/<id>/, and upserts one comment. The
// choices it makes (which walkthroughs, where stills go, what the comment says)
// are pure and pinned here; the workflow itself only runs them.
const REPO = "hyperscaleav/omniglass";
const SHA = "0123456789abcdef0123456789abcdef01234567";
const HEAD = "fedcba9876543210fedcba9876543210fedcba98";
const RUN = "https://github.com/hyperscaleav/omniglass/actions/runs/42";
const ARTIFACT = "https://github.com/hyperscaleav/omniglass/actions/runs/42/artifacts/7";

const manifest = (id: string, extra: Partial<Manifest> = {}): Manifest => ({
  id,
  title: `Title of ${id}`,
  description: `What ${id} shows.`,
  steps: [
    { n: 1, kind: "goto", caption: "Open the page.", still: "step-01.png" },
    { n: 2, kind: "click", caption: "Open one.", still: "step-02.png" },
  ],
  poster: "step-02.png",
  ...extra,
});

const body = (recorded: Manifest[], failed: string[] = []) =>
  commentBody({ repo: REPO, pr: 925, sha: SHA, headSha: HEAD, runUrl: RUN, artifactUrl: ARTIFACT, recorded, failed });

describe("which walkthroughs a PR records", () => {
  const all = ["find-a-place", "monitoring", "sample-simulation"];

  it("records the walkthroughs whose scripts the PR adds or changes", () => {
    const changed = ["web/src/pages/Explore.tsx", "web/e2e/walkthroughs/find-a-place.yaml", "docs/x.md"];
    expect(selectWalkthroughs(changed, all)).toEqual(["find-a-place"]);
  });

  it("records every walkthrough when the PR touches no script, since any console change can move a flow", () => {
    expect(selectWalkthroughs(["web/src/pages/Explore.tsx"], all)).toEqual(all);
    expect(selectWalkthroughs([], all)).toEqual(all);
  });

  it("ignores fixtures, recorder files, and a script the PR deleted", () => {
    const changed = [
      "web/e2e/walkthroughs/fixtures/tiny.yaml",
      "web/e2e/walkthrough.mjs",
      "web/e2e/walkthroughs/retired.yaml",
    ];
    expect(selectWalkthroughs(changed, all)).toEqual(all);
  });

  it("keeps the tree's order and drops duplicates", () => {
    const changed = ["web/e2e/walkthroughs/sample-simulation.yaml", "web/e2e/walkthroughs/find-a-place.yaml", "web/e2e/walkthroughs/find-a-place.yaml"];
    expect(selectWalkthroughs(changed, all)).toEqual(["find-a-place", "sample-simulation"]);
  });

  it("refuses an id that is not a safe path segment, naming it", () => {
    expect(() => selectWalkthroughs([], ["ok", "Bad Name"])).toThrow('walkthrough id "Bad Name"');
  });

  it("fails when there is nothing to record", () => {
    expect(() => selectWalkthroughs([], [])).toThrow("no walkthroughs");
  });
});

describe("where a PR's stills are committed", () => {
  it("is .github/screenshots/<pr>/walkthroughs/<id>", () => {
    expect(stillsDir(925, "find-a-place")).toBe(".github/screenshots/925/walkthroughs/find-a-place");
  });

  it("refuses a PR number or id that could escape the directory", () => {
    expect(() => stillsDir(0, "a")).toThrow("PR number");
    expect(() => stillsDir(1.5, "a")).toThrow("PR number");
    expect(() => stillsDir(925, "../x")).toThrow('walkthrough id "../x"');
  });
});

describe("the raw URL a still is embedded by", () => {
  it("pins an immutable commit SHA, never a branch", () => {
    expect(rawUrl(REPO, SHA, ".github/screenshots/925/walkthroughs/a/poster.png")).toBe(
      `https://raw.githubusercontent.com/${REPO}/${SHA}/.github/screenshots/925/walkthroughs/a/poster.png`,
    );
    expect(() => rawUrl(REPO, "main", "x.png")).toThrow("full commit SHA");
  });

  it("refuses a malformed repository", () => {
    expect(() => rawUrl("not a repo", SHA, "x.png")).toThrow("owner/name");
  });
});

describe("the walkthrough comment", () => {
  it("opens with the marker the upsert finds it by", () => {
    expect(body([manifest("a")]).startsWith(`${WALKTHROUGH_MARKER}\n`)).toBe(true);
  });

  it("says what was recorded, from which head, and links the run and the video artifact", () => {
    const b = body([manifest("a")]);
    expect(b).toContain(`\`${HEAD.slice(0, 7)}\``);
    expect(b).toContain(`](${RUN})`);
    expect(b).toContain(`](${ARTIFACT})`);
    expect(b).toContain("1 walkthrough recorded");
  });

  it("embeds each walkthrough's poster and every step's still by SHA, with its caption", () => {
    const b = body([manifest("a"), manifest("b")]);
    const dir = `https://raw.githubusercontent.com/${REPO}/${SHA}/.github/screenshots/925/walkthroughs`;
    expect(b).toContain(`#### Title of a`);
    expect(b).toContain(`What a shows.`);
    expect(b).toContain(`![Title of a: poster](${dir}/a/poster.png)`);
    expect(b).toContain(`1. Open the page.`);
    expect(b).toContain(`![a step 1](${dir}/a/step-01.png)`);
    expect(b).toContain(`![b step 2](${dir}/b/step-02.png)`);
    expect(b).toContain("2 walkthroughs recorded");
  });

  it("names every failed walkthrough, since a walkthrough is a test", () => {
    const b = body([manifest("a")], ["broken"]);
    expect(b).toContain("1 walkthrough recorded, 1 failed");
    expect(b).toContain("`broken`");
    expect(b).toMatch(/failed.*run log/s);
  });

  it("still reads when every walkthrough failed", () => {
    const b = body([], ["x", "y"]);
    expect(b).toContain("0 walkthroughs recorded, 2 failed");
    expect(b).not.toContain("raw.githubusercontent.com");
  });

  it("escapes markup in titles and captions so a script cannot inject HTML", () => {
    const b = body([manifest("a", { title: "A <b>bold</b> [link]", steps: [{ n: 1, kind: "goto", caption: "<img src=x>", still: "step-01.png" }], poster: "step-01.png" })]);
    expect(b).not.toContain("<b>");
    expect(b).not.toContain("<img src=x>");
    expect(b).toContain("&lt;img src=x&gt;");
    expect(b).toContain("A &lt;b&gt;bold&lt;/b&gt; \\[link\\]");
  });

  it("refuses a manifest whose stills are not the recorder's names", () => {
    expect(() => body([manifest("a", { poster: "../../x.png" })])).toThrow("still");
  });
});
