import { describe, it, expect, vi, beforeEach } from "vitest";
import {
  listComponentTypes,
  createComponentType,
  updateComponentType,
  componentTypeByName,
  resolveComponentTypeIcon,
  componentTypeTree,
  flattenComponentTypeTree,
  type ComponentType,
} from "./component_types";
import { uuidFor } from "./testids";

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
}

describe("component_types data layer", () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  it("lists the registry and normalizes a missing default_tags to empty", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(
      jsonResponse({
        component_types: [{ id: uuidFor("ct-display"), name: "display", display_name: "Display", official: true, stem: "display", icon: "monitor", abbrev: "fp" }],
      }),
    );
    const rows = await listComponentTypes();
    expect(rows).toHaveLength(1);
    expect(rows[0]).toMatchObject({ name: "display", stem: "display", icon: "monitor", abbrev: "fp", default_tags: [] });
    const req = fetchMock.mock.calls[0][0] as Request;
    expect(req.url).toContain("/api/v1/component-types");
  });

  it("creates a custom type under a parent via the typed literal path", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(
      jsonResponse({ id: uuidFor("ct-ceiling-mic"), name: "ceiling-mic", display_name: "Ceiling Mic", official: false, parent: "mic", parent_id: uuidFor("ct-mic") }, 201),
    );
    await createComponentType({ name: "ceiling-mic", display_name: "Ceiling Mic", parent_id: "mic" });
    const req = fetchMock.mock.calls[0][0] as Request;
    expect(req.method).toBe("POST");
    expect(req.url).toContain("/api/v1/component-types");
    const sent = await req.json();
    expect(sent).toMatchObject({ name: "ceiling-mic", display_name: "Ceiling Mic", parent_id: "mic" });
  });

  it("updates a type's facts by id, never its parent", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(jsonResponse({}));
    await updateComponentType(uuidFor("ct-mic"), { icon: "mic-2" });
    const req = fetchMock.mock.calls[0][0] as Request;
    expect(req.method).toBe("PATCH");
    expect(req.url).toContain(`/api/v1/component-types/${uuidFor("ct-mic")}`);
    const sent = await req.json();
    expect(sent).toEqual({ icon: "mic-2" });
  });
});

// The seeded litmus tree (internal/seed/component_types.yaml): mic is a root
// with its own stem/abbrev/icon, wireless-mic/ceiling-mic/boundary-mic are
// children that leave every fact blank (inherit). parent_id links by uuid
// (the canonical handle); parent carries the name for display and is what
// resolveComponentTypeIcon's ancestor walk actually follows.
// resolved_icon is what the SERVER sends per row (#695): the row's own icon
// where it has one, its nearest ancestor's where it does not. It is written out
// per row rather than computed, because computing it in the fixture would
// reintroduce the very walk this stopped being the console's job.
const mic: ComponentType = { id: uuidFor("ct-mic"), name: "mic", display_name: "Microphone", official: true, forked: false, stem: "mic", abbrev: "mic", icon: "mic", resolved_icon: "mic", default_tags: [] };
const wirelessMic: ComponentType = { id: uuidFor("ct-wireless-mic"), name: "wireless-mic", display_name: "Wireless Microphone", official: true, forked: false, parent: "mic", parent_id: uuidFor("ct-mic"), resolved_icon: "mic", default_tags: [] };
const ceilingMic: ComponentType = { id: uuidFor("ct-ceiling-mic"), name: "ceiling-mic", display_name: "Ceiling Microphone", official: true, forked: false, parent: "mic", parent_id: uuidFor("ct-mic"), resolved_icon: "mic", default_tags: [] };
const display: ComponentType = { id: uuidFor("ct-display"), name: "display", display_name: "Display", official: true, forked: false, stem: "display", abbrev: "fp", icon: "monitor", resolved_icon: "monitor", default_tags: [] };
const interactiveDisplay: ComponentType = { id: uuidFor("ct-interactive-display"), name: "interactive-display", display_name: "Interactive Display", official: true, forked: false, parent: "display", parent_id: uuidFor("ct-display"), resolved_icon: "monitor", default_tags: [] };
const genericDevice: ComponentType = { id: uuidFor("ct-generic-device"), name: "generic-device", display_name: "Generic Device", official: true, forked: false, stem: "device", abbrev: "dev", icon: "box", resolved_icon: "box", default_tags: [] };

const seededTree: ComponentType[] = [mic, wirelessMic, ceilingMic, display, interactiveDisplay, genericDevice];

describe("resolveComponentTypeIcon", () => {
  it("returns the icon the server resolved for a root that sets one", () => {
    const byName = componentTypeByName(seededTree);
    expect(resolveComponentTypeIcon("display", byName)).toBe("monitor");
    expect(resolveComponentTypeIcon("mic", byName)).toBe("mic");
  });

  it("shows the inherited icon the server sent for a child that leaves it blank", () => {
    const byName = componentTypeByName(seededTree);
    expect(resolveComponentTypeIcon("interactive-display", byName)).toBe("monitor");
    expect(resolveComponentTypeIcon("ceiling-mic", byName)).toBe("mic");
  });

  it("falls back to box for a name outside the registry", () => {
    const byName = componentTypeByName(seededTree);
    expect(resolveComponentTypeIcon("no-such-type", byName)).toBe("box");
    expect(resolveComponentTypeIcon(undefined, byName)).toBe("box");
  });

  // The point of #695: the console reads the ANSWER, it does not recompute it.
  // This row's served icon disagrees with everything its chain says, and a
  // client-side walk fails here, which is the only way to tell the two
  // implementations apart from out here.
  it("shows what the server sent even where a chain walk would answer otherwise", () => {
    const odd: ComponentType = { ...ceilingMic, name: "odd", resolved_icon: "sparkles" };
    const byName = componentTypeByName([...seededTree, odd]);
    expect(resolveComponentTypeIcon("odd", byName)).toBe("sparkles");
  });

  it("falls back to box when the server resolved no icon at all", () => {
    const bare: ComponentType = { ...ceilingMic, name: "bare", resolved_icon: undefined };
    const byName = componentTypeByName([...seededTree, bare]);
    expect(resolveComponentTypeIcon("bare", byName)).toBe("box");
  });
});

describe("componentTypeTree / flattenComponentTypeTree", () => {
  it("groups by parent_id, sorted by display name at each level, roots before children", () => {
    const roots = componentTypeTree(seededTree);
    // Alphabetical by display name: Display, Generic Device, Microphone.
    expect(roots.map((r) => r.name)).toEqual(["display", "generic-device", "mic"]);
    const displayRoot = roots.find((r) => r.name === "display")!;
    expect(displayRoot.children.map((c) => c.name)).toEqual(["interactive-display"]);
    expect(displayRoot.depth).toBe(0);
    expect(displayRoot.children[0].depth).toBe(1);
    const micRoot = roots.find((r) => r.name === "mic")!;
    // Alphabetical: Ceiling Microphone, Wireless Microphone.
    expect(micRoot.children.map((c) => c.name)).toEqual(["ceiling-mic", "wireless-mic"]);
  });

  it("flattens depth-first, a parent immediately followed by its children", () => {
    const flat = flattenComponentTypeTree(componentTypeTree(seededTree));
    expect(flat.map((n) => n.name)).toEqual([
      "display", "interactive-display",
      "generic-device",
      "mic", "ceiling-mic", "wireless-mic",
    ]);
    expect(flat.map((n) => n.depth)).toEqual([0, 1, 0, 0, 1, 1]);
  });
});
