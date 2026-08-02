import { describe, it, expect } from "vitest";
import { diffFields } from "./auditdiff";

// The audit drawer's diff classification (#473): pure rows over the old/new
// images, so the drawer's added/removed/changed styling has a spec.
describe("diffFields", () => {
  it("classifies changed, same, added, and removed fields across both images", () => {
    const rows = diffFields(
      { name: "probe-1", display: "Probe", retired: true },
      { name: "probe-1", display: "Probe One", owner: "ops" },
    );
    expect(rows).toEqual([
      { key: "display", before: "Probe", after: "Probe One", state: "changed" },
      { key: "name", before: "probe-1", after: "probe-1", state: "same" },
      { key: "owner", before: undefined, after: "ops", state: "added" },
      { key: "retired", before: "true", after: undefined, state: "removed" },
    ]);
  });

  it("a create (no old image) marks every field added; a delete (no new) removed", () => {
    expect(diffFields(null, { name: "x" })).toEqual([{ key: "name", before: undefined, after: "x", state: "added" }]);
    expect(diffFields({ name: "x" }, null)).toEqual([{ key: "name", before: "x", after: undefined, state: "removed" }]);
  });

  it("renders nested values as compact JSON, not [object Object]", () => {
    const rows = diffFields(null, { locks: { theme: true } });
    expect(rows[0].after).toBe('{"theme":true}');
  });

  it("collapses a non-object image to one synthetic value row", () => {
    expect(diffFields(null, ["a", "b"])).toEqual([{ key: "value", before: undefined, after: '["a","b"]', state: "added" }]);
  });

  it("returns nothing when the event recorded no images", () => {
    expect(diffFields(null, null)).toEqual([]);
  });
});
