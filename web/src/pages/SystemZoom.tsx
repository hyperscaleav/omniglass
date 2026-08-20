import { For, Show, createEffect, createMemo } from "solid-js";
import { useNavigate, useParams, useSearchParams } from "@solidjs/router";
import { useQuery } from "@tanstack/solid-query";
import Page from "../components/Page";
import Breadcrumb from "../components/Breadcrumb";
import HealthBadge from "../components/HealthBadge";
import HealthHistory from "../components/HealthHistory";
import Eyebrow from "../components/Eyebrow";
import FleetShell from "../components/FleetShell";
import { fleetTiles } from "../lib/fleet_tiles";
import { createSignal } from "solid-js";
import type { Chip } from "../lib/predicate";
import { FLEET_VIEW_KEY, ancestors, fleetView, locationIndex } from "../lib/fleet";
import { systemHealth, systemHealthKey } from "../lib/health";
import { systemRoles, systemRolesKey } from "../lib/system_roles";
import { systemMetrics, systemMetricsKey } from "../lib/system_metrics";
import { STANDARDS_KEY, listStandards } from "../lib/standards";
import { mapMarkers, parseStandardMap } from "../lib/system_map";
import SystemMap from "../components/SystemMap";
import TabRail from "../components/TabRail";
import { alarmRows, componentCards, sinceOf, systemZoomVM, type ComponentCard } from "../lib/system_zoom";
import { vitalRows } from "../lib/component_leaf";
import { slotStrip } from "../lib/slot_strip";
import { SYSTEMS_KEY, listSystems } from "../lib/systems";
import { COMPONENTS_KEY, listComponents } from "../lib/components";
import { PRODUCTS_KEY, listProducts } from "../lib/products";
import { entityLabel } from "../lib/entities";
import { describeError, fmtTime } from "../lib/format";
import { durationText } from "../lib/timeline";
import { severityRank } from "../lib/alarms";

