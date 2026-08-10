import { describe, it, expect, vi, beforeEach } from "vitest";
import {
  listSystemTypes,
  createSystemType,
  updateSystemType,
  systemTypeByName,
  resolveSystemTypeIcon,
  systemTypeTree,
  flattenSystemTypeTree,
  type SystemType,
} from "./system_types";
import { uuidFor } from "./testids";

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
}

describe("system_types data layer", () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  it("lists the registry off the typed literal path", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(
      jsonResponse({
        system_types: [{ id: uuidFor("st-room"), name: "room", display_name: "Room", official: true, stem: "room", icon: "door-open", abbrev: "rm" }],
      }),
    );
    const rows = await listSystemTypes();
    expect(rows).toHaveLength(1);
    expect(rows[0]).toMatchObject({ name: "room", stem: "room", icon: "door-open", abbrev: "rm" });
    const req = fetchMock.mock.calls[0][0] as Request;
    expect(req.url).toContain("/api/v1/system-types");
  });

  it("creates a custom type under a parent", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(
      jsonResponse({ id: uuidFor("st-lab"), name: "lab", display_name: "Lab", official: false, parent: "room", parent_id: uuidFor("st-room") }, 201),
    );
    await createSystemType({ name: "lab", display_name: "Lab", parent_id: "room", abbrev: "lab" });
    const req = fetchMock.mock.calls[0][0] as Request;
    expect(req.method).toBe("POST");
    expect(req.url).toContain("/api/v1/system-types");
    expect(await req.json()).toMatchObject({ name: "lab", display_name: "Lab", parent_id: "room", abbrev: "lab" });
  });

  it("updates a type's facts by id, never its parent", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(jsonResponse({}));
    await updateSystemType(uuidFor("st-room"), { abbrev: "rm2" });
    const req = fetchMock.mock.calls[0][0] as Request;
    expect(req.method).toBe("PATCH");
    expect(req.url).toContain(`/api/v1/system-types/${uuidFor("st-room")}`);
    expect(await req.json()).toEqual({ abbrev: "rm2" });
  });
});

// The shipped tree (internal/seed/system_types.yaml), three deep on purpose:
// `av` is the root, `room` overrides its icon MID-CHAIN, and `board` sets its
// own stem and abbrev while leaving icon to inherit. `sign` is the second
// branch, with a leaf (interactive-sign) that overrides icon at the bottom.
const av: SystemType = { id: uuidFor("st-av"), name: "av", display_name: "AV", official: true, stem: "av", abbrev: "av", icon: "layers" };
const room: SystemType = { id: uuidFor("st-room"), name: "room", display_name: "Room", official: true, parent: "av", parent_id: uuidFor("st-av"), stem: "room", abbrev: "rm", icon: "door-open" };
const board: SystemType = { id: uuidFor("st-board"), name: "board", display_name: "Boardroom", official: true, parent: "room", parent_id: uuidFor("st-room"), stem: "boardroom", abbrev: "br" };
const huddle: SystemType = { id: uuidFor("st-huddle"), name: "huddle", display_name: "Huddle Room", official: true, parent: "room", parent_id: uuidFor("st-room"), stem: "huddle", abbrev: "hud" };
const sign: SystemType = { id: uuidFor("st-sign"), name: "sign", display_name: "Signage", official: true, parent: "av", parent_id: uuidFor("st-av"), stem: "signage", abbrev: "sgn", icon: "tv" };
const videoWall: SystemType = { id: uuidFor("st-video-wall"), name: "video-wall", display_name: "Video Wall", official: true, parent: "sign", parent_id: uuidFor("st-sign"), stem: "video-wall", abbrev: "vw" };
const interactiveSign: SystemType = { id: uuidFor("st-interactive-sign"), name: "interactive-sign", display_name: "Interactive Sign", official: true, parent: "sign", parent_id: uuidFor("st-sign"), stem: "interactive-sign", abbrev: "isgn", icon: "touchpad" };

const shipped: SystemType[] = [av, room, board, huddle, sign, videoWall, interactiveSign];

describe("resolveSystemTypeIcon", () => {
  it("returns a node's own icon directly", () => {
    const byName = systemTypeByName(shipped);
    expect(resolveSystemTypeIcon("av", byName)).toBe("layers");
    expect(resolveSystemTypeIcon("interactive-sign", byName)).toBe("touchpad");
  });

  it("stops at the NEAREST ancestor that sets one, not the root", () => {
    const byName = systemTypeByName(shipped);
    // board leaves icon blank; room overrides av mid-chain, so room wins.
    expect(resolveSystemTypeIcon("board", byName)).toBe("door-open");
    expect(resolveSystemTypeIcon("board", byName)).not.toBe("layers");
    expect(resolveSystemTypeIcon("video-wall", byName)).toBe("tv");
    expect(resolveSystemTypeIcon("video-wall", byName)).not.toBe("layers");
  });

  it("falls back to map-pin for a name outside the registry", () => {
    const byName = systemTypeByName(shipped);
    expect(resolveSystemTypeIcon("no-such-type", byName)).toBe("map-pin");
    expect(resolveSystemTypeIcon(undefined, byName)).toBe("map-pin");
  });
});

describe("systemTypeTree / flattenSystemTypeTree", () => {
  it("groups by parent_id, sorted by display name at each level", () => {
    const roots = systemTypeTree(shipped);
    expect(roots.map((r) => r.name)).toEqual(["av"]);
    const avRoot = roots[0];
    // Alphabetical by display name: Room, Signage.
    expect(avRoot.children.map((c) => c.name)).toEqual(["room", "sign"]);
    expect(avRoot.depth).toBe(0);
    expect(avRoot.children[0].depth).toBe(1);
    expect(avRoot.children[0].children[0].depth).toBe(2);
  });

  it("flattens depth-first, a parent immediately followed by its children", () => {
    const flat = flattenSystemTypeTree(systemTypeTree(shipped));
    expect(flat.map((n) => n.name)).toEqual([
      "av",
      "room", "board", "huddle",
      "sign", "interactive-sign", "video-wall",
    ]);
    expect(flat.map((n) => n.depth)).toEqual([0, 1, 2, 2, 1, 2, 2]);
  });
});
