import { describe, expect, it } from "vitest";
import { canHoldChildren, typeRanks } from "./location_type_graph";
import { ROOT_PLACEMENT } from "./location_types";

// Location types are customer data (ADR-0102): a fleet may run campus, building,
// floor, room, or plot, sector, coordinate, or anything else. Everything the
// console decides about a type's place in the hierarchy is read off the
// registry's own allowed_parent_types, never off a type's name.

const t = (name: string, parents: string[]) => ({ name, allowed_parent_types: parents });

// A hierarchy that shares no word with the shipped defaults.
const agri = [
  t("plot", [ROOT_PLACEMENT]),
  t("sector", ["plot"]),
  t("coordinate", ["sector"]),
];

describe("typeRanks", () => {
  it("ranks a hierarchy by how far each type sits from the top, whatever it is called", () => {
    const d = typeRanks(agri);
    expect(d.get("plot")).toBe(0);
    expect(d.get("sector")).toBe(1);
    expect(d.get("coordinate")).toBe(2);
  });

  it("ranks a type after every type it may sit under, so a looser placement does not lift it", () => {
    // The shipped registry, as seeded: a building may sit at the root, a room
    // under a campus. Ranking by the shortest road would put buildings level
    // with campuses and rooms level with floors.
    const d = typeRanks([
      t("campus", [ROOT_PLACEMENT]),
      t("building", [ROOT_PLACEMENT, "campus"]),
      t("floor", ["building", "campus"]),
      t("room", ["floor", "building", "campus"]),
    ]);
    expect([d.get("campus"), d.get("building"), d.get("floor"), d.get("room")]).toEqual([0, 1, 2, 3]);
  });

  it("puts a type that may sit anywhere at the top", () => {
    expect(typeRanks([t("anywhere", [])]).get("anywhere")).toBe(0);
  });

  it("leaves a type no road reaches unranked rather than guessing, and survives a cycle", () => {
    const d = typeRanks([t("a", ["b"]), t("b", ["a"]), t("top", [ROOT_PLACEMENT])]);
    expect(d.get("top")).toBe(0);
    expect(d.has("a")).toBe(false);
    expect(d.has("b")).toBe(false);
  });
});

describe("canHoldChildren", () => {
  it("is true for a type some other type may sit under, and false for the bottom of the hierarchy", () => {
    expect(canHoldChildren(agri, "sector")).toBe(true);
    expect(canHoldChildren(agri, "coordinate")).toBe(false);
  });

  it("does not care what the type is called: a configured room may hold desks", () => {
    expect(canHoldChildren([t("room", [ROOT_PLACEMENT]), t("desk", ["room"])], "room")).toBe(true);
  });

  it("is true for every type when some type may sit anywhere", () => {
    expect(canHoldChildren([t("leafish", [ROOT_PLACEMENT]), t("free", [])], "leafish")).toBe(true);
  });
});
