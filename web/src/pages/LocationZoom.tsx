import { For, Show, createEffect, createMemo } from "solid-js";
import { Dynamic } from "solid-js/web";
import { useNavigate, useParams, useSearchParams } from "@solidjs/router";
import { useQuery } from "@tanstack/solid-query";
import Page from "../components/Page";
import Breadcrumb from "../components/Breadcrumb";
import Eyebrow from "../components/Eyebrow";
import TagPills from "../components/TagPills";
import TabRail from "../components/TabRail";
import ConfigureFace from "../components/ConfigureFace";
import DetailGate from "../components/DetailGate";
import OutlineWorkspace from "../components/OutlineWorkspace";
import SystemSummary from "../components/SystemSummary";
import BladeStack from "../components/BladeStack";
import { resolveIcon } from "../components/icons";
import { BladesContext, createBladeController } from "../lib/blades";
import { fleetRegistry } from "../lib/fleetBlades";
import { FLEET_VIEW_KEY, ancestors, childrenIndex, fleetView, locationIndex } from "../lib/fleet";
import { LOCATIONS_KEY, listLocations } from "../lib/locations";
import { LOCATION_TYPES_KEY, listLocationTypes } from "../lib/location_types";
import { landingFor, systemsAtPlace } from "../lib/detail";
import { entityLabel } from "../lib/entities";
import { can, useMe } from "../lib/auth";
import { describeError } from "../lib/format";

