import { api } from "../api/client";
import type { components } from "../api/schema.gen";

// The membership data layer: thin typed wrappers over the generated client.
//
// MEMBERSHIP is a component's binding to a system, and a ROLE is what that
// membership does. They are one attachment at two levels rather than two facts
// that can drift apart, which is why staffing a role creates the membership.
//
// It is MANY-VALUED: a shared device belongs to every system it serves, so a
// divisible room's shared bar is a member of both halves. The console reads it
// from the component's end; membership itself is written by staffing a role.
//
// PRIMARY is the membership that answers a question asked with no system in hand.
// It is a default for context-free callers, not a resolution rule: anything naming
// a system resolves against that system. A component's first membership takes it
// automatically, so a component in exactly one system never meets the concept.

export type Member = components["schemas"]["SystemMemberBody"];

export const componentSystemsKey = (name: string) => ["component-systems", name] as const;

// componentSystems reads the systems this component belongs to. A shared device
// answers with more than one, which is the whole reason the relation exists.
export async function componentSystems(name: string): Promise<Member[]> {
  const { data, error } = await api.GET("/components/{name}/memberships", {
    params: { path: { name } },
  });
  if (error) throw error;
  return (data?.memberships ?? []) as Member[];
}
