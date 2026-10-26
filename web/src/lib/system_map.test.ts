import { describe, expect, it } from "vitest";
import { mapMarkers, parseStandardMap, markerAnchor } from "./system_map";
import type { SystemZoomVM } from "./system_zoom";

// The map join (#791): the standard declares where each role position sits;
// the system's staffing says who is there. A marker is the pair.
const decl = JSON.stringify({
  aspect: 1.5,
  positions: [
    { role: "main-display", position: 1, x: 0.5, y: 0.06 },
    { role: "room-mic", position: 1, x: 0.32, y: 0.52 },
    { role: "room-mic", position: 2, x: 0.68, y: 0.52 },
  ],
});

const vm = {
  unconditional: [
    {
      name: "room-mic", label: "Room Microphone", quorum: 2, satisfying: 1, short: 1, spare: 0,
      impact: "degraded", impaired: true, active: true, acceptedTypes: [], pinnedProducts: [],
      gap: "occupant-down",
      occupants: [
        { name: "mic-1", componentId: "c-mic", position: 1, down: true, sharedWith: [], positionLabel: "Left" },
      ],
    },
    {
      name: "main-display", label: "Main Display", quorum: 1, satisfying: 0, short: 1, spare: 0,
      impact: "outage", impaired: true, active: true, acceptedTypes: [], pinnedProducts: [],
      gap: "unstaffed", occupants: [],
    },
  ],
  choices: [],
  noRole: [],
} as unknown as SystemZoomVM;

describe("the system map", () => {
  it("parses a declaration and rejects garbage as null, never a throw", () => {
    expect(parseStandardMap(decl)!.aspect).toBe(1.5);
    expect(parseStandardMap(undefined)).toBeNull();
    expect(parseStandardMap("not json")).toBeNull();
    expect(parseStandardMap(JSON.stringify({ aspect: 0, positions: [] }))).toBeNull();
  });

  it("joins each declared position to its occupant, empty positions included", () => {
    const markers = mapMarkers(parseStandardMap(decl)!, vm);
    expect(markers).toHaveLength(3);
    const mic1 = markers.find((m) => m.roleLabel === "Room Microphone" && m.position === 1)!;
    expect(mic1.occupant).toMatchObject({ name: "mic-1", componentId: "c-mic", down: true });
    expect(mic1.positionLabel).toBe("Left");
    expect(mic1.x).toBe(0.32);
    // Nobody holds mic position 2 or the display: the marker renders hollow.
    expect(markers.filter((m) => !m.occupant)).toHaveLength(2);
  });

  it("ignores a declared position for a role the build in use does not carry", () => {
    const declGhost = JSON.stringify({ aspect: 1, positions: [{ role: "ghost-role", position: 1, x: 0.5, y: 0.5 }] });
    expect(mapMarkers(parseStandardMap(declGhost)!, vm)).toHaveLength(0);
  });
});

// Where a marker hangs off its point (#1074, toward #1072): a marker near a
// side or the bottom edge anchors inward, so its icon and the label under it
// stay inside the map box instead of spilling out of it.
describe("markerAnchor", () => {
  it("centres a marker in the open and anchors one near an edge inward", () => {
    expect(markerAnchor(0.5, 0.5)).toEqual({ x: "center", y: "below" });
    expect(markerAnchor(0.02, 0.5).x).toBe("start");
    expect(markerAnchor(0.97, 0.5).x).toBe("end");
    expect(markerAnchor(0.5, 0.95).y).toBe("above");
  });
});
