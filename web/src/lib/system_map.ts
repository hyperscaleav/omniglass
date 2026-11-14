// The system map's pure core (#791, ADR-0128): the standard declares WHERE a
// role position sits (normalized 2D against an aspect); the system's staffing
// says WHO is there. A marker is the join, and an empty position is a marker
// too (a dashed ring): the map shows the room as built, gaps included.
import type { SlotVM, SystemZoomVM } from "./system_zoom";

export type StandardMapDecl = {
  aspect: number;
  positions: { role: string; position: number; x: number; y: number }[];
};

export type MapMarker = {
  x: number;
  y: number;
  role: string;
  roleLabel: string;
  position: number;
  positionLabel?: string;
  occupant?: { name: string; componentId: string; down: boolean };
};

// parseStandardMap reads the wire's raw map value. Anything malformed is
// null, never a throw: a bad declaration means "no map tab", not a broken
// page (the write side validates; this is the read side's tolerance).
export function parseStandardMap(raw: string | undefined | null): StandardMapDecl | null {
  if (!raw) return null;
  try {
    const m = JSON.parse(raw) as StandardMapDecl;
    if (!m || typeof m.aspect !== "number" || m.aspect <= 0 || !Array.isArray(m.positions)) return null;
    return m;
  } catch {
    return null;
  }
}

export function mapMarkers(decl: StandardMapDecl, vm: SystemZoomVM): MapMarker[] {
  const activeSlots: SlotVM[] = [
    ...vm.unconditional.filter((s) => s.active),
    ...vm.choices.flatMap((c) => c.alternates.flatMap((a) => a.roles.filter((r) => r.active))),
  ];
  const byRole = new Map(activeSlots.map((s) => [s.name, s]));
  const markers: MapMarker[] = [];
  for (const p of decl.positions) {
    const slot = byRole.get(p.role);
    // A position for a role the build in use does not carry stays off the
    // map: the declaration may cover both alternates of a choice.
    if (!slot) continue;
    const occ = slot.occupants.find((o) => o.position === p.position);
    markers.push({
      x: p.x,
      y: p.y,
      role: p.role,
      roleLabel: slot.label,
      position: p.position,
      positionLabel: occ?.positionLabel,
      occupant: occ ? { name: occ.name, componentId: occ.componentId, down: occ.down } : undefined,
    });
  }
  return markers;
}

// The marker's geometry in px, mirroring the classes components/SystemMap
// puts on it: an 18 px icon whose centre sits on the point, inside px-1 py-0.5
// padding, a 2 px gap, then the label (11 px type, 1.25 leading, at most 112 px
// wide and two lines). Layout is decided from these numbers, not from measured
// DOM, so it is one pure function that jsdom and a browser agree on.
const ICON = 18;
const PAD_X = 4;
const PAD_Y = 2;
const GAP = 2;
const LINE_H = 14;
const LABEL_MAX_W = 112;
const LABEL_MAX_LINES = 2;
// The label font is monospace at 11 px: 0.6 em per glyph.
const CHAR_W = 6.6;
// Clearance kept between a marker and the map's edge before it anchors inward.
const EDGE_GUTTER = 8;

export type Size = { w: number; h: number };
export type Rect = { x: number; y: number; w: number; h: number };

export function rectsOverlap(a: Rect, b: Rect): boolean {
  return a.x < b.x + b.w && b.x < a.x + a.w && a.y < b.y + b.h && b.y < a.y + a.h;
}

// labelSize estimates a label's rendered box from its text: as wide as the
// text up to the cap, wrapping to a second line before it clamps. A balanced
// two-line label renders narrower than this, never wider, so the estimate is
// the safe side for both edge and collision checks.
export function labelSize(text: string): Size {
  const natural = text.length * CHAR_W;
  const lines = Math.min(LABEL_MAX_LINES, Math.max(1, Math.ceil(natural / LABEL_MAX_W)));
  return { w: Math.min(LABEL_MAX_W, Math.ceil(natural)), h: lines * LINE_H };
}

// markerAnchor says how a marker hangs off its point (#1074, #1072). In the
// open it centres under the point, label below; where its label would come
// within a gutter of a side it anchors inward, and where the label would run
// past the bottom it rides above the icon, so the icon and its label stay
// inside the map box. Decided in pixels from the rendered box and the label's
// size: the same normalized position anchors differently on a phone and a
// desktop. Pure, so the rule is tested without layout.
export type MarkerAnchor = { x: "start" | "center" | "end"; y: "below" | "above" };
export function markerAnchor(x: number, y: number, box: Size, label: Size): MarkerAnchor {
  const px = x * box.w;
  const half = (Math.max(ICON, label.w) + 2 * PAD_X) / 2;
  const below = y * box.h - (PAD_Y + ICON / 2) + 2 * PAD_Y + ICON + GAP + label.h;
  return {
    x: px - half < EDGE_GUTTER ? "start" : px + half > box.w - EDGE_GUTTER ? "end" : "center",
    y: below > box.h - EDGE_GUTTER ? "above" : "below",
  };
}

