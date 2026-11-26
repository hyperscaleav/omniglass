import { describe, it, expect } from "vitest";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import {
  CAMERA,
  beatEnds,
  beatStill,
  CAPTURE_CSS,
  LOOP_MAX_BYTES,
  LOOP_MAX_SECONDS,
  LOOP_MAX_SECONDS_CAP,
  LOOP_MIN_SECONDS,
  budgetProblems,
  loopMaxBytes,
  cameraTail,
  retime,
  buildManifest,
  cameraTrack,
  lapseBadges,
  MIN_LAPSE_BADGE,
  spotFor,
  MAX_FAST,
  LOOP_TOLERANCE,
  PACE,
  authoredSeconds,
  lapseSpeed,
  overrunReport,
  frameFor,
  frameRect,
  missingText,
  conditionMet,
  destProblem,
  expandAddress,
  parseShots,
  pick,
  pointerX,
  referencedResources,
  replayProblems,
  shotFiles,
} from "../e2e/marketinglib.mjs";

const LAB = "lab: { scenario: corporate, systems: 1000 }\n";
const STILL = "  - id: a-still\n    kind: still\n    alt: A still.\n    start: /explore\n";

describe("parseShots", () => {
  it("accepts a minimal list and defaults the viewport", () => {
    const { list, problems } = parseShots(`${LAB}shots:\n${STILL}`);
    expect(problems).toEqual([]);
    expect(list?.lab).toEqual({ scenario: "corporate", systems: 1000, omnibotReplay: "", healthHistory: false });
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

  it("takes a health_history switch on the lab, off unless set, and refuses one that is not a boolean", () => {
    const shots = "shots:\n  - id: s\n    kind: still\n    alt: x\n    start: /a\n";
    expect(parseShots(`lab: { scenario: corporate, systems: 1000 }\n${shots}`).list?.lab.healthHistory).toBe(false);
    expect(parseShots(`lab: { scenario: corporate, systems: 1000, health_history: true }\n${shots}`).list?.lab.healthHistory).toBe(true);
    expect(parseShots(`lab: { scenario: corporate, systems: 1000, health_history: yes please }\n${shots}`).problems).toContain(
      "lab health_history is true or false: whether the lab seeds its scenario's health storyline",
    );
  });

  it("rejects a lab without a size, and a non-YAML list", () => {
    expect(parseShots("lab: { scenario: corporate }\nshots: []\n").problems).toContain(
      "lab must be { scenario, systems (a positive whole number), omnibot_replay? }",
    );
    expect(parseShots("{ unclosed").problems[0]).toContain("is not YAML");
  });

  it("parses the committed shot list: eight shots, the two kinds, every one with alt", () => {
    const text = readFileSync(join(__dirname, "../e2e/marketing/shots.yaml"), "utf8");
    const { list, problems } = parseShots(text);
    expect(problems).toEqual([]);
    // The captures' lab seeds the corporate storyline's 30 days of health (#2729).
    expect(list?.lab).toMatchObject({ scenario: "corporate", systems: 1000, omnibotReplay: "corporate-outage", healthHistory: true });
    const byId = Object.fromEntries((list?.shots ?? []).map((s: { id: string; kind: string }) => [s.id, s.kind]));
    expect(byId).toEqual({
      "fleet-overview": "loop",
      "omniglass-teaser": "loop",
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
    expect(budgetProblems("a", { seconds: 7, bytes: 900_000 })).toEqual([]);
    expect(budgetProblems("a", { seconds: LOOP_MIN_SECONDS, bytes: LOOP_MAX_BYTES })).toEqual([]);
    expect(budgetProblems("a", { seconds: LOOP_MAX_SECONDS, bytes: 1 })).toEqual([]);
  });

  it("names the shot and the limit for a loop too short, too long or too big", () => {
    expect(budgetProblems("quick", { seconds: 4.2, bytes: 1000 })).toEqual(["shot quick: loop is 4.2s, under the 5s minimum"]);
    expect(budgetProblems("slow", { seconds: 10.5, bytes: 1000 })).toEqual(["shot slow: loop is 10.5s, over the 9s maximum and its 15% allowance for the lab's speed (10.35s)"]);
    expect(budgetProblems("fat", { seconds: 7, bytes: 1_500_001 })).toEqual(["shot fat: loop is 1500001 bytes, over the 1500000-byte limit"]);
  });

  it("lets a loop run up to LOOP_TOLERANCE past its maximum, since the lab sets how long its waits take", () => {
    expect(LOOP_TOLERANCE).toBe(0.15);
    expect(budgetProblems("a", { seconds: 9.5, bytes: 1000 })).toEqual([]);
    expect(budgetProblems("a", { seconds: 10.35, bytes: 1000 })).toEqual([]);
    expect(budgetProblems("a", { seconds: 10.36, bytes: 1000 })).toHaveLength(1);
    expect(budgetProblems("a", { seconds: 16, bytes: 1000 }, 14)).toEqual([]);
  });

  it("reports every limit a loop is over, and an unmeasured duration", () => {
    expect(budgetProblems("both", { seconds: 20, bytes: 2_000_000 })).toHaveLength(2);
    expect(budgetProblems("nan", { seconds: NaN, bytes: 1000 })[0]).toContain("under the 5s minimum");
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

  it("carries a loop's beats and drops them from a still", () => {
    const beats = [{ step: 2, from: 1, to: 2.5, frame: { x: 0, y: 0, width: 720, height: 450 } }];
    const m = buildManifest({ generatedAt: "x", lab, shots: [{ ...shots[0], beats }, { ...shots[1], beats }] });
    expect(m.shots[0]).toMatchObject({ id: "a-loop", beats });
    expect(m.shots[1]).not.toHaveProperty("beats");
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
  it("hides the lab banner, and nothing else", () => {
    expect(CAPTURE_CSS).toContain('[data-testid="lab-banner"]');
    // Placeholders go; data never does: no rule reaches a table, a row or a figure.
    expect(CAPTURE_CSS.split("\n")).toHaveLength(1);
    expect(CAPTURE_CSS).not.toMatch(/table|tr\b|treeitem|health|alarm/i);
  });
});

const LOOP = (steps: string) =>
  `${LAB}shots:\n  - id: a-loop\n    kind: loop\n    alt: A loop.\n    start: /explore\n    steps:\n${steps}`;

describe("focus and zoom on a step", () => {
  it("takes here, out, a target of its own, and a zoom with a focus", () => {
    const { list, problems } = parseShots(
      LOOP(
        "      - click: { role: button, name: Go }\n        focus: here\n        zoom: 1.5\n" +
          "      - wait: { text: Done }\n        focus: { selector: '#panel' }\n" +
          "      - press: Escape\n        focus: out\n",
      ),
    );
    expect(problems).toEqual([]);
    const steps = list?.shots[0].steps ?? [];
    expect(steps.map((s: { focus?: unknown; zoom?: number }) => [s.focus, s.zoom])).toEqual([
      ["here", 1.5],
      [{ selector: "#panel" }, undefined],
      ["out", undefined],
    ]);
  });

  it("names a zoom out of range, a zoom with no focus, and here on a step with no target", () => {
    const { problems } = parseShots(
      LOOP(
        "      - click: a\n        focus: here\n        zoom: 3.5\n" +
          "      - click: b\n        zoom: 1.5\n" +
          "      - press: Enter\n        focus: here\n",
      ),
    );
    expect(problems).toEqual([
      `shot a-loop: step 1 (click): zoom must be a number from ${CAMERA.minZoom} to ${CAMERA.maxZoom}`,
      "shot a-loop: step 2 (click): zoom goes with a focus on a target",
      "shot a-loop: step 3 (press): focus here needs a step with a target",
    ]);
  });

  it("refuses a focus on a still and on a loop's prep, which is cut", () => {
    const still = parseShots(`${LAB}shots:\n${STILL}    steps:\n      - click: a\n        focus: here\n`);
    expect(still.problems).toEqual(["shot a-still: step 1 has a focus: the camera moves on a loop only"]);
    const prep = parseShots(LOOP("      - click: a\n").replace("    steps:", "    prep:\n      - click: b\n        focus: here\n    steps:"));
    expect(prep.problems).toEqual(["shot a-loop: prep step 1 has a focus: prep is cut from the recording"]);
  });
});

describe("expect on a beat", () => {
  it("takes the text a focused beat's frame must show", () => {
    const { list, problems } = parseShots(LOOP("      - click: a\n        focus: here\n        expect: [Reachability lost, display-1]\n"));
    expect(problems).toEqual([]);
    expect(list?.shots[0].steps[0].expect).toEqual(["Reachability lost", "display-1"]);
  });

  it("names an expect with no focus, on a focus out, and one that is not a list of text", () => {
    const { problems } = parseShots(
      LOOP(
        "      - click: a\n        expect: [x]\n" +
          "      - click: b\n        focus: out\n        expect: [x]\n" +
          "      - click: c\n        focus: here\n        expect: x\n" +
          "      - click: d\n        focus: here\n        expect: []\n",
      ),
    );
    expect(problems).toEqual([
      "shot a-loop: step 1 (click): expect goes with a focus on a target",
      "shot a-loop: step 2 (click): expect goes with a focus on a target",
      "shot a-loop: step 3 (click): expect is a list of the text the frame must show",
      "shot a-loop: step 4 (click): expect is a list of the text the frame must show",
    ]);
  });
});

describe("a beat's frame and its text", () => {
  const vp = { width: 1440, height: 900 };

  it("is the part of the capture the landed camera shows, and the part of that on screen", () => {
    expect(frameRect({ scale: 2, x: 720, y: 450 }, vp)).toEqual({
      rect: { x: 360, y: 225, width: 720, height: 450 },
      onScreen: { x: 360, y: 225, width: 720, height: 450 },
    });
    // Centred near the right edge: the frame reaches past it, the backdrop shows there.
    expect(frameRect({ scale: 2, x: 1300, y: 450 }, vp)).toEqual({
      rect: { x: 940, y: 225, width: 720, height: 450 },
      onScreen: { x: 940, y: 225, width: 500, height: 450 },
    });
  });

  it("finds expected text across pieces and spacing, and names what is missing", () => {
    const seen = ["Reachability lost", "display-1", "impairs  Main\nDisplay"];
    expect(missingText(seen, ["Reachability lost display-1", "impairs Main Display"])).toEqual([]);
    expect(missingText(seen, ["Acknowledge", "display-1"])).toEqual(["Acknowledge"]);
  });
});

describe("frameFor", () => {
  const vp = { width: 1440, height: 900 };

  it("is the whole frame for no box", () => {
    expect(frameFor(null, vp)).toEqual({ scale: 1, x: 720, y: 450 });
  });

  it("fits a box with a margin, capped at the largest zoom", () => {
    // 576 x 360 with a 10% margin each side fits at 2x.
    expect(frameFor({ x: 432, y: 270, width: 576, height: 360 }, vp).scale).toBe(2);
    expect(frameFor({ x: 700, y: 440, width: 10, height: 10 }, vp).scale).toBe(CAMERA.maxZoom);
    expect(frameFor({ x: 0, y: 0, width: 1440, height: 900 }, vp).scale).toBe(1);
  });

  it("takes a zoom over the fit, and centres the box even at the capture's edge", () => {
    // A chip in the top-left corner sits in the middle of the frame.
    const f = frameFor({ x: 10, y: 10, width: 40, height: 20 }, vp, 1.5);
    expect(f).toEqual({ scale: 1.5, x: 30, y: 20 });
  });

  it("keeps a box taller than the frame's top in view, a margin in", () => {
    // At 1.2 the frame is 750 tall; a 1000-tall card at y=100 keeps its top 75 in.
    const f = frameFor({ x: 400, y: 100, width: 600, height: 1000 }, vp, 1.2);
    expect(f).toEqual({ scale: 1.2, x: 700, y: 100 - 75 + 375 });
  });
});

describe("beatEnds", () => {
  it("ends a beat at the next focus, through a wait that closed it early", () => {
    const beats = [{ from: 2.468, to: 2.475 }, { from: 4.286, to: 6.9 }];
    const events = [{ t: 2.468, box: null }, { t: 4.286, box: null }];
    expect(beatEnds(beats, events)).toEqual([{ from: 2.468, to: 4.286 }, { from: 4.286, to: 6.9 }]);
  });
});

describe("beatStill", () => {
  it("takes the still when the frame was checked, once the camera has landed", () => {
    expect(beatStill({ from: 1, at: 3, to: 5 })).toBe(3);
    expect(beatStill({ from: 2.497, at: 2.499, to: 4.292 })).toBe(2.497 + CAMERA.ease);
  });

  it("never past the beat's end", () => {
    expect(beatStill({ from: 1, at: 1.1, to: 1.3 })).toBe(1.25);
  });
});

describe("cameraTrack", () => {
  const vp = { width: 1440, height: 900 };
  const box = { x: 600, y: 400, width: 100, height: 50 };

  it("eases in on a focus and back out before the loop's end", () => {
    const moves = cameraTrack([{ t: 1, box, zoom: 1.5 }], vp, 6);
    expect(moves).toEqual([
      { at: 1, dur: CAMERA.ease, scale: 1.5, x: 650, y: 425, spot: { x: 588, y: 388, width: 124, height: 74 } },
      { at: 6 - CAMERA.ease, dur: CAMERA.ease, scale: 1, x: 720, y: 450, spot: null },
    ]);
  });

  it("waits for a move to land before the next, and drops one that changes nothing", () => {
    const moves = cameraTrack(
      [
        { t: 1, box, zoom: 1.5 },
        { t: 1.2, box: null },
        { t: 2.5, box: null },
      ],
      vp,
      6,
    );
    expect(moves.map((m: { at: number; scale: number }) => [m.at, m.scale])).toEqual([
      [1, 1.5],
      [1 + CAMERA.ease, 1],
    ]);
  });

  it("drops a focus too late to land, and still ends on the whole frame", () => {
    const moves = cameraTrack([{ t: 1, box, zoom: 2 }, { t: 5.8, box: null }], vp, 6);
    expect(moves.at(-1)).toMatchObject({ scale: 1, x: 720, y: 450 });
    expect(moves.at(-1)!.at + moves.at(-1)!.dur).toBeLessThanOrEqual(6);
  });

  it("is empty with no focus", () => {
    expect(cameraTrack([], vp, 6)).toEqual([]);
  });
});

describe("the spotlight and the sped-up badge", () => {
  const vp = { width: 1440, height: 900 };

  it("lights the focus box with a margin, kept on the capture", () => {
    expect(spotFor({ x: 600, y: 400, width: 100, height: 50 }, vp)).toEqual({ x: 588, y: 388, width: 124, height: 74 });
    expect(spotFor({ x: 4, y: 0, width: 100, height: 50 }, vp)).toEqual({ x: 0, y: 0, width: 116, height: 62 });
  });

  it("lights nothing for the whole frame, or a box that fills it", () => {
    expect(spotFor(null, vp)).toBeNull();
    expect(spotFor({ x: 0, y: 0, width: 1400, height: 880 }, vp)).toBeNull();
  });

  it("badges each lapse in the clip's own time, and skips one too short to read", () => {
    const lapses = [{ from: 1, to: 3, speed: 2 }, { from: 5, to: 65, speed: 32 }, { from: 70, to: 71, speed: 4 }];
    expect(lapseBadges(lapses)).toEqual([
      { from: 1, to: 2, speed: 2 },
      { from: 4, to: 5.875, speed: 32 },
    ]);
  });

  it("badges a lapse that plays exactly the shortest badged length, whatever rounding does", () => {
    for (const [from, real] of [[12.345, 3.333], [4.2, 5.7], [0.1, 9.9]]) {
      const lapses = [{ from, to: from + real, speed: lapseSpeed(real, MIN_LAPSE_BADGE) }];
      expect(lapseBadges(lapses)).toHaveLength(1);
    }
  });

  it("reads a lapse's derived speed as a whole number on its badge", () => {
    expect(lapseBadges([{ from: 0, to: 28.5, speed: 31.67 }])).toEqual([{ from: 0, to: 0.9, speed: 32 }]);
  });
});

describe("the camera's tail and time-lapsed waits", () => {
  it("keeps recording after a clip's last focus, and not after a zoom-out", () => {
    expect(cameraTail([])).toBe(0);
    expect(cameraTail([{ t: 1, box: { x: 1, y: 1, width: 5, height: 5 } }])).toBeCloseTo(0.75);
    expect(cameraTail([{ t: 1, box: { x: 1, y: 1, width: 5, height: 5 } }, { t: 2, box: null }])).toBe(0);
    // the last event by time, whatever order they were logged in
    expect(cameraTail([{ t: 5, box: null }, { t: 1, box: { x: 1, y: 1, width: 5, height: 5 } }])).toBe(0);
  });

  it("shortens each second inside a lapse by its speed and leaves the rest", () => {
    const lapses = [{ from: 2, to: 6, speed: 4 }];
    expect(retime(1, lapses)).toBe(1);
    expect(retime(2, lapses)).toBe(2);
    expect(retime(4, lapses)).toBeCloseTo(2.5);
    expect(retime(6, lapses)).toBeCloseTo(3);
    expect(retime(8, lapses)).toBeCloseTo(5);
    expect(retime(3, [])).toBe(3);
  });

  it("composes lapses in order", () => {
    const lapses = [{ from: 1, to: 3, speed: 2 }, { from: 5, to: 9, speed: 4 }];
    expect(retime(9, lapses)).toBeCloseTo(9 - 1 - 3);
    expect(retime(10, lapses)).toBeCloseTo(6);
  });

  it("takes lapse on a wait only, as the seconds of clip it plays in", () => {
    const ok = parseShots(`${LAB}shots:\n  - id: s\n    kind: loop\n    alt: x\n    start: /a\n    steps:\n      - wait: { text: Done }\n        lapse: 1.2\n`);
    expect(ok.problems).toEqual([]);
    expect(ok.list?.shots[0].steps[0]).toMatchObject({ lapse: 1.2 });
    const bad = parseShots(`${LAB}shots:\n  - id: s\n    kind: loop\n    alt: x\n    start: /a\n    steps:\n      - click: a\n        lapse: 1\n      - wait: { text: Done }\n        lapse: 0\n      - wait: { text: Done }\n        lapse: 9.5\n`);
    expect(bad.problems).toContain("shot s: step 1 (click): lapse belongs on a wait");
    expect(bad.problems.filter((p: string) => p.includes("lapse is the seconds"))).toHaveLength(2);
  });

  it("refuses the retired fast, pointing at lapse", () => {
    const { problems } = parseShots(`${LAB}shots:\n  - id: s\n    kind: loop\n    alt: x\n    start: /a\n    steps:\n      - wait: { text: Done }\n        fast: 4\n`);
    expect(problems).toEqual(["shot s: step 1 (wait): fast is retired; lapse: <seconds> plays the wait in that many seconds of the clip"]);
  });

  it("speeds a lapse up as much as its wait needs, never slows one down, and caps it at MAX_FAST", () => {
    expect(lapseSpeed(28.5, 0.9)).toBeCloseTo(31.67);
    expect(lapseSpeed(0.4, 0.9)).toBe(1);
    expect(lapseSpeed(0, 0.9)).toBe(1);
    expect(lapseSpeed(120, 0.9)).toBe(MAX_FAST);
  });
});

describe("a loop's authored length", () => {
  const shot = (steps: string, extra = "") => parseShots(LOOP(steps).replace("    kind: loop\n", `    kind: loop\n${extra}`));

  it("is what the shot list spends: holds, the recorder's pace, each lapse and the closing settle", () => {
    const { list, problems } = shot("      - goto: /b\n        hold: 500\n      - click: a\n      - type: { target: a, text: abcd }\n      - wait: { text: Done }\n        lapse: 1.5\n      - wait: { text: Done }\n      - press: Enter\n        hold: 1000\n");
    expect(problems).toEqual([]);
    const s = list!.shots[0];
    const want = (PACE.load + 500 + PACE.glide + PACE.glide + 4 * PACE.key + 1500 + 1000 + PACE.settle) / 1000;
    expect(authoredSeconds(s)).toBeCloseTo(want);
  });

  it("adds the camera's ease back out when the last focus leaves it zoomed", () => {
    const zoomed = shot("      - click: a\n        focus: here\n").list!.shots[0];
    const out = shot("      - click: a\n        focus: here\n      - press: Enter\n        focus: out\n").list!.shots[0];
    expect(authoredSeconds(zoomed) - authoredSeconds(out)).toBeCloseTo(CAMERA.ease + 0.15);
  });

  it("fails the parse when the steps alone are over the maximum, before any lab is started", () => {
    const { problems } = shot("      - click: a\n        hold: 8000\n      - click: b\n        hold: 2000\n");
    expect(problems).toEqual(["shot a-loop: its steps spend 10.8s before the lab adds any, over the 9s maximum"]);
  });
});

describe("where an overrun's seconds went", () => {
  it("names the steps the lab stretched most past what the shot list spends on them", () => {
    const steps = [
      { n: 1, kind: "click", target: "a", clip: 0.45, authored: 0.45 },
      { n: 2, kind: "wait", target: "text Done", clip: 3.2, authored: 0 },
      { n: 3, kind: "click", target: "b", clip: 1.7, authored: 0.5 },
      { n: 4, kind: "press", target: "", clip: 1.0, authored: 1.0 },
    ];
    expect(overrunReport("s", 9.6, 9, steps)).toEqual([
      "shot s: loop is 9.6s, past its 9s maximum; the lab's speed added 4.4s, most in:",
      "  step 2 (wait text Done): 3.2s, 3.2s past its 0.0s",
      "  step 3 (click b): 1.7s, 1.2s past its 0.5s",
    ]);
  });

  it("says nothing for a loop inside its maximum", () => {
    expect(overrunReport("s", 8.9, 9, [])).toEqual([]);
  });
});

describe("a loop's own maximum", () => {
  it("defaults to the usual maximum, and a loop may raise it up to the cap", () => {
    const plain = parseShots(LOOP("      - click: a\n"));
    expect(plain.list?.shots[0].maxSeconds).toBe(LOOP_MAX_SECONDS);
    const long = parseShots(LOOP("      - click: a\n").replace("    kind: loop\n", "    kind: loop\n    max_seconds: 14\n"));
    expect(long.problems).toEqual([]);
    expect(long.list?.shots[0].maxSeconds).toBe(14);
  });

  it("refuses one at or under the usual maximum, over the cap, or on a still", () => {
    for (const v of [LOOP_MAX_SECONDS, LOOP_MAX_SECONDS_CAP + 1]) {
      const { problems } = parseShots(LOOP("      - click: a\n").replace("    kind: loop\n", `    kind: loop\n    max_seconds: ${v}\n`));
      expect(problems).toEqual([`shot a-loop: max_seconds is over ${LOOP_MAX_SECONDS} and at most ${LOOP_MAX_SECONDS_CAP}`]);
    }
    const still = parseShots(`${LAB}shots:\n${STILL}    max_seconds: 12\n`);
    expect(still.problems).toEqual(["shot a-still: max_seconds belongs on a loop"]);
  });

  it("is the limit the budget holds the loop to", () => {
    expect(budgetProblems("long", { seconds: 12, bytes: 1000 }, 14)).toEqual([]);
    expect(budgetProblems("long", { seconds: 16.5, bytes: 1000 }, 14)).toEqual(["shot long: loop is 16.5s, over the 14s maximum and its 15% allowance for the lab's speed (16.1s)"]);
  });

  it("earns a byte budget at the same rate past 15 seconds, and only past it", () => {
    expect(loopMaxBytes(LOOP_MAX_SECONDS)).toBe(LOOP_MAX_BYTES);
    expect(loopMaxBytes(15)).toBe(LOOP_MAX_BYTES);
    expect(loopMaxBytes(30)).toBe(2 * LOOP_MAX_BYTES);
    expect(budgetProblems("teaser", { seconds: 28, bytes: 2_900_000 }, 30)).toEqual([]);
    expect(budgetProblems("teaser", { seconds: 28, bytes: 3_000_001 }, 30)).toEqual(["shot teaser: loop is 3000001 bytes, over the 3000000-byte limit"]);
    expect(budgetProblems("short", { seconds: 8, bytes: 1_500_001 })).toHaveLength(1);
  });
});

describe("a call step", () => {
  const CALL = "      - call: { put: '/components/{a.$comp.display-1}/impairment', body: { unplugged: false } }\n";

  it("is an API request mid-clip, with its method, path and body", () => {
    const { list, problems } = parseShots(LOOP("      - click: a\n" + CALL + "        hold: 500\n"));
    expect(problems).toEqual([]);
    expect(list?.shots[0].steps[1]).toMatchObject({ kind: "call", method: "PUT", path: "/components/{a.$comp.display-1}/impairment", body: { unplugged: false }, hold: 500 });
  });

  it("names a missing method, a bad path, a missing body and a focus", () => {
    const { problems } = parseShots(
      LOOP(
        "      - call: { body: {} }\n" +
          "      - call: { put: components/x, body: {} }\n" +
          "      - call: { post: /secrets }\n" +
          "      - call: { patch: /endpoints/x, body: {} }\n        focus: '#x'\n",
      ),
    );
    expect(problems).toEqual([
      "shot a-loop: step 1 (call) needs exactly one of put, post, patch",
      'shot a-loop: step 2 (call): put must be an API path starting with "/"',
      "shot a-loop: step 3 (call): needs a body mapping",
      "shot a-loop: step 4 (call): a call is off camera, so it takes no focus",
    ]);
  });

  it("addresses the lab the way a setup item does, so the preflight checks it", () => {
    const refs = referencedResources([{ kind: "call", method: "PUT", path: "/components/{a.$comp.display-1}/impairment", body: {} }], null);
    expect(refs).toEqual([{ kind: "component", id: "a.$comp.display-1", path: "/components/{a.$comp.display-1}" }]);
  });
});

describe("where the pointer rests on a step's target", () => {
  const box = { x: 100, y: 50, width: 360, height: 36 };
  it("clicks and hovers in the middle", () => {
    expect(pointerX("click", box)).toBe(280);
    expect(pointerX("hover", box)).toBe(280);
  });
  it("types at the field's start", () => {
    expect(pointerX("type", box)).toBe(124);
  });
  it("picks a select's value from its chevron end, so the value stays in sight", () => {
    expect(pointerX("select", box)).toBe(444);
    // A narrow select still rests no further left than its middle.
    expect(pointerX("select", { x: 0, y: 0, width: 20, height: 20 })).toBe(10);
  });
});

describe("the questions a shot asks the lab's recorded omnibot (#2526)", () => {
  const ask = (text: string) => ({ id: "omnibot-ask", steps: [{ kind: "click" }, { kind: "type", target: 'textarea[aria-label="Ask omnibot"]', text }] });
  const prompts = ["Why is the 86-inch meeting room degraded?", undefined as unknown as string, "What is wrong with the main display?"];
  it("passes a question that holds one of the recording's prompts, as the replay compares them", () => {
    expect(replayProblems([ask("why is the 86-inch   meeting room degraded")], prompts)).toEqual([]);
    expect(replayProblems([ask("So: what is wrong with the main display?")], prompts)).toEqual([]);
  });
  it("names a question the recording would refuse, by shot and step", () => {
    const got = replayProblems([ask("Why is this room degraded?")], prompts);
    expect(got).toHaveLength(1);
    expect(got[0]).toContain('omnibot-ask step 2 asks omnibot "Why is this room degraded?"');
  });
  it("leaves other typing alone", () => {
    expect(replayProblems([{ id: "search", steps: [{ kind: "type", target: "input[type=search]", text: "lobby" }] }], prompts)).toEqual([]);
  });
  it("the shipped shot list asks only what its recording answers", () => {
    const list = parseShots(readFileSync(join(__dirname, "../e2e/marketing/shots.yaml"), "utf8")).list!;
    const rec = readFileSync(join(__dirname, `../../internal/lab/omnibot/${list.lab.omnibotReplay}.yaml`), "utf8");
    const recorded = [...rec.matchAll(/^\s*-?\s*prompt:\s*(.+)$/gm)].map((m) => m[1].trim());
    expect(recorded).toContain("Why is the 86-inch meeting room degraded?");
    expect(replayProblems(list.shots as never, recorded)).toEqual([]);
  });
});