// The system zoom (#636): the typed slots a system needs filled, at the
// identity route behind ?zoom=1 (ADR-0126). One card per role with the
// server's own arithmetic, roles grouped by choice with the active alternate
// marked and the losing build rendered as the legal alternative it is, the
// shared occupants chipped with the other system they serve, and the members
// filling no role in their own strip, which is a state and not an error.
export default function SystemZoom() {
  const params = useParams<{ id: string }>();
  const navigate = useNavigate();
  const id = () => params.id;
  // Pinned at setup, like HealthHistory's: a "now" that moved under the
  // since-line would re-age it on every unrelated re-render.
  const pageNow = Date.now();

  const view = useQuery(() => ({ queryKey: FLEET_VIEW_KEY, queryFn: fleetView }));
  const health = useQuery(() => ({ queryKey: systemHealthKey(id()), queryFn: () => systemHealth(id()) }));
  const declared = useQuery(() => ({ queryKey: systemRolesKey(id()), queryFn: () => systemRoles(id()) }));

  const system = createMemo(() => (view.data?.systems ?? []).find((s) => s.id === id()));

  // A name-shaped address resolves to the uuid, query kept (#759's rule; the
  // zoom branch runs before the inventory detail's own fallback could).
  createEffect(() => {
    if (!view.data || system()) return;
    const matches = (view.data.systems ?? []).filter((s) => s.name === id());
    if (matches.length === 1) navigate(`/systems/${matches[0].id}${window.location.search}`, { replace: true });
  });

  const strip = createMemo(() => (health.data ? slotStrip(health.data) : undefined));
  const systemsList = useQuery(() => ({ queryKey: SYSTEMS_KEY, queryFn: listSystems }));
  const standard = createMemo(() => (systemsList.data ?? []).find((x) => x.id === id())?.standard);

  const vm = createMemo(() => {
    if (!view.data || !health.data || !declared.data) return undefined;
    return systemZoomVM(health.data, declared.data, view.data, id());
  });

  const alarms = createMemo(() => (view.data && health.data ? alarmRows(health.data, view.data, id()) : []));
  const bodyOf = createMemo(() => (vm() ? componentCards(vm()!) : undefined));
  const metricsQ = useQuery(() => ({ queryKey: systemMetricsKey(id()), queryFn: () => systemMetrics(id()) }));
  const kpis = createMemo(() => vitalRows(metricsQ.data ?? []));
  const standards = useQuery(() => ({ queryKey: STANDARDS_KEY, queryFn: listStandards }));
  const mapDecl = createMemo(() => {
    const std = (standards.data ?? []).find((x) => x.name === standard());
    const raw = (std as { map?: unknown } | undefined)?.map;
    return parseStandardMap(raw === undefined ? undefined : JSON.stringify(raw));
  });
  const [search] = useSearchParams();
  const tab = () => {
    const t = Array.isArray(search.tab) ? search.tab[0] : search.tab;
    return t === "map" && mapDecl() ? "map" : "overview";
  };
  const comps = useQuery(() => ({ queryKey: COMPONENTS_KEY, queryFn: listComponents }));
  const prods = useQuery(() => ({ queryKey: PRODUCTS_KEY, queryFn: listProducts }));
  const productOf = (componentId: string): string | undefined => {
    const c = (comps.data ?? []).find((x) => x.id === componentId);
    const pr = c?.product ? (prods.data ?? []).find((p) => p.name === c.product) : undefined;
    return pr ? entityLabel(pr) : c?.product ?? undefined;
  };
  const tiles = createMemo(() => (view.data ? fleetTiles(view.data) : undefined));
  const [chips, setChips] = createSignal<Chip[]>([]);

  const crumbs = createMemo(() => {
    if (!view.data) return [];
    const chain = system()?.location ? ancestors(system()!.location!, locationIndex(view.data)) : [];
    return [
      { key: "fleet", label: "Fleet", onClick: () => navigate("/fleet") },
      ...chain.map((l) => ({
        key: l.id,
        label: entityLabel(l),
        onClick: () => navigate(`/locations/${l.id}?zoom=1`),
      })),
    ];
  });

  const pending = () => view.isPending || health.isPending || declared.isPending;
  const failed = () => view.isError || health.isError || declared.isError;

  return (
    <Page
      title={system() ? entityLabel(system()!) : "System"}
      breadcrumb={<Breadcrumb crumbs={crumbs()} />}
    >
      <Show when={!pending()} fallback={<div class="skeleton h-32 w-full" />}>
        <Show
          when={!failed()}
          fallback={
            <div role="alert" class="alert alert-error alert-soft text-sm">
              {describeError(view.error ?? health.error ?? declared.error)}
            </div>
          }
        >
          <FleetShell
            storageKey="fleet"
            tiles={tiles()}
            rows={[]}
            filterKeys={[]}
            chips={chips}
            onChips={setChips}
            header={
              <div data-testid="system-header" class="flex flex-wrap items-center gap-x-3 gap-y-1 text-sm">
                <HealthBadge verdict={health.data?.verdict} size="sm" />
                <Show when={health.data && sinceOf(health.data, pageNow)}>
                  {(sc) => <span data-testid="since-line" class="tabular-nums text-base-content/70">since {fmtTime(sc().ts)} · {durationText(sc().ms)}</span>}
                </Show>
                {/* Slot arithmetic leads only when hardware is MISSING: a
                    deployed room fills every role (#785), and a down
                    occupant is a failure the alarms explain, not a gap, so
                    the trigger is empty squares, never merely unfilled. */}
                <Show when={strip() && strip()!.empty > 0}>
                  {(_) => <span class="tabular-nums text-base-content/80">{strip()!.filled} of {strip()!.total} slots filled</span>}
                </Show>
                <Show when={standard()}>
                  <span class="text-base-content/30">·</span>
                  <span class="font-mono text-xs text-base-content/60">{standard()}</span>
                </Show>
                <Show when={system()?.location}>
                  {(loc) => (
                    <>
                      <span class="text-base-content/30">·</span>
                      <button type="button" class="cursor-pointer text-xs text-primary hover:underline" onClick={() => navigate(`/locations/${loc()}?zoom=1`)}>Open location</button>
                    </>
                  )}
                </Show>
              </div>
            }
          >
          <Show when={vm()}>
            {(z) => (
              <div class="flex min-w-0 flex-1 flex-col">
                <TabRail tabs={mapDecl() ? [{ key: "overview", label: "Overview" }, { key: "map", label: "Map" }] : [{ key: "overview", label: "Overview" }]} />
                <Show when={tab() === "map" && mapDecl()}>
                  {(decl) => <SystemMap decl={decl()} markers={mapMarkers(decl(), z())} />}
                </Show>
                <div class="flex flex-col gap-5 p-4" classList={{ hidden: tab() !== "overview" }}>
                  {/* Cause before arithmetic (#785): what is wrong, on which
                      component, impairing which role, since when. */}
                  <Show when={alarms().length > 0}>
                    <section data-testid="alarm-strip" class="flex flex-col gap-1.5 rounded-box border p-3" classList={{ "border-error/40 bg-error/5": severityRank(alarms()[0].severity) === 0, "border-warning/40 bg-warning/5": severityRank(alarms()[0].severity) !== 0 }}>
                      <h2 class="eyebrow">Active alarms</h2>
                      <For each={alarms()}>
                        {(a) => (
                          <div class="flex flex-wrap items-baseline gap-x-2 gap-y-0.5 text-sm">
                            <span class="badge badge-sm" classList={{ "badge-error badge-soft": a.severity === "critical", "badge-warning badge-soft": a.severity !== "critical" }}>{a.severity}</span>
                            <span>{a.message}</span>
                            <Show when={a.componentId} fallback={<span class="font-mono text-xs text-base-content/60">{a.component}</span>}>
                              <button type="button" class="cursor-pointer font-mono text-xs text-base-content/80 hover:underline" onClick={() => navigate(`/components/${a.componentId}?zoom=1`)}>{a.component}</button>
                            </Show>
                            <span class="text-xs text-base-content/50">impairs {a.roleLabel} · {durationText(pageNow - Date.parse(a.raisedAt))}</span>
                          </div>
                        )}
                      </For>
                    </section>
                  </Show>
                  <Show when={kpis().length > 0}>
                    <div data-testid="kpi-tiles" class="flex flex-wrap gap-3">
                      <For each={kpis()}>
                        {(k) => (
                          <div class="flex min-w-36 flex-col gap-0.5 rounded-box border border-base-300 bg-base-100 p-3 pr-6">
                            <Eyebrow label={k.label} />
                            <span class="font-mono text-xl tabular-nums">{String(k.value)}</span>
                            <span class="text-[10px] text-base-content/45">{k.sampled ? "sampled" : "contract default"}</span>
                          </div>
                        )}
                      </For>
                    </div>
                  </Show>
                  <div data-testid="health-history">
                    <HealthHistory transitions={health.data?.transitions ?? []} verdict={health.data?.verdict} />
                  </div>
                  {/* Components-first (#790): the unit is the component, its
                      role a badge. Role chrome renders only where it says
                      something a badge cannot: quorum beyond one, a shortfall,
                      or a role nobody staffed. */}
                  <Show when={bodyOf() && bodyOf()!.cards.length > 0}>
                    <div class="grid grid-cols-[repeat(auto-fill,minmax(13rem,1fr))] gap-3">
                      <For each={bodyOf()!.cards}>{(c) => <CompCard card={c} />}</For>
                    </div>
                  </Show>
                  <For each={bodyOf()?.groups ?? []}>
                    {(g) => (
                      <section
                        data-testid={`rolegroup-${g.name}`}
                        class="flex flex-col gap-2 rounded-box border border-dashed p-3"
                        classList={{
                          "border-error/45": g.short > 0 && g.impact === "outage",
                          "border-warning/45": g.short > 0 && g.impact === "degraded",
                          "border-incomplete/50": g.short > 0 && g.members.length === 0,
                          "border-base-content/20": g.short === 0,
                        }}
                      >
                        <div class="flex flex-wrap items-baseline gap-2">
                          <span class="badge badge-sm badge-outline">{g.label}</span>
                          <span class="text-xs tabular-nums text-base-content/60">
                            {g.satisfying} of {g.quorum}
                            <Show when={g.spare > 0}>{` + ${g.spare} spare`}</Show>
                          </span>
                        </div>
                        <div class="grid grid-cols-[repeat(auto-fill,minmax(13rem,1fr))] gap-3">
                          <For each={g.memberCards}>{(c) => <CompCard card={c} inGroup />}</For>
                          <For each={Array.from({ length: g.short })}>
                            {() => (
                              <div class="flex min-h-16 items-center justify-center rounded-md border border-dashed border-base-content/25 text-xs text-base-content/45">
                                empty
                              </div>
                            )}
                          </For>
                        </div>
                      </section>
                    )}
                  </For>
                </div>
              </div>
            )}
          </Show>
          </FleetShell>
        </Show>
      </Show>
    </Page>
  );

  function CompCard(props: { card: ComponentCard; inGroup?: boolean }) {
    const c = () => props.card;
    return (
      <button
        type="button"
        data-testid={`compcard-${c().componentId}`}
        class="flex cursor-pointer flex-col gap-1 rounded-md border p-2.5 text-left hover:border-primary/50"
        classList={{ "border-error/50 bg-error/5": c().down, "border-base-300 bg-base-100": !c().down }}
        onClick={() => navigate(`/components/${c().componentId}?zoom=1`)}
      >
        <div class="flex items-center gap-1.5">
          <span class="h-1.5 w-1.5 flex-none rounded-full" classList={{ "bg-error": c().down, "bg-success": !c().down }} />
          <span class="font-mono text-[12px]" classList={{ "text-base-content/40 line-through": c().down }}>{c().name}</span>
        </div>
        <Show when={productOf(c().componentId)}>
          <span class="truncate text-xs text-base-content/60">{productOf(c().componentId)}</span>
        </Show>
        <div class="mt-0.5 flex flex-wrap gap-1">
          <For each={c().roles.filter((r) => !(props.inGroup && r.position === undefined && c().roles.length === 1))}>
            {(r) => (
              <span class="badge badge-xs badge-outline">
                {r.label}
                <Show when={r.position}>{` · ${r.position}`}</Show>
              </span>
            )}
          </For>
          <Show when={c().noRole && c().roles.length === 0}>
            <span class="badge badge-xs badge-ghost">no role</span>
          </Show>
          <Show when={c().shared.length > 0}>
            <span class="badge badge-xs badge-ghost">also {c().shared.join(", ")}</span>
          </Show>
        </div>
      </button>
    );
  }
}
