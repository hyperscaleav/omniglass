import { For, Show, createEffect, createMemo, createSignal } from "solid-js";
import { useNavigate, useParams, useSearchParams } from "@solidjs/router";
import { useQueries, useQuery } from "@tanstack/solid-query";
import Page from "../components/Page";
import Breadcrumb from "../components/Breadcrumb";
import HealthBadge from "../components/HealthBadge";
import HealthHistory from "../components/HealthHistory";
import Eyebrow from "../components/Eyebrow";
import DetailGate from "../components/DetailGate";
import PlaceCard from "../components/PlaceCard";
import MemberRows from "../components/MemberRows";
import OutlineWorkspace from "../components/OutlineWorkspace";
import { FLEET_VIEW_KEY, fleetView, childrenIndex } from "../lib/fleet";
import { systemHealth, systemHealthKey, verdictOf } from "../lib/health";
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
import { alarmRows, componentCards, sinceOf, systemZoomVM } from "../lib/system_zoom";
import { slotStrip } from "../lib/slot_strip";
import { SYSTEMS_KEY, listSystems } from "../lib/systems";
import { COMPONENTS_KEY, listComponents } from "../lib/components";
import { PRODUCTS_KEY, listProducts } from "../lib/products";
import { COMPONENT_TYPES_KEY, listComponentTypes, resolveComponentTypeIcon } from "../lib/component_types";
import { entityLabel } from "../lib/entities";
import { can, useMe } from "../lib/auth";
import { describeError, fmtTime } from "../lib/format";
import { durationText } from "../lib/timeline";
import { componentAlarms, componentAlarmsKey, severityRank } from "../lib/alarms";
import { incidentReasons, incidentRows, incidentsOf, markerX, uptimePct, type MemberAlarms } from "../lib/system_history";
import { spans as timelineSpans } from "../lib/timeline";
import { systemEvents, systemEventsKey, systemLogs, systemLogsKey } from "../lib/system_activity";
import { metricSeries, metricSeriesKey } from "../lib/series";
import { memberModel, systemCrumbs, systemsAtPlace } from "../lib/detail";
import TimeseriesChart from "../components/TimeseriesChart";

