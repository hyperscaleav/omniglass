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
// `av` is the root, `room` overrides its icon MID-CHAIN, and `board` sets its
// own stem and abbrev while leaving icon to inherit. `sign` is the second
// branch, with a leaf (interactive-sign) that overrides icon at the bottom.
// resolved_icon is what the SERVER sends per row (#695): the row's own icon
// where it has one, its nearest ancestor's where it does not. It is written out
// per row here rather than computed, because computing it in the fixture would
// reintroduce the very walk this stopped being the console's job.
const av: SystemType = { id: uuidFor("st-av"), name: "av", label: "AV", official: true, stem: "av", abbrev: "av", icon: "layers", resolved_icon: "layers" };
const room: SystemType = { id: uuidFor("st-room"), name: "room", label: "Room", official: true, parent: "av", parent_id: uuidFor("st-av"), stem: "room", abbrev: "rm", icon: "door-open", resolved_icon: "door-open" };
const board: SystemType = { id: uuidFor("st-board"), name: "board", label: "Boardroom", official: true, parent: "room", parent_id: uuidFor("st-room"), stem: "boardroom", abbrev: "br", resolved_icon: "door-open" };
const huddle: SystemType = { id: uuidFor("st-huddle"), name: "huddle", label: "Huddle Room", official: true, parent: "room", parent_id: uuidFor("st-room"), stem: "huddle", abbrev: "hud", resolved_icon: "door-open" };
const sign: SystemType = { id: uuidFor("st-sign"), name: "sign", label: "Signage", official: true, parent: "av", parent_id: uuidFor("st-av"), stem: "signage", abbrev: "sgn", icon: "tv", resolved_icon: "tv" };
const videoWall: SystemType = { id: uuidFor("st-video-wall"), name: "video-wall", label: "Video Wall", official: true, parent: "sign", parent_id: uuidFor("st-sign"), stem: "video-wall", abbrev: "vw", resolved_icon: "tv" };
const interactiveSign: SystemType = { id: uuidFor("st-interactive-sign"), name: "interactive-sign", label: "Interactive Sign", official: true, parent: "sign", parent_id: uuidFor("st-sign"), stem: "interactive-sign", abbrev: "isgn", icon: "touchpad", resolved_icon: "touchpad" };

const shipped: SystemType[] = [av, room, board, huddle, sign, videoWall, interactiveSign];

describe("resolveSystemTypeIcon", () => {
  it("returns the icon the server resolved for a node that sets one", () => {
    const byName = systemTypeByName(shipped);
    expect(resolveSystemTypeIcon("av", byName)).toBe("layers");
    expect(resolveSystemTypeIcon("interactive-sign", byName)).toBe("touchpad");
  });

  it("shows the inherited icon the server sent for a node that sets none", () => {
    const byName = systemTypeByName(shipped);
    // board leaves icon blank; room overrides av mid-chain, so room wins, and
    // the server is the one that decided that.
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

  // The point of #695: the console reads the ANSWER, it does not recompute it.
  // This row's served icon disagrees with everything its chain says, and the
  // console shows what it was sent. A client-side walk fails this test, which
  // is the only way to tell the two implementations apart from out here.
  it("shows what the server sent even where a chain walk would answer otherwise", () => {
    const odd: SystemType = { ...board, name: "odd", resolved_icon: "sparkles" };
    const byName = systemTypeByName([...shipped, odd]);
    expect(resolveSystemTypeIcon("odd", byName)).toBe("sparkles");
  });

  // A chain with no icon anywhere resolves to nothing on the wire, and the
  // fallback glyph is the console's to supply.
  it("falls back to map-pin when the server resolved no icon at all", () => {
    const bare: SystemType = { ...board, name: "bare", resolved_icon: undefined };
    const byName = systemTypeByName([...shipped, bare]);
    expect(resolveSystemTypeIcon("bare", byName)).toBe("map-pin");
  });
});

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
