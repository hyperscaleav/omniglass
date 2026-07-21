import { api } from "../api/client";

// The component-capabilities data layer: thin typed wrappers over the generated
// client, plus the pure split that tells the two origins apart.
//
// A CAPABILITY is what a component can do ("microphone", "speaker"), and it is
// what a system role checks before letting the component fill it. The set a
// component provides is RESOLVED: what its PRODUCT declares, plus what the
// component adds, minus what the component suppresses. A component with no product
// still has whatever it declares itself, so a productless component can be staffed.
//
// The API returns the resolved set only (a flat list of ids), so the origins are
// derived here against the product's own declaration: anything resolved that the
// product does not declare was ADDED on this component, and anything the product
// declares that is not resolved was SUPPRESSED on it. That derivation is pure, so
// it is unit-tested without a server, and the panel stays declarative.

export const componentCapabilitiesKey = (name: string) => ["component-capabilities", name] as const;

// componentCapabilities reads what this component actually provides: the set the
// role-assignment guard checks.
export async function componentCapabilities(name: string): Promise<string[]> {
  const { data, error } = await api.GET("/components/{name}/capabilities", { params: { path: { name } } });
  if (error) throw error;
  return (data?.capabilities ?? []) as string[];
}

// setComponentCapability records this component's own fact about a capability:
// present true adds one its product does not claim, present false suppresses one
// it does. Idempotent.
export async function setComponentCapability(name: string, capability: string, present: boolean): Promise<void> {
  const { error } = await api.PUT("/components/{name}/capabilities/{capability}", {
    params: { path: { name, capability } },
    body: { present },
  });
  if (error) throw error;
}

// clearComponentCapability removes the component's own fact, so the capability
// falls back to whatever its product declares.
export async function clearComponentCapability(name: string, capability: string): Promise<void> {
  const { error } = await api.DELETE("/components/{name}/capabilities/{capability}", {
    params: { path: { name, capability } },
  });
  if (error) throw error;
}

// The origin of one capability line, which is what the panel groups on:
//   product   the product declares it and the component leaves it alone
//   component the component adds it (its product does not declare it)
//   suppressed the product declares it and the component takes it away
export type CapabilityOrigin = "product" | "component" | "suppressed";

export type CapabilityLine = { id: string; origin: CapabilityOrigin };

// splitCapabilities joins the resolved set to the product's declaration and labels
// each line with where it comes from. A suppressed capability is NOT in the
// resolved set (that is what suppression means), so it is carried here as its own
// origin rather than dropped: the operator has to see what the component is
// refusing to inherit in order to restore it. The result is sorted by id, so the
// list is stable across a refetch.
export function splitCapabilities(resolved: string[], product: string[]): CapabilityLine[] {
  const fromProduct = new Set(product);
  const lines: CapabilityLine[] = resolved.map((id) => ({
    id,
    origin: fromProduct.has(id) ? ("product" as const) : ("component" as const),
  }));
  const have = new Set(resolved);
  for (const id of product) if (!have.has(id)) lines.push({ id, origin: "suppressed" });
  return lines.sort((a, b) => a.id.localeCompare(b.id));
}
