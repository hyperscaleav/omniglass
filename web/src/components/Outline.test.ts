import { describe, expect, it } from "vitest";
import { rowSubject } from "./Outline";
import type { LocationNode } from "../lib/outline";
import { uuidFor } from "../lib/testids";

// The outline's subject stamp (#2122): each row is handed to omnibot by name,
// a location by its own (the deepest of a folded chain), a system or a
// component by theirs; the unplaced row is no place to hand over.

const node = (chain: { id: string; name: string }[]) =>
  ({ id: chain[chain.length - 1].id, chain: chain.map((c) => ({ ...c, label: c.name.toUpperCase(), typeName: "room" })) }) as unknown as LocationNode;

describe("rowSubject", () => {
  it("is a location by the deepest name of its chain", () => {
    const n = node([{ id: uuidFor("hq"), name: "hq" }, { id: uuidFor("hq.west"), name: "hq.west" }]);
    expect(rowSubject({ kind: "location", key: "2", depth: 0, node: n, parent: null })).toEqual({ kind: "location", ref: "hq.west" });
  });

  it("is a system or a component by its name", () => {
    const n = node([{ id: uuidFor("hq"), name: "hq" }]);
    const group = { system: { id: uuidFor("hq.boardroom"), name: "hq.boardroom", label: "Boardroom", standard: "", health: null }, components: [] };
    expect(rowSubject({ kind: "group", key: "1:s", depth: 1, group, location: n, parent: "1" })).toEqual({ kind: "system", ref: "hq.boardroom" });
    const row = { id: uuidFor("hq.boardroom.display-1"), name: "hq.boardroom.display-1", label: "Display", product: "", icon: "", health: null, also: [] };
    expect(rowSubject({ kind: "component", key: "1:s:c", depth: 2, row, location: n, parent: "1:s" } as never)).toEqual({ kind: "component", ref: "hq.boardroom.display-1" });
  });

  it("is nothing for the unplaced row", () => {
    const n = node([{ id: uuidFor("unplaced"), name: "" }]);
    expect(rowSubject({ kind: "location", key: "unplaced", depth: 0, node: n, parent: null })).toBeUndefined();
  });
});
