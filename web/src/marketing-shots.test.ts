import { describe, it, expect } from "vitest";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import {
  CAPTURE_CSS,
  LOOP_MAX_BYTES,
  LOOP_MAX_SECONDS,
  LOOP_MIN_SECONDS,
  budgetProblems,
  buildManifest,
  conditionMet,
  destProblem,
  expandAddress,
  parseShots,
  pick,
  referencedResources,
  shotFiles,
} from "../e2e/marketinglib.mjs";

const LAB = "lab: { scenario: corporate, systems: 1000 }\n";
const STILL = "  - id: a-still\n    kind: still\n    alt: A still.\n    start: /explore\n";

describe("parseShots", () => {
  it("accepts a minimal list and defaults the viewport", () => {
    const { list, problems } = parseShots(`${LAB}shots:\n${STILL}`);
    expect(problems).toEqual([]);
    expect(list?.lab).toEqual({ scenario: "corporate", systems: 1000, omnibotReplay: "" });
    expect(list?.shots[0].viewport).toEqual({ width: 1440, height: 900 });
  });

  it("takes a list viewport, and a shot's own over it", () => {
    const { list } = parseShots(
      `${LAB}viewport: { width: 1280, height: 800 }\nshots:\n${STILL}  - id: wide\n    kind: still\n    alt: Wide.\n    start: /explore\n    viewport: { width: 1920, height: 1080 }\n`,
    );
    expect(list?.shots.map((s: { viewport: unknown }) => s.viewport)).toEqual([
      { width: 1280, height: 800 },
      { width: 1920, height: 1080 },
    ]);
  });

  it("rejects a shot without alt, naming the shot", () => {
    const { list, problems } = parseShots(`${LAB}shots:\n  - id: mute\n    kind: still\n    start: /explore\n`);
    expect(list).toBeNull();
    expect(problems).toContain("shot mute: needs alt text (the page's description of the capture)");
  });

  it("rejects a bad id, a duplicate id and an unknown kind", () => {
    const { problems } = parseShots(
      `${LAB}shots:\n  - id: Bad_Id\n    kind: still\n    alt: x\n    start: /a\n  - id: dup\n    kind: still\n    alt: x\n    start: /a\n  - id: dup\n    kind: movie\n    alt: x\n    start: /a\n`,
    );
    expect(problems.join("\n")).toContain("shot Bad_Id: id must be lowercase letters, digits and hyphens");
    expect(problems).toContain("shot dup: id is used twice");
    expect(problems).toContain("shot dup: kind is still or loop");
  });

  it("rejects a loop with no steps and a caption on a step", () => {
    const { problems } = parseShots(
      `${LAB}shots:\n  - id: empty\n    kind: loop\n    alt: x\n    start: /a\n  - id: talky\n    kind: loop\n    alt: x\n    start: /a\n    steps:\n      - click: button\n        caption: hi\n`,
    );
    expect(problems).toContain("shot empty: a loop needs steps");
    expect(problems).toContain("shot talky: step 1 has a caption: a marketing capture has none");
  });

  it("rejects a step with two kinds, an unknown key and a bad hold", () => {
    const { problems } = parseShots(
      `${LAB}shots:\n  - id: s\n    kind: loop\n    alt: x\n    start: /a\n    steps:\n      - click: a\n        hover: b\n      - click: a\n        speed: 2\n      - click: a\n        hold: 99999\n`,
    );
    expect(problems.join("\n")).toContain("shot s: step 1 must have exactly one kind");
    expect(problems).toContain('shot s: step 2 (click) has unknown key "speed"');
    expect(problems.join("\n")).toContain("shot s: step 3 (click): hold must be whole milliseconds");
  });

  it("parses the steps of the walkthrough grammar", () => {
    const { list, problems } = parseShots(
      `${LAB}shots:\n  - id: g\n    kind: loop\n    alt: x\n    start: /a\n    steps:\n      - goto: /b\n      - click: { role: button, name: Go }\n      - type: { target: "#f", text: hello }\n      - select: { target: "#s", value: v }\n      - press: Enter\n      - wait: { text: Done, timeout: 5000 }\n        hold: 300\n`,
    );
    expect(problems).toEqual([]);
    expect(list?.shots[0].steps.map((s: { kind: string }) => s.kind)).toEqual(["goto", "click", "type", "select", "press", "wait"]);
    expect(list?.shots[0].steps[5]).toMatchObject({ timeout: 5000, hold: 300 });
  });

  it("takes replace on a type step, and rejects it elsewhere or non-boolean", () => {
    const ok = parseShots(`${LAB}shots:\n  - id: s\n    kind: loop\n    alt: x\n    start: /a\n    steps:\n      - type: { target: "#f", text: hi, replace: true }\n`);
    expect(ok.problems).toEqual([]);
    expect(ok.list?.shots[0].steps[0]).toMatchObject({ kind: "type", replace: true });
    const bad = parseShots(`${LAB}shots:\n  - id: s\n    kind: loop\n    alt: x\n    start: /a\n    steps:\n      - type: { target: "#f", text: hi, replace: yes please }\n      - select: { target: "#f", value: v, replace: true }\n`);
    expect(bad.problems).toContain("shot s: step 1 (type): replace is true or false");
    expect(bad.problems).toContain('shot s: step 2 (select) has unknown key "replace"');
  });

  it("parses prep steps off a loop, and refuses them on a still", () => {
    const ok = parseShots(`${LAB}shots:\n  - id: s\n    kind: loop\n    alt: x\n    start: /a\n    prep:\n      - click: button\n    steps:\n      - hover: a\n`);
    expect(ok.problems).toEqual([]);
    expect(ok.list?.shots[0].prep).toHaveLength(1);
    const bad = parseShots(`${LAB}shots:\n  - id: s\n    kind: still\n    alt: x\n    start: /a\n    prep:\n      - click: button\n        caption: no\n`);
    expect(bad.problems).toContain("shot s: prep step 1 has a caption: a marketing capture has none");
    expect(bad.problems).toContain("shot s: prep belongs on a loop (a still is shot after its steps)");
  });

  it("parses setup: calls and awaited state, and rejects a malformed item", () => {
    const ok = parseShots(
      `${LAB}setup:\n  - put: /components/{a.$comp.b}/impairment\n    body: { unplugged: true }\n  - wait: { get: /alarms, field: alarms.0.active, equals: true, timeout: 5000 }\nshots:\n${STILL}`,
    );
    expect(ok.problems).toEqual([]);
    expect(ok.list?.setup[0]).toMatchObject({ kind: "call", method: "PUT" });
    expect(ok.list?.setup[1]).toMatchObject({ kind: "await", field: "alarms.0.active", timeout: 5000 });
    const bad = parseShots(`${LAB}setup:\n  - put: components/x\n  - frob: 1\nshots:\n${STILL}`);
    expect(bad.problems.join("\n")).toContain("the list setup item 1: put must be an API path starting with");
    expect(bad.problems.join("\n")).toContain("the list setup item 2 must be one of put, post, patch or wait");
  });

  it("rejects a lab without a size, and a non-YAML list", () => {
    expect(parseShots("lab: { scenario: corporate }\nshots: []\n").problems).toContain(
      "lab must be { scenario, systems (a positive whole number), omnibot_replay? }",
    );
    expect(parseShots("{ unclosed").problems[0]).toContain("is not YAML");
  });

  it("parses the committed shot list: seven shots, the two kinds, every one with alt", () => {
    const text = readFileSync(join(__dirname, "../e2e/marketing/shots.yaml"), "utf8");
    const { list, problems } = parseShots(text);
    expect(problems).toEqual([]);
    expect(list?.lab).toMatchObject({ scenario: "corporate", systems: 1000, omnibotReplay: "corporate-outage" });
    const byId = Object.fromEntries((list?.shots ?? []).map((s: { id: string; kind: string }) => [s.id, s.kind]));
    expect(byId).toEqual({
      "fleet-overview": "loop",
      "fleet-explore": "loop",
      "fleet-scale": "still",
      "credential-fix": "loop",
      "alarm-outage": "loop",
      "system-why": "still",
      "omnibot-ask": "loop",
    });
    for (const s of list?.shots ?? []) expect(s.alt.length).toBeGreaterThan(20);
  });
});

