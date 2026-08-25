import { For, Show, createEffect, createMemo } from "solid-js";
import { useNavigate, useParams, useSearchParams } from "@solidjs/router";
import { useQueries, useQuery } from "@tanstack/solid-query";
import Page from "../components/Page";
import Breadcrumb from "../components/Breadcrumb";
import HealthBadge from "../components/HealthBadge";
import HealthHistory from "../components/HealthHistory";
import Eyebrow from "../components/Eyebrow";
import FleetShell from "../components/FleetShell";
import { systemTileSpec } from "../lib/fleet_tiles";
import { createSignal } from "solid-js";
import type { Chip } from "../lib/predicate";
import { FLEET_VIEW_KEY, ancestors, fleetView, locationIndex } from "../lib/fleet";
import { systemHealth, systemHealthKey } from "../lib/health";
import { systemRoles, systemRolesKey } from "../lib/system_roles";
import { systemMetrics, systemMetricsKey } from "../lib/system_metrics";
import { STANDARDS_KEY, listStandards } from "../lib/standards";
import { mapMarkers, parseStandardMap } from "../lib/system_map";
import SystemMap from "../components/SystemMap";
import BladeStack from "../components/BladeStack";
import { BladesContext, createBladeController } from "../lib/blades";
import { fleetRegistry } from "../lib/fleetBlades";
import TabRail from "../components/TabRail";
import ConfigureFace from "../components/ConfigureFace";
import { alarmRows, componentCards, sinceOf, systemZoomVM, type ComponentCard } from "../lib/system_zoom";
import { vitalRows } from "../lib/component_leaf";
import { slotStrip } from "../lib/slot_strip";
import { SYSTEMS_KEY, listSystems } from "../lib/systems";
import { COMPONENTS_KEY, listComponents } from "../lib/components";
import { PRODUCTS_KEY, listProducts } from "../lib/products";
import { entityLabel } from "../lib/entities";
import { can, useMe } from "../lib/auth";
import { describeError, fmtTime } from "../lib/format";
import { durationText } from "../lib/timeline";
import { componentAlarms, componentAlarmsKey, severityRank } from "../lib/alarms";
import { incidentReasons, incidentRows, incidentsOf, markerX, uptimePct, type MemberAlarms } from "../lib/system_history";
import { spans as timelineSpans } from "../lib/timeline";
import { systemEvents, systemEventsKey, systemLogs, systemLogsKey } from "../lib/system_activity";
import { metricSeries, metricSeriesKey } from "../lib/series";
import TimeseriesChart from "../components/TimeseriesChart";

