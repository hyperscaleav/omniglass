import { describe, it, expect } from "vitest";
import { readdirSync, readFileSync } from "node:fs";
import { join } from "node:path";
import {
  MAX_BYTES,
  MAX_SECONDS,
  budgetProblems,
  parseWalkthrough,
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
        { type: { target: "#q", text: "display" }, caption: "d" },
        { select: { target: { role: "combobox", name: "Product" }, value: "x" }, caption: "e" },
        { press: "Escape", caption: "f", hold: 500 },
        { wait: { text: "Hits", timeout: 5000 }, caption: "g" },
        { wait: { selector: "[data-testid=sim-table]" }, caption: "h" },
      ],
      { description: "D" },
    );
    expect(w.title).toBe("T");
    expect(w.description).toBe("D");
    expect(w.steps.map((s) => s.kind)).toEqual(["goto", "click", "click", "type", "select", "press", "wait", "wait"]);
    expect(w.steps[0].path).toBe("/simulation");
    expect(w.steps[3]).toMatchObject({ n: 4, target: "#q", text: "display", hold: 0 });
    expect(w.steps[5]).toMatchObject({ key: "Escape", hold: 500 });
    expect(w.steps[6]).toMatchObject({ target: { text: "Hits" }, timeout: 5000 });
    expect(w.steps[7].timeout).toBe(10_000);
  });

  it("rejects an unknown top-level key and a missing title, naming the file", () => {
    const m = bad({ titel: "T", steps: [{ press: "Enter", caption: "a" }] });
    expect(m).toContain('w.yaml: unknown key "titel"');
    expect(m).toContain("w.yaml: needs a title");
  });

  it("rejects an unknown kind, naming the step", () => {
    expect(bad({ title: "T", steps: [{ press: "Enter", caption: "a" }, { hover: "#x", caption: "b" }] })).toContain(
      'w.yaml: step 2 has no kind: one of goto, click, type, select, press, wait (unknown kind or key "hover")',
    );
  });

  it("rejects an unknown key on a step, naming the step and its kind", () => {
    expect(bad({ title: "T", steps: [{ click: "#x", caption: "a", timeout: 3 }] })).toBe('w.yaml: step 1 (click) has unknown key "timeout"');
    expect(bad({ title: "T", steps: [{ type: { target: "#x", text: "a", delay: 9 }, caption: "a" }] })).toBe(
      'w.yaml: step 1 (type) has unknown key "delay"',
    );
    expect(bad({ title: "T", steps: [{ click: { role: "row", label: "x" }, caption: "a" }] })).toContain(
      'w.yaml: step 1 (click) has unknown key "label"',
    );
  });

  it("rejects two kinds on one step, a missing caption, and a bad wait", () => {
    expect(bad({ title: "T", steps: [{ click: "#x", press: "Enter", caption: "a" }] })).toBe("w.yaml: step 1 has more than one kind: click, press");
    expect(bad({ title: "T", steps: [{ press: "Enter" }] })).toBe("w.yaml: step 1 (press) needs a caption");
    expect(bad({ title: "T", steps: [{ wait: { timeout: 100 }, caption: "a" }] })).toContain("step 1 (wait) needs text, selector, or role");
    expect(bad({ title: "T", steps: [{ wait: { text: "x", timeout: 0 }, caption: "a" }] })).toContain("timeout must be whole milliseconds");
    expect(bad({ title: "T", steps: [{ goto: "simulation", caption: "a" }] })).toContain('must be a console path starting with "/"');
    expect(bad({ title: "T", steps: [] })).toContain("needs steps");
  });

  it("takes the poster from the step that asks, else the last, and only one", () => {
    expect(ok([{ press: "a", caption: "a" }, { press: "b", caption: "b" }]).poster).toBe(2);
    expect(ok([{ press: "a", caption: "a", poster: true }, { press: "b", caption: "b" }]).poster).toBe(1);
    expect(bad({ title: "T", steps: [{ press: "a", caption: "a", poster: true }, { press: "b", caption: "b", poster: true }] })).toBe(
      "w.yaml: only one step may be the poster (steps 1, 2 are)",
    );
    expect(bad({ title: "T", steps: [{ press: "a", caption: "a", poster: "yes" }] })).toBe("w.yaml: step 1 (press): poster is true or absent");
  });

  it("names the file on a YAML syntax error", () => {
    expect(() => parseWalkthrough("title: [unclosed", F)).toThrow(/^w\.yaml: not valid YAML/);
  });

  it("lists every problem at once", () => {
    const m = bad({ title: "T", steps: [{ hover: 1 }, { click: "#x" }] });
    expect(m.split("\n")).toHaveLength(2);
  });
});

describe("walkthrough failure and budget messages", () => {
  const step = validateWalkthrough({ title: "T", steps: [{ wait: { text: "Nope", timeout: 2000 }, caption: "c" }] }, F).steps[0];

  it("names the walkthrough, the step number and kind, and what it waited for", () => {
    expect(stepFailure("demo", step, 2000, "Timeout 2000ms exceeded.\n  call log...")).toBe(
      'walkthrough demo: step 1 (wait) failed: waited 2s for text "Nope" to be visible (Timeout 2000ms exceeded.)',
    );
  });

  it("holds the ruled budget: under 60 seconds and 10 MB", () => {
    expect(MAX_SECONDS).toBe(60);
    expect(MAX_BYTES).toBe(10_000_000);
    expect(budgetProblems("demo", { seconds: 59.9, bytes: 9_999_999 })).toEqual([]);
  });

  it("names which limit an over-budget walkthrough breaks", () => {
    expect(budgetProblems("demo", { seconds: 60, bytes: 1 })).toEqual(["walkthrough demo: over budget: duration 60.0s, the limit is under 60s"]);
    expect(budgetProblems("demo", { seconds: 1, bytes: 12_500_000 })).toEqual([
      "walkthrough demo: over budget: size 12.50 MB, the limit is under 10.00 MB",
    ]);
    expect(budgetProblems("demo", { seconds: 5, bytes: 340_000 }, { maxSeconds: 2, maxBytes: 1000 })).toEqual([
      "walkthrough demo: over budget: duration 5.0s, the limit is under 2s",
      "walkthrough demo: over budget: size 0.34 MB, the limit is under 1000 bytes",
    ]);
  });

  it("names stills by zero-padded step number", () => {
    expect(stillName(3)).toBe("step-03.png");
    expect(stillName(12)).toBe("step-12.png");
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
    it(`${f.slice(dir.length + 1)} validates`, () => {
      expect(() => parseWalkthrough(readFileSync(f, "utf8"), f)).not.toThrow();
    });
  }
});
