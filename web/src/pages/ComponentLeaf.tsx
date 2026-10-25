import { For, Show, createEffect, createMemo } from "solid-js";
import { useLocation, useNavigate, useParams, useSearchParams } from "@solidjs/router";
import { useQueries, useQuery } from "@tanstack/solid-query";
import Page from "../components/Page";
import Breadcrumb from "../components/Breadcrumb";
import TabRail from "../components/TabRail";
import ConfigureFace from "../components/ConfigureFace";
import BladeStack from "../components/BladeStack";
import EventsPanel from "../components/EventsPanel";
import CommandsPanel from "../components/CommandsPanel";
import { BladesContext, createBladeController } from "../lib/blades";
import { fleetRegistry } from "../lib/fleetBlades";
import HealthBadge from "../components/HealthBadge";
import Eyebrow from "../components/Eyebrow";
import DetailGate from "../components/DetailGate";
import PlaceCard from "../components/PlaceCard";
import AcknowledgeButton from "../components/AcknowledgeButton";
import AlarmTimeline, { createTimelineToggles } from "../components/AlarmTimeline";
import SeverityBadge from "../components/SeverityBadge";
import { useSeverity } from "../lib/severity";
import SimulationSection from "../components/SimulationSection";
import ReachLadder from "../components/ReachLadder";
import SessionPill from "../components/SessionPill";
import SessionNotice from "../components/SessionNotice";
import { FLEET_VIEW_KEY, fleetView } from "../lib/fleet";
import { COMPONENTS_KEY, listComponents, type Component as FleetComponent } from "../lib/components";
import { componentSystemsKey, componentSystems } from "../lib/members";
import { REACHABILITY_KEY, getReachability, lastSeenAt, type ReachEndpoint } from "../lib/reachability";
import { componentAlarmsRead, componentAlarmsReadKey, splitAlarms } from "../lib/alarms";
import { effectiveProperties, effectivePropertiesKey } from "../lib/component_properties";
import { effectiveMetrics, effectiveMetricsKey } from "../lib/component_metrics";
import { CalcRuleLink } from "../components/RuleLink";
import { durationText } from "../lib/timeline";
import { getNode, nodeKey } from "../lib/nodes";
import { PRODUCTS_KEY, listProducts } from "../lib/products";
import { LEAF_NODE_POLL_MS, LEAF_POLL_MS, collectionState, dotVerdict, identityRows, leafAlarmSince, membershipRows, vitalRows } from "../lib/component_leaf";
import { entityLabel } from "../lib/entities";
import { CLOCK_TICK_MS, createClock } from "../lib/clock";
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
// stated here plainly (a healthy node with a stale sample means the component or
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
  // The router's location, not the window's: on a client-side navigation the
  // window's query lags the route being rendered (#932).
  const location = useLocation();
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
  // A command asked for by address (an issue's toast opens its row).
  const openCommand = () => {
    const raw = Array.isArray(leafSearch.command) ? leafSearch.command[0] : leafSearch.command;
    const n = raw ? Number(raw) : NaN;
    return Number.isInteger(n) && n > 0 ? n : undefined;
  };
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

  const component = createMemo<FleetComponent | undefined>(() =>
    (components.data ?? []).find((c) => c.id === id() || c.name === id()),
  );

  // The leaf is addressed by uuid; a name-shaped address resolves through the
  // component list when unique, keeping the param (#759's rule).
  createEffect(() => {
    if (!components.data) return;
    const c = component();
    if (c && c.id !== id()) navigate(`/components/${c.id}${location.search}`, { replace: true });
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
    refetchInterval: LEAF_POLL_MS,
  }));
  const alarmsQ = useQuery(() => ({
    queryKey: componentAlarmsReadKey(id()),
    queryFn: () => componentAlarmsRead(id()),
    enabled: !!component(),
    refetchInterval: LEAF_POLL_MS,
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
    refetchInterval: LEAF_POLL_MS,
  }));
  // Every age on the card reads one ticking clock: a poll that returns the
  // same data changes nothing a query can see, so an age pinned to the data
  // would keep reading fresh after its node died.
  const pageNow = createClock(CLOCK_TICK_MS);
  const scale = useSeverity();
  // Each alarm row opens its timeline in place (#950).
  const timelines = createTimelineToggles();
  const activeAlarms = createMemo(() => splitAlarms(alarmsQ.data?.alarms ?? [], scale().levels).active);
  // The strip reads red when any active alarm is at an outage level (the
  // registry's flag, #917), amber when they only degrade.
  const inOutage = createMemo(() => activeAlarms().some((a) => scale().outage(a.severity)));
  const since = createMemo(() => leafAlarmSince(alarmsQ.data?.alarms ?? [], pageNow(), scale().levels));
  // The component's own verdict rides on its alarms read, polled; the fleet
  // view's dot (read once, for the place and the path) stands in until then.
  const verdict = createMemo(() => alarmsQ.data?.verdict ?? (view.data ? dotVerdict(view.data, component()?.id ?? "") : null));
  const identity = createMemo(() => identityRows(propsQ.data ?? []));
  const vitals = createMemo(() => vitalRows(metricsQ.data ?? []));

  const product = createMemo(() => (products.data ?? []).find((p) => p.name === component()?.product));
  const rows = createMemo(() => (view.data && memberships.data ? membershipRows(memberships.data, view.data) : []));
  // The node behind each endpoint, read one by one: a component has a node
  // or two, the fleet has many, and only these are on the card.
  const nodeNames = createMemo(() => [...new Set((reach.data?.endpoints ?? []).map((e) => e.node).filter((n): n is string => !!n))], undefined, {
    equals: (a, b) => a.length === b.length && a.every((x, i) => x === b[i]),
  });
  const nodeReads = useQueries(() => ({
    queries: nodeNames().map((n) => ({ queryKey: nodeKey(n), queryFn: () => getNode(n), refetchInterval: LEAF_NODE_POLL_MS })),
  }));
  const nodeByName = createMemo(() => new Map(nodeReads.filter((q) => q.data).map((q) => [q.data!.name, q.data!])));
  // The collection rows are kept by endpoint id, never by the objects a poll
  // hands back (each poll is fresh: last_seen moves), so a row's ladder
  // tooltip and a retry waiting on its session survive the next poll (#1059).
  const reachKey = (it: ReachEndpoint, i: number) => it.id ?? `${it.endpoint}#${i}`;
  const reachKeys = createMemo(() => (reach.data?.endpoints ?? []).map(reachKey), undefined, {
    equals: (a, b) => a.length === b.length && a.every((x, i) => x === b[i]),
  });
  const reachByKey = createMemo(() => new Map((reach.data?.endpoints ?? []).map((it, i) => [reachKey(it, i), it])));
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
              <div data-testid="activity-tab" class="flex flex-col gap-5 p-4">
                {/* What it was told and where each command stands (#1155),
                    above what it has been saying. */}
                <Show when={can(me.data, "command", "read")}>
                  <CommandsPanel component={id()} name={component()?.name ?? id()} componentId={component()?.id} open={openCommand()} />
                </Show>
                <EventsPanel name={id()} />
              </div>
            </Show>
            <Show when={leafTab() === "overview"}>
              <div class="flex min-w-0 flex-1 flex-col gap-5 p-4">
                <Show when={activeAlarms().length > 0}>
                  <section data-testid="leaf-alarms" class="flex flex-col gap-1.5 rounded-box border p-3" classList={{ "border-error/40 bg-error/5": inOutage(), "border-warning/40 bg-warning/5": !inOutage() }}>
                    <h2 class="eyebrow">Why</h2>
                    <For each={activeAlarms()}>
                      {(a) => (
                        <div class="flex flex-col gap-1.5">
                          <div class="flex flex-wrap items-baseline gap-x-2 gap-y-0.5 text-sm">
                            <SeverityBadge name={a.severity} />
                            <span>{a.message}</span>
                            <span class="text-xs text-base-content/50">
                              {durationText(pageNow() - Date.parse(a.raised_at))} · {a.acknowledged ? "acknowledged" : "unacknowledged"}
                            </span>
                            <AcknowledgeButton component={component()?.id ?? id()} alarm={a} />
                            {timelines.button(a.id, a.message)}
                          </div>
                          <Show when={timelines.isOpen(a.id)}>
                            <AlarmTimeline component={component()?.id ?? id()} alarmId={a.id} id={timelines.panelId(a.id)} label={timelines.label(a.message)} />
                          </Show>
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
                        <dd class="flex items-baseline gap-1.5 font-mono text-xs">
                          <span>{String(row.value)}</span>
                          <Show when={row.sourceRuleId}>
                            <CalcRuleLink name={row.sourceRule!} id={row.sourceRuleId!} />
                          </Show>
                        </dd>
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
                <Eyebrow label="Vitals" hint="The latest sample per series; a dot marks the component speaking, not a contract default standing in." />
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
                          <Show when={row.sourceRuleId}>
                            <CalcRuleLink name={row.sourceRule!} id={row.sourceRuleId!} />
                          </Show>
                        </dd>
                      </>
                    )}
                  </For>
                </dl>
              </section>
            </Show>
            <section data-testid="leaf-collection" class="rounded-box border border-base-300 bg-base-100 p-3.5 text-sm">
              <Eyebrow label="Collection" hint="How this component is read: each endpoint, the node collecting it, and the ladder it climbs bottom up (a ping, informational only; the transport answering, the gate; then the login). A held session's state sits beside it. A stale sample under a healthy node points at the component or the path, not at collection." />
              <Show when={reach.data && (reach.data.endpoints ?? []).length > 0} fallback={<p class="text-base-content/60">No endpoint declared yet, so nothing collects from this component.</p>}>
                <ul class="mt-1 flex flex-col gap-1.5">
                  <For each={reachKeys()}>
                    {(key) => (
                      <Show when={reachByKey().get(key)}>
                        {(iface) => {
                          const state = () => collectionState(iface(), iface().node ? nodeByName().get(iface().node!) : undefined, pageNow());
                          return (
                            <li class="flex flex-col gap-1.5">
                              <div class="flex flex-wrap items-center gap-2 text-xs">
                                <span class="min-w-14 font-medium">{iface().endpoint}</span>
                                {/* The ladder (#1059), so component-versus-path
                                    reads at a glance and the layered gate is
                                    visible: the ping answers the path, the
                                    transport answers the service, the login
                                    answers the credential. */}
                                <ReachLadder iface={iface()} />
                                <SessionPill iface={iface()} now={pageNow()} />
                                <Show when={iface().node}>
                                  <span class="text-base-content/60">via {iface().node}</span>
                                </Show>
                                <Show when={lastSeenAt(iface())}>
                                  {(ts) => <span class="text-base-content/50">last seen {ageWords(ts(), pageNow())}</span>}
                                </Show>
                                <span data-testid={`collection-${iface().endpoint}`} class="rounded border border-base-content/20 px-1.5 py-0.5">
                                  {
                                    {
                                      collecting: "collecting",
                                      "component-or-path": "stale sample; the node is healthy, so check the component or the path",
                                      "node-offline": "node offline",
                                      down: "component reports down",
                                      rejected: "polls stopped",
                                      "session-down": "polls failing",
                                      unknown: "no sample yet",
                                    }[state().kind]
                                  }
                                </span>
                              </div>
                              <SessionNotice
                                iface={iface()}
                                component={component()?.id ?? id()}
                                now={pageNow()}
                                onFix={iface().id && can(me.data, "endpoint", "read") ? () => blades.push({ kind: "endpoint", id: iface().id! }) : undefined}
                                fixLabel="Open endpoint"
                              />
                            </li>
                          );
                        }}
                      </Show>
                    )}
                  </For>
                </ul>
              </Show>
            </section>
            {/* A simulated component says so, with its live hits (#890). */}
            <Show when={component()}>{(c) => <SimulationSection componentId={c().id} />}</Show>
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
