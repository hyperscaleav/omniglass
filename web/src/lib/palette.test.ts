import { describe, it, expect } from "vitest";
import { askHit, displayLabel, entityDetail, fleetHref, layout, pageHit, readRecent, remember, searchHits, sections, type Hit } from "./palette";

const memory = (): Storage => {
  const m = new Map<string, string>();
  return { getItem: (k) => m.get(k) ?? null, setItem: (k, v) => void m.set(k, v), removeItem: (k) => void m.delete(k), clear: () => m.clear(), key: () => null, length: 0 } as Storage;
};

describe("search hits", () => {
  const hits = searchHits({
    locations: [{ id: "l1", name: "auditorium", place: "hq" }],
    systems: [],
    components: [
      { id: "c1", name: "panel-1", place: "hq / auditorium", system: "main-av" },
      { id: "c2", name: "panel-1", place: "hq / boardroom" },
    ],
    catalog: [
      { kind: "driver", name: "boreal-edge-snmp", label: "Boreal Edge SNMP", detail: "1.0.0" },
      { kind: "unheard-of", name: "x" },
    ],
  });

  it("opens a fleet entity at its page and tells same-named components apart by place", () => {
    const panels = hits.filter((h) => h.label === "panel-1");
    expect(panels.map((h) => h.detail)).toEqual(["hq / auditorium · in main-av", "hq / boardroom"]);
    expect(panels.map((h) => h.command?.path)).toEqual(["/components/c1", "/components/c2"]);
    expect(fleetHref("locations", "a/b")).toBe("/locations/a%2Fb");
  });

  it("opens a catalog entry at its drawer address and drops a kind the console has no drawer for", () => {
    const d = hits.find((h) => h.group === "Catalog")!;
    expect(d.command?.entity).toEqual({ kind: "driver", address: "boreal-edge-snmp" });
    expect(d.detail).toBe("Driver · 1.0.0");
    expect(hits.filter((h) => h.group === "Catalog")).toHaveLength(1);
  });

  it("has no detail when the entity has no place", () => {
    expect(entityDetail({ id: "x", name: "x" })).toBeUndefined();
  });
});

describe("display label", () => {
  it("reads the label alone when it only spells the name, and both when they differ", () => {
    expect(displayLabel("videobar-1", "Video Bar 1")).toBe("Video Bar 1");
    expect(displayLabel("amp-2", undefined)).toBe("amp-2");
    expect(displayLabel("hq-r1", "Boardroom")).toBe("Boardroom (hq-r1)");
  });
});

describe("layout", () => {
  const page = pageHit({ label: "Alarms", path: "/alarms", group: "Operate" });
  const comp: Hit = { value: "components:c1", label: "panel-1", group: "Components", command: { label: "panel-1", path: "/components/c1" } };
  const recent: Hit = { value: "recent:/x", label: "x", group: "Recent", command: { label: "x", path: "/x" } };
  const base = { pages: [page], found: [comp], recent: [recent], canAsk: true, searching: false };

  it("orders groups Pages, then what was found, and ends with Ask omnibot, never first when anything matched", () => {
    const rows = layout({ ...base, query: "pan" });
    expect(rows.map((r) => r.group)).toEqual(["Pages", "Components", "Actions"]);
    expect(rows[0].ask).toBeUndefined();
    expect(rows.at(-1)).toEqual(askHit("pan"));
  });

  it("is the Ask row alone when nothing matched", () => {
    expect(layout({ ...base, pages: [], found: [], query: "zzz" })).toEqual([askHit("zzz")]);
  });

  it("shows recents then every page before anything is typed", () => {
    expect(layout({ ...base, query: "" }).map((r) => r.group)).toEqual(["Recent", "Pages", "Actions"]);
  });

  it("holds the Ask row back while a typed search is out, but not an empty query", () => {
    expect(layout({ ...base, found: [], pages: [], query: "pan", searching: true })).toEqual([]);
    expect(layout({ ...base, query: "", searching: true }).at(-1)?.group).toBe("Actions");
  });

  it("leaves Ask out for someone who may not run omnibot", () => {
    expect(layout({ ...base, query: "pan", canAsk: false }).some((r) => r.group === "Actions")).toBe(false);
  });

  it("folds ordered rows into groups", () => {
    expect(sections(layout({ ...base, query: "pan" })).map((s) => [s.label, s.options.length])).toEqual([["Pages", 1], ["Components", 1], ["Actions", 1]]);
  });
});

describe("recent items", () => {
  const hit: Hit = { value: "v", label: "panel-1", detail: "hq", group: "Components" };

  it("keeps the latest first, once each, capped, per person", () => {
    const s = memory();
    for (let i = 0; i < 8; i++) remember("u1", { ...hit, label: `n${i}` }, `/components/${i}`, s);
    remember("u1", { ...hit, label: "n3" }, "/components/3", s);
    expect(readRecent("u1", s).map((h) => h.command?.path)).toEqual(["/components/3", "/components/7", "/components/6", "/components/5", "/components/4"]);
    expect(readRecent("u2", s)).toEqual([]);
  });

  it("does not remember the ask row, and survives a broken store", () => {
    const s = memory();
    remember("u1", askHit("hi"), "", s);
    expect(readRecent("u1", s)).toEqual([]);
    s.setItem("og-palette-recent:u1", "{not json");
    expect(readRecent("u1", s)).toEqual([]);
  });
});
