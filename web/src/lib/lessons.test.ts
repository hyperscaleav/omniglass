import { describe, expect, it } from "vitest";
import { CAPABILITIES, LESSONS, doneSet, lessonBySlug, resolveLink, stepFor, withDone, type Capability } from "./lessons";
import { ROUTE_MANIFEST } from "./routemanifest";

const ALL_ON = Object.fromEntries(Object.keys(CAPABILITIES).map((k) => [k, true])) as Record<Capability, boolean>;

// A base-relative address resolves to a manifest route (not the catch-all).
function routes(to: string): boolean {
  const path = to.split("?")[0];
  return ROUTE_MANIFEST.some((r) => {
    if (r.path === "*") return false;
    const re = new RegExp(`^${r.path.replace(/:[^/]+/g, "[^/]+")}$`);
    return re.test(path);
  });
}

describe("the lessons", () => {
  it("are the six, in lifecycle order, each with steps", () => {
    expect(LESSONS.map((l) => l.title)).toEqual([
      "From device to samples",
      "Reach and collection",
      "Rules and alarms",
      "Health and impact",
      "Acting on it",
      "Reading the history",
    ]);
    expect(new Set(LESSONS.map((l) => l.slug)).size).toBe(6);
    for (const l of LESSONS) {
      expect(l.steps.length, l.slug).toBeGreaterThan(0);
      expect(l.minutes, l.slug).toBeGreaterThan(0);
    }
  });

  it("link only to routes the console has, today and once every sibling lands", () => {
    for (const l of LESSONS) {
      for (const s of l.steps) {
        for (const caps of [CAPABILITIES, ALL_ON]) {
          const to = resolveLink(stepFor(s, caps).link.to, { component: "00000000-0000-4000-8000-000000000000" });
          expect(to && routes(to), `${l.slug}: ${s.title} -> ${to}`).toBe(true);
        }
      }
    }
  });

  it("ship with every sibling capability off, so no step links ahead of its surface", () => {
    expect(Object.values(CAPABILITIES).every((v) => v === false)).toBe(true);
  });
});

describe("stepFor", () => {
  const step = LESSONS[0].steps[1];
  it("keeps the main-today form while the surface is missing", () => {
    expect(stepFor(step).link).toEqual(step.link);
  });
  it("takes the better form once its capability exists", () => {
    expect(stepFor(step, ALL_ON).link).toEqual(step.better!.link);
    expect(stepFor(step, ALL_ON).prose).toBe(step.better!.prose);
  });
});

describe("resolveLink", () => {
  it("fills the component by uuid", () => {
    expect(resolveLink("/components/{component}?tab=activity", { component: "abc" })).toBe("/components/abc?tab=activity");
  });
  it("is null when the link needs a component and there is none", () => {
    expect(resolveLink("/components/{component}", {})).toBeNull();
  });
  it("passes a link with no entity through", () => {
    expect(resolveLink("/platform", {})).toBe("/platform");
  });
});

describe("done", () => {
  it("keeps only slugs that still name a lesson", () => {
    expect([...doneSet(["rules-and-alarms", "gone", 3])]).toEqual(["rules-and-alarms"]);
    expect(doneSet(undefined).size).toBe(0);
  });
  it("marks and unmarks in lesson order", () => {
    const d = doneSet(["rules-and-alarms"]);
    expect(withDone(d, "device-to-samples", true)).toEqual(["device-to-samples", "rules-and-alarms"]);
    expect(withDone(d, "rules-and-alarms", false)).toEqual([]);
  });
  it("finds a lesson by slug", () => {
    expect(lessonBySlug("acting-on-it")?.title).toBe("Acting on it");
    expect(lessonBySlug("nope")).toBeUndefined();
  });
});
