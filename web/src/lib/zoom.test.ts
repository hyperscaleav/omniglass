import { describe, expect, it } from "vitest";
import { zoomChips } from "./zoom";
import type { FleetView } from "./fleet";
import { uuidFor } from "./testids";

// The zoom ladder resolved from an explicit anchor through the ancestor chain,
// never from a stored location id (#633 acceptance): arriving deep by URL must
// leave every chip live, which only holds if the chip derives everything it
// shows from the chain the view itself carries.

const loc = (handle: string, name: string, label: string, type: string, parent?: string) => ({
  id: uuidFor(handle),
  name,
  label,
  location_type: type,
  location_type_id: uuidFor(`type-${type}`),
  parent: parent ? uuidFor(parent) : "",
  verdict: "healthy",
});

const view: FleetView = {
  locations: [
    loc("z-hq", "hq", "Headquarters", "campus"),
    loc("z-b1", "b1", "", "building", "z-hq"),
    loc("z-room", "boardroom-a", "Boardroom A", "room", "z-b1"),
    loc("z-depot", "depot", "Service Depot", "building"),
  ],
  systems: [
    {
      id: uuidFor("z-s-board"),
      name: "boardroom",
      label: "Boardroom",
      location: uuidFor("z-room"),
      verdict: "degraded",
      dots: [{ component: uuidFor("z-c-bar"), name: "videobar-1", verdict: "degraded", primary: true, shared: false }],
    },
  ],
} as unknown as FleetView;

describe("zoomChips", () => {
  it("at the fleet anchor the fleet chip is active and counts roots; the deeper chips are disabled with no count", () => {
    const chips = zoomChips("fleet", {}, view);
    expect(chips.map((c) => c.id)).toEqual(["fleet", "location", "system", "component"]);
    expect(chips[0]).toMatchObject({ label: "Fleet", active: true, enabled: true, count: "2" });
    for (const chip of chips.slice(1)) {
      expect(chip.active).toBe(false);
      expect(chip.enabled).toBe(false);
      expect(chip.count).toBe("");
    }
  });

  it("at a deep anchor every chip is live, and the location chip resolves its label through the ancestor chain", () => {
    const chips = zoomChips(
      "component",
      { locationId: uuidFor("z-room"), systemId: uuidFor("z-s-board"), componentId: uuidFor("z-c-bar") },
      view,
    );
    expect(chips.every((c) => c.enabled)).toBe(true);
    expect(chips[3].active).toBe(true);
    // The label is the anchored location's own label read from the chain, and
    // the count is the chain's depth: campus, building, room.
    expect(chips[1].label).toBe("Boardroom A");
    expect(chips[1].count).toBe("3");
    // The system chip renders the system's label, from the view.
    expect(chips[2].label).toBe("Boardroom");
    // Nothing about a chip is the raw uuid: the ladder renders words.
    for (const chip of chips) expect(chip.label).not.toMatch(/^00000000-/);
  });

  it("an anchor naming a location the view does not carry disables the location chip rather than inventing a label", () => {
    const chips = zoomChips("location", { locationId: uuidFor("z-elsewhere") }, view);
    expect(chips[1].enabled).toBe(false);
    expect(chips[1].label).toBe("Location");
  });
});
