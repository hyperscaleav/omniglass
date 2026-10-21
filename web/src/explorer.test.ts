import { describe, it, expect } from "vitest";
import { existsSync, readFileSync } from "node:fs";
import { join } from "node:path";
import { load as parse } from "js-yaml";
import {
  LENSES,
  adoptBody,
  digestKey,
  LENS_BACKSTOP_DAYS,
  WEEKLY_SUGGESTION_CAP,
  findingProblems,
  issueKey,
  keyLine,
  missionProblems,
  parseRunLog,
  pickMission,
  planFiling,
  planRollover,
  sightingDate,
  runLine,
  weekOf,
} from "../e2e/explorerlib.mjs";

const mission = (id: string, surfaces: string[], lenses = ["ux"]) => ({ id, persona: "p", user: "operator", goal: "g", scenario: "dev", lenses, surfaces, done: "d" });

describe("the shipped missions", () => {
  it("are well formed", () => {
    const doc = parse(readFileSync(join(__dirname, "../e2e/explorer/missions.yaml"), "utf8"));
    expect(missionProblems(doc)).toEqual([]);
  });
  it("name only repo paths that exist, so a rename cannot silently blind a mission", () => {
    const doc = parse(readFileSync(join(__dirname, "../e2e/explorer/missions.yaml"), "utf8")) as { missions: { id: string; surfaces: string[] }[] };
    const root = join(__dirname, "../..");
    const missing = doc.missions.flatMap((m) => m.surfaces.filter((s) => !existsSync(join(root, s))).map((s) => `${m.id}: ${s}`));
    expect(missing).toEqual([]);
  });
});

describe("missionProblems", () => {
  it("refuses an unknown lens, a repeated id and too many lenses", () => {
    const p = missionProblems({ missions: [mission("a", ["web/"], ["vibes"]), mission("a", ["web/"], ["ux", "design", "performance"])] });
    expect(p.join("\n")).toMatch(/unknown lens "vibes"/);
    expect(p.join("\n")).toMatch(/id is used twice/);
    expect(p.join("\n")).toMatch(/names 1 to 2 lenses/);
  });
});

describe("the run log", () => {
  it("round-trips through its marker line, oldest first", () => {
    const a = { date: "2026-10-20", mission: "triage", lenses: ["ux", "performance"] };
    const b = { date: "2026-10-19", mission: "onboard", lenses: ["design"] };
    expect(parseRunLog([`report\n${runLine(a)}\n`, runLine(b)])).toEqual([b, a]);
  });
});

describe("pickMission", () => {
  const ms = [mission("alarms", ["web/src/pages/alarms", "internal/rules"]), mission("fleet", ["web/src/pages/fleet"]), mission("files", ["internal/file"])];

  const ch = (date: string, path: string) => ({ date, path });

  it("prefers the mission whose surfaces changed since its own last run, most changed first", () => {
    const runs = [
      { date: "2026-10-10", mission: "alarms", lenses: ["ux"] },
      { date: "2026-10-11", mission: "files", lenses: ["ux"] },
    ];
    const changes = [ch("2026-10-15", "internal/file/x.go"), ch("2026-10-15", "web/src/pages/alarms/a.tsx"), ch("2026-10-16", "internal/rules/r.go")];
    const got = pickMission(ms, runs, changes, "2026-10-20");
    expect(got.mission.id).toBe("alarms");
    expect(got.why).toMatch(/2 change/);
  });

  it("never lets one busy mission starve the rest", () => {
    // alarms' surfaces change every day; across nine days every mission runs.
    const runs: { date: string; mission: string; lenses: string[] }[] = [];
    const counts: Record<string, number> = {};
    for (let d = 1; d <= 9; d++) {
      const today = `2026-10-${String(d).padStart(2, "0")}`;
      const changes = Array.from({ length: d }, (_, k) => ch(`2026-10-${String(k + 1).padStart(2, "0")}`, "internal/rules/r.go"));
      const { mission } = pickMission(ms, runs, changes, today);
      runs.push({ date: today, mission: mission.id, lenses: ["ux"] });
      counts[mission.id] = (counts[mission.id] ?? 0) + 1;
    }
    expect(counts).toEqual({ alarms: 3, fleet: 3, files: 3 });
  });

  it("else takes the least recently run, never-run first", () => {
    const runs = [
      { date: "2026-10-18", mission: "alarms", lenses: ["ux"] },
      { date: "2026-10-19", mission: "fleet", lenses: ["ux"] },
    ];
    expect(pickMission(ms, runs, [], "2026-10-20").mission.id).toBe("files");
    expect(pickMission(ms.slice(0, 2), runs, [], "2026-10-20").mission.id).toBe("alarms");
  });

  it("brings a lens unused for the backstop window, within the cap", () => {
    const recent = (l: string) => ({ date: "2026-10-19", mission: "x", lenses: [l] });
    const runs = [{ date: "2026-10-01", mission: "x", lenses: [] as string[] }, ...LENSES.filter((l) => l !== "accessibility").map(recent)];
    const one = pickMission([mission("m", ["web/"], ["ux"])], runs, [], "2026-10-20");
    expect(one.lenses).toEqual(["ux", "accessibility"]);
    const two = pickMission([mission("m", ["web/"], ["ux", "design"])], runs, [], "2026-10-20");
    expect(two.lenses).toEqual(["ux", "accessibility"]);
    expect(pickMission([mission("m", ["web/"], ["ux", "design"])], [], [], "2026-10-20").lenses).toEqual(["ux", "design"]);
    const fresh = [...runs, { date: `2026-10-${20 - LENS_BACKSTOP_DAYS + 1}`, mission: "x", lenses: ["accessibility"] }];
    expect(pickMission([mission("m", ["web/"], ["ux"])], fresh, [], "2026-10-20").lenses).toEqual(["ux"]);
  });
});

