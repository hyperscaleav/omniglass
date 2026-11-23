import { For, Show, createMemo } from "solid-js";
import { Dynamic } from "solid-js/web";
import { useNavigate } from "@solidjs/router";
import { useQuery, useQueryClient } from "@tanstack/solid-query";
import Eyebrow from "./Eyebrow";
import TagPills from "./TagPills";
import TabRail from "./TabRail";
import ConfigureFace from "./ConfigureFace";
import DetailGate from "./DetailGate";
import OutlineWorkspace from "./OutlineWorkspace";
import SystemSummary from "./SystemSummary";
import HealthBadge from "./HealthBadge";
import { resolveIcon } from "./icons";
import { useBlades } from "../lib/blades";
import { createLeaveGuard } from "../lib/leave_guard";
import { createTabState, type DetailHost } from "../lib/tabstate";
import { useEntityDelete } from "../lib/entity_delete";
import { FLEET_VIEW_KEY, ancestors, childrenIndex, fleetView, locationIndex } from "../lib/fleet";
import { LOCATIONS_KEY, deleteLocation, listLocations } from "../lib/locations";
import { LOCATION_TYPES_KEY, listLocationTypes } from "../lib/location_types";
import { locationHealth, locationHealthKey } from "../lib/health";
import { sinceOf } from "../lib/system_zoom";
import { contentsOf, systemsAtLocation } from "../lib/detail";
import { entityLabel } from "../lib/entities";
import { can, useMe } from "../lib/auth";
import { describeError, fmtTime } from "../lib/format";
import { durationText } from "../lib/timeline";
import { LEAF_POLL_MS } from "../lib/component_leaf";

