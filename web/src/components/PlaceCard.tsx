import { Show } from "solid-js";
import { Dynamic } from "solid-js/web";
import { A } from "@solidjs/router";
import { useQuery } from "@tanstack/solid-query";
import Button from "./Button";
import Eyebrow from "./Eyebrow";
import TagPills from "./TagPills";
import { resolveIcon } from "./icons";
import { useBlades } from "../lib/blades";
import { FLEET_VIEW_KEY, fleetView } from "../lib/fleet";
import { LOCATIONS_KEY, listLocations } from "../lib/locations";
import { LOCATION_TYPES_KEY, listLocationTypes } from "../lib/location_types";
import { entityLabel } from "../lib/entities";

// PlaceCard (#872): a place as context. Systems are what Omniglass monitors;
// places are folders and metadata about where, so a detail view shows its
// place as one compact card above the system rather than as a view of its
// own. It names the place's type in the registry's words and its tags, and
// opens the place's own panel (its form, its Edit) without leaving the view.
//
// `showName` is off where the view is already titled by the place (a sole
// system's view), so the room is never named twice.
export default function PlaceCard(props: { placeId: string; showName: boolean; contents?: string }) {
  const blades = useBlades();
  const view = useQuery(() => ({ queryKey: FLEET_VIEW_KEY, queryFn: fleetView }));
  const locations = useQuery(() => ({ queryKey: LOCATIONS_KEY, queryFn: listLocations }));
  const types = useQuery(() => ({ queryKey: LOCATION_TYPES_KEY, queryFn: listLocationTypes }));
  const place = () => (view.data?.locations ?? []).find((l) => l.id === props.placeId);
  const type = () => (types.data ?? []).find((t) => t.name === place()?.location_type);
  const tags = () => (locations.data ?? []).find((l) => l.id === props.placeId)?.effective_tags ?? {};
  return (
    <Show when={place()}>
      {(p) => (
        <section data-testid="place-card" class="card flex-row flex-wrap items-center gap-x-3 gap-y-1.5 border border-base-300 bg-base-200 px-4 py-2.5 text-sm">
          <Eyebrow label="Place" hint="Where this sits. A place is a folder and its facts; the system in it is what Omniglass monitors." />
          <span class="flex min-w-0 items-center gap-2">
            <span class="flex-none text-base-content/50"><Dynamic component={resolveIcon(type()?.icon || "map-pin")} size={16} /></span>
            <Show when={props.showName}>
              <A href={`/locations/${p().id}`} class="truncate font-medium hover:underline">{entityLabel(p())}</A>
            </Show>
            <span data-testid="place-type" class="text-base-content/60">{type() ? entityLabel(type()!) : p().location_type}</span>
          </span>
          <Show when={props.contents}>
            <span class="text-base-content/30">{"·"}</span>
            <span class="text-base-content/60">{props.contents}</span>
          </Show>
          <TagPills tags={tags()} />
          <span class="flex-1" />
          <Button size="sm" intent="quiet" onClick={() => blades.push({ kind: "location", id: p().id })}>Place details</Button>
        </section>
      )}
    </Show>
  );
}
