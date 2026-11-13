import { describe, expect, it } from "vitest";
import {
  labelSize,
  layoutMarkers,
  mapMarkers,
  markerAnchor,
  parseStandardMap,
  rectsOverlap,
  type LabelSpec,
  type Rect,
  type Size,
} from "./system_map";
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

// Where a marker hangs off its point (#1074, #1072): a marker whose label would
// cross a side or the bottom of the box anchors inward, so its icon and the
// label stay inside the map box. The decision is in pixels, from the rendered
// box and the label's own size, so a phone-width map decides differently from
// a desktop one for the same normalized position.
describe("markerAnchor", () => {
  const label: Size = { w: 112, h: 28 };

  it("centres a marker in the open and anchors one near an edge inward", () => {
    const box = { w: 672, h: 448 };
    expect(markerAnchor(0.5, 0.5, box, label)).toEqual({ x: "center", y: "below" });
    expect(markerAnchor(0.02, 0.5, box, label).x).toBe("start");
    expect(markerAnchor(0.97, 0.5, box, label).x).toBe("end");
    expect(markerAnchor(0.5, 0.95, box, label).y).toBe("above");
  });

  it("anchors the same position by the box it renders in: start at 320 px, centred at 1200 px", () => {
    expect(markerAnchor(0.2, 0.5, { w: 320, h: 213 }, label).x).toBe("start");
    expect(markerAnchor(0.2, 0.5, { w: 1200, h: 800 }, label).x).toBe("center");
    expect(markerAnchor(0.8, 0.5, { w: 320, h: 213 }, label).x).toBe("end");
    expect(markerAnchor(0.8, 0.5, { w: 1200, h: 800 }, label).x).toBe("center");
  });

  it("flips the label above when it would run past the bottom, by pixels not by a fraction", () => {
    // 0.8 of a 213 px box leaves 43 px under the point; a two-line label needs 52.
    expect(markerAnchor(0.5, 0.8, { w: 320, h: 213 }, label).y).toBe("above");
    expect(markerAnchor(0.5, 0.8, { w: 1200, h: 800 }, label).y).toBe("below");
  });

  it("a narrow label needs less room than a wide one", () => {
    const box = { w: 320, h: 213 };
    expect(markerAnchor(0.2, 0.5, box, { w: 112, h: 28 }).x).toBe("start");
    expect(markerAnchor(0.2, 0.5, box, { w: 30, h: 14 }).x).toBe("center");
  });
});

describe("labelSize", () => {
  it("is the text's width up to the 112 px cap, wrapping to a second line before it clamps", () => {
    expect(labelSize("Display")).toEqual({ w: 47, h: 14 });
    expect(labelSize("Podium Microphone \u00b7 mic-1")).toEqual({ w: 112, h: 28 });
    expect(labelSize("A".repeat(200))).toEqual({ w: 112, h: 28 });
  });
});

// The degraded scenario's Auditorium, in its standard's declared positions
// (internal/template/catalog/standards.yaml): the microphones sit 0.2 apart on
// a 1.8 aspect map, so at phone width their 112 px labels overprint.
const AUDITORIUM: LabelSpec[] = [
  { x: 0.5, y: 0.06, text: "Projection \u00b7 projector-1" },
  { x: 0.85, y: 0.12, text: "Confidence Display \u00b7 display-2" },
  { x: 0.2, y: 0.22, text: "Podium Microphone \u00b7 mic-1" },
  { x: 0.4, y: 0.2, text: "Stage Microphone 1 \u00b7 mic-3" },
  { x: 0.6, y: 0.2, text: "Stage Microphone 2 \u00b7 mic-4" },
  { x: 0.15, y: 0.4, text: "Speakers 1 \u00b7 spk-1" },
  { x: 0.85, y: 0.4, text: "Speakers 2 \u00b7 spk-2" },
  { x: 0.15, y: 0.75, text: "Speakers 3 \u00b7 spk-3" },
  { x: 0.85, y: 0.75, text: "Speakers 4 \u00b7 spk-4" },
  { x: 0.35, y: 0.94, text: "Camera 1 \u00b7 cam-1" },
  { x: 0.65, y: 0.94, text: "Camera 2 \u00b7 cam-2" },
  { x: 0.08, y: 0.3, text: "Touch Control \u00b7 panel-1" },
];

