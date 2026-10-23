import { describe, it, expect } from "vitest";
import {
  WALKTHROUGH_MARKER,
  cdnUrl,
  commentBody,
  evidenceFiles,
  evidenceRef,
  rawUrl,
  selectWalkthroughs,
  type Manifest,
} from "../e2e/walkthroughpr.mjs";

// Walkthroughs as PR evidence (#925, #939): the run:walkthrough workflow records
// the PR's walkthroughs, commits their preview, poster, stills and video to the
// PR's own evidence ref (refs/walkthroughs/pr-<n>, never the PR branch, so
// nothing reaches main), and upserts one comment. The choices it makes (which
// walkthroughs, which files, where they go, what the comment says) are pure and
// pinned here; the workflow itself only runs them.
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

const body = (recorded: Manifest[], failed: string[] = [], stackUp = true) =>
  commentBody({ repo: REPO, pr: 925, sha: SHA, headSha: HEAD, runUrl: RUN, artifactUrl: ARTIFACT, recorded, failed, stackUp });

describe("which walkthroughs a PR records", () => {
  const all = ["find-a-place", "monitoring", "sample-simulation"];

  it("records only the scripts a scripts-only PR adds or changes", () => {
    const changed = ["web/e2e/walkthroughs/find-a-place.yaml", "docs/x.md"];
    expect(selectWalkthroughs(changed, all)).toEqual(["find-a-place"]);
  });

  it("records every walkthrough when the PR changes anything else under web/, even beside a script", () => {
    expect(selectWalkthroughs(["web/src/pages/Explore.tsx", "web/e2e/walkthroughs/find-a-place.yaml"], all)).toEqual(all);
    expect(selectWalkthroughs(["web/src/pages/Explore.tsx"], all)).toEqual(all);
    expect(selectWalkthroughs(["web/e2e/walkthrough.mjs"], all)).toEqual(all);
    expect(selectWalkthroughs(["web/e2e/walkthroughs/fixtures/tiny.yaml"], all)).toEqual(all);
  });

  it("records every walkthrough when the PR touches no script", () => {
    expect(selectWalkthroughs(["internal/api/x.go"], all)).toEqual(all);
    expect(selectWalkthroughs([], all)).toEqual(all);
  });

  it("records every walkthrough when the only script the PR touched is one it deleted", () => {
    expect(selectWalkthroughs(["web/e2e/walkthroughs/retired.yaml"], all)).toEqual(all);
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

describe("where a PR's evidence is committed", () => {
  it("is the PR's own ref under refs/walkthroughs/, never a branch", () => {
    expect(evidenceRef(925)).toBe("refs/walkthroughs/pr-925");
    expect(evidenceRef(925).startsWith("refs/heads/")).toBe(false);
  });

  it("refuses a PR number that is not a positive integer", () => {
    expect(() => evidenceRef(0)).toThrow("PR number");
    expect(() => evidenceRef(-1)).toThrow("PR number");
    expect(() => evidenceRef(1.5)).toThrow("PR number");
    expect(() => evidenceRef(Number("x"))).toThrow("PR number");
  });
});

describe("the files a walkthrough's evidence carries", () => {
  it("is the poster, every step's still and the video, each under the walkthrough's directory", () => {
    expect(evidenceFiles(manifest("a"))).toEqual(["a/poster.png", "a/step-01.png", "a/step-02.png", "a/a.mp4"]);
  });

  it("leads with the animated preview when the recording has one", () => {
    expect(evidenceFiles(manifest("a", { preview: "preview.webp" }))).toEqual([
      "a/preview.webp",
      "a/poster.png",
      "a/step-01.png",
      "a/step-02.png",
      "a/a.mp4",
    ]);
  });

  it("refuses a manifest whose id, stills or preview could escape the directory", () => {
    expect(() => evidenceFiles(manifest("../x"))).toThrow('walkthrough id "../x"');
    expect(() => evidenceFiles(manifest("a", { poster: "../../x.png" }))).toThrow("still");
    expect(() => evidenceFiles(manifest("a", { steps: [{ n: 1, kind: "goto", caption: "c", still: "/etc/passwd" }], poster: "step-01.png" }))).toThrow("still");
    expect(() => evidenceFiles(manifest("a", { preview: "../x.webp" }))).toThrow("preview");
  });
});

describe("the raw URL a still is embedded by", () => {
  it("pins an immutable commit SHA, never a branch", () => {
    expect(rawUrl(REPO, SHA, "a/poster.png")).toBe(
      `https://raw.githubusercontent.com/${REPO}/${SHA}/a/poster.png`,
    );
    expect(() => rawUrl(REPO, "main", "x.png")).toThrow("full commit SHA");
  });

  it("refuses a malformed repository", () => {
    expect(() => rawUrl("not a repo", SHA, "x.png")).toThrow("owner/name");
  });
});

describe("the CDN URL a video is linked by", () => {
  it("is jsDelivr at the immutable commit SHA, which serves an mp4 as video with range requests, so it seeks", () => {
    expect(cdnUrl(REPO, SHA, "a/a.mp4")).toBe(`https://cdn.jsdelivr.net/gh/${REPO}@${SHA}/a/a.mp4`);
  });

  it("refuses a branch, and a malformed repository", () => {
    expect(() => cdnUrl(REPO, "main", "a/a.mp4")).toThrow("full commit SHA");
    expect(() => cdnUrl("not a repo", SHA, "a/a.mp4")).toThrow("owner/name");
  });
});

describe("the walkthrough comment", () => {
  it("opens with the marker the upsert finds it by", () => {
    expect(body([manifest("a")]).startsWith(`${WALKTHROUGH_MARKER}\n`)).toBe(true);
  });

  it("says what was recorded, from which head, and links the run and the artifact", () => {
    const b = body([manifest("a")]);
    expect(b).toContain(`\`${HEAD.slice(0, 7)}\``);
    expect(b).toContain(`](${RUN})`);
    expect(b).toContain(`](${ARTIFACT})`);
    expect(b).toContain("1 walkthrough recorded");
  });

  it("names the evidence ref the files are committed to, not a path on the PR branch", () => {
    const b = body([manifest("a")]);
    expect(b).toContain("`refs/walkthroughs/pr-925`");
    expect(b).not.toContain(".github/screenshots");
  });

  it("links a scrubbable video per walkthrough through jsDelivr, by the evidence commit's SHA", () => {
    const b = body([manifest("a"), manifest("b")]);
    expect(b).toContain(`[Watch (scrubbable)](https://cdn.jsdelivr.net/gh/${REPO}@${SHA}/a/a.mp4)`);
    expect(b).toContain(`[Watch (scrubbable)](https://cdn.jsdelivr.net/gh/${REPO}@${SHA}/b/b.mp4)`);
    expect(b.indexOf("/a/a.mp4")).toBeLessThan(b.indexOf("#### Title of b"));
  });

  it("puts the video link under the title, above the animated preview that stays inline", () => {
    const b = body([manifest("a", { preview: "preview.webp" })]);
    const watch = b.indexOf("[Watch (scrubbable)]");
    expect(watch).toBeGreaterThan(b.indexOf("#### Title of a"));
    expect(watch).toBeLessThan(b.indexOf("animated preview]("));
  });

  it("embeds each walkthrough's poster and every step's still by SHA, with its caption", () => {
    const b = body([manifest("a"), manifest("b")]);
    const dir = `https://raw.githubusercontent.com/${REPO}/${SHA}`;
    expect(b).toContain(`#### Title of a`);
    expect(b).toContain(`What a shows.`);
    expect(b).toContain(`![Title of a: poster](${dir}/a/poster.png)`);
    expect(b).toContain(`1. Open the page.`);
    expect(b).toContain(`![a step 1](${dir}/a/step-01.png)`);
    expect(b).toContain(`![b step 2](${dir}/b/step-02.png)`);
    expect(b).toContain("2 walkthroughs recorded");
  });

  it("leads each walkthrough with its animated preview, by SHA, above the poster and the folded stills", () => {
    const b = body([manifest("a", { preview: "preview.webp" })]);
    const dir = `https://raw.githubusercontent.com/${REPO}/${SHA}/a`;
    const preview = b.indexOf(`![Title of a: animated preview](${dir}/preview.webp)`);
    expect(preview).toBeGreaterThan(b.indexOf("#### Title of a"));
    expect(preview).toBeLessThan(b.indexOf(`(${dir}/poster.png)`));
    expect(b.indexOf(`(${dir}/poster.png)`)).toBeLessThan(b.indexOf("<details>"));
  });

  it("embeds no preview for a recording that has none", () => {
    expect(body([manifest("a")])).not.toContain("preview.webp");
  });

  it("refuses a preview that is not the recorder's preview name", () => {
    expect(() => body([manifest("a", { preview: "../../x.webp" })])).toThrow("preview");
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
    expect(b).not.toContain("cdn.jsdelivr.net");
  });

  it("escapes markup in titles and captions so a script cannot inject HTML", () => {
    const b = body([manifest("a", { title: "A <b>bold</b> [link]", steps: [{ n: 1, kind: "goto", caption: "<img src=x>", still: "step-01.png" }], poster: "step-01.png" })]);
    expect(b).not.toContain("<b>");
    expect(b).not.toContain("<img src=x>");
    expect(b).toContain("&lt;img src=x&gt;");
    expect(b).toContain("A &lt;b&gt;bold&lt;/b&gt; \\[link\\]");
  });

  it("escapes a backslash before a bracket, so an escaped link cannot come back alive", () => {
    const b = body([manifest("a", { steps: [{ n: 1, kind: "goto", caption: "\\[x\\](https://evil)", still: "step-01.png" }], poster: "step-01.png" })]);
    expect(b).toContain("1. \\\\\\[x\\\\\\](https://evil)");
  });

  it("folds newlines to a space, so a script cannot forge a heading or break the fold", () => {
    const b = body([manifest("a", { title: "T\n### forged", description: "line one\r\n</details>\n# h", steps: [{ n: 1, kind: "goto", caption: "a\n\n## b", still: "step-01.png" }], poster: "step-01.png" })]);
    expect(b).toContain("#### T ### forged");
    expect(b).toContain("line one &lt;/details&gt; # h");
    expect(b).toContain("1. a  ## b");
    expect(b).not.toMatch(/^#{1,2} /m);
    expect(b).not.toMatch(/^### forged/m);
    expect(b.match(/<\/details>/g)).toHaveLength(1);
  });

  it("says the stack never came up, rather than blaming every walkthrough", () => {
    const b = body([], ["a", "b"], false);
    expect(b).toContain("The walkthrough stack never came up");
    expect(b).not.toContain("failed:");
    expect(b).not.toContain("`a`");
    expect(b).toContain("0 walkthroughs recorded");
  });

  it("refuses a manifest whose stills are not the recorder's names", () => {
    expect(() => body([manifest("a", { poster: "../../x.png" })])).toThrow("still");
  });
});
