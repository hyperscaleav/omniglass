import { For, Show, createEffect, createMemo } from "solid-js";
import { useNavigate, useParams, useSearchParams } from "@solidjs/router";
import { useQueries, useQuery } from "@tanstack/solid-query";
import Page from "../components/Page";
import Breadcrumb from "../components/Breadcrumb";
import TabRail from "../components/TabRail";
import ConfigureFace from "../components/ConfigureFace";
import BladeStack from "../components/BladeStack";
import EventsPanel from "../components/EventsPanel";
import { BladesContext, createBladeController } from "../lib/blades";
import { fleetRegistry } from "../lib/fleetBlades";
import HealthBadge from "../components/HealthBadge";
import Eyebrow from "../components/Eyebrow";
import DetailGate from "../components/DetailGate";
import PlaceCard from "../components/PlaceCard";
import AcknowledgeButton from "../components/AcknowledgeButton";
import { FLEET_VIEW_KEY, fleetView } from "../lib/fleet";
import { COMPONENTS_KEY, listComponents, type Component as FleetComponent } from "../lib/components";
import { componentSystemsKey, componentSystems } from "../lib/members";
import { REACHABILITY_KEY, getReachability, layerWord } from "../lib/reachability";
import { componentAlarms, componentAlarmsKey, splitAlarms } from "../lib/alarms";
import { effectiveProperties, effectivePropertiesKey } from "../lib/component_properties";
import { effectiveMetrics, effectiveMetricsKey } from "../lib/component_metrics";
import { durationText } from "../lib/timeline";
import { NODES_KEY, listNodes } from "../lib/nodes";
import { PRODUCTS_KEY, listProducts } from "../lib/products";
import { collectionState, dotVerdict, identityRows, leafAlarmSince, membershipRows, vitalRows } from "../lib/component_leaf";
import { entityLabel } from "../lib/entities";
import { systemRoles, systemRolesKey } from "../lib/system_roles";
import { componentCrumbs, isRoomOf, rolesOf } from "../lib/detail";
import { can, useMe } from "../lib/auth";
import { describeError, fmtTime } from "../lib/format";

// The component's detail view (#637, reoriented in #872): a component is a
// piece of a system. Its place is a card of context (its own, or its system's
// standing in for one it does not have, and the card says which); then the
// component itself: verdict, since-when (what took it down), product, and its
// three tabs. Overview reads why, what it is, the systems it serves and the
// role it fills in each, its vitals, and collection in context: the one place
// a node appears to an operator, because the distinction that matters is
// stated here plainly (a healthy node with a stale sample means the device or
// the path, not collection), which is why nodes need no view of their own.
// ageWords renders a sample age in plain words, coarse on purpose: the card
// answers "how stale", not "how many milliseconds".
function ageWords(ts: string, now: number = Date.now()): string {
  const s = Math.max(0, Math.round((now - new Date(ts).getTime()) / 1000));
  if (s < 90) return `${s} s ago`;
  const m = Math.round(s / 60);
  if (m < 90) return `${m} min ago`;
  const h = Math.round(m / 60);
  if (h < 36) return `${h} h ago`;
  return `${Math.round(h / 24)} d ago`;
}