const inside = (r: Rect, box: Size) => r.x >= 0 && r.y >= 0 && r.x + r.w <= box.w && r.y + r.h <= box.h;

describe("layoutMarkers", () => {
  it("separates two markers 4 px apart: the earlier keeps its place, the later label gives way", () => {
    const box = { w: 672, h: 373 };
    const px = (n: number) => n / box.w;
    const out = layoutMarkers(
      [
        { x: px(300), y: 0.5, text: "Left Microphone" },
        { x: px(304), y: 0.5, text: "Right Microphone" },
      ],
      box,
    );
    expect(out[0].anchor).toEqual({ x: "center", y: "below" });
    expect(out[0].label).not.toBeNull();
    // The later label moved (here, above its icon) or collapsed; either way no overprint.
    expect(out[1].anchor.y === "above" || out[1].label === null).toBe(true);
    for (const [a, b] of [[0, 1], [1, 0]]) {
      if (out[a].label) {
        expect(rectsOverlap(out[a].label!, out[b].icon)).toBe(false);
        if (out[b].label) expect(rectsOverlap(out[a].label!, out[b].label!)).toBe(false);
      }
    }
  });

  it("collapses a label to its icon when no anchor is clear", () => {
    const box = { w: 672, h: 373 };
    // Three markers on one point: only so many places around an icon exist.
    const specs = Array.from({ length: 8 }, () => ({ x: 0.5, y: 0.5, text: "Wireless Microphone" }));
    const out = layoutMarkers(specs, box);
    expect(out.some((p) => p.label === null)).toBe(true);
    expect(out[0].label).not.toBeNull();
  });

  it("leaves markers that already clear each other exactly as markerAnchor placed them", () => {
    const box = { w: 1200, h: 667 };
    const out = layoutMarkers(AUDITORIUM, box);
    out.forEach((p, i) => {
      const s = AUDITORIUM[i];
      expect(p.label).not.toBeNull();
      expect(p.anchor).toEqual(markerAnchor(s.x, s.y, box, labelSize(s.text)));
    });
  });

  it.each([320, 480, 672, 1280])("the Auditorium at %i px: every label inside the box, none over another label or icon", (w) => {
    const box = { w, h: w / 1.8 };
    const out = layoutMarkers(AUDITORIUM, box);
    expect(out).toHaveLength(AUDITORIUM.length);
    out.forEach((p, i) => {
      if (!p.label) return;
      expect(inside(p.label, box), `label ${i} inside`).toBe(true);
      out.forEach((q, j) => {
        if (i === j) return;
        expect(rectsOverlap(p.label!, q.icon), `label ${i} over icon ${j}`).toBe(false);
        if (q.label) expect(rectsOverlap(p.label!, q.label), `label ${i} over label ${j}`).toBe(false);
      });
    });
  });

  it("keeps the labels it can at phone width and collapses only the rest", () => {
    const out = layoutMarkers(AUDITORIUM, { w: 320, h: 178 });
    const shown = out.filter((p) => p.label).length;
    // 12 markers in a 178 px box: a handful keep their label, the rest are icons with tooltips.
    expect(shown).toBeGreaterThanOrEqual(3);
    expect(shown).toBeLessThan(AUDITORIUM.length);
  });

  it("is deterministic: the same markers and box lay out identically", () => {
    const box = { w: 320, h: 178 };
    expect(layoutMarkers(AUDITORIUM, box)).toEqual(layoutMarkers(AUDITORIUM, box));
  });

  it("an empty map is an empty layout", () => {
    expect(layoutMarkers([], { w: 320, h: 178 })).toEqual([]);
  });
});
