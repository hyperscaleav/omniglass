// The system card's slot strip (#630 design fidelity): one square per slot
// the standard wants filled, computed over the ACTIVE roles' quorum from the
// health read. A filled square carries its occupant's state, an empty one is
// outlined. The gap line beneath uses the design's own words. Pure, and no
// verdict is computed: filled/down/empty are the server's assigned and down
// lists read back.

import type { FleetHealth } from "./health";
import { activeRoles } from "./health";

export type SlotSquare = { kind: "filled" | "down" | "empty"; role: string; occupant?: string };

export type SlotStrip = { squares: SlotSquare[]; filled: number; total: number; down: number; empty: number; note: string };

export function slotStrip(health: FleetHealth): SlotStrip {
  const squares: SlotSquare[] = [];
  for (const r of activeRoles(health)) {
    const assigned = r.assigned_to ?? [];
    const downSet = new Set(r.down ?? []);
    // Occupants first (each a slot), then the remaining quorum as empty.
    for (const name of assigned) squares.push({ kind: downSet.has(name) ? "down" : "filled", role: r.name, occupant: name });
    for (let i = assigned.length; i < r.quorum; i++) squares.push({ kind: "empty", role: r.name });
  }
  const filled = squares.filter((s) => s.kind === "filled").length;
  const down = squares.filter((s) => s.kind === "down").length;
  const empty = squares.filter((s) => s.kind === "empty").length;
  const total = squares.length;
  const note = empty > 0
    ? `${empty} required ${empty === 1 ? "slot" : "slots"} empty`
    : down > 0
      ? `${down} ${down === 1 ? "slot" : "slots"} down`
      : `${filled} of ${total} slots filled`;
  return { squares, filled, total, down, empty, note };
}
