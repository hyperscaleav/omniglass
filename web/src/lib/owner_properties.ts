import { api } from "../api/client";
import type { components } from "../api/schema.gen";

// The owner-properties data layer: thin typed wrappers over the generated client
// for the VALUE side of the property model, generic over the three owner kinds
// that resolve a contract. A property is a canonical, typed key from the property
// catalog. A CLASSIFIER declares a set of them as its contract (a product for a
// component, a standard for a system, a location type for a location), each with
// an optional default and a required flag, and the owner either inherits the
// default or overrides it. A property may also be set directly on an owner with no
// contract declaring it (an ad-hoc, off-contract value), which is all an owner with
// no classifier has.
//
// The effective read resolves both in one list: value is the override when set and
// the contract default otherwise, is_set marks the override, and from_contract
// separates the two origins. The three arcs share the EffectivePropertyBody row
// shape (never hand-typed) and the same PUT / DELETE bodies, so one module drives
// all three and they cannot drift apart. The data_type set matches a variable's
// value type, so the coercion helpers in lib/variables (displayValue / parseInput)
// are reused.

export type PropertyOwnerKind = "component" | "system" | "location";

// An owner is addressed by kind plus its unique name (the URL address of a
// component, system, or location).
export type PropertyOwner = { kind: PropertyOwnerKind; name: string };

export type EffectiveProperty = components["schemas"]["EffectivePropertyBody"];

// One cache namespace per owner kind, so two owners that share a name (a system
// and a location both called "hq") never collide.
export const ownerPropertiesKey = (kind: PropertyOwnerKind, name: string) => [`${kind}-properties`, name] as const;

// ownerProperties reads an owner's effective properties: every property its
// classifier's contract declares, resolved to the owner's own value or the
// contract default, plus every property set directly on the owner.
export async function ownerProperties(kind: PropertyOwnerKind, name: string): Promise<EffectiveProperty[]> {
  const res =
    kind === "system"
      ? await api.GET("/systems/{name}/properties", { params: { path: { name } } })
      : kind === "location"
        ? await api.GET("/locations/{name}/properties", { params: { path: { name } } })
        : await api.GET("/components/{name}/properties", { params: { path: { name } } });
  if (res.error) throw res.error;
  return (res.data?.properties ?? []) as EffectiveProperty[];
}

// setOwnerProperty declares a value for one property on an owner, overriding the
// contract default (or adding an off-contract value when the contract does not
// declare it). Idempotent: a later set replaces the value. The value is coerced to
// the property's data_type by the caller, so an int property carries a number, not
// a string.
export async function setOwnerProperty(
  kind: PropertyOwnerKind,
  name: string,
  property: string,
  value: unknown,
): Promise<void> {
  const path = { name, property };
  const res =
    kind === "system"
      ? await api.PUT("/systems/{name}/properties/{property}", { params: { path }, body: { value } })
      : kind === "location"
        ? await api.PUT("/locations/{name}/properties/{property}", { params: { path }, body: { value } })
        : await api.PUT("/components/{name}/properties/{property}", { params: { path }, body: { value } });
  if (res.error) throw res.error;
}

// clearOwnerProperty removes the owner's declared value, so a contract property
// falls back to its default and an off-contract one leaves the effective read
// entirely.
export async function clearOwnerProperty(kind: PropertyOwnerKind, name: string, property: string): Promise<void> {
  const path = { name, property };
  const res =
    kind === "system"
      ? await api.DELETE("/systems/{name}/properties/{property}", { params: { path } })
      : kind === "location"
        ? await api.DELETE("/locations/{name}/properties/{property}", { params: { path } })
        : await api.DELETE("/components/{name}/properties/{property}", { params: { path } });
  if (res.error) throw res.error;
}
