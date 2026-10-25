import { describe, it, expect } from "vitest";
import {
  CLIP_END,
  CLIP_START,
  DEMO_MARKER,
  FRESHNESS_MARKER,
  DEMO_MARKERS,
  PENDING_SHA,
  WALKTHROUGH_MARKER,
  cdnUrl,
  clipBody,
  commentBody,
  evidenceFiles,
  evidenceRef,
  evidenceRefsToPrune,
  everyDemo,
  freshnessReport,
  demoTag,
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

const NONE = { recorded: [], failed: [], unstarted: [] };
const body = (recorded: Manifest[], failed: string[] = [], unstarted: string[] = []) =>
  commentBody({ repo: REPO, pr: 925, sha: SHA, headSha: HEAD, runUrl: RUN, artifactUrl: ARTIFACT, walkthroughs: { recorded, failed, unstarted }, tapes: NONE });
const tapeBody = (recorded: TapeManifest[], failed: string[] = [], unstarted: string[] = [], walkthroughs: Manifest[] = []) =>
  commentBody({
    repo: REPO,
    pr: 925,
    sha: SHA,
    headSha: HEAD,
    runUrl: RUN,
    artifactUrl: ARTIFACT,
    walkthroughs: { recorded: walkthroughs, failed: [], unstarted: [] },
    tapes: { recorded, failed, unstarted },
  });

describe("the demo tag a PR body carries", () => {
  it("is a line of its own, demo: and one or more demo ids, spaces or commas between", () => {
    expect(demoTag("## Demo\n\ndemo: http-logins\n")).toEqual({ ids: ["http-logins"] });
    expect(demoTag("demo: http-logins, find-a-place monitoring")).toEqual({ ids: ["http-logins", "find-a-place", "monitoring"] });
    expect(demoTag("- demo: http-logins")).toEqual({ ids: ["http-logins"] });
    expect(demoTag("  demo:find-a-place  \r\nnext line")).toEqual({ ids: ["find-a-place"] });
  });

  it("gathers every tag line in the body, once each, in order", () => {
    expect(demoTag("demo: b\ntext\ndemo: a b")).toEqual({ ids: ["b", "a"] });
  });

  it("says none, with the reason in parentheses", () => {
    expect(demoTag("demo: none (docs-only change)")).toEqual({ none: "docs-only change" });
    expect(demoTag("- demo: none (test-only, no behavior a client can observe)")).toEqual({ none: "test-only, no behavior a client can observe" });
  });

  it("is absent when no line is a tag: the ship-review's Demo line, a bold bullet and prose are not", () => {
    expect(demoTag("")).toBeNull();
    expect(demoTag("Demo:      terminal demo http-logins")).toBeNull();
    expect(demoTag("- **Demo:** terminal demo `http-logins`")).toBeNull();
    expect(demoTag("The demo: it shows a login.")).toBeNull();
    expect(demoTag("demos: http-logins")).toBeNull();
  });

  it("ignores a tag quoted in a code fence or an HTML comment, as a template or an example quotes it", () => {
    expect(demoTag("```\ndemo: http-logins\n```")).toBeNull();
    expect(demoTag("~~~md\ndemo: none (x)\n~~~\ndemo: a")).toEqual({ ids: ["a"] });
    expect(demoTag("<!-- write demo: <id> here\ndemo: <id>\n-->")).toBeNull();
  });

  it("refuses a tag line that names nothing, or a word that is not a demo id, naming it", () => {
    expect(() => demoTag("demo:")).toThrow("demo: names no demo");
    expect(() => demoTag("demo: Http-Logins")).toThrow('"Http-Logins" is not a demo id');
    expect(() => demoTag("demo: the tape (see below)")).toThrow('"(see" is not a demo id');
  });

  it("refuses none with no reason, and none beside a demo", () => {
    expect(() => demoTag("demo: none")).toThrow("demo: none (<reason>)");
    expect(() => demoTag("demo: none ()")).toThrow("demo: none (<reason>)");
    expect(() => demoTag("demo: none docs-only")).toThrow("demo: none (<reason>)");
    expect(() => demoTag("demo: none (docs)\ndemo: a")).toThrow("either names demos or says none");
    expect(() => demoTag("demo: none (a)\ndemo: none (b)")).toThrow("says none once");
  });
});

describe("which demos a PR records", () => {
  const tapes = ["http-logins", "node-enroll"];
  const walks = ["find-a-place", "monitoring", "sample-simulation"];
  const select = (changed: string[], body = "") => selectDemos(changed, demoTag(body), tapes, walks);
  const ids = (changed: string[], body = "") => select(changed, body).map((d) => d.id);

  it("records exactly the demos the PR tags", () => {
    expect(ids(["internal/api/x.go"], "demo: http-logins")).toEqual(["http-logins"]);
    expect(ids([], "demo: monitoring node-enroll")).toEqual(["node-enroll", "monitoring"]);
  });

  it("records a tape or walkthrough script the PR adds or changes, tagged or not", () => {
    expect(ids(["scripts/demo/http-logins.tape", "docs/x.md"])).toEqual(["http-logins"]);
    expect(ids(["web/e2e/walkthroughs/find-a-place.yaml"], "demo: none (docs-only)")).toEqual(["find-a-place"]);
    expect(ids(["scripts/demo/node-enroll.tape"], "demo: monitoring")).toEqual(["node-enroll", "monitoring"]);
  });

  it("fans out to nothing else: touching the console, a recorder, the stack or the evidence pipeline records no extra demo", () => {
    const wide = [
      "web/src/pages/Explore.tsx",
      "web/e2e/walkthrough.mjs",
      "web/e2e/walkthroughs/fixtures/tiny.yaml",
      "web/e2e/tapes.sh",
      "web/e2e/tapelib.mjs",
      "web/e2e/tape/prelude.sh",
      "scripts/demo/README",
      "web/e2e/lib/stack.sh",
      "web/e2e/demo.sh",
      "web/e2e/walkthroughpr.mjs",
      ".github/workflows/demo.yml",
    ];
    expect(ids(wide)).toEqual([]);
    expect(ids(wide, "demo: sample-simulation")).toEqual(["sample-simulation"]);
  });

  it("records none when the PR tags none, says none, and changes no demo", () => {
    expect(ids(["internal/api/x.go"])).toEqual([]);
    expect(ids(["internal/api/x.go"], "demo: none (refactor, no behavior)")).toEqual([]);
    expect(ids([])).toEqual([]);
  });

  it("records nothing for a script the PR deleted, since it is no longer in the tree", () => {
    expect(ids(["web/e2e/walkthroughs/retired.yaml", "scripts/demo/retired.tape"])).toEqual([]);
  });

  it("says why each demo is recorded, and which recorder plays it", () => {
    expect(select(["scripts/demo/http-logins.tape"], "demo: http-logins monitoring")).toEqual([
      { id: "http-logins", kind: "tape", why: "tagged, and changed by the PR" },
      { id: "monitoring", kind: "walkthrough", why: "tagged" },
    ]);
    expect(select(["web/e2e/walkthroughs/find-a-place.yaml"])).toEqual([{ id: "find-a-place", kind: "walkthrough", why: "changed by the PR" }]);
  });

  it("keeps the tree's order, tapes first, and drops duplicates", () => {
    const changed = ["scripts/demo/node-enroll.tape", "web/e2e/walkthroughs/sample-simulation.yaml", "web/e2e/walkthroughs/find-a-place.yaml", "web/e2e/walkthroughs/find-a-place.yaml"];
    expect(ids(changed, "demo: http-logins node-enroll")).toEqual(["http-logins", "node-enroll", "find-a-place", "sample-simulation"]);
  });

  it("refuses a tag naming a demo the tree does not have, naming it and where it would be", () => {
    expect(() => ids([], "demo: http-login")).toThrow('names no demo "http-login" (neither scripts/demo/http-login.tape nor web/e2e/walkthroughs/http-login.yaml)');
  });

  it("refuses an id that is not a safe path segment, naming it", () => {
    expect(() => selectDemos([], null, ["ok"], ["Bad Name"])).toThrow('walkthrough id "Bad Name"');
    expect(() => selectDemos([], null, ["Bad Tape"], ["ok"])).toThrow('tape id "Bad Tape"');
  });

  it("refuses an id two demos share, since a demo id names one demo and one evidence directory", () => {
    expect(() => selectDemos([], null, ["same"], ["same", "other"])).toThrow('demo id "same" names both a tape and a walkthrough');
  });

  it("fails when the tree has no demos at all", () => {
    expect(() => selectDemos([], null, [], [])).toThrow("no demos");
  });
});

describe("every demo, as the freshness run on main records them", () => {
  it("is every tape, then every walkthrough, each in the tree's order", () => {
    expect(everyDemo(["b-tape", "a-tape"], ["w"])).toEqual([
      { id: "b-tape", kind: "tape", why: "the freshness run records every demo" },
      { id: "a-tape", kind: "tape", why: "the freshness run records every demo" },
      { id: "w", kind: "walkthrough", why: "the freshness run records every demo" },
    ]);
  });

  it("holds the same id rules as a PR's selection", () => {
    expect(() => everyDemo(["same"], ["same"])).toThrow("names both");
    expect(() => everyDemo([], [])).toThrow("no demos");
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
      walkthroughs: { recorded, failed: ["c"], unstarted: [] },
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

  it("says which demos never ran because their stack never came up, rather than blaming them", () => {
    const b = body([manifest("a")], [], ["b", "c"]);
    expect(b).toContain("1 walkthrough recorded, 2 did not run");
    expect(b).toContain("**Did not run**");
    expect(b).toMatch(/stack never came up.*not the demo/s);
    expect(b).toContain("- `b` (walkthrough)");
    expect(b).toContain("- `c` (walkthrough)");
    expect(b).not.toContain("**Failed**");
  });

  it("refuses an unstarted id that is not a safe path segment", () => {
    expect(() => body([], [], ["../x"])).toThrow('walkthrough id "../x"');
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
    const b = tapeBody([tapeManifest("t")], [], [], [manifest("a", { preview: "preview.webp" })]);
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

  it("names a terminal demo that never ran, apart from one that failed", () => {
    const b = tapeBody([], ["t"], ["u"]);
    expect(b).toContain("0 demos recorded, 1 failed, 1 did not run");
    expect(b).toContain("- `t` (terminal demo)");
    expect(b).toContain("- `u` (terminal demo)");
    expect(b.indexOf("- `t`")).toBeLessThan(b.indexOf("**Did not run**"));
  });

  it("escapes markup in a tape's title and description", () => {
    const b = tapeBody([tapeManifest("t", { title: "T <img src=x> [l]", description: "a\n# h" })]);
    expect(b).not.toContain("<img src=x>");
    expect(b).toContain("#### T &lt;img src=x&gt; \\[l\\]");
    expect(b).not.toMatch(/^# h/m);
  });

  it("says nothing was recorded only when nothing was selected to", () => {
    expect(commentBody({ repo: REPO, pr: 925, sha: SHA, headSha: HEAD, runUrl: RUN, artifactUrl: ARTIFACT, tapes: NONE, walkthroughs: NONE })).toContain("Nothing was recorded");
    expect(tapeBody([tapeManifest("t")])).not.toContain("Nothing was recorded");
    expect(tapeBody([], [], ["u"])).not.toContain("Nothing was recorded");
  });

  it("refuses a terminal demo that never passed its budget", () => {
    expect(() => tapeBody([tapeManifest("t", { budget: undefined })])).toThrow("budget mark");
  });

  it("tells the author how to record the current head again, by the demo label", () => {
    expect(tapeBody([tapeManifest("t")])).toContain("remove and re-add the `run:demo` label");
  });

  it("says the clip also plays in the PR body's Demo section", () => {
    expect(tapeBody([tapeManifest("t")])).toContain("PR body's Demo section");
  });
});

describe("the clip the PR body's Demo section carries", () => {
  const raw = `https://raw.githubusercontent.com/${REPO}/${SHA}`;
  const cdn = `https://cdn.jsdelivr.net/gh/${REPO}@${SHA}`;
  const clip = (tapes: TapeManifest[], walks: Manifest[], failed: string[] = [], unstarted: string[] = []) =>
    clipBody({ repo: REPO, sha: SHA, headSha: HEAD, tapes: { recorded: tapes, failed, unstarted }, walkthroughs: { recorded: walks, failed: [], unstarted: [] } });

  it("is fenced by its own markers, which the publish job finds it by", () => {
    expect(CLIP_START).toBe("<!-- omniglass-demo-clip -->");
    expect(CLIP_END).toBe("<!-- /omniglass-demo-clip -->");
  });

  it("plays a terminal demo's GIF inline under a line linking its scrubbable MP4, pinned to the evidence commit", () => {
    const c = clip([tapeManifest("t")], []);
    expect(c).toContain(`**Tape t** (\`t\`, terminal demo, 1m 8s): [Watch (scrubbable)](${cdn}/t/t.mp4)`);
    expect(c).toContain(`![Tape t: terminal recording](${raw}/t/t.gif)`);
    expect(c.indexOf("Watch (scrubbable)")).toBeLessThan(c.indexOf("t.gif)"));
  });

  it("plays a walkthrough's animated preview, or its poster when it has none, under its scrubbable video", () => {
    const c = clip([], [manifest("a", { preview: "preview.webp" }), manifest("b")]);
    expect(c).toContain(`**Title of a** (\`a\`, walkthrough, 2 steps): [Watch (scrubbable)](${cdn}/a/a.mp4)`);
    expect(c).toContain(`![Title of a: animated preview](${raw}/a/preview.webp)`);
    expect(c).toContain(`![Title of b: poster](${raw}/b/poster.png)`);
    expect(c).not.toContain("step-01.png");
  });

  it("lists terminal demos first, says which head they were recorded from, and names the ones that failed or never ran", () => {
    const c = clip([tapeManifest("t")], [manifest("a")], ["u"], ["v"]);
    expect(c.indexOf("**Tape t**")).toBeLessThan(c.indexOf("**Title of a**"));
    expect(c).toContain(`\`${HEAD.slice(0, 7)}\``);
    expect(c).toContain("**Failed:** `u` (terminal demo)");
    expect(c).toContain("**Did not run:** `v` (terminal demo)");
  });

  it("carries neither marker, nor any HTML a title could smuggle in to close the block early", () => {
    const c = clip([tapeManifest("t", { title: "x <!-- /omniglass-demo-clip --> y" })], [manifest("a", { title: "<img src=x>" })]);
    expect(c).not.toContain(CLIP_START);
    expect(c).not.toContain(CLIP_END);
    expect(c).not.toContain("<img");
    expect(c).not.toContain("<!--");
  });

  it("is written against the pending SHA, every link of which publish pins to the evidence commit", () => {
    const pending = clipBody({ repo: REPO, sha: PENDING_SHA, headSha: HEAD, tapes: { recorded: [tapeManifest("t")], failed: [], unstarted: [] }, walkthroughs: NONE });
    expect(pending.replaceAll(PENDING_SHA, SHA)).toBe(clip([tapeManifest("t")], []));
  });

  it("refuses a manifest that never passed its budget, or whose files are not the recorder's", () => {
    expect(() => clip([tapeManifest("t", { budget: undefined })], [])).toThrow("budget mark");
    expect(() => clip([], [manifest("a", { preview: "../x.webp" })])).toThrow("preview");
  });
});

describe("the freshness report the nightly run files", () => {
  const RUN_URL = "https://github.com/hyperscaleav/omniglass/actions/runs/7";
  const report = (tapes: { recorded: TapeManifest[]; failed: string[]; unstarted: string[] }, walks: { recorded: Manifest[]; failed: string[]; unstarted: string[] }) =>
    freshnessReport({ runUrl: RUN_URL, headSha: HEAD, tapes, walkthroughs: walks });

  it("is green when every demo recorded, saying how many from which commit of main", () => {
    const r = report({ recorded: [tapeManifest("t")], failed: [], unstarted: [] }, { recorded: [manifest("a"), manifest("b")], failed: [], unstarted: [] });
    expect(r.ok).toBe(true);
    expect(r.summary).toContain("All 3 demos recorded green");
    expect(r.summary).toContain(`\`${HEAD.slice(0, 7)}\``);
    expect(r.summary).toContain(`](${RUN_URL})`);
  });

  it("is red when any demo failed or never ran, and its body names each with its kind", () => {
    const r = report({ recorded: [tapeManifest("t")], failed: ["u"], unstarted: [] }, { recorded: [], failed: [], unstarted: ["w"] });
    expect(r.ok).toBe(false);
    expect(r.body.startsWith(`${FRESHNESS_MARKER}\n`)).toBe(true);
    expect(r.body).toContain("2 of 3 demos did not record green");
    expect(r.body).toContain("- `u` (terminal demo)");
    expect(r.body).toContain("**Did not run**");
    expect(r.body).toContain("- `w` (walkthrough)");
    expect(r.body).toContain("make demo ID=u");
    expect(r.body).toContain(`](${RUN_URL})`);
  });

  it("refuses an id that is not a safe path segment", () => {
    expect(() => report({ recorded: [], failed: ["../x"], unstarted: [] }, NONE)).toThrow('tape id "../x"');
  });
});
