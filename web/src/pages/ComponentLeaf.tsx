import { For, Show, createEffect, createMemo } from "solid-js";
import { useNavigate, useParams, useSearchParams } from "@solidjs/router";
import { useQuery } from "@tanstack/solid-query";
import Page from "../components/Page";
import Breadcrumb from "../components/Breadcrumb";
import TabRail from "../components/TabRail";
import ConfigureFace from "../components/ConfigureFace";
import HealthBadge from "../components/HealthBadge";
import FleetShell from "../components/FleetShell";
import { componentTileSpec } from "../lib/fleet_tiles";
import { createSignal } from "solid-js";
import type { Chip } from "../lib/predicate";
import { FLEET_VIEW_KEY, ancestors, fleetView, locationIndex } from "../lib/fleet";
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
import { can, useMe } from "../lib/auth";
import {describeError, fmtTime } from "../lib/format";

// The component leaf (#637): the end of the walk. What it is (product,
// vendor, driver), where it sits (the clickable ancestor chain), the
// memberships with the primary marked, and collection in context: the one
// place a node appears to an operator, because the distinction that matters
// is stated here plainly (a healthy node with a stale sample means the device
// or the path, not collection) and that sentence is why nodes need no zoom of
// their own.
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
  const [leafSearch] = useSearchParams();
  const leafTabs = createMemo(() => [
    { key: "overview", label: "Overview" },
    ...(can(me.data, "component", "update") ? [{ key: "configure", label: "Configure" }] : []),
  ]);
  const leafTab = () => {
    const t = Array.isArray(leafSearch.tab) ? leafSearch.tab[0] : leafSearch.tab;
    return t && leafTabs().some((x) => x.key === t) ? t : "overview";
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

  const tiles = createMemo(() =>
    view.data && component()
      ? componentTileSpec(view.data, component()!.id, activeAlarms().length, (reach.data?.interfaces ?? []).length)
      : undefined,
  );
  const [filterChips, setFilterChips] = createSignal<Chip[]>([]);

  const chain = createMemo(() => (view.data && component()?.location_id ? ancestors(component()!.location_id!, locationIndex(view.data)) : []));

  const crumbs = createMemo(() => {
    if (!view.data) return [];
    const chainList = chain();
    // Fleet / places / primary system: the walk this leaf sits at the end of.
    // The leaf itself is the page title, so it is not the last crumb.
    const primary = rows().find((r) => r.primary);
    return [
      { key: "fleet", label: "Fleet", onClick: () => navigate("/fleet") },
      ...chainList.map((l) => ({ key: l.id, label: entityLabel(l), onClick: () => navigate(`/locations/${l.id}`) })),
      ...(primary && primary.systemId ? [{ key: primary.systemId, label: primary.label, onClick: () => navigate(`/systems/${primary.systemId}`) }] : []),
    ];
  });

  return (
    <Page
      title={component() ? entityLabel(component()!) : "Component"}
      breadcrumb={<Breadcrumb crumbs={crumbs()} />}
    >
      <Show when={!view.isPending && !components.isPending} fallback={<div class="skeleton h-32 w-full" />}>
        <Show
          when={!view.isError && !components.isError}
          fallback={
            <div role="alert" class="alert alert-error alert-soft text-sm">
              {describeError(view.error ?? components.error)}
            </div>
          }
        >
          <div class="flex flex-col gap-3">
          <TabRail tabs={leafTabs()} />
          <Show when={leafTab() === "configure"}>
            <div class="card border border-base-300 bg-base-200 p-0"><ConfigureFace kind="component" id={id()} /></div>
          </Show>
          <Show when={leafTab() === "overview"}>
<FleetShell
            storageKey="fleet"
            tiles={tiles()}
            rows={[]}
            filterKeys={[]}
            chips={filterChips}
            onChips={setFilterChips}
          >
          <div class="flex min-w-0 flex-1 flex-col gap-5 p-4">
            {/* State, age, identity: the same header shape every zoom opens
                with. A component has no transitions read, so since-when is
                what took it down: the worst active alarm's age (#786). */}
            <div data-testid="leaf-header" class="flex flex-wrap items-center gap-x-3 gap-y-1 text-sm">
              <HealthBadge verdict={verdict() ?? undefined} size="sm" />
              <Show when={since()}>
                {(sc) => <span data-testid="since-line" class="tabular-nums text-base-content/70">since {fmtTime(sc().ts)} · {durationText(sc().ms)}</span>}
              </Show>
              <span class="font-mono text-xs text-base-content/60">{component()?.name}</span>
            </div>
            <Show when={activeAlarms().length > 0}>
              <section data-testid="leaf-alarms" class="flex flex-col gap-1.5 rounded-box border p-3" classList={{ "border-error/40 bg-error/5": activeAlarms()[0].severity === "critical", "border-warning/40 bg-warning/5": activeAlarms()[0].severity !== "critical" }}>
                <h2 class="eyebrow">Active alarms</h2>
                <For each={activeAlarms()}>
                  {(a) => (
                    <div class="flex flex-wrap items-baseline gap-x-2 gap-y-0.5 text-sm">
                      <span class="badge badge-sm" classList={{ "badge-error badge-soft": a.severity === "critical", "badge-warning badge-soft": a.severity !== "critical" }}>{a.severity}</span>
                      <span>{a.message}</span>
                      <span class="text-xs text-base-content/50">
                        {durationText(pageNow - Date.parse(a.raised_at))} · {a.acknowledged ? "acknowledged" : "unacknowledged"}
                      </span>
                    </div>
                  )}
                </For>
              </section>
            </Show>
            <div class="grid grid-cols-1 gap-3 md:grid-cols-2">
              <section data-testid="leaf-identity" class="rounded-box border border-base-300 bg-base-100 p-3.5 text-sm">
                <h2 class="eyebrow">What it is</h2>
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
              <section data-testid="leaf-placement" class="rounded-box border border-base-300 bg-base-100 p-3.5 text-sm">
                <h2 class="eyebrow">Where it sits</h2>
                <dl class="mt-2 grid grid-cols-[6rem_1fr] gap-x-3 gap-y-1">
                  <dt class="text-base-content/50">Location</dt>
                  <dd class="flex flex-wrap gap-1">
                    <For each={chain()}>
                      {(l, i) => (
                        <>
                          <Show when={i() > 0}><span class="text-base-content/30">/</span></Show>
                          <button type="button" class="cursor-pointer text-primary hover:underline" title={l.location_type} onClick={() => navigate(`/locations/${l.id}`)}>{entityLabel(l)}</button>
                        </>
                      )}
                    </For>
                    <Show when={chain().length === 0}><span class="text-base-content/50">unplaced</span></Show>
                  </dd>
                  <Show when={rows().find((r) => r.primary)}>
                    {(p) => (
                      <>
                        <dt class="text-base-content/50">System</dt>
                        <dd>
                          <Show when={p().systemId} fallback={p().label}>
                            <button type="button" class="cursor-pointer text-primary hover:underline" onClick={() => navigate(`/systems/${p().systemId}`)}>{p().label}</button>
                          </Show>
                        </dd>
                      </>
                    )}
                  </Show>
                </dl>
              </section>
            </div>

            <section data-testid="leaf-memberships" class="rounded-box border border-base-300 bg-base-100 p-3.5 text-sm">
              <h2 class="eyebrow">Slots it fills</h2>
              <Show when={rows().length > 0} fallback={<p class="text-base-content/60">Not in any system yet.</p>}>
                <ul class="mt-2 divide-y divide-base-300 rounded-field border border-base-300">
                  <For each={rows()}>
                    {(row) => (
                      <li class="flex items-center gap-2 px-3 py-1.5">
                        <Show
                          when={row.systemId}
                          fallback={<span>{row.label}</span>}
                        >
                          <button type="button" class="cursor-pointer hover:underline" onClick={() => navigate(`/systems/${row.systemId}`)}>
                            {row.label}
                          </button>
                        </Show>
                        <Show when={row.where}>
                          <span class="text-xs text-base-content/55">{row.where}</span>
                        </Show>
                        <Show when={row.primary}>
                          <span class="ml-auto rounded-field border border-primary/40 px-1.5 py-0.5 text-[10px] uppercase tracking-wider text-primary">primary</span>
                        </Show>
                      </li>
                    )}
                  </For>
                </ul>
                <Show when={rows().length > 1}>
                  <p class="mt-1.5 text-xs text-base-content/50">Location follows the primary system.</p>
                </Show>
              </Show>
            </section>

            <Show when={vitals().length > 0}>
              <section data-testid="leaf-vitals" class="rounded-box border border-base-300 bg-base-100 p-3.5 text-sm">
                <h2 class="eyebrow">Vitals</h2>
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
                <p class="mt-1.5 text-[11px] text-base-content/45">The latest sample per series; a dot marks the device speaking, not a default standing in.</p>
              </section>
            </Show>
            <section data-testid="leaf-collection" class="rounded-box border border-base-300 bg-base-100 p-3.5 text-sm">
              <h2 class="eyebrow">Collection</h2>
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
          </FleetShell>
          </Show>
          </div>
        </Show>
      </Show>
    </Page>
  );
}
