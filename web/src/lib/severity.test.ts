import { describe, it, expect } from "vitest";
import {
  SEVERITY_BADGE,
  SEVERITY_COLORS,
  compareSeverity,
  draftFromLevel,
  emptyLevelDraft,
  levelCreateBody,
  levelUpdateBody,
  deriveEffect,
  effectOf,
  rankPlace,
  severityBadge,
  severityScale,
  validateLevelDraft,
} from "./severity";
import { sortAlarms } from "./alarms";
import { MAJOR_LEVEL, SEEDED_LEVELS } from "./testseverity";

// The severity catalog's pure core (#917): a level name in, its label, badge
// class, rank and outage flag out, and alarms ordered by the catalog's rank.
describe("severityScale", () => {
  const scale = severityScale(SEEDED_LEVELS);

  it.each([
    ["info", "Info", "badge-ghost", 10, "none"],
    ["warning", "Warning", "badge-soft badge-warning", 20, "degraded"],
    ["average", "Average", "badge-average", 30, "degraded"],
    ["high", "High", "badge-soft badge-error", 40, "outage"],
    ["disaster", "Disaster", "badge-disaster", 50, "outage"],
  ])("maps %s to its label, class and declared health effect", (name, label, badge, rank, effect) => {
    expect(scale.look(name)).toEqual({ name, label, badge, rank, outage: effect === "outage", effect, known: true });
    expect(scale.outage(name)).toBe(effect === "outage");
    expect(scale.effect(name)).toBe(effect);
  });

  it("falls back to the raw name in the neutral chip for a name it does not know", () => {
    expect(scale.look("critical")).toEqual({ name: "critical", label: "critical", badge: "badge-ghost", rank: null, outage: false, effect: "degraded", known: false });
  });

  it("treats every name as unknown while the levels have not loaded", () => {
    const empty = severityScale(undefined);
    expect(empty.look("high").known).toBe(false);
    expect(empty.outage("high")).toBe(false);
    expect(empty.levels).toEqual([]);
  });

  it("orders its levels least to most severe whatever order they arrive in", () => {
    const s = severityScale([...SEEDED_LEVELS, MAJOR_LEVEL].reverse());
    expect(s.levels.map((l) => l.name)).toEqual(["info", "warning", "average", "major", "high", "disaster"]);
  });

  it("uses the name when a level has no label", () => {
    expect(severityScale([{ ...MAJOR_LEVEL, label: undefined }]).look("major").label).toBe("major");
  });
});

describe("severity badge classes", () => {
  it("never paints a severity with badge-neutral", () => {
    expect(Object.values(SEVERITY_BADGE).join(" ")).not.toMatch(/badge-neutral/);
  });

  it("keeps darkred distinguishable from red", () => {
    expect(severityBadge("darkred")).not.toBe(severityBadge("red"));
  });

  it("maps blue and green to the daisyUI semantics, and an unknown color to neutral", () => {
    expect(severityBadge("blue")).toBe("badge-soft badge-info");
    expect(severityBadge("green")).toBe("badge-soft badge-success");
    expect(severityBadge("purple")).toBe("badge-ghost");
    expect(severityBadge(undefined)).toBe("badge-ghost");
  });
});

describe("sorting by the catalog's rank", () => {
  const at = (severity: string, raised_at: string) => ({ severity, raised_at });

  it("puts the worst first, the newest first within a level, and unknown names last", () => {
    const rows = [
      at("info", "2026-08-15T10:00:00Z"),
      at("mystery", "2026-08-15T13:00:00Z"),
      at("high", "2026-08-15T09:00:00Z"),
      at("warning", "2026-08-15T11:00:00Z"),
      at("disaster", "2026-08-15T08:00:00Z"),
      at("high", "2026-08-15T12:00:00Z"),
    ];
    expect(sortAlarms(rows, SEEDED_LEVELS).map((r) => `${r.severity}@${r.raised_at.slice(11, 13)}`)).toEqual([
      "disaster@08",
      "high@12",
      "high@09",
      "warning@11",
      "info@10",
      "mystery@13",
    ]);
  });

  it("slots an operator-defined level by its rank: major sits between average and high", () => {
    const levels = [...SEEDED_LEVELS, MAJOR_LEVEL];
    const rows = [at("average", "2026-08-15T10:00:00Z"), at("high", "2026-08-15T10:00:00Z"), at("major", "2026-08-15T10:00:00Z")];
    expect(sortAlarms(rows, levels).map((r) => r.severity)).toEqual(["high", "major", "average"]);
  });

  it("orders by recency alone when no levels are known", () => {
    const rows = [at("high", "2026-08-15T09:00:00Z"), at("info", "2026-08-15T10:00:00Z")];
    expect(sortAlarms(rows).map((r) => r.severity)).toEqual(["info", "high"]);
  });

  it("compares two names worst first", () => {
    const cmp = compareSeverity(SEEDED_LEVELS);
    expect(cmp("high", "warning")).toBeLessThan(0);
    expect(cmp("warning", "high")).toBeGreaterThan(0);
    expect(cmp("high", "high")).toBe(0);
    expect(cmp("nope", "info")).toBeGreaterThan(0);
  });
});