describe("budgetProblems", () => {
  it("passes a loop inside the budget", () => {
    expect(budgetProblems("a", { seconds: 12, bytes: 900_000 })).toEqual([]);
    expect(budgetProblems("a", { seconds: LOOP_MIN_SECONDS, bytes: LOOP_MAX_BYTES })).toEqual([]);
    expect(budgetProblems("a", { seconds: LOOP_MAX_SECONDS, bytes: 1 })).toEqual([]);
  });

  it("names the shot and the limit for a loop too short, too long or too big", () => {
    expect(budgetProblems("quick", { seconds: 6.2, bytes: 1000 })).toEqual(["shot quick: loop is 6.2s, under the 8s minimum"]);
    expect(budgetProblems("slow", { seconds: 16, bytes: 1000 })).toEqual(["shot slow: loop is 16.0s, over the 15s maximum"]);
    expect(budgetProblems("fat", { seconds: 10, bytes: 1_500_001 })).toEqual(["shot fat: loop is 1500001 bytes, over the 1500000-byte limit"]);
  });

  it("reports every limit a loop is over, and an unmeasured duration", () => {
    expect(budgetProblems("both", { seconds: 20, bytes: 2_000_000 })).toHaveLength(2);
    expect(budgetProblems("nan", { seconds: NaN, bytes: 1000 })[0]).toContain("under the 8s minimum");
  });
});

