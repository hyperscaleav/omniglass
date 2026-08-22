// The system map's pure core (#791, ADR-0128): the standard declares WHERE a
// role position sits (normalized 2D against an aspect); the system's staffing
// says WHO is there. A marker is the join, and an empty position is a marker
// too (hollow): the map shows the room as built, gaps included.
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
