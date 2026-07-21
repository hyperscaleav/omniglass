import { api } from "../api/client";
import type { components } from "../api/schema.gen";

// The system-roles data layer: thin typed wrappers over the generated client.
//
// A ROLE is a slot a system needs filled (a table microphone, a main display). It
// is declared either on a STANDARD, in which case every system conforming to that
// standard inherits it live, or directly on a SYSTEM (an ad-hoc role, which is how
// a one-off system gets roles at all). Each role names the CAPABILITIES a component
// must ALL provide to fill it, and carries a QUORUM: how many components the role
// wants before it counts as staffed.
//
// The read on a system is RESOLVED: the standard's declarations plus the system's
// own, each with who fills it and how far short of quorum it is. The read on a
// standard is the declaration itself, since a standard staffs nothing.
//
// Assignment is the guarded write: the server refuses (422) unless the component
// provides every capability the role requires, and says which ones are missing.
// The refusal is the point, so callers surface the server's message verbatim
// rather than a generic failure.

// A role as a SYSTEM sees it: the declaration, plus who fills it here.
export type EffectiveRole = components["schemas"]["EffectiveRoleBody"];
// A role as its OWNER declares it (the standard's line, and the write's echo).
export type DeclaredRole = components["schemas"]["SystemRoleBody"];
// The declaration body: the capability list replaces the required set wholesale.
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
// naming the missing capabilities when the component does not provide everything
// the role requires; the caller shows that message.
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

// staffingLabel reads the quorum against the fill count, in the operator's words:
// how many the role wants, how many it has. The shortfall itself is the server's
// `understaffed`, not recomputed here, so the console and the API never disagree
// about whether a role is staffed.
export function staffingLabel(role: { quorum: number; assigned: number }): string {
  return `${role.quorum} wanted, ${role.assigned} assigned`;
}
