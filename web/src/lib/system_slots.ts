import { api } from "../api/client";
import type { EffectiveSlotBody, SlotSpecBody, SystemSlotBody } from "../api/api.gen";

// The system-slots data layer: thin typed wrappers over the generated client.
//
// A SLOT is a slot a system needs filled (a table microphone, a main display). It
// is declared either on a STANDARD, in which case every system conforming to that
// standard inherits it live, or directly on a SYSTEM (an ad-hoc slot, which is how
// a one-off system gets slots at all). Each slot names the TYPED-SLOT guard a
// filling component's product must clear (#626): the component_types it accepts
// (self or a descendant) and, optionally, the specific products it pins, and
// carries a QUORUM: how many components the slot wants before it counts as
// staffed.
//
// The read on a system is RESOLVED: the standard's declarations plus the system's
// own, each with who fills it and how far short of quorum it is. The read on a
// standard is the declaration itself, since a standard staffs nothing.
//
// Assignment is the guarded write: the server refuses (422) unless the component's
// product is classified within an accepted type (and, if pinned, is one of the
// named products), and names both parties. The refusal is the point, so callers
// surface the server's message verbatim rather than a generic failure.

// A slot as a SYSTEM sees it: the declaration, plus who fills it here.
export type EffectiveSlot = EffectiveSlotBody;
// A slot as its OWNER declares it (the standard's line, and the write's echo).
export type DeclaredSlot = SystemSlotBody;
// The declaration body. Partial by default: what it carries changes, what it
// omits is left alone. update_mask names the fields the write means to set
// regardless, which is the only way to clear one (#666); accepted_types and
// pinned_products still replace their set wholesale when written.
export type SlotSpec = SlotSpecBody;

// One cache namespace per arc, so a standard and a system that share an address
// never collide.
// The prefix every system's slots share, for a write that moves them all.
export const SYSTEM_SLOTS_PREFIX = ["system-slots"] as const;
export const systemSlotsKey = (name: string) => [...SYSTEM_SLOTS_PREFIX, name] as const;
export const standardSlotsKey = (id: string) => ["standard-slots", id] as const;

// systemSlots reads every slot this system needs filled: those its standard
// declares (from_standard true) plus those declared directly on it.
export async function systemSlots(name: string): Promise<EffectiveSlot[]> {
  const { data, error } = await api.GET("/systems/{name}/slots", { params: { path: { name } } });
  if (error) throw error;
  return (data?.slots ?? []) as EffectiveSlot[];
}

// standardSlots reads what the standard declares; every conforming system
// inherits these live.
export async function standardSlots(id: string): Promise<DeclaredSlot[]> {
  const { data, error } = await api.GET("/standards/{id}/slots", { params: { path: { id } } });
  if (error) throw error;
  return (data?.slots ?? []) as DeclaredSlot[];
}

// setStandardSlot declares a slot on the standard, or revises it in place: the
// slot is addressed by name, so the write is idempotent. A PATCH, and partial:
// what the body carries changes, what it omits is left alone, and what its
// update_mask names is written whether the body carries it or not (#666).
export async function setStandardSlot(id: string, slot: string, body: SlotSpec): Promise<DeclaredSlot> {
  const { data, error } = await api.PATCH("/standards/{id}/slots/{slot}", { params: { path: { id, slot } }, body });
  if (error) throw error;
  return data as DeclaredSlot;
}

// deleteStandardSlot withdraws the slot from the standard, and with it every
// assignment conforming systems made to it.
export async function deleteStandardSlot(id: string, slot: string): Promise<void> {
  const { error } = await api.DELETE("/standards/{id}/slots/{slot}", { params: { path: { id, slot } } });
  if (error) throw error;
}

// setSystemSlot declares a slot directly on the system (an ad hoc slot, which is
// how a one-off system gets slots at all), or revises it in place. The same
// idempotent, partial PATCH as setStandardSlot. A name the system's standard
// already declares is refused with a 409: a slot name means one slot in a
// system, so the caller shows the server's message.
export async function setSystemSlot(name: string, slot: string, body: SlotSpec): Promise<DeclaredSlot> {
  const { data, error } = await api.PATCH("/systems/{name}/slots/{slot}", { params: { path: { name, slot } }, body });
  if (error) throw error;
  return data as DeclaredSlot;
}

