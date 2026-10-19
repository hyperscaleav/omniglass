import { describe, it, expect } from "vitest";
import {
  SEVERITY_BADGE,
  SEVERITY_COLORS,
  compareSeverity,
  draftFromLevel,
  emptyLevelDraft,
  levelCreateBody,
  levelUpdateBody,
  outageFloor,
  rankPlace,
  severityBadge,
  severityScale,
  validateLevelDraft,
} from "./severity";
import { sortAlarms } from "./alarms";
import { MAJOR_LEVEL, SEEDED_LEVELS } from "./testseverity";

// The severity registry's pure core (#917): a level name in, its label, badge
// class, rank and outage flag out, and alarms ordered by the registry's rank.
describe("severityScale", () => {
  const scale = severityScale(SEEDED_LEVELS);

  it.each([
    ["info", "Info", "badge-ghost", 10, false],
    ["warning", "Warning", "badge-soft badge-warning", 20, false],
    ["average", "Average", "badge-average", 30, false],
    ["high", "High", "badge-soft badge-error", 40, true],
    ["disaster", "Disaster", "badge-disaster", 50, true],
  ])("maps %s to its label, class and outage flag", (name, label, badge, rank, outage) => {
    expect(scale.look(name)).toEqual({ name, label, badge, rank, outage, known: true });
    expect(scale.outage(name)).toBe(outage);
  });

  it("falls back to the raw name in the neutral chip for a name it does not know", () => {
    expect(scale.look("critical")).toEqual({ name: "critical", label: "critical", badge: "badge-ghost", rank: null, outage: false, known: false });
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

describe("sorting by the registry's rank", () => {
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
      rank: "Its place among the levels.",
    });
  });

  it("refuses a name or a rank another level holds, and a rank that is not a whole number above zero", () => {
    const d = { name: "average", label: "", color: "orange" as const, rank: "30" };
    expect(validateLevelDraft(d, levels)).toEqual({
      name: "A level is already named average.",
      rank: "Average already holds rank 30; each level has its own.",
    });
    for (const rank of ["0", "-5", "2.5", "1e2", " 3x"]) {
      expect(validateLevelDraft({ ...d, name: "p2", rank }, levels).rank).toBe("A whole number above zero.");
    }
    expect(validateLevelDraft({ ...d, name: "Major Two" }, levels).name).toBe("Lowercase letters, digits and single hyphens, e.g. major.");
  });

  it("lets a level keep its own name and rank on edit", () => {
    expect(validateLevelDraft(draftFromLevel(MAJOR_LEVEL), levels, "major")).toEqual({});
  });

  it("says where a rank sits: between two levels, above the worst, below the mildest", () => {
    expect(rankPlace(35, SEEDED_LEVELS)).toBe("Between Average (30) and High (40).");
    expect(rankPlace(60, SEEDED_LEVELS)).toBe("Above Disaster (50), the worst level.");
    expect(rankPlace(5, SEEDED_LEVELS)).toBe("Below Info (10), the mildest level.");
    // A level being edited is not its own neighbour.
    expect(rankPlace(36, levels, "major")).toBe("Between Average (30) and High (40).");
  });

  it("reads the outage floor off the levels the server marks", () => {
    expect(outageFloor(levels)).toBe(40);
    expect(outageFloor([])).toBeNull();
  });

  it("creates with a trimmed name and leaves a blank label off", () => {
    expect(levelCreateBody({ name: " p2 ", label: " ", color: "orange", rank: "35" })).toEqual({ name: "p2", color: "orange", rank: 35 });
    expect(levelCreateBody({ name: "p2", label: "P2", color: "orange", rank: "35" }).label).toBe("P2");
  });

  it("patches only what an edit changed", () => {
    expect(levelUpdateBody(draftFromLevel(MAJOR_LEVEL), MAJOR_LEVEL)).toEqual({});
    expect(levelUpdateBody({ ...draftFromLevel(MAJOR_LEVEL), label: "P2" }, MAJOR_LEVEL)).toEqual({ label: "P2" });
    expect(levelUpdateBody({ name: "p2", label: "Major", color: "red", rank: "45" }, MAJOR_LEVEL)).toEqual({ name: "p2", color: "red", rank: 45 });
  });

  it("offers every color the badge map knows, mildest first", () => {
    expect([...SEVERITY_COLORS].sort()).toEqual(Object.keys(SEVERITY_BADGE).sort());
    expect(SEVERITY_COLORS[0]).toBe("gray");
  });
});
