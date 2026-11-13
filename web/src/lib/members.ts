import { api } from "../api/client";
import type { components } from "../api/schema.gen";
import { entityLabel } from "./entities";
import type { TreeNode } from "./treeselect";

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

// The system's end of the same relation (#940): every member of one system,
// each carrying its own recorded verdict. A system's view polls its members'
// health here, one request for the system, never the whole fleet view.
export const systemMembersKey = (name: string) => ["system-members", name] as const;

export async function systemMembers(name: string): Promise<Member[]> {
  const { data, error } = await api.GET("/systems/{name}/members", {
    params: { path: { name } },
  });
  if (error) throw error;
  return (data?.members ?? []) as Member[];
}

// The writes (#767). Membership is its own verb, not a field on the component's
// PATCH or move: the three routes below are the whole write surface, and each
// needs system:update on the system AND component:update on the component.
// Callers surface the server's refusal verbatim (a 403 names which end failed,
// a 404 is the non-disclosing out-of-scope answer, a 409 is a role still held).

// addSystemMember puts the component in the system. Idempotent; the component's
// first membership becomes its primary on its own.
export async function addSystemMember(system: string, component: string): Promise<void> {
  const { error } = await api.PUT("/systems/{name}/members/{component}", {
    params: { path: { name: system, component } },
  });
  if (error) throw error;
}

// removeSystemMember takes the component out of the system. Refused with a 409
// while it still fills a role there.
export async function removeSystemMember(system: string, component: string): Promise<void> {
  const { error } = await api.DELETE("/systems/{name}/members/{component}", {
    params: { path: { name: system, component } },
  });
  if (error) throw error;
}

// setPrimaryMember makes this membership the component's default system. A
// component that is not a member there is a 404, so the caller adds first.
export async function setPrimaryMember(system: string, component: string): Promise<void> {
  const { error } = await api.POST("/systems/{name}/members/{component}:setPrimary", {
    params: { path: { name: system, component } },
  });
  if (error) throw error;
}

// One step of a membership change, in the order it must run.
export type MembershipStep = { op: "add" | "setPrimary" | "remove"; system: string };

// planMembershipChanges turns what the operator asked for into the calls that
// make it so, as data, so the ordering rules are testable without a network.
//
// `wantPrimary` is the primary the operator chose: undefined means they never
// touched it, "" means they cleared it, anything else is a system id. `removed`
// are the other memberships they took out. The primary moves first (add if not
// yet a member, then setPrimary, since setPrimary on a non-member is a 404) and
// removals run last, so a refusal partway leaves the component in a system
// rather than in none. A system being promoted is never removed, even if its
// chip was staged for removal first.
export function planMembershipChanges(current: Member[], wantPrimary: string | undefined, removed: ReadonlySet<string>): MembershipStep[] {
  const steps: MembershipStep[] = [];
  const currentPrimary = current.find((m) => m.primary)?.system_id ?? "";
  const isMember = (id: string) => current.some((m) => m.system_id === id);
  let promoted = "";
  if (wantPrimary !== undefined && wantPrimary !== currentPrimary) {
    if (wantPrimary === "") {
      steps.push({ op: "remove", system: currentPrimary });
    } else {
      promoted = wantPrimary;
      if (!isMember(wantPrimary)) steps.push({ op: "add", system: wantPrimary });
      steps.push({ op: "setPrimary", system: wantPrimary });
    }
  }
  for (const id of removed) {
    if (id === promoted || id === currentPrimary || !isMember(id)) continue;
    steps.push({ op: "remove", system: id });
  }
  return steps;
}

// applyMembershipSteps runs a plan in order against one component, stopping at
// the first refusal (the thrown error is the server's problem body).
export async function applyMembershipSteps(component: string, steps: MembershipStep[]): Promise<void> {
  for (const s of steps) {
    if (s.op === "add") await addSystemMember(s.system, component);
    else if (s.op === "setPrimary") await setPrimaryMember(s.system, component);
    else await removeSystemMember(s.system, component);
  }
}

// After a write, both ends of the relation and every cached copy of it are
// stale: the component's own memberships, and each system's member list
// (SystemZoom polls it), under whatever address (name or uuid) the caller keyed
// it by. The prefixes cover all of them. The component's row (primary_system) is
// the form's own COMPONENTS_KEY invalidation.
export const MEMBERSHIP_KEYS = [["component-systems"], ["system-members"]] as const;

// bindableSystemNodes: the systems a component may be put into, as picker
// nodes. Those the caller's system:update SCOPE actually reaches (#707), read
// off each row's server-computed `actions`, so a form never offers a choice the
// platform refuses on submit. Shared by the create form and the edit face so
// the two cannot disagree about what is offered. A parent outside the set is
// dropped, so the tree never points at a node it does not hold.
export function bindableSystemNodes(systems: { id: string; name: string; label?: string; parent_id?: string | null; actions?: string[] }[]): TreeNode[] {
  const rows = systems.filter((sy) => sy.actions?.includes("update"));
  const present = new Set(rows.map((sy) => sy.id));
  return rows.map((sy) => ({ id: sy.id, value: sy.id, label: entityLabel(sy), parentId: sy.parent_id && present.has(sy.parent_id) ? sy.parent_id : undefined }));
}