// deleteSystemSlot withdraws a slot declared on the system, and with it every
// assignment to it. A slot inherited from the standard is withdrawn on the
// standard, not here (404).
export async function deleteSystemSlot(name: string, slot: string): Promise<void> {
  const { error } = await api.DELETE("/systems/{name}/slots/{slot}", { params: { path: { name, slot } } });
  if (error) throw error;
}

// assignSlot puts a component in the slot for this system. Refused with a 422
// naming both parties when the component's product is not a typed match; the
// caller shows that message.
export async function assignSlot(system: string, slot: string, component: string): Promise<void> {
  const { error } = await api.PUT("/systems/{name}/slots/{slot}/assignments/{component}", {
    params: { path: { name: system, slot, component } },
  });
  if (error) throw error;
}

// unassignSlot takes the component out of the slot, leaving it understaffed until
// another fills it.
export async function unassignSlot(system: string, slot: string, component: string): Promise<void> {
  const { error } = await api.DELETE("/systems/{name}/slots/{slot}/assignments/{component}", {
    params: { path: { name: system, slot, component } },
  });
  if (error) throw error;
}

// swapSlotPositions exchanges two 1-based positions within a slot: the only
// reorder primitive the server exposes (there is no "move to index N" route,
// position stays an ordering attribute of an assignment, not a second
// address for one; see identity_shape.go).
export async function swapSlotPositions(system: string, slot: string, position: number, withPos: number): Promise<void> {
  const { error } = await api.POST("/systems/{name}/slots/{slot}:swapPositions", {
    params: { path: { name: system, slot } },
    body: { position, with: withPos },
  });
  if (error) throw error;
}

// swapPath decomposes a moveItem(list, from, to)-shaped reorder (listmodel.ts)
// into the sequence of REAL-position swaps that reproduces it, since
// swapSlotPositions exchanges exactly two positions and nothing shifts around
// them. positions[i] is AssignedTo[i]'s own 1-based position (EffectiveSlotBody's
// Positions field): a slot's occupied positions are not assumed dense, because
// an unassign leaves a gap rather than compacting (#626,
// TestUnassignLeavesGapThenRefills), so a slot with occupants at [1, 3] must
// swap positions 1 and 3, never the "1 and 2" an index-plus-one assumption
// would guess and 404 or misorder against (task 9 review, finding C3).
//
// from/to are 0-based indices into positions (and assigned_to, index for
// index), matching moveItem's own signature. moveItem's shift is equivalent
// to a chain of ADJACENT-RANK swaps walking the moved item across (the
// standard splice-as-swap-chain identity); each step exchanges whichever two
// occupants currently hold ranks i and i+step. Critically, positions ITSELF
// never needs to change mid-walk: swapping two occupants changes who sits at
// a position, never which positions are occupied, so "rank i's real
// position" is the same positions[i] at every step, not a value that moves
// with its occupant. (An earlier draft of this function mutated a working
// copy as if it did; verifying the gapped case by hand against moveItem's
// own output is what caught it, and system_slots.test.ts's gapped
// "reproduces moveItem" case pins the regression.)
export function swapPath(positions: number[], from: number, to: number): [number, number][] {
  if (from === to) return [];
  const path: [number, number][] = [];
  const step = from < to ? 1 : -1;
  for (let i = from; i !== to; i += step) {
    path.push([positions[i], positions[i + step]]);
  }
  return path;
}

// staffingLabel reads the quorum against the fill count, in the operator's words:
// how many the slot wants, how many it has. The shortfall itself is the server's
// `understaffed`, not recomputed here, so the console and the API never disagree
// about whether a slot is staffed.
export function staffingLabel(slot: { quorum: number; assigned: number }): string {
  return `${slot.quorum} wanted, ${slot.assigned} assigned`;
}
