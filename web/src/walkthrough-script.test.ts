import { describe, it, expect } from "vitest";
import { readdirSync, readFileSync } from "node:fs";
import { join } from "node:path";
import {
  MAX_BYTES,
  MAX_PREVIEW_BYTES,
  MAX_SECONDS,
  PREVIEW_MIN_YMAX,
  budgetLimits,
  previewOpeningProblem,
  paintSeconds,
  parseLumas,
  previewScanProblem,
  previewStartFromLumas,
  TITLE_HOLD_MS,
  readyFailure,
  budgetProblems,
  idProblem,
  parseWalkthrough,
  runSetup,
  setupCollisions,
  stepFailure,
  stillName,
  validateWalkthrough,
} from "../e2e/walkthroughlib.mjs";

// The walkthrough script format (#924) is data, so its validation is pure and
// strict: a typo in a script fails before a browser starts, naming the file and
// the step, never as a puzzling recording. The recorder's browser half is
// proven against the real stack by make test-walkthroughs.
const F = "w.yaml";
const ok = (steps: unknown[], extra = {}) => validateWalkthrough({ title: "T", steps, ...extra }, F);
const bad = (doc: unknown) => {
  try {
    validateWalkthrough(doc, F);
  } catch (e) {
    return (e as Error).message;
  }
  throw new Error("expected the walkthrough to be rejected");
};

