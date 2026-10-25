import { describe, it, expect } from "vitest";
import {
  DEMO_MARKER,
  DEMO_MARKERS,
  PENDING_SHA,
  WALKTHROUGH_MARKER,
  cdnUrl,
  commentBody,
  evidenceFiles,
  evidenceRef,
  evidenceRefsToPrune,
  rawUrl,
  selectDemos,
  tapeEvidenceFiles,
  type Manifest,
  type TapeManifest,
} from "../e2e/walkthroughpr.mjs";

// Demos as PR evidence (#925, #939, #1132): the run:demo workflow records the
// PR's demos, terminal recordings of its tapes and walkthroughs of its console
// flows, commits their files to the PR's own evidence ref
// (refs/walkthroughs/pr-<n>, never the PR branch, so nothing reaches main), and
// upserts one comment listing every demo. The choices it makes (which demos,
// which files, where they go, what the comment says) are pure and pinned here;
// the workflow itself only runs them.
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

const tapeManifest = (id: string, extra: Partial<TapeManifest> = {}): TapeManifest => ({
  id,
  title: `Tape ${id}`,
  description: `What tape ${id} shows.`,
  gif: `${id}.gif`,
  mp4: `${id}.mp4`,
  budget: { seconds: 68.4, gifBytes: 3_000_000, mp4Bytes: 2_000_000 },
  ...extra,
});

const NONE = { recorded: [], failed: [], stackUp: true };
const body = (recorded: Manifest[], failed: string[] = [], stackUp = true) =>
  commentBody({ repo: REPO, pr: 925, sha: SHA, headSha: HEAD, runUrl: RUN, artifactUrl: ARTIFACT, walkthroughs: { recorded, failed, stackUp }, tapes: NONE });
const tapeBody = (recorded: TapeManifest[], failed: string[] = [], stackUp = true, walkthroughs: Manifest[] = []) =>
  commentBody({
    repo: REPO,
    pr: 925,
    sha: SHA,
    headSha: HEAD,
    runUrl: RUN,
    artifactUrl: ARTIFACT,
    walkthroughs: { recorded: walkthroughs, failed: [], stackUp: true },
    tapes: { recorded, failed, stackUp },
  });

