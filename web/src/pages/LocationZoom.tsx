import { Show, createEffect, createMemo } from "solid-js";
import { useLocation, useNavigate, useParams } from "@solidjs/router";
import { useQuery } from "@tanstack/solid-query";
import Page from "../components/Page";
import TemplateNotice from "../components/TemplateNotice";
import Breadcrumb from "../components/Breadcrumb";
import DetailGate from "../components/DetailGate";
import LocationDetail from "../components/LocationDetail";
import BladeStack from "../components/BladeStack";
import { BladesContext, createBladeController } from "../lib/blades";
import { fleetRegistry } from "../lib/fleetBlades";
import { FLEET_VIEW_KEY, ancestors, fleetView, locationIndex } from "../lib/fleet";
import { landingFor } from "../lib/detail";
import { entityLabel } from "../lib/entities";
import { describeError } from "../lib/format";

// A location's detail view (#635, reoriented in #872). Systems are the unit
// Omniglass monitors and locations are folders and metadata, so a location holding
// exactly one system has no view of its own: its address lands on that
// system, whose view shows the location as a card of context. What remains here
// is the folder: a location holding no system (a campus, a building, a floor),
// or one shared by several. The page is the address (the breadcrumb, the
// name and sole-system landings, the miss faces) around LocationDetail, the
// one body the fleet blade renders too (#2423).
//
// A location holding one system configures on that system's Configure tab, in a
// Location section of its own (#872), so even an address asking to configure
// the location lands there: ?tab=configure opens the tab, and ?edit=1 carries
// over as edit=location, the location form's own intent.
export default function LocationZoom() {
  const params = useParams<{ id: string }>();
  const navigate = useNavigate();
  // The router's location, not the window's: on a client-side navigation the
  // window's query lags the route being rendered (#932).
  const route = useLocation();
  const id = () => params.id;
  const blades = createBladeController();

  const view = useQuery(() => ({ queryKey: FLEET_VIEW_KEY, queryFn: fleetView }));

  const location = createMemo(() => (view.data ? locationIndex(view.data).get(id()) : undefined));
  // A name-shaped address resolves to the uuid, query kept (#759's rule).
  // Only an unambiguous name resolves: names scope to placement, so a bare
  // name can legally be two rows.
  createEffect(() => {
    if (!view.data || location()) return;
    const matches = (view.data.locations ?? []).filter((l) => l.name === id());
    if (matches.length === 1) navigate(`/locations/${matches[0].id}${route.search}`, { replace: true });
  });
  // A location holding one system lands on it, query kept; a configure intent
  // becomes the system's Configure tab and the location's own edit.
  createEffect(() => {
    if (!view.data || !location()) return;
    const landing = landingFor(view.data, id());
    if (landing.kind !== "system") return;
    const q = new URLSearchParams(route.search);
    if (q.get("edit") === "1") {
      q.delete("edit");
      q.set("tab", "configure");
      q.set("edit", "location");
    }
    const qs = q.toString();
    navigate(`/systems/${landing.id}${qs ? `?${qs}` : ""}`, { replace: true });
  });

  const crumbs = createMemo(() => {
    if (!view.data) return [];
    const chain = ancestors(id(), locationIndex(view.data));
    return [
      // Back up lands where you were: the outline opened down to this row.
      { key: "explore", label: "Explore", href: `/explore?node=${encodeURIComponent(id())}` },
      // The trail ends at the parent: the location itself is the page title.
      ...chain.slice(0, -1).map((l) => ({ key: l.id, label: entityLabel(l), href: `/locations/${l.id}` })),
    ];
  });

  // A miss is judged only once the fleet view is current: the create
  // handoff lands here while the cached view predates the new row.
  const ambiguous = () => !!view.data && !view.isFetching && !location() && (view.data.locations ?? []).filter((x) => x.name === id()).length > 1;
  const missing = () => !!view.data && !view.isFetching && !location() && !ambiguous();

  return (
    <BladesContext.Provider value={blades}>
      <Page title={location() ? entityLabel(location()!) : "Location"} name={location()?.name} breadcrumb={<Breadcrumb crumbs={crumbs()} />}>
        {/* A create from a template lands here with ?from= (#1329). */}
        <TemplateNotice />
        <DetailGate
          noun="location"
          missing={missing()}
          ambiguous={ambiguous()}
          pending={view.isPending}
          error={view.isError ? describeError(view.error) : null}
          retrying={view.isFetching}
          onRetry={() => void view.refetch()}
        >
          {/* Keyed on the id: another location is another body (#854). */}
          <Show when={location()?.id} keyed>{(lid) => <LocationDetail id={lid} host="page" />}</Show>
        </DetailGate>
      </Page>
      <BladeStack controller={blades} registry={fleetRegistry} />
    </BladesContext.Provider>
  );
}
