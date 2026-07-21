import {
  clearOwnerProperty,
  ownerProperties,
  ownerPropertiesKey,
  setOwnerProperty,
  type EffectiveProperty,
} from "./owner_properties";

// The component arc of the owner-properties layer, in component language: a
// component's contract comes from the PRODUCT it is an instance of. The logic is
// owner-generic (lib/owner_properties), so the component, system, and location
// arcs cannot drift; these wrappers keep the component call sites reading as what
// they address.

export type { EffectiveProperty };

export const effectivePropertiesKey = (component: string) => ownerPropertiesKey("component", component);

export const effectiveProperties = (component: string): Promise<EffectiveProperty[]> =>
  ownerProperties("component", component);

export const setComponentProperty = (component: string, property: string, value: unknown): Promise<void> =>
  setOwnerProperty("component", component, property, value);

export const clearComponentProperty = (component: string, property: string): Promise<void> =>
  clearOwnerProperty("component", component, property);
