import { describe, expect, it } from "vitest";
import { columnIndex, requireRole, viewKey, type ViewResult } from "./views";

const result: ViewResult = {
  columns: [
    { name: "component", type: "string", role: "label" },
    { name: "state", type: "string", role: "value" },
    { name: "since", type: "time", role: "time" },
  ],
  rows: [
    ["disp-1", "up", "2026-08-03T12:00:00Z"],
    ["cam-1", "down", "2026-08-03T12:01:00Z"],
  ],
};

const mapping = { label: "component", value: "state", time: "since" };

describe("columnIndex", () => {
  it("resolves a column name to its cell position", () => {
    expect(columnIndex(result, "state")).toBe(1);
    expect(columnIndex(result, "since")).toBe(2);
  });

  it("returns -1 for a column the result does not carry", () => {
    expect(columnIndex(result, "nope")).toBe(-1);
  });
});

describe("requireRole", () => {
  it("resolves a mapped role to its cell position", () => {
    expect(requireRole(result, mapping, "value")).toBe(1);
    expect(requireRole(result, mapping, "label")).toBe(0);
  });

  it("throws NAMING the role and the column when the mapping points at an absent column", () => {
    // A field-mapping that survives a column rename must fail loudly: rendering
    // blank cells would look like "no data" and hide the contract break.
    const broken = { ...mapping, value: "verdict" };
    expect(() => requireRole(result, broken, "value")).toThrowError(/value.*verdict/);
  });

  it("throws when the role is not mapped at all", () => {
    expect(() => requireRole(result, mapping, "series")).toThrowError(/series/);
  });
});

describe("viewKey", () => {
  it("keys a view by name and params, so two params sets cache apart", () => {
    expect(viewKey("event-feed", ["owner=disp-1"])).not.toEqual(viewKey("event-feed", ["owner=cam-1"]));
    expect(viewKey("event-feed", [])).not.toEqual(viewKey("estate-counts", []));
  });

  it("keys the same request identically regardless of param order, so a re-render does not refetch", () => {
    expect(viewKey("sample-history", ["owner=a", "key=b"])).toEqual(viewKey("sample-history", ["key=b", "owner=a"]));
  });

  it("treats an absent params list as the empty one", () => {
    expect(viewKey("estate-counts")).toEqual(viewKey("estate-counts", []));
  });

  it("keys pages apart, so one page cannot overwrite another's cached rows", () => {
    expect(viewKey("event-feed", [], "tok-2")).not.toEqual(viewKey("event-feed", []));
    expect(viewKey("event-feed", [], "tok-2")).not.toEqual(viewKey("event-feed", [], "tok-3"));
  });
});