describe("destProblem", () => {
  it("names an unset, empty or relative destination and accepts an absolute one", () => {
    expect(destProblem(undefined)).toContain("MARKETING_DEST is not set");
    expect(destProblem("")).toContain("MARKETING_DEST is not set");
    expect(destProblem("images/shots")).toBe('MARKETING_DEST "images/shots" is not an absolute path');
    expect(destProblem("/srv/landing/public/images/omniglass/shots")).toBeNull();
  });
});

describe("buildManifest", () => {
  const lab = { scenario: "corporate", systems: 1000 };
  const shots = [
    { id: "a-loop", kind: "loop" as const, file: "a-loop.mp4", poster: "a-loop.webp", alt: "A loop.", width: 1440, height: 900 },
    { id: "a-still", kind: "still" as const, file: "a-still.webp", alt: "A still.", width: 1440, height: 900 },
  ];

  it("writes bare names, alt text and the lab, with a poster on loops only", () => {
    const m = buildManifest({ generatedAt: "2026-11-23T00:00:00.000Z", lab, shots });
    expect(m).toEqual({
      generatedAt: "2026-11-23T00:00:00.000Z",
      lab: { scenario: "corporate", systems: 1000 },
      shots: [
        { id: "a-loop", kind: "loop", file: "a-loop.mp4", poster: "a-loop.webp", alt: "A loop.", width: 1440, height: 900 },
        { id: "a-still", kind: "still", file: "a-still.webp", alt: "A still.", width: 1440, height: 900 },
      ],
    });
  });

  it("refuses a missing alt, a path in a file name and a loop with no poster", () => {
    expect(() => buildManifest({ generatedAt: "x", lab, shots: [{ ...shots[1], alt: "" }] })).toThrow("shot a-still: alt is required");
    expect(() => buildManifest({ generatedAt: "x", lab, shots: [{ ...shots[1], file: "../x.webp" }] })).toThrow("is not a bare file name");
    expect(() => buildManifest({ generatedAt: "x", lab, shots: [{ ...shots[0], poster: undefined }] })).toThrow("is not a bare file name");
  });

  it("names a shot's files by kind", () => {
    expect(shotFiles({ id: "x", kind: "loop" })).toEqual({ file: "x.mp4", poster: "x.webp" });
    expect(shotFiles({ id: "x", kind: "still" })).toEqual({ file: "x.webp", poster: "x.webp" });
  });
});

describe("addresses and conditions", () => {
  it("encodes a braced address whole, leaving the route around it", () => {
    expect(expandAddress("/components/{a.b.$comp.display-1}/impairment")).toBe("/components/a.b.%24comp.display-1/impairment");
    expect(expandAddress("/endpoints/{a.$comp.v/https}")).toBe("/endpoints/a.%24comp.v%2Fhttps");
    expect(expandAddress("/systems/hq-mr86-1/health")).toBe("/systems/hq-mr86-1/health");
  });

  it("reads a dotted field through objects and arrays", () => {
    const body = { alarms: [{ active: true }, { active: false }], health: "degraded" };
    expect(pick(body, "alarms.0.active")).toBe(true);
    expect(pick(body, "alarms.1.active")).toBe(false);
    expect(pick(body, "health")).toBe("degraded");
    expect(pick(body, "alarms.5.active")).toBeUndefined();
    expect(pick(null, "a")).toBeUndefined();
  });

  it("compares by value", () => {
    expect(conditionMet(true, true)).toBe(true);
    expect(conditionMet("rejected", "up")).toBe(false);
    expect(conditionMet({ a: 1 }, { a: 1 })).toBe(true);
  });

  it("finds the components, systems and endpoints a list names, not what it creates", () => {
    const { list: parsed } = parseShots(
      `${LAB}setup:\n  - put: /components/{a.$comp.d}/impairment\n    body: { unplugged: true }\n  - post: /secrets\n    body: { name: s }\n  - patch: /endpoints/{a.$comp.v/https}\n    body: { inputs: {} }\n  - wait: { get: /systems/hq-mr86-1/health, field: health, equals: degraded }\nshots:\n  - id: s\n    kind: still\n    alt: x\n    start: /systems/hq-mr65-59\n`,
    );
    const list = parsed!;
    const refs = referencedResources(list.setup, list.shots[0].start);
    expect(refs.map((r: { kind: string; id: string }) => `${r.kind}:${r.id}`)).toEqual([
      "component:a.$comp.d",
      "endpoint:a.$comp.v/https",
      "system:hq-mr86-1",
      "system:hq-mr65-59",
    ]);
    expect(refs[0].path).toBe("/components/{a.$comp.d}");
  });
});

describe("the capture-only stylesheet", () => {
  it("hides the lab banner and a soon nav item, and nothing else", () => {
    expect(CAPTURE_CSS).toContain('[data-testid="lab-banner"]');
    expect(CAPTURE_CSS).toContain("li:has(> a > [data-soon])");
    // Placeholders go; data never does: no rule reaches a table, a row or a figure.
    expect(CAPTURE_CSS.split("\n")).toHaveLength(2);
    expect(CAPTURE_CSS).not.toMatch(/table|tr\b|treeitem|health|alarm/i);
  });
});
