import { createMemo, type JSX } from "solid-js";
import { useQuery } from "@tanstack/solid-query";
import TreeSelect from "./TreeSelect";
import { LOCATIONS_KEY, listLocations, type Location } from "../lib/locations";
import { LOCATION_TYPES_KEY, listLocationTypes } from "../lib/location_types";
import { locationTreeNodes } from "../lib/location_tree";

// LocationSelect: the one field that picks a location. It reads the fleet's
// locations and their types itself and draws them as the location tree
// (TreeSelect over lib/location_tree), so every place an operator chooses a
// location shows the same nesting and the same sibling order, and the picking
// UX changes here once for all of them. A guard test
// (LocationSelect.guard.test.ts) fails on a location field built any other way.
//
// addressOf is the value the field commits (default the uuid; an outpost
// commits the name, the simulation and template forms the dotted path).
// pickable narrows what may be chosen without dropping the rest from the tree.
// excludeSubtreeOf takes a location uuid: the reparent self-guard.
export default function LocationSelect(p: {
  value: string;
  onChange: (v: string) => void;
  rootLabel?: string;
  addressOf?: (l: Location) => string;
  pickable?: (l: Location) => boolean;
  excludeSubtreeOf?: string;
  id?: string;
  disabled?: boolean;
  class?: string;
  // False holds the reads back, for a caller that knows the viewer cannot read
  // locations: the field then offers only its root option.
  enabled?: boolean;
}): JSX.Element {
  const enabled = () => p.enabled ?? true;
  const locations = useQuery(() => ({ queryKey: LOCATIONS_KEY, queryFn: listLocations, enabled: enabled() }));
  const types = useQuery(() => ({ queryKey: LOCATION_TYPES_KEY, queryFn: listLocationTypes, enabled: enabled() }));
  const items = createMemo(() =>
    locationTreeNodes(locations.data ?? [], types.data ?? [], { addressOf: p.addressOf, pickable: p.pickable }),
  );
  return (
    <TreeSelect
      id={p.id}
      items={items()}
      value={p.value}
      onChange={p.onChange}
      rootLabel={p.rootLabel}
      excludeSubtreeOf={p.excludeSubtreeOf}
      disabled={p.disabled}
      class={p.class}
    />
  );
}