// The system zoom (#636): the typed slots a system needs filled, at the
// identity route, the DEFAULT face since ADR-0129. One card per role with the
// server's own arithmetic, roles grouped by choice with the active alternate
// marked and the losing build rendered as the legal alternative it is, the
// shared occupants chipped with the other system they serve, and the members
// filling no role in their own strip, which is a state and not an error.
export default function SystemZoom() {
  const params = useParams<{ id: string }>();
  const navigate = useNavigate();
  const me = useMe();
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
  // Every declared metric, sampled or not: the Data tab's pickable series.
  const kpiMetrics = createMemo(() => (metricsQ.data ?? []).filter((m) => m.from_contract || m.is_sampled));
  // The expanded row's metric, if any: the table is the view, the full
  // chart an accordion under its row.
  const [seriesPick, setSeriesPick] = createSignal<string | undefined>(undefined);

  const [search] = useSearchParams();
  const tabs = createMemo(() => [
    { key: "overview", label: "Overview" },
    ...(mapDecl() ? [{ key: "map", label: "Map" }] : []),
    { key: "history", label: "History" },
    { key: "events", label: "Events" },
    { key: "logs", label: "Logs" },
    ...(kpiMetrics().length > 0 ? [{ key: "data", label: "Data" }] : []),
    // Editing is a facet of the workspace (#800): the tab renders only for a
    // caller holding an edit verb; the sections inside gate per verb again.
    ...(can(me.data, "system", "update") ? [{ key: "configure", label: "Configure" }] : []),
  ]);
  const tab = () => {
    const t = Array.isArray(search.tab) ? search.tab[0] : search.tab;
    if (t && tabs().some((x) => x.key === t)) return t;
    // ?edit=1 means the one editor (#800): a bare edit intent lands on
    // Configure, whose own hook then begins the edit.
    const editing = (Array.isArray(search.edit) ? search.edit[0] : search.edit) === "1";
    if (editing && tabs().some((x) => x.key === "configure")) return "configure";
    return "overview";
  };
  // Every member's alarm history, cleared ones included: the causes beside
  // the verdict spans. Fan-out per component (rooms are small); each query
  // shares the leaf's cache key, so walking to a leaf is warm.
  const memberIds = createMemo(() => {
    const b = bodyOf();
    if (!b) return [] as { name: string; componentId: string }[];
    const all = [...b.cards, ...b.groups.flatMap((g) => g.memberCards)];
    return all.map((c) => ({ name: c.name, componentId: c.componentId }));
  });
  const alarmHistories = useQueries(() => ({
    queries: memberIds().map((m) => ({
      queryKey: componentAlarmsKey(m.componentId),
      queryFn: () => componentAlarms(m.componentId),
    })),
  }));
  // Pinned once, like pageNow: incident ages must not re-age mid-render.
  // One series query per declared metric, alive only on the Data tab: the
  // stacked table draws every sparkline at once (#795 review), so the
  // fan-out is the tab's whole read.
  const seriesQueries = useQueries(() => ({
    queries: kpiMetrics().map((m) => ({
      queryKey: metricSeriesKey("systems", id(), m.metric_type_name, 24),
      queryFn: () => metricSeries("systems", id(), m.metric_type_name, 24),
      enabled: tab() === "data",
    })),
  }));
  const metricSeriesData = () => kpiMetrics().map((_, i) => seriesQueries[i]?.data);
  const eventsQ = useQuery(() => ({ queryKey: systemEventsKey(id()), queryFn: () => systemEvents(id()), enabled: tab() === "events" }));
  const logsQ = useQuery(() => ({ queryKey: systemLogsKey(id()), queryFn: () => systemLogs(id()), enabled: tab() === "logs" }));
  const verdictSpans = createMemo(() =>
    timelineSpans((health.data?.transitions ?? []).map((t) => ({ ts: t.ts, value: t.verdict })), health.data?.verdict ?? null, pageNow),
  );
  const uptime = createMemo(() => uptimePct(verdictSpans()));
  const incidentList = createMemo(() => incidentsOf(verdictSpans(), pageNow));
  const uptimeTone = () => {
    const u = uptime();
    if (u === null) return "none";
    const v = Number(u);
    return v >= 99 ? "good" : v >= 95 ? "warn" : "bad";
  };
  const [openIncident, setOpenIncident] = createSignal<number | null>(null);
  const incidents = createMemo(() => {
    const members: MemberAlarms[] = memberIds().map((m, i) => ({
      component: m.name,
      componentId: m.componentId,
      alarms: alarmHistories[i]?.data ?? [],
    }));
    return incidentRows(members, pageNow, 30 * 24);
  });
  const otherAlarms = createMemo(() => {
    const inIncident = new Set(incidentList().flatMap((inc) => incidentReasons(inc, incidents()).map((r) => r.id)));
    return incidents().filter((r) => !inIncident.has(r.id));
  });
  const comps = useQuery(() => ({ queryKey: COMPONENTS_KEY, queryFn: listComponents }));
  const prods = useQuery(() => ({ queryKey: PRODUCTS_KEY, queryFn: listProducts }));
  const productOf = (componentId: string): string | undefined => {
    const c = (comps.data ?? []).find((x) => x.id === componentId);
    const pr = c?.product ? (prods.data ?? []).find((p) => p.name === c.product) : undefined;
    return pr ? entityLabel(pr) : c?.product ?? undefined;
  };
  const tiles = createMemo(() => (view.data && system() ? systemTileSpec(view.data, health.data, system()!.id) : undefined));
  const [chips, setChips] = createSignal<Chip[]>([]);

  const crumbs = createMemo(() => {
    if (!view.data) return [];
    const chain = system()?.location ? ancestors(system()!.location!, locationIndex(view.data)) : [];
    return [
      { key: "fleet", label: "Fleet", onClick: () => navigate("/fleet") },
      ...chain.map((l) => ({
        key: l.id,
        label: entityLabel(l),
        onClick: () => navigate(`/locations/${l.id}`),
      })),
    ];
  });

  const pending = () => view.isPending || health.isPending || declared.isPending;
  const failed = () => view.isError || health.isError || declared.isError;

  // Components open in a blade that can expand to its route (ADR-0129's
  // altitude rule, wired here in #799): every member, alarm, and map marker
  // pushes the component blade on this stack instead of leaving the page.
  const blades = createBladeController();
  const openComponent = (id: string) => blades.push({ kind: "component", id });

  return (
    <BladesContext.Provider value={blades}>
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
                      <button type="button" class="cursor-pointer text-xs text-primary hover:underline" onClick={() => navigate(`/locations/${loc()}`)}>Open location</button>
                    </>
                  )}
                </Show>
              </div>
            }
          >
          <Show when={vm()}>
            {(z) => (
              <div class="flex min-w-0 flex-1 flex-col">
                <TabRail tabs={tabs()} activeKey={tab} />
                <Show when={tab() === "map" && mapDecl()}>
                  {(decl) => <SystemMap decl={decl()} markers={mapMarkers(decl(), z())} onOpen={openComponent} />}
                </Show>
                <Show when={tab() === "configure"}>
                  <ConfigureFace kind="system" id={id()} />
                </Show>
                <Show when={tab() === "events"}>
                  <section data-testid="events-tab" class="flex flex-col gap-2 p-4">
                    <Eyebrow label="Events" hint="The room's story on the event lane: the system's own events and its members', newest first, each row labeled by the owner that raised it. The last 24 hours, capped." />
                    <Show when={(eventsQ.data ?? []).length > 0} fallback={<p class="text-sm text-base-content/50">No events in the window.</p>}>
                      <ul class="divide-y divide-base-300 rounded-box border border-base-300 text-sm">
                        <For each={eventsQ.data ?? []}>
                          {(e) => (
                            <li class="flex flex-wrap items-baseline gap-x-2 gap-y-0.5 px-3 py-2">
                              <span class="text-xs tabular-nums text-base-content/50">{fmtTime(e.ts)}</span>
                              <span class="font-mono text-xs text-base-content/70">{e.owner}</span>
                              <span class="font-mono text-xs">{e.key}</span>
                              <span class="min-w-0 flex-1 truncate text-base-content/80">{e.message}</span>
                            </li>
                          )}
                        </For>
                      </ul>
                    </Show>
                  </section>
                </Show>
                <Show when={tab() === "logs"}>
                  <section data-testid="logs-tab" class="flex flex-col gap-2 p-4">
                    <Eyebrow label="Logs" hint="The members' raw log lines merged newest first, each naming the component that wrote it. The last 24 hours, capped; a node's own logs live on the node." />
                    <Show when={(logsQ.data ?? []).length > 0} fallback={<p class="text-sm text-base-content/50">No lines in the window.</p>}>
                      <div class="overflow-x-auto rounded-box border border-base-300 bg-base-100 p-2 font-mono text-[11.5px] leading-relaxed">
                        <For each={logsQ.data ?? []}>
                          {(l) => (
                            <div class="whitespace-pre" classList={{ "text-error": l.severity === "error" || l.severity === "critical", "text-warning": l.severity === "warning", "text-base-content/70": !l.severity || l.severity === "info" }}>
                              {`${fmtTime(l.ts)}  ${(l.component ?? "").padEnd(12)} ${(l.severity ?? "").padEnd(7)} ${l.message}`}
                            </div>
                          )}
                        </For>
                      </div>
                    </Show>
                  </section>
                </Show>
                <Show when={tab() === "data"}>
                  <section data-testid="data-tab" class="flex flex-col gap-3 p-4">
                    <Eyebrow label="Data" hint="Every metric the standard declares, stacked: the last 24 hours as a sparkline beside the latest value. Click a row for the full chart. Raw samples, capped; a series still on its contract default has nothing to chart yet." />
                    <div class="overflow-x-auto rounded-box border border-base-300">
                      <table class="table table-sm">
                        <thead>
                          <tr>
                            <th>Metric</th>
                            <th>Last 24h</th>
                            <th class="text-right">Latest</th>
                            <th></th>
                          </tr>
                        </thead>
                        <tbody>
                          <For each={kpiMetrics()}>
                            {(m, i) => (
                              <>
                                <tr
                                  data-testid={`metric-row-${m.metric_type_name}`}
                                  class="cursor-pointer hover:bg-base-content/5"
                                  onClick={() => setSeriesPick(seriesPick() === m.metric_type_name ? undefined : m.metric_type_name)}
                                >
                                  <td>{entityLabel({ name: m.metric_type_name, label: m.label })}</td>
                                  <td>
                                    <TimeseriesChart spark samples={metricSeriesData()[i()] ?? []} now={pageNow} windowMs={24 * 3600_000} />
                                  </td>
                                  <td class="text-right font-mono tabular-nums">{m.value === null || m.value === undefined ? "" : String(m.value)}</td>
                                  <td class="text-right text-[10px] text-base-content/45">{m.is_sampled ? "sampled" : "contract default"}</td>
                                </tr>
                                <Show when={seriesPick() === m.metric_type_name}>
                                  <tr>
                                    <td colspan="4" class="bg-base-200/40">
                                      <TimeseriesChart samples={metricSeriesData()[i()] ?? []} now={pageNow} windowMs={24 * 3600_000} />
                                    </td>
                                  </tr>
                                </Show>
                              </>
                            )}
                          </For>
                        </tbody>
                      </table>
                    </div>
                  </section>
                </Show>
                <Show when={tab() === "history"}>
                  <section data-testid="history-tab" class="flex flex-col gap-4 p-4">
                    <div class="flex flex-wrap items-stretch gap-3">
                      <div data-testid="uptime-kpi" class="flex min-w-36 flex-col justify-between gap-1 rounded-box border border-base-300 bg-base-100 p-3.5">
                        <Eyebrow label="Uptime" hint="The healthy share of the recorded window: the health KPI over time, the number a status page leads with. The timeline beside it is the same record drawn out." />
                        <span
                          class="font-mono text-3xl tabular-nums"
                          classList={{
                            "text-success": uptimeTone() === "good",
                            "text-warning": uptimeTone() === "warn",
                            "text-error": uptimeTone() === "bad",
                          }}
                        >
                          {uptime() ?? "–"}<span class="text-lg">%</span>
                        </span>
                        <span class="text-[10px] text-base-content/45">of the last 30 days</span>
                      </div>
                      <div class="flex min-w-60 flex-1 flex-col justify-center rounded-box border border-base-300 bg-base-100 p-3.5" data-testid="health-history-full">
                        <HealthHistory compact transitions={health.data?.transitions ?? []} verdict={health.data?.verdict} />
                        <div class="relative mt-1 h-2">
                          <For each={incidents()}>
                            {(inc) => (
                              <span
                                data-testid={`incident-marker-${inc.id}`}
                                class="absolute top-0 h-0 w-0 -translate-x-1/2 border-x-4 border-b-6 border-x-transparent"
                                classList={{ "border-b-error": inc.severity === "critical", "border-b-warning": inc.severity !== "critical" }}
                                style={{ left: `${markerX(inc.raisedAt, pageNow, 30 * 24 * 3600_000) * 100}%` }}
                                title={`${inc.message} · ${inc.component}`}
                              />
                            )}
                          </For>
                        </div>
                      </div>
                    </div>

                    <div>
                      <Eyebrow label="Incidents" hint="One entry per contiguous stretch away from healthy, however many verdict changes it contained, ongoing first. Expand an entry for the transitions inside it and the alarms that explain them." />
                      <Show when={incidentList().length > 0} fallback={<p class="mt-2 text-sm text-base-content/50">Nothing in this window. The room held healthy the whole way.</p>}>
                        <ul data-testid="incident-list" class="mt-2 flex flex-col gap-2">
                          <For each={incidentList()}>
                            {(inc, idx) => {
                              const reasons = () => incidentReasons(inc, incidents());
                              const open = () => openIncident() === idx();
                              return (
                                <li
                                  data-testid={`incident-${idx()}`}
                                  class="rounded-box border"
                                  classList={{
                                    "border-error/45 bg-error/5": inc.worst === "outage",
                                    "border-warning/45 bg-warning/5": inc.worst === "degraded",
                                    "border-incomplete/45": inc.worst === "incomplete",
                                    "border-base-300": inc.worst === "healthy",
                                  }}
                                >
                                  <button type="button" class="flex w-full cursor-pointer flex-wrap items-baseline gap-x-3 gap-y-1 px-3 py-2 text-left text-sm" onClick={() => setOpenIncident(open() ? null : idx())}>
                                    <HealthBadge verdict={inc.worst} size="xs" />
                                    <span class="font-medium">
                                      {inc.worst === "incomplete" ? "Commissioning" : inc.worst === "outage" ? "Outage" : "Degraded"}
                                      {inc.ongoing ? " · ongoing" : ""}
                                    </span>
                                    <span class="text-xs tabular-nums text-base-content/60">
                                      {fmtTime(new Date(inc.from).toISOString())}
                                      {" → "}
                                      {inc.ongoing ? "now" : fmtTime(new Date(inc.to).toISOString())}
                                      {" · "}
                                      {durationText((inc.ongoing ? pageNow : inc.to) - inc.from)}
                                    </span>
                                    <span class="ml-auto text-xs text-base-content/40">{open() ? "collapse" : "expand"}</span>
                                  </button>
                                  <Show when={open()}>
                                    <div class="border-t border-base-300 px-3 py-2 text-sm">
                                      {/* One span is the header row said again: list the
                                          inside only when the stretch actually changed. */}
                                      <Show when={inc.spans.length > 1}>
                                      <ul class="flex flex-col gap-1 text-xs text-base-content/70">
                                        <For each={[...inc.spans].reverse()}>
                                          {(sp) => (
                                            <li class="flex flex-wrap items-baseline gap-2">
                                              <HealthBadge verdict={sp.value} size="xs" />
                                              <span class="tabular-nums">{fmtTime(new Date(sp.from).toISOString())}</span>
                                              <span class="text-base-content/45">held {durationText(sp.to - sp.from)}</span>
                                            </li>
                                          )}
                                        </For>
                                      </ul>
                                      </Show>
                                      <Show
                                        when={reasons().length > 0}
                                        fallback={<p class="mt-2 text-xs text-base-content/50">No alarm overlaps this stretch: a commissioning gap (a role nobody staffed), not a failure.</p>}
                                      >
                                        <ul class="mt-2 flex flex-col gap-1">
                                          <For each={reasons()}>
                                            {(r) => (
                                              <li class="flex flex-wrap items-baseline gap-x-2 text-xs">
                                                <span class="badge badge-xs" classList={{ "badge-error badge-soft": r.severity === "critical", "badge-warning badge-soft": r.severity !== "critical" }}>{r.severity}</span>
                                                <span>{r.message}</span>
                                                <button type="button" class="cursor-pointer font-mono text-base-content/70 hover:underline" onClick={(e) => { e.stopPropagation(); openComponent(r.componentId); }}>{r.component}</button>
                                                <span class="text-base-content/45 tabular-nums">
                                                  {fmtTime(r.raisedAt)} → {r.clearedAt ? fmtTime(r.clearedAt) : "ongoing"}
                                                </span>
                                              </li>
                                            )}
                                          </For>
                                        </ul>
                                      </Show>
                                    </div>
                                  </Show>
                                </li>
                              );
                            }}
                          </For>
                        </ul>
                      </Show>
                    </div>

                    <Show when={otherAlarms().length > 0}>
                      <div>
                        <Eyebrow label="Other alarms" hint="Alarms in the window that never overlapped an unhealthy stretch: the component complained, and the room absorbed it." />
                        <ul class="mt-2 divide-y divide-base-300 rounded-box border border-base-300 text-sm">
                          <For each={otherAlarms()}>
                            {(r) => (
                              <li class="flex flex-wrap items-baseline gap-x-2 px-3 py-1.5 text-xs">
                                <span class="badge badge-xs" classList={{ "badge-error badge-soft": r.severity === "critical", "badge-warning badge-soft": r.severity !== "critical" }}>{r.severity}</span>
                                <span>{r.message}</span>
                                <button type="button" class="cursor-pointer font-mono text-base-content/70 hover:underline" onClick={() => openComponent(r.componentId)}>{r.component}</button>
                                <span class="ml-auto text-base-content/45 tabular-nums">{fmtTime(r.raisedAt)}</span>
                              </li>
                            )}
                          </For>
                        </ul>
                      </div>
                    </Show>
                  </section>
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
                              {(cid) => <button type="button" class="cursor-pointer font-mono text-xs text-base-content/80 hover:underline" onClick={() => openComponent(cid())}>{a.component}</button>}
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
    <BladeStack controller={blades} registry={fleetRegistry} />
    </BladesContext.Provider>
  );

  function CompCard(props: { card: ComponentCard; inGroup?: boolean }) {
    const c = () => props.card;
    return (
      <button
        type="button"
        data-testid={`compcard-${c().componentId}`}
        class="flex cursor-pointer flex-col gap-1 rounded-md border p-2.5 text-left hover:border-primary/50"
        classList={{ "border-error/50 bg-error/5": c().down, "border-base-300 bg-base-100": !c().down }}
        onClick={() => openComponent(c().componentId)}
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