// iconRect is the icon with its padding, centred on the marker's point.
function iconRect(x: number, y: number, box: Size): Rect {
  return { x: x * box.w - (PAD_X + ICON / 2), y: y * box.h - (PAD_Y + ICON / 2), w: ICON + 2 * PAD_X, h: ICON + 2 * PAD_Y };
}

// markerRects is the box a marker occupies at an anchor, and the label's own
// box inside it (null for an icon-only marker), in px from the map's top-left.
function markerRects(x: number, y: number, box: Size, anchor: MarkerAnchor, label: Size | null) {
  const px = x * box.w;
  const py = y * box.h;
  const iconHalf = PAD_X + ICON / 2;
  const w = Math.max(ICON, label?.w ?? 0) + 2 * PAD_X;
  const h = 2 * PAD_Y + ICON + (label ? GAP + label.h : 0);
  const left = anchor.x === "start" ? px - iconHalf : anchor.x === "end" ? px + iconHalf - w : px - w / 2;
  const top = anchor.y === "above" ? py + (PAD_Y + ICON / 2) - h : py - (PAD_Y + ICON / 2);
  const marker: Rect = { x: left, y: top, w, h };
  if (!label) return { marker, label: null };
  const lx = anchor.x === "start" ? px - ICON / 2 : anchor.x === "end" ? px + ICON / 2 - label.w : px - label.w / 2;
  const ly = anchor.y === "above" ? top + PAD_Y : top + PAD_Y + ICON + GAP;
  return { marker, label: { x: lx, y: ly, w: label.w, h: label.h } };
}

export type LabelSpec = { x: number; y: number; text: string };
// A placed marker: where it anchors, its box, its icon's box (fixed on the
// point whatever the anchor), and its label's box. A null
// label means the label collapsed and only the icon shows (its text stays in
// the marker's tooltip and accessible name).
export type MarkerPlacement = { anchor: MarkerAnchor; marker: Rect; icon: Rect; label: Rect | null };

const X_ANCHORS = ["center", "start", "end"] as const;
const inBox = (r: Rect, box: Size) => r.x >= 0 && r.y >= 0 && r.x + r.w <= box.w && r.y + r.h <= box.h;

// layoutMarkers resolves label collisions over the whole marker list,
// deterministically and in declaration order: each marker takes its
// markerAnchor placement if that keeps its label inside the box and clear of
// the other markers' icons and the labels already placed (an icon is on its
// declared point; only the label moves); otherwise the next anchor in a fixed order (the
// label's other side first, then the other horizontal anchors); and if every anchor
// collides, the label collapses to its icon, the one fallback that cannot
// overprint. An earlier marker never moves for a later one. Icons that
// themselves sit on top of each other are the declaration's to fix.
export function layoutMarkers(specs: LabelSpec[], box: Size): MarkerPlacement[] {
  const icons = specs.map((s) => iconRect(s.x, s.y, box));
  const placed: MarkerPlacement[] = [];
  const clear = (label: Rect, self: number) =>
    icons.every((r, i) => i === self || !rectsOverlap(label, r)) &&
    placed.every((p) => (p.label ? !rectsOverlap(label, p.label) : true));
  for (const [self, s] of specs.entries()) {
    const label = labelSize(s.text);
    const pref = markerAnchor(s.x, s.y, box, label);
    const flip = pref.y === "below" ? "above" : "below";
    const candidates: MarkerAnchor[] = [pref.x, ...X_ANCHORS.filter((x) => x !== pref.x)].flatMap((x) => [
      { x, y: pref.y },
      { x, y: flip },
    ]);
    let chosen: MarkerPlacement | null = null;
    for (const anchor of candidates) {
      const r = markerRects(s.x, s.y, box, anchor, label);
      if (inBox(r.label!, box) && clear(r.label!, self)) {
        chosen = { anchor, icon: icons[self], ...r };
        break;
      }
    }
    chosen ??= { anchor: pref, icon: icons[self], ...markerRects(s.x, s.y, box, { x: "center", y: pref.y }, null) };
    placed.push(chosen);
  }
  return placed;
}