describe("which demos a PR records", () => {
  const tapes = ["http-logins", "node-enroll"];
  const walks = ["find-a-place", "monitoring", "sample-simulation"];
  const select = (changed: string[]) => selectDemos(changed, tapes, walks);

  it("records only the tapes and walkthrough scripts a PR adds or changes, when nothing else moves a recorder", () => {
    expect(select(["scripts/demo/http-logins.tape", "internal/api/x.go", "docs/x.md"])).toEqual(["http-logins"]);
    expect(select(["web/e2e/walkthroughs/find-a-place.yaml", "docs/x.md"])).toEqual(["find-a-place"]);
    expect(select(["scripts/demo/node-enroll.tape", "web/e2e/walkthroughs/monitoring.yaml"])).toEqual(["node-enroll", "monitoring"]);
  });

  it("records every walkthrough when the PR changes anything else under web/, even beside a script", () => {
    expect(select(["web/src/pages/Explore.tsx", "web/e2e/walkthroughs/find-a-place.yaml"])).toEqual(walks);
    expect(select(["web/src/pages/Explore.tsx"])).toEqual(walks);
    expect(select(["web/e2e/walkthrough.mjs"])).toEqual(walks);
    expect(select(["web/e2e/walkthroughs/fixtures/tiny.yaml"])).toEqual(walks);
    expect(select(["web/src/pages/Explore.tsx", "scripts/demo/http-logins.tape"])).toEqual(["http-logins", ...walks]);
  });

  it("records every tape when the PR changes the terminal recorder, and not every walkthrough for that alone", () => {
    expect(select(["web/e2e/tapes.sh"])).toEqual(tapes);
    expect(select(["web/e2e/tapelib.mjs", "web/e2e/tape.mjs"])).toEqual(tapes);
    expect(select(["web/e2e/tape/prelude.sh"])).toEqual(tapes);
    expect(select(["web/e2e/tape/fixtures/passes.tape"])).toEqual(tapes);
    expect(select(["scripts/demo/README"])).toEqual(tapes);
  });

  it("records every demo of both kinds when the PR changes the stack both recorders play on", () => {
    expect(select(["web/e2e/lib/stack.sh"])).toEqual([...tapes, ...walks]);
  });

  it("records every demo when the PR names none and moves no recorder, as the walkthrough label always has", () => {
    expect(select(["internal/api/x.go"])).toEqual([...tapes, ...walks]);
    expect(select([])).toEqual([...tapes, ...walks]);
  });

  it("records every demo when the only script the PR touched is one it deleted", () => {
    expect(select(["web/e2e/walkthroughs/retired.yaml", "scripts/demo/retired.tape"])).toEqual([...tapes, ...walks]);
  });

  it("keeps the tree's order, tapes first, and drops duplicates", () => {
    const changed = ["scripts/demo/node-enroll.tape", "scripts/demo/http-logins.tape", "web/e2e/walkthroughs/sample-simulation.yaml", "web/e2e/walkthroughs/find-a-place.yaml", "web/e2e/walkthroughs/find-a-place.yaml"];
    expect(select(changed)).toEqual(["http-logins", "node-enroll", "find-a-place", "sample-simulation"]);
  });

  it("refuses an id that is not a safe path segment, naming it", () => {
    expect(() => selectDemos([], ["ok"], ["Bad Name"])).toThrow('walkthrough id "Bad Name"');
    expect(() => selectDemos([], ["Bad Tape"], ["ok"])).toThrow('tape id "Bad Tape"');
  });

  it("refuses an id two demos share, since a demo id names one demo and one evidence directory", () => {
    expect(() => selectDemos([], ["same"], ["same", "other"])).toThrow('demo id "same" names both a tape and a walkthrough');
  });

  it("fails when there is nothing to record", () => {
    expect(() => selectDemos([], [], [])).toThrow("no demos");
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

describe("which evidence refs the scheduled prune deletes", () => {
  const now = new Date("2026-10-23T00:00:00Z");
  const daysAgo = (d: number) => new Date(now.getTime() - d * 86_400_000).toISOString();
  const refs = ["refs/walkthroughs/pr-1", "refs/walkthroughs/pr-2", "refs/walkthroughs/pr-3", "refs/walkthroughs/pr-4"];

  it("deletes the ref of a PR closed more than the retention ago, and keeps a recently closed or open one", () => {
    const prs = [
      { number: 1, state: "closed", closed_at: daysAgo(31) },
      { number: 2, state: "closed", closed_at: daysAgo(29) },
      { number: 3, state: "open", closed_at: null },
      { number: 4, state: "closed", closed_at: daysAgo(400) },
    ];
    expect(evidenceRefsToPrune(refs, prs, now, 30)).toEqual(["refs/walkthroughs/pr-1", "refs/walkthroughs/pr-4"]);
  });

  it("keeps a ref whose PR it was not told about, and never touches a ref outside the evidence pattern", () => {
    const prs = [{ number: 9, state: "closed", closed_at: daysAgo(90) }];
    expect(evidenceRefsToPrune(["refs/walkthroughs/pr-1", "refs/heads/main", "refs/walkthroughs/pr-9x", "refs/walkthroughs/pr-9"], prs, now, 30)).toEqual([
      "refs/walkthroughs/pr-9",
    ]);
  });

  it("keeps a closed PR whose close time is missing or unreadable", () => {
    const prs = [
      { number: 1, state: "closed", closed_at: null },
      { number: 2, state: "closed", closed_at: "not a date" },
    ];
    expect(evidenceRefsToPrune(refs, prs, now, 30)).toEqual([]);
  });

  it("refuses a retention that is not a positive whole number of days", () => {
    expect(() => evidenceRefsToPrune(refs, [], now, 0)).toThrow("retention");
    expect(() => evidenceRefsToPrune(refs, [], now, 1.5)).toThrow("retention");
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

describe("the files a terminal demo's evidence carries", () => {
  it("is the GIF and the MP4, under the demo's directory", () => {
    expect(tapeEvidenceFiles(tapeManifest("t"))).toEqual(["t/t.gif", "t/t.mp4"]);
  });

  it("refuses a manifest whose id or files could escape the directory, or that never passed", () => {
    expect(() => tapeEvidenceFiles(tapeManifest("../x"))).toThrow('tape id "../x"');
    expect(() => tapeEvidenceFiles(tapeManifest("t", { gif: "../../x.gif" }))).toThrow("t.gif");
    expect(() => tapeEvidenceFiles(tapeManifest("t", { mp4: "other.mp4" }))).toThrow("t.mp4");
    expect(() => tapeEvidenceFiles(tapeManifest("t", { budget: undefined }))).toThrow("budget mark");
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

describe("the demo comment", () => {
  it("written against the pending SHA, becomes the pinned comment once publish swaps in the evidence commit's", () => {
    expect(PENDING_SHA).toBe("0000000000000000000000000000000000000000");
    const recorded = [manifest("a", { preview: "preview.webp" }), manifest("b")];
    const pending = commentBody({
      repo: REPO,
      pr: 925,
      sha: PENDING_SHA,
      headSha: HEAD,
      runUrl: RUN,
      artifactUrl: ARTIFACT,
      walkthroughs: { recorded, failed: ["c"], stackUp: true },
      tapes: NONE,
    });
    expect(pending.replaceAll(PENDING_SHA, SHA)).toBe(body(recorded, ["c"]));
  });

  it("opens with the marker the upsert finds it by", () => {
    expect(body([manifest("a")]).startsWith(`${DEMO_MARKER}\n`)).toBe(true);
    expect(tapeBody([tapeManifest("t")]).startsWith(`${DEMO_MARKER}\n`)).toBe(true);
  });

  it("is found again by its own marker or by the walkthrough comment's earlier one, so a PR labelled before demos keeps one comment", () => {
    expect(DEMO_MARKERS).toEqual([DEMO_MARKER, WALKTHROUGH_MARKER]);
    expect(DEMO_MARKER).not.toBe(WALKTHROUGH_MARKER);
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
    expect(b).toContain("0 demos recorded, 2 failed");
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
    expect(b).toContain("0 demos recorded");
  });

  it("refuses a manifest whose stills are not the recorder's names", () => {
    expect(() => body([manifest("a", { poster: "../../x.png" })])).toThrow("still");
  });
});

describe("the demo comment's terminal demos", () => {
  const raw = `https://raw.githubusercontent.com/${REPO}/${SHA}`;
  const cdn = `https://cdn.jsdelivr.net/gh/${REPO}@${SHA}`;

  it("plays each terminal demo's GIF inline by the evidence commit's SHA, under its title and description", () => {
    const b = tapeBody([tapeManifest("t")]);
    expect(b).toContain("### Demos");
    expect(b).toContain("#### Tape t");
    expect(b).toContain("What tape t shows.");
    expect(b).toContain(`![Tape t: terminal recording](${raw}/t/t.gif)`);
    expect(b.indexOf("#### Tape t")).toBeLessThan(b.indexOf("t.gif)"));
  });

  it("links its MP4 through jsDelivr as the scrubbable video, above the GIF", () => {
    const b = tapeBody([tapeManifest("t")]);
    const watch = b.indexOf(`[Watch (scrubbable)](${cdn}/t/t.mp4)`);
    expect(watch).toBeGreaterThan(b.indexOf("#### Tape t"));
    expect(watch).toBeLessThan(b.indexOf("t.gif)"));
    expect(b).toContain("`t`, terminal demo, 1m 8s:");
  });

  it("lists every demo in one comment, terminal demos first, then walkthroughs", () => {
    const b = tapeBody([tapeManifest("t")], [], true, [manifest("a", { preview: "preview.webp" })]);
    expect(b).toContain("1 terminal demo and 1 walkthrough recorded");
    expect(b.indexOf("#### Tape t")).toBeLessThan(b.indexOf("#### Title of a"));
    expect(b).toContain(`(${cdn}/a/a.mp4)`);
    expect(b).toContain(`(${raw}/a/preview.webp)`);
    expect(b.match(new RegExp(DEMO_MARKER, "g"))).toHaveLength(1);
  });

  it("names a failed terminal demo with its kind", () => {
    const b = tapeBody([tapeManifest("t")], ["broken"]);
    expect(b).toContain("1 terminal demo recorded, 1 failed");
    expect(b).toContain("- `broken` (terminal demo)");
    expect(b).toMatch(/a command it typed exited non-zero/);
  });

  it("says the terminal demos' stack never came up, rather than blaming every tape", () => {
    const b = tapeBody([], ["t", "u"], false);
    expect(b).toContain("The terminal demos' stack never came up");
    expect(b).not.toContain("- `t`");
    expect(b).toContain("0 demos recorded");
  });

  it("escapes markup in a tape's title and description", () => {
    const b = tapeBody([tapeManifest("t", { title: "T <img src=x> [l]", description: "a\n# h" })]);
    expect(b).not.toContain("<img src=x>");
    expect(b).toContain("#### T &lt;img src=x&gt; \\[l\\]");
    expect(b).not.toMatch(/^# h/m);
  });

  it("refuses a terminal demo that never passed its budget", () => {
    expect(() => tapeBody([tapeManifest("t", { budget: undefined })])).toThrow("budget mark");
  });

  it("tells the author how to record the current head again, by the demo label", () => {
    expect(tapeBody([tapeManifest("t")])).toContain("remove and re-add the `run:demo` label");
  });
});
