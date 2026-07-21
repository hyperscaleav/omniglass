import { api } from "../api/client";
import type { components } from "../api/schema.gen";

// The component-properties data layer: thin typed wrappers over the generated
// client. A property is a canonical, typed key from the property catalog. A
// product declares a set of them as its CONTRACT (each with an optional default
// and a required flag), and every component that is an instance of that product
// inherits the contract, overriding a default with a value of its own. A property
// may also be set directly on a component without the contract declaring it (an
// ad-hoc, off-contract value), which is all a component with no product has.
//
// The effective read resolves both in one list: value is the override when set and
// the contract default otherwise, is_set marks the override, and from_contract
// separates the two origins. Row shapes are the generated EffectivePropertyBody,
// never hand-typed. The data_type set matches a variable's value type, so the
// coercion helpers in lib/variables (displayValue / parseInput) are reused.

export type EffectiveProperty = components["schemas"]["EffectivePropertyBody"];

export const effectivePropertiesKey = (component: string) => ["component-properties", component] as const;

// effectiveProperties reads a component's effective properties: every property its
// product's contract declares, resolved to the component's own value or the
// contract default, plus every property set directly on the component.
export async function effectiveProperties(component: string): Promise<EffectiveProperty[]> {
  const { data, error } = await api.GET("/components/{name}/properties", {
    params: { path: { name: component } },
  });
  if (error) throw error;
  return (data?.properties ?? []) as EffectiveProperty[];
}

// setComponentProperty declares a value for one property on a component,
// overriding the contract default (or adding an off-contract value when the
// contract does not declare it). Idempotent: a later set replaces the value. The
// value is coerced to the property's data_type by the caller, so an int property
// carries a number, not a string.
export async function setComponentProperty(component: string, property: string, value: unknown): Promise<void> {
  const { error } = await api.PUT("/components/{name}/properties/{property}", {
    params: { path: { name: component, property } },
    body: { value },
  });
  if (error) throw error;
}

// clearComponentProperty removes the component's declared value, so a contract
// property falls back to its default and an off-contract one leaves the effective
// read entirely.
export async function clearComponentProperty(component: string, property: string): Promise<void> {
  const { error } = await api.DELETE("/components/{name}/properties/{property}", {
    params: { path: { name: component, property } },
  });
  if (error) throw error;
}
