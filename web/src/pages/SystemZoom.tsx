import { Show, createEffect, createMemo } from "solid-js";
import { useLocation, useNavigate, useParams } from "@solidjs/router";
import { useQuery } from "@tanstack/solid-query";
import Page from "../components/Page";
import Breadcrumb from "../components/Breadcrumb";
import DetailGate from "../components/DetailGate";
import TemplateNotice from "../components/TemplateNotice";
import BladeStack from "../components/BladeStack";
import SystemDetail from "../components/SystemDetail";
import { BladesContext, createBladeController } from "../lib/blades";
import { fleetRegistry } from "../lib/fleetBlades";
import { FLEET_VIEW_KEY, fleetView } from "../lib/fleet";
import { entityLabel } from "../lib/entities";
import { systemCrumbs, systemsAtLocation } from "../lib/detail";
import { describeError } from "../lib/format";

// The system's detail page (#636, reoriented in #872): the page is the
// address (the breadcrumb, a name-shaped address resolving to the uuid, the
// miss and ambiguity faces) around SystemDetail, the one body the fleet blade
// renders too (#2423). Systems are the unit Omniglass monitors, so a sole
// system's page wears its location's name, and its path ends at the
// location's parent, so the room is named once. The page owns a blade stack,
// so every member the body opens stacks here.
export default function SystemZoom() {
  const params = useParams<{ id: string }>();
  const navigate = useNavigate();
  // The router's location, not the window's: on a client-side navigation the
  // window's query lags the route being rendered (#932).
  const route = useLocation();
  const id = () => params.id;
  const blades = createBladeController();

  const view = useQuery(() => ({ queryKey: FLEET_VIEW_KEY, queryFn: fleetView }));
  const system = createMemo(() => (view.data?.systems ?? []).find((s) => s.id === id()));

  // A name-shaped address resolves to the uuid, query kept (#759's rule).
  createEffect(() => {
    if (!view.data || system()) return;
    const matches = (view.data.systems ?? []).filter((s) => s.name === id());
    if (matches.length === 1) navigate(`/systems/${matches[0].id}${route.search}`, { replace: true });
  });

  const locationId = () => system()?.location ?? null;
  const location = createMemo(() => (locationId() ? (view.data?.locations ?? []).find((l) => l.id === locationId()) : undefined));
  // A sole system is the room: the view wears the location's name. A shared
  // location keeps its own view, so a system there wears its own.
  const sole = createMemo(() => (view.data && locationId() ? systemsAtLocation(view.data, locationId()!).length === 1 : false));
  const title = () => (sole() && location() ? entityLabel(location()!) : system() ? entityLabel(system()!) : "System");

  const crumbs = createMemo(() => {
    const trail = view.data ? systemCrumbs(view.data, id()) : [];
    return [
      // Back up lands where you were: the outline opened down to this row.
      { key: "explore", label: "Explore", href: `/explore?node=${encodeURIComponent(id())}` },
      ...trail.map((c) => ({ key: c.id, label: c.label, href: `/locations/${c.id}` })),
    ];
  });

  // A miss is judged only once the fleet view is current: the create
  // handoff lands here while the cached view predates the new row.
  const ambiguous = () => !!view.data && !view.isFetching && !system() && (view.data.systems ?? []).filter((x) => x.name === id()).length > 1;
  const missing = () => !!view.data && !view.isFetching && !system() && !ambiguous();

  return (
    <BladesContext.Provider value={blades}>
    <Page title={title()} name={system()?.name} breadcrumb={<Breadcrumb crumbs={crumbs()} />}>
      {/* A create from a template lands here with ?from= (#1329). */}
      <TemplateNotice />
      <DetailGate
        noun="system"
        missing={missing()}
        ambiguous={ambiguous()}
        pending={view.isPending}
        error={view.error ? describeError(view.error) : null}
        retrying={view.isFetching}
        onRetry={() => void view.refetch()}
      >
        {/* Keyed on the id: another system is another body (#854). */}
        <Show when={system()?.id} keyed>{(sid) => <SystemDetail id={sid} host="page" />}</Show>
      </DetailGate>
    </Page>
    <BladeStack controller={blades} registry={fleetRegistry} />
    </BladesContext.Provider>
  );
}
