import { api } from "../api/client";
import type { components } from "../api/schema.gen";

// The system-roles data layer: thin typed wrappers over the generated client.
//
// A ROLE is a slot a system needs filled (a table microphone, a main display). It
// is declared either on a STANDARD, in which case every system conforming to that
// standard inherits it live, or directly on a SYSTEM (an ad-hoc role, which is how
// a one-off system gets roles at all). Each role names the TYPED-SLOT guard a
// filling component's product must clear (#626): the component_types it accepts
// (self or a descendant) and, optionally, the specific products it pins, and
// carries a QUORUM: how many components the role wants before it counts as
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

// A role as a SYSTEM sees it: the declaration, plus who fills it here.
export type EffectiveRole = components["schemas"]["EffectiveRoleBody"];
// A role as its OWNER declares it (the standard's line, and the write's echo).
export type DeclaredRole = components["schemas"]["SystemRoleBody"];
// The declaration body: accepted_types and pinned_products each replace their
// set wholesale.
export type RoleSpec = components["schemas"]["RoleSpecBody"];

// One cache namespace per arc, so a standard and a system that share an address
// never collide.
export const systemRolesKey = (name: string) => ["system-roles", name] as const;
export const standardRolesKey = (id: string) => ["standard-roles", id] as const;

// systemRoles reads every role this system needs filled: those its standard
// declares (from_standard true) plus those declared directly on it.
export async function systemRoles(name: string): Promise<EffectiveRole[]> {
  const { data, error } = await api.GET("/systems/{name}/roles", { params: { path: { name } } });
  if (error) throw error;
  return (data?.roles ?? []) as EffectiveRole[];
}

// standardRoles reads what the standard declares; every conforming system
// inherits these live.
export async function standardRoles(id: string): Promise<DeclaredRole[]> {
  const { data, error } = await api.GET("/standards/{id}/roles", { params: { path: { id } } });
  if (error) throw error;
  return (data?.roles ?? []) as DeclaredRole[];
}

// setStandardRole declares a role on the standard, or revises it in place: the
// role is addressed by name, so the write is idempotent.
export async function setStandardRole(id: string, role: string, body: RoleSpec): Promise<DeclaredRole> {
  const { data, error } = await api.PUT("/standards/{id}/roles/{role}", { params: { path: { id, role } }, body });
  if (error) throw error;
  return data as DeclaredRole;
}

// deleteStandardRole withdraws the role from the standard, and with it every
// assignment conforming systems made to it.
export async function deleteStandardRole(id: string, role: string): Promise<void> {
  const { error } = await api.DELETE("/standards/{id}/roles/{role}", { params: { path: { id, role } } });
  if (error) throw error;
}

// assignRole puts a component in the role for this system. Refused with a 422
// naming both parties when the component's product is not a typed match; the
// caller shows that message.
export async function assignRole(system: string, role: string, component: string): Promise<void> {
  const { error } = await api.PUT("/systems/{name}/roles/{role}/assignments/{component}", {
    params: { path: { name: system, role, component } },
  });
  if (error) throw error;
}

// unassignRole takes the component out of the role, leaving it understaffed until
// another fills it.
export async function unassignRole(system: string, role: string, component: string): Promise<void> {
  const { error } = await api.DELETE("/systems/{name}/roles/{role}/assignments/{component}", {
    params: { path: { name: system, role, component } },
  });
  if (error) throw error;
}

// swapRolePositions exchanges two 1-based positions within a role: the only
// reorder primitive the server exposes (there is no "move to index N" route,
// position stays an ordering attribute of an assignment, not a second
// address for one; see identity_shape.go).
export async function swapRolePositions(system: string, role: string, position: number, withPos: number): Promise<void> {
  const { error } = await api.POST("/systems/{name}/roles/{role}:swapPositions", {
    params: { path: { name: system, role } },
    body: { position, with: withPos },
  });
  if (error) throw error;
}

// swapPath decomposes a moveItem(list, from, to)-shaped reorder (listmodel.ts)
// into the sequence of REAL-position swaps that reproduces it, since
// swapRolePositions exchanges exactly two positions and nothing shifts around
// them. positions[i] is AssignedTo[i]'s own 1-based position (EffectiveRoleBody's
// Positions field): a role's occupied positions are not assumed dense, because
// an unassign leaves a gap rather than compacting (#626,
// TestUnassignLeavesGapThenRefills), so a role with occupants at [1, 3] must
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
// own output is what caught it, and system_roles.test.ts's gapped
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
// how many the role wants, how many it has. The shortfall itself is the server's
// `understaffed`, not recomputed here, so the console and the API never disagree
// about whether a role is staffed.
export function staffingLabel(role: { quorum: number; assigned: number }): string {
  return `${role.quorum} wanted, ${role.assigned} assigned`;
}

// roleByComponent pivots the roles read: which role (if any) each component
// fills in this system, the by-device complement of the by-role lens
// (RolesPanel). A component filling no role is simply absent from the map,
// not an error: membership is its own relation precisely because a member can
// hold no role at all (see MembersPanel, which consumes this to say so).
// #626's one-role-per-component invariant makes this a safe 1:1 lookup: no
// component can appear under two roles here.
export function roleByComponent(roles: EffectiveRole[]): Map<string, EffectiveRole> {
  const m = new Map<string, EffectiveRole>();
  for (const r of roles) for (const c of r.assigned_to ?? []) m.set(c, r);
  return m;
}