describe("walkthrough script validation", () => {
  it("normalizes every step kind", () => {
    const w = ok(
      [
        { goto: "/simulation", caption: "a" },
        { click: '[data-testid="sim-row"] >> nth=0', caption: "b" },
        { click: { role: "button", name: "Close", exact: true }, caption: "c" },
        { hover: { role: "button", name: "Latency" }, caption: "i" },
        { type: { target: "#q", text: "display" }, caption: "d" },
        { select: { target: { role: "combobox", name: "Product" }, value: "x" }, caption: "e" },
        { press: "Escape", caption: "f", hold: 500 },
        { wait: { text: "Hits", timeout: 5000, exact: true }, caption: "g" },
        { wait: { selector: "[data-testid=sim-table]" }, caption: "h" },
      ],
      { description: "D" },
    );
    expect(w.title).toBe("T");
    expect(w.description).toBe("D");
    expect(w.steps.map((s) => s.kind)).toEqual(["goto", "click", "click", "hover", "type", "select", "press", "wait", "wait"]);
    expect(w.steps[0].path).toBe("/simulation");
    expect(w.steps[3]).toMatchObject({ n: 4, target: { role: "button", name: "Latency" } });
    expect(w.steps[4]).toMatchObject({ n: 5, target: "#q", text: "display", hold: 0 });
    expect(w.steps[6]).toMatchObject({ key: "Escape", hold: 500 });
    expect(w.steps[7]).toMatchObject({ target: { text: "Hits" }, timeout: 5000 });
    expect(w.steps[8].timeout).toBe(10_000);
  });

  it("rejects an unknown top-level key and a missing title, naming the file", () => {
    const m = bad({ titel: "T", steps: [{ press: "Enter", caption: "a" }] });
    expect(m).toContain('w.yaml: unknown key "titel"');
    expect(m).toContain("w.yaml: needs a title");
  });

  it("rejects an unknown kind, naming the step", () => {
    expect(bad({ title: "T", steps: [{ press: "Enter", caption: "a" }, { drag: "#x", caption: "b" }] })).toContain(
      'w.yaml: step 2 has no kind: one of goto, click, hover, type, select, press, wait (unknown kind or key "drag")',
    );
  });

  it("rejects an unknown key on a step, naming the step and its kind", () => {
    expect(bad({ title: "T", steps: [{ click: "#x", caption: "a", timeout: 3 }] })).toContain('w.yaml: step 1 (click) has unknown key "timeout"');
    expect(bad({ title: "T", steps: [{ type: { target: "#x", text: "a", delay: 9 }, caption: "a" }] })).toContain(
      'w.yaml: step 1 (type) has unknown key "delay"',
    );
    expect(bad({ title: "T", steps: [{ click: { role: "row", label: "x" }, caption: "a" }] })).toContain(
      'w.yaml: step 1 (click) has unknown key "label"',
    );
  });

  it("rejects two kinds on one step, a missing caption, and a bad wait", () => {
    expect(bad({ title: "T", steps: [{ click: "#x", press: "Enter", caption: "a" }] })).toBe("w.yaml: step 1 has more than one kind: click, press");
    expect(bad({ title: "T", steps: [{ press: "Enter" }] })).toContain("w.yaml: step 1 (press) needs a caption");
    expect(bad({ title: "T", steps: [{ wait: { timeout: 100 }, caption: "a" }] })).toContain("step 1 (wait) needs text, selector, or role");
    expect(bad({ title: "T", steps: [{ wait: { text: "x", timeout: 0 }, caption: "a" }] })).toContain("timeout must be whole milliseconds");
    expect(bad({ title: "T", steps: [{ goto: "simulation", caption: "a" }] })).toContain('must be a console path starting with "/"');
    expect(bad({ title: "T", steps: [] })).toContain("needs steps");
  });

  it("takes the poster from the step that asks, else the last, and only one", () => {
    const done = { wait: { text: "x" }, caption: "c" };
    expect(ok([{ press: "a", caption: "a" }, done]).poster).toBe(2);
    expect(ok([{ press: "a", caption: "a", poster: true }, done]).poster).toBe(1);
    expect(bad({ title: "T", steps: [{ press: "a", caption: "a", poster: true }, { ...done, poster: true }] })).toBe(
      "w.yaml: only one step may be the poster (steps 1, 2 are)",
    );
    expect(bad({ title: "T", steps: [{ press: "a", caption: "a", poster: "yes" }, done] })).toBe("w.yaml: step 1 (press): poster is true or absent");
  });

  it("refuses exact on a selector target, where it means nothing", () => {
    expect(bad({ title: "T", steps: [{ wait: { selector: "#x", exact: true }, caption: "a" }] })).toBe(
      'w.yaml: step 1 (wait) has unknown key "exact"',
    );
  });

  it("refuses a script that ends on an action, since nothing proves the action worked", () => {
    for (const last of [{ click: "#x" }, { hover: "#x" }, { press: "Escape" }, { type: { target: "#q", text: "a" } }, { select: { target: "#p", value: "v" } }]) {
      const kind = Object.keys(last)[0];
      expect(bad({ title: "T", steps: [{ goto: "/a", caption: "a" }, { ...last, caption: "b" }] })).toBe(
        `w.yaml: step 2 (${kind}) is the last step: end on a wait for the result it should produce, or the walkthrough proves nothing`,
      );
    }
    expect(ok([{ goto: "/a", caption: "a" }]).steps).toHaveLength(1);
    expect(ok([{ press: "Escape", caption: "a" }, { wait: { role: "table" }, caption: "b" }]).steps).toHaveLength(2);
  });

  it("names the file on a YAML syntax error", () => {
    expect(() => parseWalkthrough("title: [unclosed", F)).toThrow(/^w\.yaml: not valid YAML/);
  });

  it("lists every problem at once", () => {
    const m = bad({ title: "T", steps: [{ drag: 1 }, { wait: { text: "x" } }] });
    expect(m.split("\n")).toHaveLength(2);
  });
});

// A walkthrough's setup (#1197): what the scene needs that the walkthrough
// does not teach, made through the API off camera before the title card
// (set-up-monitoring's display in Bay 2, which nothing collects from yet).
describe("walkthrough setup", () => {
  const body = { name: "bay-2-display", product: "boreal-edge-55", location: "bay-2" };
  it("is optional, and a list of API creates when present", () => {
    expect(ok([{ wait: { text: "x" }, caption: "a" }]).setup).toEqual([]);
    const w = ok([{ wait: { text: "x" }, caption: "a" }], { setup: [{ post: "/components", body }] });
    expect(w.setup).toEqual([{ method: "POST", path: "/components", body }]);
  });
  it("refuses a setup that is not a list of { post, body }, naming the item", () => {
    const m = bad({ title: "T", steps: [{ wait: { text: "x" }, caption: "a" }], setup: [{ post: "components", body }, { get: "/x" }, { post: "/y" }] });
    expect(m).toContain('setup 1 (post) must be an API path starting with "/"');
    expect(m).toContain('setup 2 has unknown key "get"');
    expect(m).toContain("setup 3 (post) needs a body");
    expect(bad({ title: "T", steps: [{ wait: { text: "x" }, caption: "a" }], setup: { post: "/x" } })).toContain("setup must be a list");
  });

});

