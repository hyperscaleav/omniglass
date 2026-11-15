import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, within } from "@solidjs/testing-library";
import { Router, Route } from "@solidjs/router";
import SystemMap from "./SystemMap";
import { rectsOverlap, type MapMarker, type Rect, type StandardMapDecl } from "../lib/system_map";
import type { MemberContext } from "../lib/detail";

// The map's label fit (#1072): at any width no label runs past the map box
// and no two labels overprint. The Auditorium below is the degraded
// scenario's, in the positions the standard declares
// (internal/template/catalog/standards.yaml).

const decl: StandardMapDecl = { aspect: 1.8, positions: [] };

type Spec = [role: string, roleLabel: string, position: number, x: number, y: number, name?: string];
const SPECS: Spec[] = [
  ["projection", "Projection", 1, 0.5, 0.06, "projector-1"],
  ["confidence-display", "Confidence Display", 1, 0.85, 0.12, "display-2"],
  ["podium-mic", "Podium Microphone", 1, 0.2, 0.22, "mic-1"],
  ["stage-mic", "Stage Microphone", 1, 0.4, 0.2, "mic-3"],
  ["stage-mic", "Stage Microphone", 2, 0.6, 0.2, "mic-4"],
  ["speakers", "Speakers", 1, 0.15, 0.4, "spk-1"],
  ["speakers", "Speakers", 2, 0.85, 0.4, "spk-2"],
  ["speakers", "Speakers", 3, 0.15, 0.75],
  ["speakers", "Speakers", 4, 0.85, 0.75, "spk-4"],
  ["camera", "Camera", 1, 0.35, 0.94, "cam-1"],
  ["camera", "Camera", 2, 0.65, 0.94, "cam-2"],
  ["touch-control", "Touch Control", 1, 0.08, 0.3, "panel-1"],
];
const markers: MapMarker[] = SPECS.map(([role, roleLabel, position, x, y, name]) => ({
  x,
  y,
  role,
  roleLabel,
  position,
  occupant: name ? { name, componentId: `c-${role}-${position}`, down: false } : undefined,
}));

const member: MemberContext = {
  verdict: () => "healthy",
  alarm: () => undefined,
  product: () => "",
  label: (id, name) => `${SPECS.find(([r, , p]) => `c-${r}-${p}` === id)![1]} · ${name}`,
  icon: () => "",
};

// jsdom has no layout: give the map box a width the way a browser would, and
// the ResizeObserver a way to deliver it.
let boxWidth = 0;
beforeEach(() => {
  vi.stubGlobal(
    "ResizeObserver",
    class {
      constructor(private cb: ResizeObserverCallback) {}
      observe(el: Element) {
        this.cb([{ target: el, contentRect: { width: boxWidth } } as unknown as ResizeObserverEntry], this as unknown as ResizeObserver);
      }
      disconnect() {}
      unobserve() {}
    },
  );
  vi.spyOn(HTMLElement.prototype, "clientWidth", "get").mockImplementation(() => boxWidth);
});
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

function mount(width: number) {
  boxWidth = width;
  render(() => (
    <Router>
      <Route path="*" component={() => <SystemMap decl={decl} markers={markers} member={member} />} />
    </Router>
  ));
  return screen.getByTestId("system-map");
}

// The laid-out label box, in px from the map's top-left, as the marker renders it.
const labelBox = (el: HTMLElement): Rect | null => {
  const raw = el.querySelector("[data-maplabel]")?.getAttribute("data-label-box");
  if (!raw) return null;
  const [x, y, w, h] = raw.split(" ").map(Number);
  return { x, y, w, h };
};

describe("the system map's labels fit and do not collide (#1072)", () => {
  it.each([320, 1280])("at %i px: no label extends past the map box and no two labels intersect", (width) => {
    const map = mount(width);
    const height = width / decl.aspect;
    const els = within(map).getAllByTestId(/^mapmarker-/);
    expect(els).toHaveLength(markers.length);
    const boxes = els.map(labelBox).filter((b): b is Rect => b !== null);
    expect(boxes.length).toBeGreaterThan(0);
    for (const b of boxes) {
      expect(b.x).toBeGreaterThanOrEqual(0);
      expect(b.y).toBeGreaterThanOrEqual(0);
      expect(b.x + b.w).toBeLessThanOrEqual(width);
      expect(b.y + b.h).toBeLessThanOrEqual(height);
    }
    for (let i = 0; i < boxes.length; i++) for (let j = i + 1; j < boxes.length; j++) expect(rectsOverlap(boxes[i], boxes[j])).toBe(false);
  });

  it("a wide map keeps every label; a phone-width one collapses the labels that cannot fit to their icons", () => {
    const wide = within(mount(1280)).getAllByTestId(/^mapmarker-/);
    expect(wide.every((el) => el.querySelector("[data-maplabel]"))).toBe(true);
    cleanup();
    const narrow = within(mount(320)).getAllByTestId(/^mapmarker-/);
    const collapsed = narrow.filter((el) => !el.querySelector("[data-maplabel]"));
    expect(collapsed.length).toBeGreaterThan(0);
    expect(collapsed.length).toBeLessThan(narrow.length);
    // A collapsed marker is still named (for assistive tech) and still carries its tooltip trigger.
    for (const el of collapsed) {
      expect(el.getAttribute("aria-label")).toBeTruthy();
      expect(el.querySelector("[data-glyph]")).toBeTruthy();
    }
  });

  it("anchors the same marker by the box: a 0.2 position hangs inward on a phone and centres on a desktop", () => {
    const anchorOf = (w: number) => {
      const el = within(mount(w)).getByTestId("mapmarker-podium-mic-1");
      const cls = el.getAttribute("class")!;
      cleanup();
      return cls;
    };
    expect(anchorOf(320)).toContain("items-start");
    expect(anchorOf(1280)).toContain("items-center");
  });
});
