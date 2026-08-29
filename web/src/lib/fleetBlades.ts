import { type BladeDef } from "./blades";
import { systemBlade, componentBlade, locationBlade } from "../components/EntityBlade";
import { propertyResolutionBlade } from "../components/PropertiesPanel";
import { interfaceBlade, interfaceCreateBlade } from "../components/interfaceBlades";

// The fleet's cross-entity blade registry (#799): one blade per kind,
// self-fetching by id, so any page pushes any kind. Since #826 the blade
// hosts the EntityForm, whose kind panels drill into a property's resolution
// and a component's interfaces, so those blades ride here too: any stack
// that serves a fleet blade can serve what the form pushes from inside it.
export const fleetRegistry: Record<string, BladeDef> = {
  system: systemBlade,
  component: componentBlade,
  location: locationBlade,
  "property-resolution": propertyResolutionBlade,
  interface: interfaceBlade,
  "interface-create": interfaceCreateBlade,
};