// A walkthrough's ready condition (#1645): a state the scene needs that only
// time makes (the server's first report window), waited for in the console off
// camera before the title card, so the wait never counts against the budget.
describe("walkthrough ready", () => {
  const done = [{ wait: { text: "x" }, caption: "a" }];
  it("is optional, and a console path with a visible result to wait for when present", () => {
    expect(ok(done).ready).toBeNull();
    const w = ok(done, { ready: { goto: "/platform", wait: { selector: "#rows", timeout: 120_000 } } });
    expect(w.ready).toEqual({ path: "/platform", target: { selector: "#rows" }, timeout: 120_000 });
    expect(ok(done, { ready: { goto: "/platform", wait: { text: "Requests" } } }).ready?.timeout).toBe(10_000);
  });
  it("refuses a ready that is not { goto, wait }, naming what is wrong", () => {
    const m = bad({ title: "T", steps: done, ready: { goto: "platform", wait: { timeout: 1 }, hold: 3 } });
    expect(m).toContain('ready has unknown key "hold"');
    expect(m).toContain('ready (goto) must be a console path starting with "/"');
    expect(m).toContain("ready (wait) needs text, selector, or role");
    expect(bad({ title: "T", steps: done, ready: { goto: "/p", wait: { text: "x", timeout: 300_000 } } })).toContain(
      "ready (wait): timeout must be whole milliseconds, 1 to 180000",
    );
    expect(bad({ title: "T", steps: done, ready: { goto: "/p" } })).toContain("ready needs wait: a visible result");
    expect(bad({ title: "T", steps: done, ready: "/p" })).toContain("ready must be { goto, wait }");
  });
  it("names what a ready that never came waited for, on which path, and for how long", () => {
    const w = ok(done, { ready: { goto: "/platform", wait: { selector: "#rows", timeout: 90_000 } } });
    expect(readyFailure("d", w.ready!, "Timeout 90000ms exceeded.\n  call log")).toBe(
      'walkthrough d: not ready: waited 90s on /platform for selector "#rows" to be visible (Timeout 90000ms exceeded.)',
    );
  });
});

describe("walkthrough setup requests", () => {
  const body = { name: "bay-2-display", product: "boreal-edge-55", location: "bay-2" };
  type Call = { url: string; init: RequestInit };
  const fake = (statuses: number[]) => {
    const calls: Call[] = [];
    const fetch = async (url: string, init: RequestInit) => {
      calls.push({ url, init });
      const status = statuses[calls.length - 1];
      return new Response(status === 409 ? '{"detail":"exists"}' : "{}", { status });
    };
    return { calls, fetch };
  };
  const setup = [{ method: "POST" as const, path: "/components", body }];

  it("posts each create to the API with the owner token, and takes an existing one as made", async () => {
    for (const status of [201, 409]) {
      const f = fake([status]);
      await runSetup(setup, { base: "http://server:8080", token: "ogp_t", fetch: f.fetch });
      expect(f.calls).toHaveLength(1);
      expect(f.calls[0].url).toBe("http://server:8080/api/v1/components");
      expect(f.calls[0].init.method).toBe("POST");
      expect((f.calls[0].init.headers as Record<string, string>).Authorization).toBe("Bearer ogp_t");
      expect(JSON.parse(f.calls[0].init.body as string)).toEqual(body);
    }
  });
  it("fails naming the setup item, the request and the answer", async () => {
    const f = fake([422]);
    await expect(runSetup(setup, { base: "http://s", token: "t", fetch: f.fetch })).rejects.toThrow(/setup 1: POST \/components answered 422/);
  });
  it("needs a token only when there is something to set up", async () => {
    await expect(runSetup([], { base: "http://s", token: undefined, fetch: fake([]).fetch })).resolves.toBeUndefined();
    await expect(runSetup(setup, { base: "http://s", token: undefined, fetch: fake([201]).fetch })).rejects.toThrow(/OG_TOKEN/);
  });
});

