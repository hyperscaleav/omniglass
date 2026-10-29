import { api } from "../api/client";
import type { components as schemas } from "../api/schema.gen";
import { can, type Me } from "./auth";
import { entityLabel } from "./entities";
import type { Component } from "./components";
import type { System } from "./systems";

// The identity bindings data layer (#1389, ADR-1282): thin typed wrappers over
// the generated client.
//
// A CONDUIT is a component that speaks for many owners (a cloud service, a
// controller fronting many components): it collects once, and each value its
// driver identifies (a match key, a registered property type, and its value)
// lands on the one system or component an IDENTITY BINDING ties to that
// (conduit, key, value). A value naming an identity nobody bound lands on no
// owner and waits in the conduit's UNBOUND inbox until one is made. A binding
// writes onto both of its ends, so it costs update on both: the conduit and
// the owner.

export type IdentityBinding = schemas["schemas"]["IdentityBindingBody"];
export type IdentityBindings = schemas["schemas"]["ListIdentityBindingsOutputBody"];
export type UnboundIdentity = schemas["schemas"]["UnboundIdentityBody"];
export type UnboundIdentities = schemas["schemas"]["UnboundIdentitiesOutputBody"];
export type OwnerKind = IdentityBinding["owner_kind"];

export const identityBindingsKey = (component: string) => ["identity-bindings", component] as const;
export const unboundIdentitiesKey = (component: string) => ["unbound-identities", component] as const;

export async function listIdentityBindings(component: string): Promise<IdentityBindings> {
  const { data, error } = await api.GET("/components/{name}/identity-bindings", { params: { path: { name: component } } });
  if (error) throw error;
  return data as IdentityBindings;
}

export async function listUnboundIdentities(component: string): Promise<UnboundIdentities> {
  const { data, error } = await api.GET("/components/{name}/unbound-identities", { params: { path: { name: component } } });
  if (error) throw error;
  return data as UnboundIdentities;
}

// bindIdentity ties one identity the conduit reports to an owner, addressed by
// its uuid so two owners sharing a name never collide (ADR-0062).
export async function bindIdentity(component: string, key: string, value: string, ownerKind: OwnerKind, owner: string): Promise<IdentityBinding> {
  const { data, error } = await api.PUT("/components/{name}/identity-bindings/{key}/{value}", {
    params: { path: { name: component, key, value } },
    body: { owner_kind: ownerKind, owner },
  });
  if (error) throw error;
  return data as IdentityBinding;
}

export async function unbindIdentity(component: string, key: string, value: string): Promise<void> {
  const { error } = await api.DELETE("/components/{name}/identity-bindings/{key}/{value}", {
    params: { path: { name: component, key, value } },
  });
  if (error) throw error;
}

// identityRowKey is a row's stable handle across polls: a key and a value
// joined by a character neither carries in practice, so the rows keep their
// open picker and focus when a fresh read lands.
export const identityRowKey = (key: string, value: string) => `${key}\u0000${value}`;

// ownerChoices is what the bind picker offers: the systems and components the
// caller may update (the row's own actions, scope-aware, falling back to the
// permission on a row that carries none), never the conduit itself (a value
// with no identity already lands there), each kind sorted by what it reads as.
// The server is the authority; this only keeps the picker from offering an
// owner it would refuse.
// mayUpdateOwner is whether the caller may write onto one owner: the kind's
// update permission, and the owner row's own actions (scope-aware) when the
// row carries them.
export const mayUpdateOwner = (me: Me | null | undefined, kind: OwnerKind, actions?: string[]) =>
  can(me, kind, "update") && (Array.isArray(actions) ? actions.includes("update") : true);

export function ownerChoices(systems: System[], components: Component[], conduitId: string, me: Me | null | undefined) {
  const may = (kind: OwnerKind, actions?: string[]) => mayUpdateOwner(me, kind, actions);
  const byLabel = (a: { name: string; label?: string }, b: { name: string; label?: string }) => entityLabel(a).localeCompare(entityLabel(b));
  return {
    systems: systems.filter((s) => may("system", s.actions)).sort(byLabel),
    components: components.filter((c) => c.id !== conduitId && may("component", c.actions)).sort(byLabel),
  };
}

// refusalWords says why the server refused a bind or an unbind, in plain words
// first and its own reason after: a 403 is a permission one end lacks (the
// server names which), a 404 an owner gone or outside the caller's read (said
// without naming more, as the server does), a 422 a binding the rules refuse.
export function refusalWords(e: unknown): string {
  const o = e as { status?: number; detail?: string; title?: string } | null | undefined;
  const why = o?.detail ?? o?.title ?? "The operation failed.";
  switch (o?.status) {
    case 403:
      return `Not allowed: ${why}`;
    case 404:
      return "Not found: the owner is gone, or outside what you can see.";
    case 422:
      return `Not a valid binding: ${why}`;
    default:
      return why;
  }
}