// Authoring a level (#930): the draft the console's form judges before it
// sends, the sentence that teaches where a rank sits, and the bodies a draft
// sends.
describe("a level draft", () => {
  const levels = [...SEEDED_LEVELS, MAJOR_LEVEL];

  it("asks for a name, a color and a rank, field by field", () => {
    expect(validateLevelDraft(emptyLevelDraft(), levels)).toEqual({
      name: "A name for the level.",
      color: "The hue its alarms wear.",
      rank: "Its position among the levels.",
    });
  });

  it("refuses a name or a rank another level holds, and a rank that is not a whole number above zero", () => {
    const d = { name: "average", label: "", color: "orange" as const, rank: "30", effect: "degraded" as const };
    expect(validateLevelDraft(d, levels)).toEqual({
      name: "A level is already named average.",
      rank: "Average already holds rank 30; each level has its own.",
    });
    for (const rank of ["0", "-5", "2.5", "1e2", " 3x"]) {
      expect(validateLevelDraft({ ...d, name: "p2", rank }, levels).rank).toBe("A whole number above zero.");
    }
    expect(validateLevelDraft({ ...d, name: "Major Two" }, levels).name).toMatch(/Lowercase/);
  });

  it("lets a level keep its own name and rank on edit", () => {
    expect(validateLevelDraft(draftFromLevel(MAJOR_LEVEL), levels, "major")).toEqual({});
  });

  it("judges a name on the server's entity name rule: a leading hyphen run, its length cap, never a uuid", () => {
    const d = { name: "", label: "", color: "orange" as const, rank: "33", effect: "degraded" as const };
    // The server's rule allows repeated and trailing hyphens; the form does too.
    expect(validateLevelDraft({ ...d, name: "p2--high" }, levels).name).toBeUndefined();
    expect(validateLevelDraft({ ...d, name: "p2-" }, levels).name).toBeUndefined();
    expect(validateLevelDraft({ ...d, name: "-p2" }, levels).name).toMatch(/Lowercase/);
    expect(validateLevelDraft({ ...d, name: "x".repeat(101) }, levels).name).toBe("At most 100 characters.");
    expect(validateLevelDraft({ ...d, name: "0190a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a5b" }, levels).name).toMatch(/uuid/);
  });

  it("only judges the name on edit when it changed", () => {
    // A level stored before the rule tightened keeps saving its other fields.
    const legacy = { ...MAJOR_LEVEL, name: "Legacy_Name" };
    expect(validateLevelDraft(draftFromLevel(legacy), [...levels, legacy], "Legacy_Name").name).toBeUndefined();
    expect(validateLevelDraft({ ...draftFromLevel(legacy), name: "Still_Bad" }, [...levels, legacy], "Legacy_Name").name).toMatch(/Lowercase/);
  });

  it("says where a rank sits: between two levels, above the worst, below the mildest", () => {
    expect(rankPlace(35, SEEDED_LEVELS)).toBe("Between Average (30) and High (40).");
    expect(rankPlace(60, SEEDED_LEVELS)).toBe("Above Disaster (50), the worst level.");
    expect(rankPlace(5, SEEDED_LEVELS)).toBe("Below Info (10), the mildest level.");
    // A level being edited is not its own neighbour.
    expect(rankPlace(36, levels, "major")).toBe("Between Average (30) and High (40).");
  });

  it("reads the effect a level declares, falling back to the deprecated flag only when it is absent", () => {
    expect(effectOf(SEEDED_LEVELS[0])).toBe("none");
    expect(effectOf({ health_effect: undefined as never, outage: true })).toBe("outage");
    expect(effectOf({ health_effect: undefined as never, outage: false })).toBe("degraded");
  });

  it("preselects the effect the server would derive from a rank", () => {
    expect([5, 25, 45].map(deriveEffect)).toEqual(["none", "degraded", "outage"]);
  });

  it("sends the chosen effect on create and only a changed one on edit", () => {
    expect(levelCreateBody({ name: "p2", label: "", color: "gray", rank: "15", effect: "none" }).health_effect).toBe("none");
    expect(levelUpdateBody({ ...draftFromLevel(MAJOR_LEVEL), effect: "outage" }, MAJOR_LEVEL)).toEqual({ health_effect: "outage" });
  });

  it("creates with a trimmed name and leaves a blank label off", () => {
    expect(levelCreateBody({ name: " p2 ", label: " ", color: "orange", rank: "35", effect: "degraded" as const })).toEqual({ name: "p2", color: "orange", rank: 35, health_effect: "degraded" });
    expect(levelCreateBody({ name: "p2", label: "P2", color: "orange", rank: "35", effect: "degraded" as const }).label).toBe("P2");
  });

  it("patches only what an edit changed", () => {
    expect(levelUpdateBody(draftFromLevel(MAJOR_LEVEL), MAJOR_LEVEL)).toEqual({});
    expect(levelUpdateBody({ ...draftFromLevel(MAJOR_LEVEL), label: "P2" }, MAJOR_LEVEL)).toEqual({ label: "P2" });
    expect(levelUpdateBody({ name: "p2", label: "Major", color: "red", rank: "45", effect: "degraded" as const }, MAJOR_LEVEL)).toEqual({ name: "p2", color: "red", rank: 45 });
  });

  it("offers every color the badge map knows, mildest first", () => {
    expect([...SEVERITY_COLORS].sort()).toEqual(Object.keys(SEVERITY_BADGE).sort());
    expect(SEVERITY_COLORS[0]).toBe("gray");
  });
});