describe("walkthrough failure and budget messages", () => {
  const step = validateWalkthrough({ title: "T", steps: [{ wait: { text: "Nope", timeout: 2000 }, caption: "c" }] }, F).steps[0];

  it("names the walkthrough, the step number and kind, the phase, and what it waited for", () => {
    expect(stepFailure("demo", step, { name: "visible", timeoutMs: 2000 }, "Timeout 2000ms exceeded.\n  call log...")).toBe(
      'walkthrough demo: step 1 (wait) failed at visible: waited 2s for text "Nope" to be visible (Timeout 2000ms exceeded.)',
    );
  });

  it("names the phase that failed with that phase's own timeout", () => {
    const w = validateWalkthrough(
      {
        title: "T",
        steps: [
          { goto: "/sim", caption: "a" },
          { click: "#x", caption: "b" },
          { type: { target: "#q", text: "ab" }, caption: "c" },
          { select: { target: "#p", value: "v" }, caption: "d" },
          { press: "Escape", caption: "e" },
          { hover: "#t", caption: "f" },
          { wait: { text: "y" }, caption: "g" },
        ],
      },
      F,
    );
    const [go, click, type, select, press, hover] = w.steps;
    expect(stepFailure("d", go, { name: "load", timeoutMs: 10_000 })).toBe("walkthrough d: step 1 (goto) failed at load: waited 10s for the console path /sim to load");
    expect(stepFailure("d", click, { name: "scroll", timeoutMs: 10_000 })).toBe('walkthrough d: step 2 (click) failed at scroll: waited 10s to scroll selector "#x" into view');
    expect(stepFailure("d", click, { name: "act", timeoutMs: 10_000 })).toBe('walkthrough d: step 2 (click) failed at act: waited 10s to click selector "#x"');
    expect(stepFailure("d", type, { name: "act", timeoutMs: 10_220 })).toBe('walkthrough d: step 3 (type) failed at act: waited 10.22s to type into selector "#q"');
    expect(stepFailure("d", select, { name: "act", timeoutMs: 10_000 })).toBe('walkthrough d: step 4 (select) failed at act: waited 10s to select "v" in selector "#p"');
    expect(stepFailure("d", press, { name: "act", timeoutMs: null }, 'Unknown key: "Escpe"')).toBe(
      'walkthrough d: step 5 (press) failed at act: could not press the key Escape (Unknown key: "Escpe")',
    );
    expect(stepFailure("d", press, { name: "still", timeoutMs: null })).toBe("walkthrough d: step 5 (press) failed at still: could not take its still");
    expect(stepFailure("d", hover, { name: "act", timeoutMs: 10_000 })).toBe('walkthrough d: step 6 (hover) failed at act: waited 10s to hover over selector "#t"');
  });

  it("holds the ruled budget: under 60 seconds and 10 MB of mp4, under 5 MB of preview", () => {
    expect(MAX_SECONDS).toBe(60);
    expect(MAX_BYTES).toBe(10_000_000);
    expect(MAX_PREVIEW_BYTES).toBe(5_000_000);
    expect(budgetProblems("demo", { seconds: 59.9, bytes: 9_999_999, previewBytes: 4_999_999 })).toEqual([]);
  });

  it("names which limit an over-budget walkthrough breaks", () => {
    expect(budgetProblems("demo", { seconds: 60, bytes: 1, previewBytes: 1 })).toEqual(["walkthrough demo: over budget: duration 60.0s, the limit is under 60s"]);
    expect(budgetProblems("demo", { seconds: 1, bytes: 12_500_000, previewBytes: 1 })).toEqual([
      "walkthrough demo: over budget: size 12.50 MB, the limit is under 10.00 MB",
    ]);
    expect(budgetProblems("demo", { seconds: 1, bytes: 1, previewBytes: 5_000_000 })).toEqual([
      "walkthrough demo: over budget: preview 5.00 MB, the limit is under 5.00 MB",
    ]);
    expect(budgetProblems("demo", { seconds: 5, bytes: 340_000, previewBytes: 120_000 }, { maxSeconds: 2, maxBytes: 1000, maxPreviewBytes: 1000 })).toEqual([
      "walkthrough demo: over budget: duration 5.0s, the limit is under 2s",
      "walkthrough demo: over budget: size 0.34 MB, the limit is under 1000 bytes",
      "walkthrough demo: over budget: preview 0.12 MB, the limit is under 1000 bytes",
    ]);
  });

  it("refuses a duration that was never measured instead of passing it", () => {
    expect(budgetProblems("demo", { seconds: Number(""), bytes: 1, previewBytes: 1 })).toEqual(["walkthrough demo: no duration measured (got 0): the recording could not be read"]);
    expect(budgetProblems("demo", { seconds: NaN, bytes: 1, previewBytes: 1 })).toEqual(["walkthrough demo: no duration measured (got NaN): the recording could not be read"]);
    expect(budgetProblems("demo", { seconds: Infinity, bytes: 1, previewBytes: 1 })).toHaveLength(1);
    expect(budgetProblems("demo", { seconds: -1, bytes: 1, previewBytes: 1 })).toHaveLength(1);
  });

  it("refuses a recording with no preview, or an empty one, instead of passing it", () => {
    expect(budgetProblems("demo", { seconds: 5, bytes: 1, previewBytes: undefined })).toEqual([
      "walkthrough demo: no preview (preview.webp) was encoded",
    ]);
    expect(budgetProblems("demo", { seconds: 5, bytes: 1, previewBytes: 0 })).toEqual(["walkthrough demo: no preview (preview.webp) was encoded"]);
  });

  it("lets the environment lower the limits, never raise them, and ignores nonsense", () => {
    const ruled = { maxSeconds: 60, maxBytes: 10_000_000, maxPreviewBytes: 5_000_000 };
    expect(budgetLimits({})).toEqual(ruled);
    expect(budgetLimits({ OG_WALKTHROUGH_MAX_SECONDS: "1", OG_WALKTHROUGH_MAX_BYTES: "1000", OG_WALKTHROUGH_MAX_PREVIEW_BYTES: "500" })).toEqual({
      maxSeconds: 1,
      maxBytes: 1000,
      maxPreviewBytes: 500,
    });
    expect(budgetLimits({ OG_WALKTHROUGH_MAX_SECONDS: "600", OG_WALKTHROUGH_MAX_BYTES: "1e12", OG_WALKTHROUGH_MAX_PREVIEW_BYTES: "1e12" })).toEqual(ruled);
    for (const v of ["", "abc", "0", "-5", "Infinity"]) {
      expect(budgetLimits({ OG_WALKTHROUGH_MAX_SECONDS: v, OG_WALKTHROUGH_MAX_BYTES: v, OG_WALKTHROUGH_MAX_PREVIEW_BYTES: v })).toEqual(ruled);
    }
  });

  it("measures when the title card was painted, in seconds into the video", () => {
    expect(paintSeconds(1_000, 1_000)).toBe(0);
    expect(paintSeconds(10_000, 10_640)).toBe(0.64);
    expect(() => paintSeconds(10_000, 9_000)).toThrow("painted before the video started");
    expect(() => paintSeconds(NaN, 1)).toThrow("not measured");
  });

  it("opens the preview on the first painted frame at or after the paint, however far the video lags", () => {
    const dark = (t: number) => ({ t, ymax: 26 });
    const lit = (t: number) => ({ t, ymax: 255 });
    // A frame before the paint is the lead-in: skipped even when bright.
    expect(previewStartFromLumas(1, [lit(0.5), dark(1), dark(1.2), lit(1.4), lit(1.6)])).toBe(1.4);
    expect(previewStartFromLumas(1, [lit(0.5), lit(1), lit(1.2)])).toBe(1);
    // A half-painted frame (the ground and the rule, no text) does not qualify.
    expect(previewStartFromLumas(0.5, [{ t: 0.6, ymax: 154 }, { t: 0.8, ymax: PREVIEW_MIN_YMAX }])).toBe(0.8);
    // A loaded runner: the card appears late in the video, still inside its hold.
    expect(previewStartFromLumas(0.5, [dark(0.5), dark(1.5), dark(2.5), lit(2.7)])).toBe(2.7);
    // None qualifies, or only one past the card's hold, or none at all.
    expect(previewStartFromLumas(1, [dark(1), dark(1.5), lit(0.9)])).toBeNull();
    expect(previewStartFromLumas(1, [dark(1), lit(1 + TITLE_HOLD_MS / 1000 + 0.04)])).toBeNull();
    expect(previewStartFromLumas(1, [])).toBeNull();
  });

  it("reads the frames from ffmpeg's signalstats listing", () => {
    const text = [
      "frame:0    pts:0       pts_time:0",
      "lavfi.signalstats.YMAX=16",
      "frame:1    pts:40      pts_time:0.04",
      "lavfi.signalstats.YMAX=235",
      "",
    ].join("\n");
    expect(parseLumas(text)).toEqual([{ t: 0, ymax: 16 }, { t: 0.04, ymax: 235 }]);
    expect(parseLumas("")).toEqual([]);
  });

  it("fails a card that never painted, naming the window scanned and the brightest luma", () => {
    const frames = [{ t: 0.4, ymax: 255 }, { t: 0.6, ymax: 26 }, { t: 1.2, ymax: 154 }, { t: 3, ymax: 255 }];
    expect(previewScanProblem("demo", 0.5, [...frames, { t: 1.5, ymax: 255 }])).toBeNull();
    expect(previewScanProblem("demo", 0.5, frames)).toBe(
      "walkthrough demo: the title card never painted between 0.50s and 2.70s (brightest luma 154 over 2 frames, the card's text reaches at least 200)",
    );
    expect(previewScanProblem("demo", 0.5, [])).toBe(
      "walkthrough demo: no frame to open the preview on between 0.50s and 2.70s (the recording has none there)",
    );
  });

  it("refuses a preview that would open on a blank frame, naming its brightest pixel", () => {
    expect(PREVIEW_MIN_YMAX).toBe(200);
    expect(previewOpeningProblem("demo", 255)).toBeNull();
    expect(previewOpeningProblem("demo", PREVIEW_MIN_YMAX)).toBeNull();
    expect(previewOpeningProblem("demo", 26)).toBe(
      "walkthrough demo: the preview opens on a blank frame (brightest luma 26, the title card's text reaches at least 200)",
    );
    expect(previewOpeningProblem("demo", NaN)).toBe("walkthrough demo: the preview's opening frame could not be measured");
  });

  it("names stills by zero-padded step number", () => {
    expect(stillName(3)).toBe("step-03.png");
    expect(stillName(12)).toBe("step-12.png");
  });
});

