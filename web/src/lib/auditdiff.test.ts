import { describe, it, expect } from "vitest";
import { diffFields, humanizeField, isTimestamp } from "./auditdiff";

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

  it("renders nested values as indented JSON, not [object Object]", () => {
    const rows = diffFields(null, { locks: { theme: true } });
    expect(rows[0].after).toBe('{\n  "theme": true\n}');
  });

  it("reads a jsonb column as the JSON it is, and compares it by value (#1088)", () => {
    const params = { target: "127.60.0.19:22", timeout: "5s" };
    const [row] = diffFields({ Params: params }, { Params: { ...params } });
    expect(row).toEqual({ key: "Params", before: '{\n  "target": "127.60.0.19:22",\n  "timeout": "5s"\n}', after: row.before, state: "same" });
  });

  it("collapses a non-object image to one synthetic value row", () => {
    expect(diffFields(null, ["a", "b"])).toEqual([{ key: "value", before: undefined, after: '[\n  "a",\n  "b"\n]', state: "added" }]);
  });

  it("returns nothing when the event recorded no images", () => {
    expect(diffFields(null, null)).toEqual([]);
  });
});

// Field names as the audit trail records them: Go struct names (PascalCase, ID
// suffixes) from the older writers and snake_case column names from the newer,
// both taken from the review lab's real audit rows (#2447).
describe("humanizeField", () => {
  const cases: [string, string][] = [
    ["LastOrphanedAt", "Last orphaned at"],
    ["LastHeartbeatAt", "Last heartbeat at"],
    ["OrphanedBatches", "Orphaned batches"],
    ["CreatedAt", "Created at"],
    ["ID", "ID"],
    ["ComponentID", "Component ID"],
    ["PlatformComponentID", "Platform component ID"],
    ["LocationTypeID", "Location type ID"],
    ["RaisedByRuleID", "Raised by rule ID"],
    ["UserID", "User ID"],
    ["DedupKey", "Dedup key"],
    ["OutOfBand", "Out of band"],
    ["PathSegments", "Path segments"],
    ["LabelGenerated", "Label generated"],
    ["owner_kind", "Owner kind"],
    ["owner_id", "Owner ID"],
    ["host_key", "Host key"],
    ["is_primary", "Is primary"],
    ["secret_type", "Secret type"],
    ["component", "Component"],
    ["value", "Value"],
    ["label", "Label"],
  ];
  for (const [raw, human] of cases) {
    it(`${raw} reads ${human}`, () => expect(humanizeField(raw)).toBe(human));
  }

  it("leaves a key with no letters or an empty key as it came", () => {
    expect(humanizeField("")).toBe("");
    expect(humanizeField("42")).toBe("42");
  });
});

describe("isTimestamp", () => {
  it("accepts the RFC 3339 instants the audit images carry and nothing looser", () => {
    expect(isTimestamp("2026-11-23T16:35:10.80497Z")).toBe(true);
    expect(isTimestamp("2026-11-23T16:35:10Z")).toBe(true);
    expect(isTimestamp("2026-11-23T16:35:10+02:00")).toBe(true);
    expect(isTimestamp("2026-11-23")).toBe(false);
    expect(isTimestamp("v2026-11-23T16:35:10Z")).toBe(false);
    expect(isTimestamp("01a2093c-33ec-79ea-9d4a-9a0b27a96650")).toBe(false);
  });
});
