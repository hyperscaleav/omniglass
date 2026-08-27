import { type BladeDef } from "./blades";
import { systemBlade, componentBlade, locationBlade } from "../components/EntityBlade";

// The fleet's cross-entity blade registry (#799): one condensed blade per
// kind, self-fetching by id, so any page pushes any kind. A system's alarm
// or member drills into its component blade in the same stack; Expand
// promotes to the identity route.
export const fleetRegistry: Record<string, BladeDef> = {
  system: systemBlade,
  component: componentBlade,
  location: locationBlade,
};