describe("walkthrough ids", () => {
  it("are lowercase letters, digits and hyphens, since they name a directory and a file", () => {
    expect(idProblem("sample-simulation")).toBeNull();
    expect(idProblem("broken-wait")).toBeNull();
    for (const id of ["", "Sample", "a_b", "../x", "a/b", "a b", "fixtures/tiny"]) {
      expect(idProblem(id)).toBe(`walkthrough id ${JSON.stringify(id)} must be lowercase letters, digits and hyphens`);
    }
  });
});

// Every script in the tree validates, so a broken walkthrough fails make test
// rather than the next recording. The fixtures are valid scripts too: they
// fail at run time (a wait that never resolves), not at parse time.
describe("the shipped walkthroughs", () => {
  const dir = join(__dirname, "../e2e/walkthroughs");
  const files = [
    ...readdirSync(dir).filter((f) => f.endsWith(".yaml")).map((f) => join(dir, f)),
    ...readdirSync(join(dir, "fixtures")).filter((f) => f.endsWith(".yaml")).map((f) => join(dir, "fixtures", f)),
  ];
  it("exist", () => expect(files.length).toBeGreaterThan(0));
  for (const f of files) {
    it(`${f.slice(dir.length + 1)} validates, with an id the recorder accepts`, () => {
      expect(() => parseWalkthrough(readFileSync(f, "utf8"), f)).not.toThrow();
      expect(idProblem(f.slice(f.lastIndexOf("/") + 1, -".yaml".length))).toBeNull();
    });
  }
});