// eslint-disable-next-line @typescript-eslint/no-explicit-any
const finding = (over: Record<string, unknown> = {}): any => ({
  lens: "ux",
  kind: "suggestion",
  severity: "medium",
  key: "ux:alarms:ack-buried",
  area: "area:ui",
  surface: "/alarms",
  claim: "Acknowledge is two menus deep",
  evidence: "probe/out/alarms.png",
  failure_scenario: "an operator triaging forty alarms opens a menu forty times",
  ...over,
});

describe("findingProblems", () => {
  it("accepts the shape", () => {
    expect(findingProblems(finding())).toEqual([]);
    expect(findingProblems(finding({ lens: "explorer" }))).toEqual([]);
  });
  it("checks the area against the repo's labels when it has them", () => {
    expect(findingProblems(finding({ area: "area:web" }), ["area:ui", "area:api"]).join()).toMatch(/not a label in the repo/);
    expect(findingProblems(finding(), ["area:ui"])).toEqual([]);
  });
  it("lets a digest bug rest on the probe as its repro", () => {
    expect(findingProblems(finding({ kind: "bug", key: "digest:a11y:label" }))).toEqual([]);
  });
  it("refuses a bug without a repro, a bad key and a stray field", () => {
    const p = findingProblems(finding({ kind: "bug", key: "Has Spaces", mood: "grim" })).join("\n");
    expect(p).toMatch(/repro/);
    expect(p).toMatch(/key/);
    expect(p).toMatch(/mood: not a finding field/);
  });
});

describe("weekOf", () => {
  it("names the Monday", () => {
    expect(weekOf("2026-10-20")).toEqual({ monday: "2026-10-19", title: "explorer: week of 2026-10-19" });
    expect(weekOf("2026-10-25").monday).toBe("2026-10-19");
    expect(weekOf("2026-10-19").monday).toBe("2026-10-19");
  });
});

describe("planFiling", () => {
  const run = { date: "2026-10-20", mission: "triage" };

  it("turns a known key into a sighting, never a second issue", () => {
    const open = [{ number: 12, title: "t", body: `x\n\n${keyLine("ux:alarms:ack-buried")}` }];
    expect(issueKey(open[0].body)).toBe("ux:alarms:ack-buried");
    const [a] = planFiling([finding()], open, 0, run);
    expect(a).toMatchObject({ action: "sighting", issue: 12 });
  });

  it("files a bug at once and a suggestion under the cap, most severe first", () => {
    const fs = [
      finding({ key: "ux:a", severity: "low" }),
      finding({ key: "ux:b", severity: "high" }),
      finding({ key: "perf:c", kind: "bug", lens: "performance", repro: "omniglass components list" }),
    ];
    const acts = planFiling(fs, [], WEEKLY_SUGGESTION_CAP - 1, run);
    expect(acts.map((a) => [a.action, a.key])).toEqual([
      ["suggestion", "ux:b"],
      ["bug", "perf:c"],
      ["over-cap", "ux:a"],
    ]);
    expect(acts[1].body).toMatch(/explorer-key: perf:c$/);
    expect(acts[1].title).toBe("fix: acknowledge is two menus deep");
  });

  it("does not sight an issue this same run already filed (a re-run after a failure)", () => {
    const open = [{ number: 12, title: "t", createdAt: "2026-10-20T15:00:00Z", body: keyLine("ux:alarms:ack-buried") }];
    expect(planFiling([finding()], open, 0, run)[0]).toMatchObject({ action: "filed-today", issue: 12 });
  });

  it("files one issue for two findings sharing a key", () => {
    expect(planFiling([finding(), finding({ lens: "design" })], [], 0, run)).toHaveLength(1);
  });
});

describe("planRollover", () => {
  it("carries what was seen again this week, closes the rest, leaves promoted work alone", () => {
    const subs = [
      { number: 1, type: "Task", state: "OPEN", sightings: ["2026-10-21"] },
      { number: 2, type: "Task", state: "OPEN", sightings: ["2026-10-12"] },
      { number: 3, type: "Feature", state: "OPEN", sightings: [] },
      { number: 4, type: "Task", state: "CLOSED", sightings: [] },
    ];
    expect(planRollover(subs, "2026-10-19")).toEqual([
      { action: "carry", issue: 1 },
      { action: "close", issue: 2 },
    ]);
  });
  it("reads a sighting's date from the comment planFiling writes", () => {
    const [a] = planFiling([finding()], [{ number: 9, title: "t", body: keyLine("ux:alarms:ack-buried") }], 0, { date: "2026-10-20", mission: "m" });
    expect(sightingDate(a.body)).toBe("2026-10-20");
  });
});

describe("digestKey", () => {
  it("is a valid finding key, stable across lenses and days", () => {
    const k = digestKey("slow-route", "GET /api/v1/components/{name}/reachability");
    expect(k).toBe("digest:slow-route:get-/api/v1/components/-name-/reachability");
    expect(findingProblems(finding({ key: k }))).toEqual([]);
  });
});

describe("adoptBody", () => {
  it("keys a person's issue once", () => {
    const b = adoptBody("Found by hand.\n", "digest:a11y:label");
    expect(issueKey(b)).toBe("digest:a11y:label");
    expect(() => adoptBody(b, "digest:a11y:other")).toThrow(/already carries/);
  });
});