export default function ComponentLeaf() {
  const params = useParams<{ id: string }>();
  const navigate = useNavigate();
  const id = () => params.id;
  const me = useMe();
  const blades = createBladeController();
  const [leafSearch] = useSearchParams();
  // Three tabs (#826): Overview, Activity, Configure. A legacy ?tab=events
  // address lands on Activity, which absorbed it.
  const leafTabs = createMemo(() => [
    { key: "overview", label: "Overview" },
    { key: "activity", label: "Activity" },
    ...(can(me.data, "component", "update") ? [{ key: "configure", label: "Configure" }] : []),
  ]);
  const LEGACY_TAB: Record<string, string> = { events: "activity" };
  const leafTab = () => {
    const raw = Array.isArray(leafSearch.tab) ? leafSearch.tab[0] : leafSearch.tab;
    const t = raw ? (LEGACY_TAB[raw] ?? raw) : raw;
    if (t && leafTabs().some((x) => x.key === t)) return t;
    const editing = (Array.isArray(leafSearch.edit) ? leafSearch.edit[0] : leafSearch.edit) === "1";
    if (editing && leafTabs().some((x) => x.key === "configure")) return "configure";
    return "overview";
  };

  const view = useQuery(() => ({ queryKey: FLEET_VIEW_KEY, queryFn: fleetView }));
  const components = useQuery(() => ({ queryKey: COMPONENTS_KEY, queryFn: listComponents }));
  const products = useQuery(() => ({ queryKey: PRODUCTS_KEY, queryFn: listProducts }));
  const nodes = useQuery(() => ({ queryKey: NODES_KEY, queryFn: listNodes }));

  const component = createMemo<FleetComponent | undefined>(() =>
    (components.data ?? []).find((c) => c.id === id() || c.name === id()),
  );

  // The leaf is addressed by uuid; a name-shaped address resolves through the
  // component list when unique, keeping the param (#759's rule).
  createEffect(() => {
    if (!components.data) return;
    const c = component();
    if (c && c.id !== id()) navigate(`/components/${c.id}${window.location.search}`, { replace: true });
  });

  const memberships = useQuery(() => ({
    queryKey: componentSystemsKey(id()),
    queryFn: () => componentSystems(id()),
    enabled: !!component(),
  }));
  const reach = useQuery(() => ({
    queryKey: REACHABILITY_KEY(id()),
    queryFn: () => getReachability(id()),
    enabled: !!component(),
  }));
  const alarmsQ = useQuery(() => ({
    queryKey: componentAlarmsKey(id()),
    queryFn: () => componentAlarms(id()),
    enabled: !!component(),
  }));
  const propsQ = useQuery(() => ({
    queryKey: effectivePropertiesKey(id()),
    queryFn: () => effectiveProperties(id()),
    enabled: !!component(),
  }));
  const metricsQ = useQuery(() => ({
    queryKey: effectiveMetricsKey(id()),
    queryFn: () => effectiveMetrics(id()),
    enabled: !!component(),
  }));
  // Pinned at setup: an age that re-ages on unrelated re-renders is churn.
  const pageNow = Date.now();
  const activeAlarms = createMemo(() => splitAlarms(alarmsQ.data ?? []).active);
  const since = createMemo(() => leafAlarmSince(alarmsQ.data ?? [], pageNow));
  const verdict = createMemo(() => (view.data ? dotVerdict(view.data, component()?.id ?? "") : null));
  const identity = createMemo(() => identityRows(propsQ.data ?? []));
  const vitals = createMemo(() => vitalRows(metricsQ.data ?? []));

  const product = createMemo(() => (products.data ?? []).find((p) => p.name === component()?.product));
  const rows = createMemo(() => (view.data && memberships.data ? membershipRows(memberships.data, view.data) : []));
  const nodeByName = createMemo(() => new Map((nodes.data ?? []).map((n) => [n.name, n])));
  // The role this component fills in each system it serves: a membership
  // says THAT it belongs; the system's declared roles say what it does there.
  const roleReads = useQueries(() => ({
    queries: rows().filter((r) => r.systemId).map((r) => ({
      queryKey: systemRolesKey(r.systemId!),
      queryFn: () => systemRoles(r.systemId!),
      staleTime: 30_000,
    })),
  }));
  const rolesIn = (systemId: string | null) => {
    if (!systemId) return [];
    const i = rows().filter((r) => r.systemId).findIndex((r) => r.systemId === systemId);
    const data = i >= 0 ? roleReads[i]?.data : undefined;
    return data ? rolesOf(component()?.name ?? "", data) : [];
  };

  const primary = createMemo(() => rows().find((r) => r.primary) ?? rows()[0]);
  // Its own place, or (when it has none) its primary system's standing in.
  const systemPlace = createMemo(() => {
    const sid = primary()?.systemId;
    return sid ? (view.data?.systems ?? []).find((s) => s.id === sid)?.location ?? null : null;
  });
  const placeId = () => component()?.location_id ?? systemPlace();
  const provenance = () => (component()?.location_id ? "set here" : systemPlace() ? "from its system" : undefined);

  const crumbs = createMemo(() => {
    if (!view.data) return [];
    const trail = componentCrumbs(view.data, placeId(), primary()?.systemId);
    return [
      // Back up lands where you were: the outline opened down to this row.
      { key: "explore", label: "Explore", onClick: () => navigate(`/explore?node=${encodeURIComponent(component()?.id ?? id())}`) },
      ...trail.map((c) => ({ key: c.id, label: c.label, onClick: () => navigate(c.kind === "system" ? `/systems/${c.id}` : `/locations/${c.id}`) })),
    ];
  });

  const missing = () => !!(view.data && components.data) && !component() && !(components.data ?? []).some((x) => x.name === id());
  const failure = () => view.error ?? components.error;

  return (
    <BladesContext.Provider value={blades}>
    <Page title={component() ? entityLabel(component()!) : "Component"} breadcrumb={<Breadcrumb crumbs={crumbs()} />}>
      <DetailGate
        noun="component"
        missing={missing()}
        pending={view.isPending || components.isPending}
        error={failure() ? describeError(failure()) : null}
        retrying={view.isFetching || components.isFetching}
        onRetry={() => { void view.refetch(); void components.refetch(); }}
      >
        <div class="flex flex-col gap-3">
          {/* In the room a system it serves makes, the place and the system
              are one thing, named under Systems it serves and in the path. */}
          <Show when={placeId() && !(view.data && isRoomOf(view.data, placeId(), rows().map((r) => r.systemId)))}>
            <PlaceCard placeId={placeId()!} showName provenance={provenance()} />
          </Show>
          <section data-testid="component-card" class="card overflow-hidden border border-base-300 bg-base-200 p-0">
            {/* State, age, identity: the header every detail view opens with.
                A component has no transitions read, so since-when is what
                took it down: the worst active alarm's age (#786). */}
            <div data-testid="leaf-header" class="flex flex-wrap items-center gap-x-3 gap-y-1 border-b border-base-300 px-4 py-3 text-sm">
              <Eyebrow label="Component" hint="A piece of a system. Its health is what its own checks and alarms say; the systems it serves read it through the roles it fills." />
              <HealthBadge verdict={verdict() ?? undefined} size="sm" />
              <Show when={since()}>
                {(sc) => <span data-testid="since-line" class="tabular-nums text-base-content/70">since {fmtTime(sc().ts)} · {durationText(sc().ms)}</span>}
              </Show>
              <span class="text-base-content/30">·</span>
              <span class="font-data text-xs text-base-content/60">{component()?.name}</span>
            </div>
            <TabRail tabs={leafTabs()} activeKey={leafTab} />
            <Show when={leafTab() === "configure"}>
              <ConfigureFace kind="component" id={id()} />
            </Show>
            <Show when={leafTab() === "activity"}>
              <div data-testid="activity-tab" class="p-4"><EventsPanel name={id()} /></div>
            </Show>
            <Show when={leafTab() === "overview"}>
              <div class="flex min-w-0 flex-1 flex-col gap-5 p-4">
                <Show when={activeAlarms().length > 0}>
                  <section data-testid="leaf-alarms" class="flex flex-col gap-1.5 rounded-box border p-3" classList={{ "border-error/40 bg-error/5": activeAlarms()[0].severity === "critical", "border-warning/40 bg-warning/5": activeAlarms()[0].severity !== "critical" }}>
                    <h2 class="eyebrow">Why</h2>
                    <For each={activeAlarms()}>
                      {(a) => (
                        <div class="flex flex-wrap items-baseline gap-x-2 gap-y-0.5 text-sm">
                          <span class="badge badge-sm" classList={{ "badge-error badge-soft": a.severity === "critical", "badge-warning badge-soft": a.severity !== "critical" }}>{a.severity}</span>
                          <span>{a.message}</span>
                          <span class="text-xs text-base-content/50">
                            {durationText(pageNow - Date.parse(a.raised_at))} · {a.acknowledged ? "acknowledged" : "unacknowledged"}
                          </span>
                          <AcknowledgeButton component={component()?.id ?? id()} alarm={a} />
                        </div>
                      )}
                    </For>
                  </section>
                </Show>
                <div class="grid grid-cols-1 gap-3 md:grid-cols-2">
              <section data-testid="leaf-identity" class="rounded-box border border-base-300 bg-base-100 p-3.5 text-sm">
                <Eyebrow label="What it is" />
                <dl class="mt-2 grid grid-cols-[6rem_1fr] gap-x-3 gap-y-1">
                  <dt class="text-base-content/50">Product</dt>
                  <dd class="flex flex-wrap items-baseline gap-x-2">
                    <span>{product() ? entityLabel(product()!) : (component()?.product ?? "no product")}</span>
                    <Show when={product() && entityLabel(product()!) !== product()!.name}>
                      <span class="font-mono text-xs text-base-content/45">{product()!.name}</span>
                    </Show>
                  </dd>
                  <Show when={product()?.vendor}>
                    <dt class="text-base-content/50">Vendor</dt>
                    <dd>{product()!.vendor}</dd>
                  </Show>
                  <Show when={product()?.driver}>
                    <dt class="text-base-content/50">Driver</dt>
                    <dd class="font-mono text-xs">{product()!.driver}</dd>
                  </Show>
                  {/* The resolved properties with a value: serial, firmware,
                      model, whatever the contract answered. The RMA facts. */}
                  <For each={identity()}>
                    {(row) => (
                      <>
                        <dt class="text-base-content/50">{row.label}</dt>
                        <dd class="font-mono text-xs">{String(row.value)}</dd>
                      </>
                    )}
                  </For>
                </dl>
              </section>
                  <section data-testid="leaf-memberships" class="rounded-box border border-base-300 bg-base-100 p-3.5 text-sm">
                    <Eyebrow label="Systems it serves" hint="Each system this component belongs to, and the role it fills there. The primary is the system answered when a question names none." />
                    <Show when={rows().length > 0} fallback={<p class="mt-2 text-base-content/60">Not in any system yet.</p>}>
                      <ul class="mt-2 divide-y divide-base-300 rounded-field border border-base-300">
                        <For each={rows()}>
                          {(row) => (
                            <li class="flex flex-wrap items-center gap-x-2 gap-y-0.5 px-3 py-1.5">
                              <Show when={row.systemId} fallback={<span>{row.label}</span>}>
                                <button type="button" class="cursor-pointer hover:underline" onClick={() => navigate(`/systems/${row.systemId}`)}>{row.label}</button>
                              </Show>
                              <For each={rolesIn(row.systemId)}>
                                {(r) => <span class="badge badge-ghost badge-sm">{r}</span>}
                              </For>
                              <Show when={row.where}>
                                <span class="text-xs text-base-content/55">{row.where}</span>
                              </Show>
                              <Show when={row.primary && rows().length > 1}>
                                <span class="ml-auto rounded-field border border-primary/40 px-1.5 py-0.5 text-[10px] uppercase tracking-wider text-primary">primary</span>
                              </Show>
                            </li>
                          )}
                        </For>
                      </ul>
                    </Show>
                  </section>
                </div>
            <Show when={vitals().length > 0}>
              <section data-testid="leaf-vitals" class="rounded-box border border-base-300 bg-base-100 p-3.5 text-sm">
                <Eyebrow label="Vitals" hint="The latest sample per series; a dot marks the device speaking, not a contract default standing in." />
                <dl class="mt-2 grid grid-cols-[minmax(7rem,max-content)_1fr] gap-x-3 gap-y-1">
                  <For each={vitals()}>
                    {(row) => (
                      <>
                        <dt class="text-base-content/50">{row.label}</dt>
                        <dd class="flex items-baseline gap-1.5 font-mono text-xs">
                          <span>{String(row.value)}</span>
                          <Show when={row.sampled} fallback={<span class="text-[10px] text-base-content/40">contract default</span>}>
                            <span class="h-1.5 w-1.5 flex-none self-center rounded-full bg-success" title="live series" />
                          </Show>
                        </dd>
                      </>
                    )}
                  </For>
                </dl>
              </section>
            </Show>
            <section data-testid="leaf-collection" class="rounded-box border border-base-300 bg-base-100 p-3.5 text-sm">
              <Eyebrow label="Collection" hint="How this component is read: each interface, the node collecting it, and the layers that answer. A stale sample under a healthy node points at the device or the path, not at collection." />
              <Show when={reach.data && (reach.data.interfaces ?? []).length > 0} fallback={<p class="text-base-content/60">No interface declared yet, so nothing collects from this component.</p>}>
                <ul class="flex flex-col gap-1">
                  <For each={reach.data!.interfaces}>
                    {(iface) => {
                      const state = () => collectionState(iface, iface.node ? nodeByName().get(iface.node) : undefined);
                      return (
                        <li class="flex flex-wrap items-center gap-2 text-xs">
                          <span class="font-medium">{iface.interface}</span>
                          {/* The layer rungs, so device-versus-path reads at
                              a glance: ping answers the path, the port
                              answers the service. */}
                          <For each={iface.layers ?? []}>
                            {(l) => (
                              <span class="rounded border border-base-content/15 px-1 text-[10px] text-base-content/60">
                                {`${l.layer} ${layerWord(l)}`}
                              </span>
                            )}
                          </For>
                          <Show when={iface.node}>
                            <span class="text-base-content/60">via {iface.node}</span>
                          </Show>
                          <Show when={iface.verdict}>
                            <span class="text-base-content/50">last sample {ageWords(iface.verdict!.ts)}</span>
                          </Show>
                          <span data-testid={`collection-${iface.interface}`} class="rounded border border-base-content/20 px-1.5 py-0.5">
                            {
                              {
                                collecting: "collecting",
                                "device-or-path": "stale sample; the node is healthy, so check the device or the path",
                                "node-offline": "node offline",
                                down: "device reports down",
                                unknown: "no sample yet",
                              }[state().kind]
                            }
                          </span>
                        </li>
                      );
                    }}
                  </For>
                </ul>
              </Show>
            </section>
              </div>
            </Show>
          </section>
        </div>
      </DetailGate>
    </Page>
    <BladeStack controller={blades} registry={fleetRegistry} />
    </BladesContext.Provider>
  );
}