// The probe replays every walkthrough on one stack, in order (#1404), while the
// recorder gives each a lab of its own, so two walkthroughs whose setups claim
// the same component or alarm and whose steps change it pass recorded and fail
// probed. The lint holds that line at validation.
describe("setup collisions", () => {
  const act = { click: "button", caption: "c" };
  const look = { wait: { text: "x" }, caption: "c" };
  const component = (name: string) => ({ post: "/components", body: { name, product: "p", location: "l" } });
  const alarm = (on: string, dedup_key: string) => ({ post: `/components/${on}/alarms`, body: { severity: "high", message: "m", dedup_key } });
  const w = (id: string, setup: unknown[], steps: unknown[]) => ({ id, ...ok(steps, { setup }) });

  it("flags two walkthroughs that set up the same component when one changes it", () => {
    const found = setupCollisions([w("a", [component("bay-2-display")], [act, look]), w("b", [component("bay-2-display")], [look])]);
    expect(found).toEqual(['walkthroughs a, b both set up component "bay-2-display" and a changes it: give each its own']);
  });
  it("flags the same alarm dedup key on one component, and names who changes it", () => {
    const found = setupCollisions([w("a", [alarm("panel-2", "k")], [act, look]), w("b", [alarm("panel-2", "k")], [act, look])]);
    expect(found).toEqual(['walkthroughs a, b both set up alarm "k" on panel-2 and a, b change it: give each its own']);
  });
  it("lets walkthroughs share what none of them changes, and tell different alarms on one component apart", () => {
    expect(setupCollisions([w("a", [component("c")], [look]), w("b", [component("c")], [look])])).toEqual([]);
    expect(setupCollisions([w("a", [alarm("panel-2", "k1")], [act, look]), w("b", [alarm("panel-2", "k2")], [act, look])])).toEqual([]);
    expect(setupCollisions([w("a", [alarm("panel-2", "k")], [act, look]), w("b", [alarm("panel-3", "k")], [act, look])])).toEqual([]);
  });
  it("ignores setup it does not understand, such as other creates", () => {
    const other = { post: "/notification-types", body: { name: "n" } };
    expect(setupCollisions([w("a", [other], [act, look]), w("b", [other], [act, look])])).toEqual([]);
  });
  it("finds none in the shipped walkthroughs", () => {
    const dir = join(__dirname, "../e2e/walkthroughs");
    const all = readdirSync(dir)
      .filter((f) => f.endsWith(".yaml"))
      .sort()
      .map((f) => ({ id: f.slice(0, -".yaml".length), ...parseWalkthrough(readFileSync(join(dir, f), "utf8"), f) }));
    expect(setupCollisions(all)).toEqual([]);
  });
});

