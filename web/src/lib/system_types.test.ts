import { describe, it, expect, vi, beforeEach } from "vitest";
import {
  listSystemTypes,
  createSystemType,
  updateSystemType,
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
        system_types: [{ id: uuidFor("st-room"), name: "room", label: "Room", official: true, stem: "room", icon: "door-open", abbrev: "rm" }],
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
      jsonResponse({ id: uuidFor("st-lab"), name: "lab", label: "Lab", official: false, parent: "room", parent_id: uuidFor("st-room") }, 201),
    );
    await createSystemType({ name: "lab", label: "Lab", parent_id: "room", abbrev: "lab" });
    const req = fetchMock.mock.calls[0][0] as Request;
    expect(req.method).toBe("POST");
    expect(req.url).toContain("/api/v1/system-types");
    expect(await req.json()).toMatchObject({ name: "lab", label: "Lab", parent_id: "room", abbrev: "lab" });
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
// used for systemTypeTree and flattenSystemTypeTree tests only.
const av: SystemType = { id: uuidFor("st-av"), name: "av", label: "AV", official: true, stem: "av", abbrev: "av", icon: "layers", resolved_icon: "layers" };
const room: SystemType = { id: uuidFor("st-room"), name: "room", label: "Room", official: true, parent: "av", parent_id: uuidFor("st-av"), stem: "room", abbrev: "rm", icon: "door-open", resolved_icon: "door-open" };
const board: SystemType = { id: uuidFor("st-board"), name: "board", label: "Boardroom", official: true, parent: "room", parent_id: uuidFor("st-room"), stem: "boardroom", abbrev: "br", resolved_icon: "door-open" };
const huddle: SystemType = { id: uuidFor("st-huddle"), name: "huddle", label: "Huddle Room", official: true, parent: "room", parent_id: uuidFor("st-room"), stem: "huddle", abbrev: "hud", resolved_icon: "door-open" };
const sign: SystemType = { id: uuidFor("st-sign"), name: "sign", label: "Signage", official: true, parent: "av", parent_id: uuidFor("st-av"), stem: "signage", abbrev: "sgn", icon: "tv", resolved_icon: "tv" };
const videoWall: SystemType = { id: uuidFor("st-video-wall"), name: "video-wall", label: "Video Wall", official: true, parent: "sign", parent_id: uuidFor("st-sign"), stem: "video-wall", abbrev: "vw", resolved_icon: "tv" };
const interactiveSign: SystemType = { id: uuidFor("st-interactive-sign"), name: "interactive-sign", label: "Interactive Sign", official: true, parent: "sign", parent_id: uuidFor("st-sign"), stem: "interactive-sign", abbrev: "isgn", icon: "touchpad", resolved_icon: "touchpad" };

const shipped: SystemType[] = [av, room, board, huddle, sign, videoWall, interactiveSign];

describe("systemTypeTree / flattenSystemTypeTree", () => {
  it("groups by parent_id, sorted by label at each level", () => {
    const roots = systemTypeTree(shipped);
    expect(roots.map((r) => r.name)).toEqual(["av"]);
    const avRoot = roots[0];
    // Alphabetical by label: Room, Signage.
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