// LocationDetail (#635, reoriented in #872, one body for both hosts since
// #2423): a location's whole detail body, rendered by its page
// (`/locations/:id`, host "page") and by the fleet blade (host "blade") alike.
// A location is a folder and its facts: its card leads with its type, its
// health (worst-wins over the systems beneath it) and since-when, and its tags;
// Overview says what it holds (the locations beneath, by type), a brief card
// per system bound here, and what is beneath it as the outline rooted here;
// Configure is the one form. The host decides only where the tab lives,
// whether Edit answers the address's ?edit=1, and where a Delete leaves to.
export default function LocationDetail(props: { id: string; host: DetailHost }) {
  const id = () => props.id;
  const navigate = useNavigate();
  const qc = useQueryClient();
  const me = useMe();
  const blades = useBlades();
  const leave = createLeaveGuard();

  const view = useQuery(() => ({ queryKey: FLEET_VIEW_KEY, queryFn: fleetView }));
  const types = useQuery(() => ({ queryKey: LOCATION_TYPES_KEY, queryFn: listLocationTypes }));
  const locations = useQuery(() => ({ queryKey: LOCATIONS_KEY, queryFn: listLocations }));
  const health = useQuery(() => ({ queryKey: locationHealthKey(id()), queryFn: () => locationHealth(id()), refetchInterval: LEAF_POLL_MS }));

  const location = createMemo(() => (view.data ? locationIndex(view.data).get(id()) : undefined));
  const row = () => (locations.data ?? []).find((l) => l.id === id());
  const type = createMemo(() => (types.data ?? []).find((t) => t.name === location()?.location_type));
  const tags = () => row()?.effective_tags ?? {};
  // By id, compared by value: the cards key on these strings, so a fresh
  // fleet read updates a card in place rather than rebuilding it.
  const systemsHere = createMemo(() => (view.data && location() ? systemsAtLocation(view.data, id()).map((s) => s.id) : []), undefined, {
    equals: (a, b) => a.length === b.length && a.every((x, i) => x === b[i]),
  });
  const hasBeneath = createMemo(() => (view.data ? (childrenIndex(view.data).get(id()) ?? []).length > 0 : false));
  const contents = () => (view.data ? contentsOf(view.data, id(), types.data ?? []) : "");
  const path = () => (view.data ? ancestors(id(), locationIndex(view.data)).slice(0, -1).map((l) => entityLabel(l)).join(" / ") : "");

  const tabs = createMemo(() => [
    { key: "overview", label: "Overview" },
    ...(can(me.data, "location", "update") ? [{ key: "configure", label: "Configure" }] : []),
  ]);
  const tabState = createTabState(props.host, tabs);
  const tab = tabState.active;

  const { destructive, err } = useEntityDelete({
    kindLabel: "location",
    id: props.id,
    name: () => row()?.name,
    actions: () => row()?.actions ?? [],
    remove: (lid) => deleteLocation(lid),
    onDeleted: () => (props.host === "blade" ? blades.close() : navigate("/explore")),
    invalidate: () => Promise.all([qc.invalidateQueries({ queryKey: [...LOCATIONS_KEY] }), qc.invalidateQueries({ queryKey: [...FLEET_VIEW_KEY] })]),
  });

  return (
    <DetailGate
      noun="location"
      missing={!!view.data && !view.isFetching && !location()}
      pending={view.isPending}
      error={view.error && !location() ? describeError(view.error) : null}
      retrying={view.isFetching}
      onRetry={() => void view.refetch()}
    >
      <div data-testid="location-detail" data-host={props.host} class="@container flex flex-col gap-3">
        <Show when={err()}>
          <div role="alert" class="alert alert-error alert-soft text-sm"><span>{err()}</span></div>
        </Show>
        <section data-testid="location-subject" class="card overflow-hidden border border-base-300 bg-base-200 p-0">
          <div data-testid="location-header" class="flex flex-wrap items-center gap-x-3 gap-y-1.5 border-b border-base-300 px-4 py-3 text-sm">
            <Eyebrow label="Location" hint="A location is a folder and its facts. The systems in it, and in the locations beneath it, are what Omniglass monitors; its health is the worst of theirs." />
            <span class="flex items-center gap-2">
              <span class="flex-none text-base-content/50"><Dynamic component={resolveIcon(type()?.icon || "map-pin")} size={16} /></span>
              <span data-testid="location-type" class="text-base-content/70">{type() ? entityLabel(type()!) : location()?.location_type}</span>
            </span>
            {/* Where it sits: the page says so in its breadcrumb; a blade has
                none, so its header names the locations above it. */}
            <Show when={props.host === "blade" && path()}>
              <span data-testid="location-path" class="text-base-content/60">{path()}</span>
            </Show>
            <HealthBadge health={health.data?.health ?? location()?.health ?? undefined} size="sm" />
            <Show when={health.data && sinceOf(health.data, Date.now())}>
              {(sc) => <span data-testid="since-line" class="tabular-nums text-base-content/70">since {fmtTime(sc().ts)} · {durationText(sc().ms)}</span>}
            </Show>
            <Show when={Object.keys(tags()).length > 0}><TagPills tags={tags()} wrap /></Show>
          </div>
          <TabRail tabs={tabs()} state={tabState} guard={leave} />
          <Show when={tab() === "configure"}>
            {/* The host keys the body on the id: another location is another
                face (#854). */}
            <ConfigureFace kind="location" id={id()} guard={leave} host={props.host} destructive={destructive} />
          </Show>
          <Show when={tab() === "overview"}>
            <div data-testid="location-overview" class="flex flex-col gap-4 p-4">
              {/* The locations beneath it; a room holding systems and no
                  locations says nothing about locations (its systems follow). */}
              <Show when={hasBeneath()}>
                <section data-testid="location-holds" class="flex flex-col gap-1">
                  <Eyebrow label="Holds" hint="The locations directly beneath this one, counted by type." />
                  <span class="text-sm text-base-content/70">{contents()}</span>
                </section>
              </Show>
              <Show when={systemsHere().length > 0}>
                <section data-testid="location-systems" class="flex flex-col gap-2">
                  <Eyebrow label={systemsHere().length === 1 ? "System here" : "Systems here"} hint="Each system bound to this location, in brief. Open one for its components, map, data and history." />
                  <div class="grid grid-cols-1 gap-3 @2xl:grid-cols-2">
                    {/* A page goes to the system's page; a blade opens it over itself. */}
                    <For each={systemsHere()}>{(sid) => <SystemSummary systemId={sid} onOpen={props.host === "blade" ? (s) => blades.push({ kind: "system", id: s }) : undefined} />}</For>
                  </div>
                </section>
              </Show>
              <Show when={hasBeneath() || systemsHere().length === 0}>
                <section data-testid="beneath" class="flex flex-col gap-2">
                  <Show when={systemsHere().length > 0}>
                    <Eyebrow label="Beneath" hint="The locations inside this one, as Explore draws them, counted by the systems they hold." />
                  </Show>
                  <OutlineWorkspace rootId={id()} host={props.host} />
                </section>
              </Show>
            </div>
          </Show>
        </section>
      </div>
    </DetailGate>
  );
}