// Platform health's walkthrough (#1730) teaches the page as it is: it opens a
// hop under "Where work is lost" (#1646) and reads the simulation section
// (#1647), each captioned as a loss ratio where 0 is healthy. The recording
// replays these steps, so the stills carry what the script reaches.
describe("read-platform-health", () => {
  const file = join(__dirname, "../e2e/walkthroughs/read-platform-health.yaml");
  const w = parseWalkthrough(readFileSync(file, "utf8"), file);
  // A click's target is its selector; a wait's is { selector }.
  const selector = (t: unknown) => (typeof t === "string" ? t : ((t as { selector?: string } | undefined)?.selector ?? ""));
  const on = (needle: string) => w.steps.filter((s) => selector(s.target).includes(needle));

  it("opens a hop under Where work is lost and waits on its explainer", () => {
    expect(on("pipeline-hop-").some((s) => s.kind === "click")).toBe(true);
    expect(on("pipeline-hop-detail").some((s) => s.kind === "wait")).toBe(true);
  });

  it("reads the hops as a loss ratio where 0 is healthy", () => {
    const captions = on("pipeline").map((s) => s.caption).join(" ");
    expect(captions).toMatch(/loss ratio/);
    expect(captions).toMatch(/0% is healthy/);
  });

  it("reads the simulation section as a loss ratio where 0 is healthy", () => {
    const sim = on("platform-simulation");
    expect(sim.some((s) => s.kind === "wait")).toBe(true);
    expect(sim.map((s) => s.caption).join(" ")).toMatch(/loss ratio.*0 is healthy/);
  });
});
