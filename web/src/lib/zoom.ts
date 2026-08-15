// The zoom ladder (#633): four chips, one per zoom level, resolved from an
// explicit anchor through the view's own ancestor chain and never from a
// stored location id. Arriving deep by URL must leave every chip live, which
// only holds if the chip derives everything it shows from what the view
// carries; a chip remembering where the operator HAS BEEN would go dead on a
// shared link.
//
// This slice renders only the fleet anchor; the later zoom slices consume the
// same function unchanged, which is why the deep-anchor case is proven here at
// the unit tier before any page can exercise it.

import { ancestors, locationIndex, type FleetView } from "./fleet";
import { entityLabel } from "./entities";

export type ZoomLevel = "fleet" | "location" | "system" | "component";

// A ZoomAnchor names what the URL currently addresses, by uuid. All fields
// empty is the fleet zoom.
export type ZoomAnchor = {
  locationId?: string | null;
  systemId?: string | null;
  componentId?: string | null;
};

export type ZoomChip = {
  id: ZoomLevel;
  label: string;
  // count is presentation-ready: the roots under the fleet chip, the chain
  // depth under an anchored location chip, and empty on a chip with nothing
  // to count. Empty rather than a placeholder: house style bans the dash.
  count: string;
  active: boolean;
  enabled: boolean;
};

export function zoomChips(zoom: ZoomLevel, anchor: ZoomAnchor, view: FleetView): ZoomChip[] {
  const index = locationIndex(view);
  const chain = anchor.locationId ? ancestors(anchor.locationId, index) : [];
  const anchored = chain.length > 0 ? chain[chain.length - 1] : null;
  const system = anchor.systemId ? (view.systems ?? []).find((s) => s.id === anchor.systemId) : undefined;
  const dot = anchor.componentId && system ? (system.dots ?? []).find((d) => d.component === anchor.componentId) : undefined;
  const roots = (view.locations ?? []).filter((l) => !l.parent).length;

  return [
    {
      id: "fleet",
      label: "Fleet",
      count: String(roots),
      active: zoom === "fleet",
      enabled: true,
    },
    {
      id: "location",
      label: anchored ? entityLabel(anchored) : "Location",
      count: anchored ? String(chain.length) : "",
      active: zoom === "location",
      enabled: anchored !== null,
    },
    {
      id: "system",
      label: system ? entityLabel(system) : "System",
      count: "",
      active: zoom === "system",
      enabled: system !== undefined,
    },
    {
      id: "component",
      label: dot ? dot.name : "Component",
      count: "",
      active: zoom === "component",
      enabled: dot !== undefined,
    },
  ];
}
