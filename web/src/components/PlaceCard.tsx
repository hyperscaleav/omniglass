import { Show } from "solid-js";
import { Dynamic } from "solid-js/web";
import { A } from "@solidjs/router";
import { can, useMe } from "../lib/auth";
import { useQuery } from "@tanstack/solid-query";
import Button from "./Button";
import Eyebrow from "./Eyebrow";
import TagPills from "./TagPills";
import { resolveIcon } from "./icons";
import { useBlades } from "../lib/blades";
import { FLEET_VIEW_KEY, ancestors, fleetView, locationIndex } from "../lib/fleet";
import { LOCATIONS_KEY, listLocations } from "../lib/locations";
import { LOCATION_TYPES_KEY, listLocationTypes } from "../lib/location_types";
import { entityLabel } from "../lib/entities";
import { systemsAtPlace } from "../lib/detail";

// PlaceCard (#872): a place as context. Systems are what Omniglass monitors;
// places are folders and metadata about where, so a detail view shows its
// place as one compact card above the system rather than as a view of its
// own. It names the place's type in the registry's words and its tags, and
// opens the place's own panel (its form, its Edit) without leaving the view.
//
// `showName` is off where the view is already titled by the place (a sole
// system's view), so the room is never named twice.
export default function PlaceCard(props: {
  placeId: string;
  showName: boolean;
  contents?: string;
  // Where the place comes from, when that is worth saying: a component's own
  // placement, or its system's place standing in for one it does not have.
  provenance?: string;
  // The places above it, for a host with no breadcrumb of its own (a drawer):
  // where it sits, the one fact the title does not already say.
  path?: boolean;
}) {
  const blades = useBlades();
  const me = useMe();
  const view = useQuery(() => ({ queryKey: FLEET_VIEW_KEY, queryFn: fleetView }));
  const locations = useQuery(() => ({ queryKey: LOCATIONS_KEY, queryFn: listLocations }));
  const types = useQuery(() => ({ queryKey: LOCATION_TYPES_KEY, queryFn: listLocationTypes }));
  const place = () => (view.data?.locations ?? []).find((l) => l.id === props.placeId);
  const type = () => (types.data ?? []).find((t) => t.name === place()?.location_type);
  const tags = () => (locations.data ?? []).find((l) => l.id === props.placeId)?.effective_tags ?? {};
  // A room holding one system IS that system: its own panel would only point
  // back at the system, and its form is Configure place, so no second panel
  // is offered for it (#872).
  const above = () => (view.data ? ancestors(props.placeId, locationIndex(view.data)).slice(0, -1).map((l) => entityLabel(l)) : []);
  const sole = () => (view.data ? systemsAtPlace(view.data, props.placeId).length === 1 : false);
  return (
    <Show when={place()}>
      {(p) => (
        <section data-testid="place-card" class="flex flex-wrap items-center gap-x-3 gap-y-1.5 rounded-box border border-base-300 bg-base-200 px-4 py-2.5 text-sm">
          <Eyebrow label="Place" hint="Where this sits. A place is a folder and its facts; the system in it is what Omniglass monitors." />
          <span class="flex min-w-0 items-center gap-2">
            <span class="flex-none text-base-content/50"><Dynamic component={resolveIcon(type()?.icon || "map-pin")} size={16} /></span>
            <Show when={props.showName}>
              <A href={`/locations/${p().id}`} class="truncate font-medium hover:underline">{entityLabel(p())}</A>
            </Show>
            <span data-testid="place-type" class="text-base-content/60">{type() ? entityLabel(type()!) : p().location_type}</span>
          </span>
          <Show when={props.path && above().length > 0}>
            <span data-testid="place-path" class="min-w-0 truncate text-base-content/50" title={above().join(" / ")}>{above().join(" / ")}</span>
          </Show>
          <Show when={props.contents}>
            <span class="text-base-content/30">{"·"}</span>
            <span class="text-base-content/60">{props.contents}</span>
          </Show>
          <Show when={props.provenance}>
            <span data-testid="place-provenance" class="rounded border border-base-content/15 px-1.5 text-[11px] leading-4 text-base-content/55">{props.provenance}</span>
          </Show>
          <Show when={Object.keys(tags()).length > 0}><TagPills tags={tags()} wrap /></Show>
          <span class="flex-1" />
          {/* The place's own configuration (its properties and their cascade)
              is its Configure tab; a sole system's place would otherwise land
              on the system, so the card links there directly. */}
          <Show when={can(me.data, "location", "update")}>
            <A href={`/locations/${p().id}?tab=configure`} class="text-sm text-base-content/70 hover:underline">Configure place</A>
          </Show>
          <Show when={!sole()}>
            <Button size="sm" intent="quiet" onClick={() => blades.push({ kind: "location", id: p().id })}>Place details</Button>
          </Show>
        </section>
      )}
    </Show>
  );
}
