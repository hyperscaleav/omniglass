import { describe, it, expect } from "vitest";
import { SEVERITY_BADGE, compareSeverity, severityBadge, severityScale } from "./severity";
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
