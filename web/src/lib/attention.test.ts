import { describe, expect, it } from "vitest";
import { ATTENTION_CHIP, attentionCount, attentionParts, attentionPhrase, fleetAttentionRows, type CountRow } from "./attention";
import { buildPredicate, type FilterKey } from "./predicate";
import { buildOutline, entriesOf } from "./outline";
import { healthOf } from "./health";
import { type FleetView } from "./fleet";
import { uuidFor } from "./testids";

// The need-attention count (#2438): one pure function over rows carrying their
// verdicts as data, so the chip's number, the chip's filter and Home's card
// cannot disagree. The fixture is the seeded fleet's shape: a healthy room, a
// degraded system whose components are mixed, an incomplete one, a shared
// component, a place holding two systems.

const row = (kind: CountRow["kind"], health: CountRow["health"]): CountRow => ({ kind, health });

describe("attentionCount", () => {
  it("counts systems and components that are outage, degraded or incomplete, never healthy or unknown", () => {
    const c = attentionCount([
      row("system", "degraded"), row("system", "incomplete"), row("system", "healthy"), row("system", null),
      row("component", "outage"), row("component", "degraded"), row("component", "incomplete"), row("component", "healthy"), row("component", null),
    ]);
    expect(c).toEqual({ systems: 2, components: 3, total: 5 });
  });

  it("counts a location wearing one system as that system", () => {
    expect(attentionCount([row("location", "outage"), row("location", null)])).toEqual({ systems: 1, components: 0, total: 1 });
  });

  it("reads verdicts as given: a system degraded by a rule the console does not know still counts", () => {
    expect(attentionCount([row("system", "degraded")]).systems).toBe(1);
  });
});

describe("attentionPhrase", () => {
  it("names what it counts, in the plural and the singular", () => {
    expect(attentionPhrase({ systems: 2, components: 3, total: 5 })).toBe("2 systems, 3 components need attention");
    expect(attentionPhrase({ systems: 1, components: 0, total: 1 })).toBe("1 system needs attention");
    expect(attentionPhrase({ systems: 0, components: 1, total: 1 })).toBe("1 component needs attention");
    expect(attentionPhrase({ systems: 0, components: 0, total: 0 })).toBe("Nothing needs attention");
  });

  it("splits into number and noun parts for a renderer that sets the numbers apart", () => {
    expect(attentionParts({ systems: 2, components: 1, total: 3 })).toEqual([{ n: 2, noun: "systems" }, { n: 1, noun: "component" }]);
  });
});

describe("the chip's number is the number of rows its filter leaves", () => {
  const loc = (h: string, label: string, type: string, parent = "") => ({
    id: uuidFor(h), name: h, label, location_type: type, location_type_id: uuidFor(`t-${type}`), parent: parent ? uuidFor(parent) : undefined, health: "healthy",
  });
  const dot = (h: string, health = "healthy", shared = false) => ({ component: uuidFor(h), name: h, health, primary: true, shared });
  const sys = (h: string, label: string, at: string, health: string, dots: ReturnType<typeof dot>[] = []) => ({
    id: uuidFor(h), name: h, label, location: at ? uuidFor(at) : undefined, health, dots,
  });
  const view = {
    locations: [loc("hq", "Headquarters", "campus"), loc("br", "Briefing Room", "room", "hq"), loc("lab", "Lab", "room", "hq"), loc("ok", "Quiet", "room", "hq")],
    systems: [
      sys("s-br", "Briefing Room", "br", "outage", [dot("disp-1", "outage"), dot("dsp-1", "degraded"), dot("tp-1"), dot("shared-mic", "incomplete", true)]),
      // two systems in one place: the place is no system, each system is
      sys("s-a", "Lab A", "lab", "degraded", [dot("shared-mic", "incomplete", true), dot("cam-1")]),
      sys("s-b", "Lab B", "lab", "healthy", [dot("cam-2")]),
      sys("s-q", "Quiet", "ok", "healthy", [dot("q-1")]),
      sys("s-nowhere", "Nowhere AV", "", "incomplete"),
    ],
  } as unknown as FleetView;
  const input = {
    view, components: [], systems: [], types: [{ name: "campus", label: "Campus", icon: "" }, { name: "room", label: "Room", icon: "" }],
    standardLabel: (h: string) => h, productLabel: (h: string) => h,
  };
  const keys: FilterKey<ReturnType<typeof entriesOf>[number]>[] = [
    { key: "name", type: "string", hint: "substring", get: (e) => e.search },
    { key: "health", type: "string", get: (e) => e.health ?? "" },
  ];

  it("equals the rows the attention chip leaves, by construction", () => {
    const entries = entriesOf(buildOutline(input));
    const shown = entries.filter(buildPredicate(keys, [ATTENTION_CHIP]));
    const c = attentionCount(shown);
    expect(c.total).toBe(shown.length);
    // Briefing Room, Lab A, Nowhere AV; the Briefing Room's three, the shared one once.
    expect(c).toEqual({ systems: 3, components: 3, total: 6 });
    expect(shown.filter((e) => e.kind === "component").map((e) => e.label).sort()).toEqual(["disp-1", "dsp-1", "shared-mic"]);
  });

  it("agrees with Home, which counts the fleet view directly", () => {
    const entries = entriesOf(buildOutline(input));
    const explore = attentionCount(entries.filter(buildPredicate(keys, [ATTENTION_CHIP])));
    expect(attentionCount(fleetAttentionRows(view))).toEqual(explore);
  });

  it("takes the fleet's verdict as the server sent it", () => {
    expect(fleetAttentionRows(view).filter((r) => r.kind === "system").map((r) => r.health)).toContain(healthOf("outage"));
  });
});
