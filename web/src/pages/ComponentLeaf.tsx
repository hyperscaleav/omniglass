import { Show, createEffect, createMemo } from "solid-js";
import { useLocation, useNavigate, useParams } from "@solidjs/router";
import { useQuery } from "@tanstack/solid-query";
import Page from "../components/Page";
import Breadcrumb from "../components/Breadcrumb";
import BladeStack from "../components/BladeStack";
import DetailGate from "../components/DetailGate";
import TemplateNotice from "../components/TemplateNotice";
import ComponentDetail from "../components/ComponentDetail";
import { BladesContext, createBladeController } from "../lib/blades";
import { fleetRegistry } from "../lib/fleetBlades";
import { FLEET_VIEW_KEY, fleetView } from "../lib/fleet";
import { COMPONENTS_KEY, listComponents, type Component as FleetComponent } from "../lib/components";
import { componentSystemsKey, componentSystems } from "../lib/members";
import { membershipRows } from "../lib/component_leaf";
import { entityLabel } from "../lib/entities";
import { componentCrumbs } from "../lib/detail";
import { describeError } from "../lib/format";

// The component's detail page (#637, reoriented in #872): the page is the
// address (the breadcrumb, a name-shaped address resolving to the uuid, the
// miss and ambiguity faces) around ComponentDetail, the one body the fleet
// blade renders too (#2423). The page owns a blade stack, so the body's
// nested opens (a system it serves, an endpoint) stack here.
export default function ComponentLeaf() {
  const params = useParams<{ id: string }>();
  const navigate = useNavigate();
  // The router's location, not the window's: on a client-side navigation the
  // window's query lags the route being rendered (#932).
  const location = useLocation();
  const id = () => params.id;
  const blades = createBladeController();

  const view = useQuery(() => ({ queryKey: FLEET_VIEW_KEY, queryFn: fleetView }));
  const components = useQuery(() => ({ queryKey: COMPONENTS_KEY, queryFn: listComponents }));

  const component = createMemo<FleetComponent | undefined>(() => {
    const all = components.data ?? [];
    const byId = all.find((c) => c.id === id());
    if (byId) return byId;
    const byName = all.filter((c) => c.name === id());
    return byName.length === 1 ? byName[0] : undefined;
  });

  // The leaf is addressed by uuid; a name-shaped address resolves through the
  // component list when unique, keeping the param (#759's rule).
  createEffect(() => {
    if (!components.data) return;
    const c = component();
    if (c && c.id !== id()) navigate(`/components/${c.id}${location.search}`, { replace: true });
  });

  // The path: the component's location, or its primary system's standing in.
  const memberships = useQuery(() => ({
    queryKey: componentSystemsKey(component()?.id ?? id()),
    queryFn: () => componentSystems(component()?.id ?? id()),
    enabled: !!component(),
  }));
  const rows = createMemo(() => (view.data && memberships.data ? membershipRows(memberships.data, view.data) : []));
  const primary = createMemo(() => rows().find((r) => r.primary) ?? rows()[0]);
  const locationId = () => {
    const sid = primary()?.systemId;
    return component()?.location_id ?? (sid ? (view.data?.systems ?? []).find((s) => s.id === sid)?.location ?? null : null);
  };
  const crumbs = createMemo(() => {
    if (!view.data) return [];
    const trail = componentCrumbs(view.data, locationId(), primary()?.systemId);
    return [
      // Back up lands where you were: the outline opened down to this row.
      { key: "explore", label: "Explore", href: `/explore?node=${encodeURIComponent(component()?.id ?? id())}` },
      ...trail.map((c) => ({ key: c.id, label: c.label, href: c.kind === "system" ? `/systems/${c.id}` : `/locations/${c.id}` })),
    ];
  });

  const ambiguous = () => !!(view.data && components.data) && !component() && (components.data ?? []).filter((x) => x.name === id()).length > 1;
  const missing = () => !!(view.data && components.data) && !component() && !ambiguous();
  const failure = () => view.error ?? components.error;

  return (
    <BladesContext.Provider value={blades}>
    <Page title={component() ? entityLabel(component()!) : "Component"} name={component()?.name} breadcrumb={<Breadcrumb crumbs={crumbs()} />}>
      {/* A create from a template lands here with ?from= (#1329). */}
      <TemplateNotice />
      <DetailGate
        noun="component"
        missing={missing()}
        ambiguous={ambiguous()}
        pending={view.isPending || components.isPending}
        error={failure() ? describeError(failure()) : null}
        retrying={view.isFetching || components.isFetching}
        onRetry={() => { void view.refetch(); void components.refetch(); }}
      >
        {/* Keyed on the id: another component is another body (#854). */}
        <Show when={component()?.id} keyed>{(cid) => <ComponentDetail id={cid} host="page" />}</Show>
      </DetailGate>
    </Page>
    <BladeStack controller={blades} registry={fleetRegistry} />
    </BladesContext.Provider>
  );
}