// The system's detail view (#636, reoriented in #872). Systems are the unit
// Omniglass monitors; places are folders and metadata; components are the
// pieces of systems. So this one view is what an operator lands on from a
// room: the place as a card of context, then the system itself (verdict,
// since-when, standard, slots only while something is missing) with its
// three tabs. Overview reads top-down: why (cause before arithmetic), the
// components as rows, the standard's map, the data. Activity is the history,
// the events and the logs; Configure is the one form.
//
// A sole system's view is titled by its place, and its path ends at the
// place's parent, so the room is named once. A place shared by several
// systems keeps its own view and stays in the path. A place with places
// beneath it lists them under the system, as the outline rooted there.
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

  // A name-shaped address resolves to the uuid, query kept (#759's rule).
  createEffect(() => {
    if (!view.data || system()) return;
    const matches = (view.data.systems ?? []).filter((s) => s.name === id());
    if (matches.length === 1) navigate(`/systems/${matches[0].id}${window.location.search}`, { replace: true });
  });

  const placeId = () => system()?.location ?? null;
  const place = createMemo(() => (placeId() ? (view.data?.locations ?? []).find((l) => l.id === placeId()) : undefined));
  // A sole system is the room: the view wears the place's name. A shared
  // place keeps its own view, so a system there wears its own.
  const sole = createMemo(() => (view.data && placeId() ? systemsAtPlace(view.data, placeId()!).length === 1 : false));
  const hasBeneath = createMemo(() => (view.data && placeId() ? (childrenIndex(view.data).get(placeId()!) ?? []).length > 0 : false));
  const title = () => (sole() && place() ? entityLabel(place()!) : system() ? entityLabel(system()!) : "System");

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
  const standards = useQuery(() => ({ queryKey: STANDARDS_KEY, queryFn: listStandards }));
  const standardRow = createMemo(() => (standards.data ?? []).find((x) => x.name === standard()));
  const mapDecl = createMemo(() => {
    const raw = (standardRow() as { map?: unknown } | undefined)?.map;
    return parseStandardMap(raw === undefined ? undefined : JSON.stringify(raw));
  });
  // Every declared metric, sampled or not: the Data section's series.
  const kpiMetrics = createMemo(() => (metricsQ.data ?? []).filter((m) => m.from_contract || m.is_sampled));
  // The expanded row's metric, if any: the table is the view, the full
  // chart an accordion under its row.
  const [seriesPick, setSeriesPick] = createSignal<string | undefined>(undefined);

  const [search] = useSearchParams();
  // Three tabs (#826): Overview, Activity, Configure. The retired tab
  // addresses map onto the tab that absorbed them, so an old link still
  // lands on its content.
  const tabs = createMemo(() => [
    { key: "overview", label: "Overview" },
    { key: "activity", label: "Activity" },
    ...(can(me.data, "system", "update") ? [{ key: "configure", label: "Configure" }] : []),
  ]);
  const LEGACY_TAB: Record<string, string> = { map: "overview", data: "overview", history: "activity", events: "activity", logs: "activity" };
  const tab = () => {
    const raw = Array.isArray(search.tab) ? search.tab[0] : search.tab;
    const t = raw ? (LEGACY_TAB[raw] ?? raw) : raw;
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
      enabled: tab() === "activity",
    })),
  }));
  // One series query per declared metric, alive only on Overview, where the
  // Data table draws every sparkline at once (#795 review).
  const seriesQueries = useQueries(() => ({
    queries: kpiMetrics().map((m) => ({
      queryKey: metricSeriesKey("systems", id(), m.metric_type_name, 24),
      queryFn: () => metricSeries("systems", id(), m.metric_type_name, 24),
      enabled: tab() === "overview",
    })),
  }));
  const metricSeriesData = () => kpiMetrics().map((_, i) => seriesQueries[i]?.data);
  const eventsQ = useQuery(() => ({ queryKey: systemEventsKey(id()), queryFn: () => systemEvents(id()), enabled: tab() === "activity" }));
  const logsQ = useQuery(() => ({ queryKey: systemLogsKey(id()), queryFn: () => systemLogs(id()), enabled: tab() === "activity" }));
  const verdictSpans = createMemo(() =>
    timelineSpans((health.data?.transitions ?? []).map((t) => ({ ts: t.ts, value: t.verdict })), health.data?.verdict ?? null, pageNow),
  );
  const uptime = createMemo(() => uptimePct(verdictSpans()));
  const incidentList = createMemo(() => incidentsOf(verdictSpans(), pageNow));
  // Healthy stays grey (#872): only an uptime that should worry someone
  // wears a hue.
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
  const ctypes = useQuery(() => ({ queryKey: COMPONENT_TYPES_KEY, queryFn: listComponentTypes }));

  const members = createMemo(() => {
    const b = bodyOf();
    if (!b) return { rows: [], groups: [] };
    const comp = new Map((comps.data ?? []).map((c) => [c.id, c] as const));
    const prod = new Map((prods.data ?? []).map((p) => [p.name, p] as const));
    const types = new Map((ctypes.data ?? []).map((t) => [t.name, t] as const));
    const dots = new Map((system()?.dots ?? []).map((d) => [d.component, d] as const));
    const alarmOf = new Map(alarms().filter((a) => a.componentId).map((a) => [a.componentId!, a.message] as const));
    return memberModel(b, {
      verdict: (cid) => verdictOf(dots.get(cid)?.verdict),
      alarm: (cid) => alarmOf.get(cid),
      product: (cid) => {
        const h = comp.get(cid)?.product;
        return h ? entityLabel(prod.get(h) ?? { name: h }) : "";
      },
      label: (cid, name) => {
        const c = comp.get(cid);
        return c ? entityLabel({ name: c.name, label: c.label }) : name;
      },
      icon: (cid) => resolveComponentTypeIcon(prod.get(comp.get(cid)?.product ?? "")?.component_type, types),
    });
  });
  const componentCount = () => (system()?.dots ?? []).length;

  const crumbs = createMemo(() => {
    const trail = view.data ? systemCrumbs(view.data, id()) : [];
    return [
      // Back up lands where you were: the outline opened down to this row.
      { key: "explore", label: "Explore", onClick: () => navigate(`/explore?node=${encodeURIComponent(id())}`) },
      ...trail.map((c) => ({ key: c.id, label: c.label, onClick: () => navigate(`/locations/${c.id}`) })),
    ];
  });

  const pending = () => view.isPending || health.isPending || declared.isPending;
  const failure = () => view.error ?? health.error ?? declared.error;
  const retry = () => { void view.refetch(); void health.refetch(); void declared.refetch(); };
  // A miss is judged only once the fleet view is current: the create
  // handoff lands here while the cached view predates the new row.
  const missing = () => !!view.data && !view.isFetching && !system() && !(view.data.systems ?? []).some((x) => x.name === id());

  // Components open in a blade that can expand to their own view (ADR-0129's
  // altitude rule): every member, alarm, and map marker pushes the component
  // blade on this stack instead of leaving the page.
  const blades = createBladeController();
  const openComponent = (cid: string) => blades.push({ kind: "component", id: cid });

  return (
    <BladesContext.Provider value={blades}>
    <Page title={title()} breadcrumb={<Breadcrumb crumbs={crumbs()} />}>
      <DetailGate
        noun="system"
        missing={missing()}
        pending={pending()}
        error={failure() ? describeError(failure()) : null}
        retrying={view.isFetching || health.isFetching}
        onRetry={retry}
      >
        <div class="flex flex-col gap-3">
          <Show when={placeId()}>
            {(pid) => <PlaceCard placeId={pid()} showName={!sole()} />}
          </Show>
          <section data-testid="system-card" class="card overflow-hidden border border-base-300 bg-base-200 p-0">
            <div data-testid="system-header" class="flex flex-wrap items-center gap-x-3 gap-y-1 border-b border-base-300 px-4 py-3 text-sm">
              <Eyebrow label="System" hint="The logical group of components that makes this place work, and the thing Omniglass monitors: its verdict is what the components filling its roles add up to." />
              <HealthBadge verdict={health.data?.verdict} size="sm" />
              <Show when={health.data && sinceOf(health.data, pageNow)}>
                {(sc) => <span data-testid="since-line" class="tabular-nums text-base-content/70">since {fmtTime(sc().ts)} · {durationText(sc().ms)}</span>}
              </Show>
              <Show when={standard()}>
                <span class="text-base-content/30">·</span>
                <span data-testid="standard" class="text-base-content/70">{standardRow() ? entityLabel(standardRow()!) : standard()}</span>
              </Show>
              <span class="text-base-content/30">·</span>
              <span data-testid="component-count" class="tabular-nums text-base-content/70">{componentCount()} {componentCount() === 1 ? "component" : "components"}</span>
              {/* Slot arithmetic speaks only when hardware is MISSING: a
                  deployed room fills every role (#785), and a down occupant
                  is a failure the alarms explain, not a gap. */}
              <Show when={strip() && strip()!.empty > 0}>
                <span class="text-base-content/30">·</span>
                <span data-testid="slots-short" class="tabular-nums text-incomplete">{strip()!.filled} of {strip()!.total} slots filled</span>
              </Show>
            </div>
            <TabRail tabs={tabs()} activeKey={tab} />
            <Show when={tab() === "configure"}>
              {/* A sole system's place has no page of its own: it configures
                  here, first, as the page reads (its card sits above the
                  system's), in its own section with its own Edit (#872). A
                  place shared by several systems keeps its own view and is
                  configured there. */}
              <Show when={sole() && placeId()}>
                {(pid) => (
                  <section class="border-b border-base-300">
                    <div class="px-4 pt-4"><Eyebrow label="Place" hint="Where this system sits: the place's own name, type, parent, properties and tags. Its edit is its own, beside the system's." /></div>
                    <ConfigureFace kind="location" id={pid()} editValue="place" testid="configure-face-place" />
                  </section>
                )}
              </Show>
              {/* Named only beside the place's section; alone it needs no name. */}
              <Show when={sole() && placeId()}>
                <div class="px-4 pt-4"><Eyebrow label="System" /></div>
              </Show>
              <ConfigureFace kind="system" id={id()} />
            </Show>
        <Show when={tab() === "activity"}>
          <section data-testid="history-tab" class="flex flex-col gap-4 p-4">
            <div class="flex flex-wrap items-stretch gap-3">
              <div data-testid="uptime-kpi" class="flex min-w-36 flex-col justify-between gap-1 rounded-box border border-base-300 bg-base-100 p-3.5">
                <Eyebrow label="Uptime" hint="The healthy share of the recorded window: the health KPI over time, the number a status page leads with. The timeline beside it is the same record drawn out." />
                <span
                  class="font-mono text-3xl tabular-nums"
                  classList={{
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
              <Show when={incidentList().length > 0} fallback={<p class="mt-2 text-sm text-base-content/50">Nothing in this window. The system held healthy the whole way.</p>}>
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
                <Eyebrow label="Other alarms" hint="Alarms in the window that never overlapped an unhealthy stretch: the component complained, and the system absorbed it." />
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
        <Show when={tab() === "activity"}>
          <section data-testid="events-tab" class="flex flex-col gap-2 p-4">
            <Eyebrow label="Events" hint="The system's story on the event lane: the system's own events and its members', newest first, each row labeled by the owner that raised it. The last 24 hours, capped." />
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
        <Show when={tab() === "activity"}>
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
            <Show when={tab() === "overview" && vm()}>
              {(z) => (
                <div data-testid="overview-tab" class="flex flex-col gap-5 p-4">
                  {/* Cause before arithmetic (#785): what is wrong, on which
                      component, impairing which role, since when. */}
                  <Show when={alarms().length > 0}>
                    <section data-testid="alarm-strip" class="flex flex-col gap-1.5 rounded-box border p-3" classList={{ "border-error/40 bg-error/5": severityRank(alarms()[0].severity) === 0, "border-warning/40 bg-warning/5": severityRank(alarms()[0].severity) !== 0 }}>
                      <h2 class="eyebrow">Why</h2>
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
                  <section class="flex flex-col gap-2">
                    <Eyebrow label="Components" hint="The pieces of this system, each with the role it fills. A role gets its own header only where it says something a column cannot: more than one wanted, a shortfall, or nobody staffing it yet." />
                    <MemberRows rows={members().rows} groups={members().groups} onOpen={openComponent} />
                  </section>
                  <Show when={mapDecl()}>
                    {(decl) => (
                      // SystemMap pads itself; the negative margin lines its
                      // eyebrow up with the sections around it.
                      <div class="-m-4">
                        <SystemMap decl={decl()} markers={mapMarkers(decl(), z())} onOpen={openComponent} />
                      </div>
                    )}
                  </Show>
        <Show when={kpiMetrics().length > 0}>
          <section data-testid="data-tab" class="flex flex-col gap-3">
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
                </div>
              )}
            </Show>
          </section>
          <Show when={sole() && hasBeneath() && placeId()}>
            {(pid) => (
              <section data-testid="beneath" class="flex flex-col gap-2">
                <Eyebrow label={`Beneath ${place() ? entityLabel(place()!) : "this place"}`} hint="The places inside this one, as Explore draws them, counted by the systems they hold." />
                <OutlineWorkspace rootId={pid()} />
              </section>
            )}
          </Show>
        </div>
      </DetailGate>
    </Page>
    <BladeStack controller={blades} registry={fleetRegistry} />
    </BladesContext.Provider>
  );
}