// A place's detail view (#635, reoriented in #872). Systems are the unit
// Omniglass monitors and places are folders and metadata, so a place holding
// exactly one system has no view of its own: its address lands on that
// system, whose view shows the place as a card of context. What remains here
// is the folder: a place holding no system (a campus, a building, a floor),
// or one shared by several. Its card is the subject, with its own tabs:
// Overview (a brief card per system it holds, then what is beneath it as the
// outline rooted here) and Configure (the one form).
//
// An address that asks to configure the place itself (?tab=configure, or
// ?edit=1, the create handoff) stays here even when the place holds one
// system, since that edit is the place's, not the system's.
export default function LocationZoom() {
  const params = useParams<{ id: string }>();
  const navigate = useNavigate();
  const id = () => params.id;
  const me = useMe();
  const blades = createBladeController();
  const [search] = useSearchParams();
  const param = (k: string) => { const v = search[k]; return Array.isArray(v) ? v[0] : v; };

  const view = useQuery(() => ({ queryKey: FLEET_VIEW_KEY, queryFn: fleetView }));
  const types = useQuery(() => ({ queryKey: LOCATION_TYPES_KEY, queryFn: listLocationTypes }));
  const locations = useQuery(() => ({ queryKey: LOCATIONS_KEY, queryFn: listLocations }));

  const place = createMemo(() => (view.data ? locationIndex(view.data).get(id()) : undefined));
  const type = createMemo(() => (types.data ?? []).find((t) => t.name === place()?.location_type));
  const tags = () => (locations.data ?? []).find((l) => l.id === id())?.effective_tags ?? {};
  // By id, compared by value: the cards key on these strings, so a fresh
  // fleet read updates a card in place rather than rebuilding it.
  const systemsHere = createMemo(() => (view.data && place() ? systemsAtPlace(view.data, id()).map((s) => s.id) : []), undefined, {
    equals: (a, b) => a.length === b.length && a.every((x, i) => x === b[i]),
  });
  const hasBeneath = createMemo(() => (view.data ? (childrenIndex(view.data).get(id()) ?? []).length > 0 : false));

  const tabs = createMemo(() => [
    { key: "overview", label: "Overview" },
    ...(can(me.data, "location", "update") ? [{ key: "configure", label: "Configure" }] : []),
  ]);
  const tab = () => {
    const t = param("tab");
    if (t && tabs().some((x) => x.key === t)) return t;
    if (param("edit") === "1" && tabs().some((x) => x.key === "configure")) return "configure";
    return "overview";
  };
  const configuring = () => param("tab") === "configure" || param("edit") === "1";

  // A name-shaped address resolves to the uuid, query kept (#759's rule).
  // Only an unambiguous name resolves: names scope to placement, so a bare
  // name can legally be two rows.
  createEffect(() => {
    if (!view.data || place()) return;
    const matches = (view.data.locations ?? []).filter((l) => l.name === id());
    if (matches.length === 1) navigate(`/locations/${matches[0].id}${window.location.search}`, { replace: true });
  });
  // A place holding one system lands on it, query kept.
  createEffect(() => {
    if (!view.data || !place() || configuring()) return;
    const landing = landingFor(view.data, id());
    if (landing.kind === "system") navigate(`/systems/${landing.id}${window.location.search}`, { replace: true });
  });

  const crumbs = createMemo(() => {
    if (!view.data) return [];
    const chain = ancestors(id(), locationIndex(view.data));
    return [
      { key: "explore", label: "Explore", onClick: () => navigate("/explore") },
      // The trail ends at the parent: the place itself is the page title.
      ...chain.slice(0, -1).map((l) => ({ key: l.id, label: entityLabel(l), onClick: () => navigate(`/locations/${l.id}`) })),
    ];
  });

  // A miss is judged only once the fleet view is current: the create
  // handoff lands here while the cached view predates the new row.
  const missing = () => !!view.data && !view.isFetching && !place() && !(view.data.locations ?? []).some((x) => x.name === id());

  return (
    <BladesContext.Provider value={blades}>
      <Page title={place() ? entityLabel(place()!) : "Location"} breadcrumb={<Breadcrumb crumbs={crumbs()} />}>
        <DetailGate
          noun="location"
          missing={missing()}
          pending={view.isPending}
          error={view.isError ? describeError(view.error) : null}
          retrying={view.isFetching}
          onRetry={() => void view.refetch()}
        >
          <section data-testid="place-subject" class="card overflow-hidden border border-base-300 bg-base-200 p-0">
            <div data-testid="place-header" class="flex flex-wrap items-center gap-x-3 gap-y-1.5 border-b border-base-300 px-4 py-3 text-sm">
              <Eyebrow label="Place" hint="A place is a folder and its facts. The systems in it, and in the places beneath it, are what Omniglass monitors." />
              <span class="flex items-center gap-2">
                <span class="flex-none text-base-content/50"><Dynamic component={resolveIcon(type()?.icon || "map-pin")} size={16} /></span>
                <span data-testid="place-type" class="text-base-content/70">{type() ? entityLabel(type()!) : place()?.location_type}</span>
              </span>
              <Show when={Object.keys(tags()).length > 0}><TagPills tags={tags()} wrap /></Show>
            </div>
            <TabRail tabs={tabs()} activeKey={tab} />
            <Show when={tab() === "configure"}>
              <ConfigureFace kind="location" id={id()} />
            </Show>
            <Show when={tab() === "overview"}>
              <div data-testid="place-overview" class="flex flex-col gap-4 p-4">
                <Show when={systemsHere().length > 0}>
                  <section class="flex flex-col gap-2">
                    <Eyebrow label={systemsHere().length === 1 ? "System here" : "Systems here"} hint="Each system bound to this place, in brief. Open one for its components, map, data and history." />
                    <div class="grid grid-cols-[repeat(auto-fill,minmax(18rem,1fr))] gap-3">
                      <For each={systemsHere()}>{(sid) => <SystemSummary systemId={sid} />}</For>
                    </div>
                  </section>
                </Show>
                <Show when={hasBeneath() || systemsHere().length === 0}>
                  <section data-testid="beneath" class="flex flex-col gap-2">
                    <Show when={systemsHere().length > 0}>
                      <Eyebrow label="Beneath" hint="The places inside this one, as Explore draws them, counted by the systems they hold." />
                    </Show>
                    <OutlineWorkspace rootId={id()} />
                  </section>
                </Show>
              </div>
            </Show>
          </section>
        </DetailGate>
      </Page>
      <BladeStack controller={blades} registry={fleetRegistry} />
    </BladesContext.Provider>
  );
}
