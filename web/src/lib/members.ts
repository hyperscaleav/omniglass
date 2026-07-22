import { api } from "../api/client";
import type { components } from "../api/schema.gen";

// The membership data layer: thin typed wrappers over the generated client.
//
// MEMBERSHIP is a component's binding to a system, and a ROLE is what that
// membership does. They are one attachment at two levels rather than two facts
// that can drift apart, which is why staffing a role creates the membership.
//
// It is MANY-VALUED: a shared device belongs to every system it serves, so a
// divisible room's shared bar is a member of both halves. The same relation reads
// from either end, which is why there are two reads here.
//
// PRIMARY is the membership that answers a question asked with no system in hand.
// It is a default for context-free callers, not a resolution rule: anything naming
// a system resolves against that system. A component's first membership takes it
// automatically, so a component in exactly one system never meets the concept.

export type Member = components["schemas"]["SystemMemberBody"];

// One cache namespace per direction, so a system and a component that happen to
// share a name never collide.
export const systemMembersKey = (name: string) => ["system-members", name] as const;
export const componentSystemsKey = (name: string) => ["component-systems", name] as const;

// systemMembers reads the components bound into this system.
export async function systemMembers(name: string): Promise<Member[]> {
  const { data, error } = await api.GET("/systems/{name}/members", { params: { path: { name } } });
  if (error) throw error;
  return (data?.members ?? []) as Member[];
}

// componentSystems reads the systems this component belongs to. A shared device
// answers with more than one, which is the whole reason the relation exists.
export async function componentSystems(name: string): Promise<Member[]> {
  const { data, error } = await api.GET("/components/{name}/memberships", {
    params: { path: { name } },
  });
  if (error) throw error;
  return (data?.memberships ?? []) as Member[];
}

export async function addMember(system: string, component: string): Promise<void> {
  const { error } = await api.PUT("/systems/{name}/members/{component}", {
    params: { path: { name: system, component } },
  });
  if (error) throw error;
}

// removeMember is refused (409) while the component still fills a role here. The
// refusal is the lesson, so callers surface the server's message rather than a
// generic failure: it says to unassign the role first.
export async function removeMember(system: string, component: string): Promise<void> {
  const { error } = await api.DELETE("/systems/{name}/members/{component}", {
    params: { path: { name: system, component } },
  });
  if (error) throw error;
}

export async function setPrimaryMember(system: string, component: string): Promise<void> {
  const { error } = await api.POST("/systems/{name}/members/{component}:setPrimary", {
    params: { path: { name: system, component } },
  });
  if (error) throw error;
}
