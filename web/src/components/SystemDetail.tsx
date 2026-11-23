import { For, Show, createMemo, createSignal } from "solid-js";
import { useNavigate } from "@solidjs/router";
import { useQueries, useQuery, useQueryClient } from "@tanstack/solid-query";
import HealthBadge from "./HealthBadge";
import HealthHistory from "./HealthHistory";
import Eyebrow from "./Eyebrow";
import DetailGate from "./DetailGate";
import LocationCard from "./LocationCard";
import MemberRows from "./MemberRows";
import OutlineWorkspace from "./OutlineWorkspace";
import { FLEET_VIEW_KEY, fleetView, childrenIndex } from "../lib/fleet";
import { systemHealth, systemHealthKey, healthOf } from "../lib/health";
import { systemSlots, systemSlotsKey } from "../lib/system_slots";
import { systemMetrics, systemMetricsKey } from "../lib/system_metrics";
import { STANDARDS_KEY, listStandards } from "../lib/standards";
import { mapMarkers, parseStandardMap } from "../lib/system_map";
import SystemMap from "./SystemMap";
import { createLeaveGuard } from "../lib/leave_guard";
import TabRail from "./TabRail";
import { createTabState } from "../lib/tabstate";
import ConfigureFace from "./ConfigureFace";
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
import { systemAlarms, systemAlarmsKey } from "../lib/alarms";
import { useSeverity } from "../lib/severity";
import AlarmTimeline, { createTimelineToggles } from "./AlarmTimeline";
import RuleLink from "./RuleLink";
import SeverityBadge from "./SeverityBadge";
import AlarmActions from "./AlarmActions";
import { stretchReasons, memberAlarmRows, stretchesOf, markerX, uptimePct, type MemberAlarms } from "../lib/system_history";
import { spans as timelineSpans } from "../lib/timeline";
import { systemEvents, systemEventsKey, systemLogs, systemLogsKey, type SystemLogs } from "../lib/system_activity";
import type { LogFilter } from "../lib/logs";
import LogFilters from "./LogFilters";
import { SERIES_POLL_MS, metricBuckets, metricBucketsBetween, metricBucketsBetweenKey, metricBucketsKey } from "../lib/series";
import { DEFAULT_RANGE, rangeHours, rangeStepSeconds, resolveRange, useTimeRange, type TimeRange } from "../lib/time_range";
import TimeRangePicker from "./TimeRangePicker";
import { CalcRuleLink } from "./RuleLink";
import { memberModel, systemsAtLocation, type MemberContext } from "../lib/detail";
import TimeseriesChart from "./TimeseriesChart";
import { systemMembers, systemMembersKey } from "../lib/members";
import { LEAF_POLL_MS } from "../lib/component_leaf";
import { useBlades } from "../lib/blades";
import { type DetailHost } from "../lib/tabstate";
import { useEntityDelete } from "../lib/entity_delete";
import { deleteSystem } from "../lib/systems";

import { CLOCK_TICK_MS, createClock } from "../lib/clock";

// The system's detail view (#636, reoriented in #872). Systems are the unit
// Omniglass monitors; locations are folders and metadata; components are the
// pieces of systems. So this one view is what an operator lands on from a
// room: the location as a card of context, then the system itself (health,
// since-when, standard, slots only while something is missing) with its
// three tabs. Overview reads top-down: why (cause before arithmetic), the
// components as rows, the standard's map, the data. Activity is the history,
// the events and the logs; Configure is the one form.
//
// A sole system's view is titled by its location, and its path ends at the
// location's parent, so the room is named once. A location shared by several
// systems keeps its own view and stays in the path. A location with locations
// beneath it lists them under the system, as the outline rooted there.
//
// It refreshes on its own while open (#940), the way a component's view does
// (#934): the system's own reads (its health report, which carries the
// health, Why and the slots' staffing; its declared slots; its members, each
// with its own health; its data) poll at the leaf's cadence. The fleet view
// is read once, for the location, the path and who the members are; nothing
// polled grows with the fleet. TanStack holds every poll while the tab is
// hidden.
// The Data table reads a day at five-minute buckets (#1269): 288 of them, the
// min/max band and the avg line, so a one-sample spike survives and the read
// is the same size however busy the metric.
// HISTORY_TICK_MS is how often the history and the charts take a new now:
// a thirty-day strip and a day's sparkline move too little in a second to
// redraw on every tick.
const HISTORY_TICK_MS = 60_000;
// SystemDetail (#636, reoriented in #872, one body for both hosts since
// #2423): a system's whole detail body, rendered by its page
// (`/systems/:id`, host "page") and by the fleet blade (host "blade") alike.
// The host decides only where the tab lives, whether Edit answers the
// address's ?edit=, and where a Delete leaves to; sections reflow by container
// query. Seams for the work that lands here: the Overview's Data section
// (#2424's metrics grid), the Activity tab's history and occurrence rows
// (#2425, #2437), the Configure faces (#2439).

export default function SystemDetail(props: { id: string; host: DetailHost }) {
  const id = () => props.id;
  const navigate = useNavigate();
  const qc = useQueryClient();
  const me = useMe();
  // Unsaved Configure drafts (the system's and its sole location's) the tab rail asks about.
  const leave = createLeaveGuard();
  // Every age on the card reads one ticking clock: a poll that returns the
  // same report changes nothing a query can see, so an age pinned to the data
  // (or to the mount) would stop moving. The history and the charts read a
  // coarser clock, so a second's tick redraws only the ages.
  const now = createClock(CLOCK_TICK_MS);
  const coarse = createClock(HISTORY_TICK_MS);

  const view = useQuery(() => ({ queryKey: FLEET_VIEW_KEY, queryFn: fleetView }));
  const health = useQuery(() => ({ queryKey: systemHealthKey(id()), queryFn: () => systemHealth(id()), refetchInterval: LEAF_POLL_MS }));
  const declared = useQuery(() => ({ queryKey: systemSlotsKey(id()), queryFn: () => systemSlots(id()), refetchInterval: LEAF_POLL_MS }));
  // Each member's own health, polled off the system's members read: the
  // fleet view's dots (read once) stand in until it answers.
  const membersQ = useQuery(() => ({ queryKey: systemMembersKey(id()), queryFn: () => systemMembers(id()), refetchInterval: LEAF_POLL_MS }));

  const system = createMemo(() => (view.data?.systems ?? []).find((s) => s.id === id()));

  const locationId = () => system()?.location ?? null;
  const location = createMemo(() => (locationId() ? (view.data?.locations ?? []).find((l) => l.id === locationId()) : undefined));
  // A sole system is the room: the view wears the location's name. A shared
  // location keeps its own view, so a system there wears its own.
  const sole = createMemo(() => (view.data && locationId() ? systemsAtLocation(view.data, locationId()!).length === 1 : false));
  const hasBeneath = createMemo(() => (view.data && locationId() ? (childrenIndex(view.data).get(locationId()!) ?? []).length > 0 : false));

  const strip = createMemo(() => (health.data ? slotStrip(health.data) : undefined));
  const systemsList = useQuery(() => ({ queryKey: SYSTEMS_KEY, queryFn: listSystems }));
  const standard = createMemo(() => (systemsList.data ?? []).find((x) => x.id === id())?.standard);

  const vm = createMemo(() => {
    if (!view.data || !health.data || !declared.data) return undefined;
    return systemZoomVM(health.data, declared.data, view.data, id());
  });

  const scale = useSeverity();
  const alarms = createMemo(() => (health.data ? alarmRows(health.data, scale().levels) : []));
  // Each alarm row in the Why strip opens its timeline in place (#950).
  const timelines = createTimelineToggles();
  // The strip reads red when any alarm on it is at an outage level (the
  // catalog's flag, #917), amber when they only degrade.
  const inOutage = createMemo(() => alarms().some((a) => scale().outage(a.severity)));
  const bodyOf = createMemo(() => (vm() ? componentCards(vm()!) : undefined));
  const metricsQ = useQuery(() => ({ queryKey: systemMetricsKey(id()), queryFn: () => systemMetrics(id()), refetchInterval: LEAF_POLL_MS }));
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

  // Three tabs (#826): Overview, Activity, Configure. The retired tab
  // addresses map onto the tab that absorbed them, so an old link still
  // lands on its content (lib/tabstate, one resolver for every kind).
  const tabs = createMemo(() => [
    { key: "overview", label: "Overview" },
    { key: "activity", label: "Activity" },
    ...(can(me.data, "system", "update") ? [{ key: "configure", label: "Configure" }] : []),
  ]);
  const tabState = createTabState(props.host, tabs, { legacy: { map: "overview", data: "overview", history: "activity", events: "activity", logs: "activity" } });
  const tab = tabState.active;
  // Every member's alarm history, cleared ones included: the causes beside
  // the health spans. One read for the whole system (#940), so the poll is
  // one request however many members the room has.
  const memberIds = createMemo(() => {
    const b = bodyOf();
    if (!b) return [] as { name: string; componentId: string }[];
    const all = [...b.cards, ...b.groups.flatMap((g) => g.memberCards)];
    return all.map((c) => ({ name: c.name, componentId: c.componentId }));
  });
  const alarmHistory = useQuery(() => ({
    queryKey: systemAlarmsKey(id()),
    queryFn: () => systemAlarms(id()),
    enabled: tab() === "activity",
    refetchInterval: LEAF_POLL_MS,
  }));
  // One series query per declared metric, alive only on Overview, where the
  // Data table draws every sparkline at once (#795 review).
  // The body's one time range (#2304). On a page it is held in the address,
  // so a link carries it; in a blade it is the blade's own (#2423), so a
  // blade never rewrites the page beneath it. A preset slides to now and
  // reads `hours` back from it; an absolute range reads its own [from, to)
  // through the views binding and holds still, so it never polls.
  const { range: asked, setRange } = props.host === "page" ? useTimeRange() : bladeRange();
  // An absolute range reads through the views read, which needs view:read: a
  // shared link opened by a caller without it reads the default instead, and
  // the data section says why rather than drawing empty charts.
  const canViews = () => can(me.data, "view", "read");
  const rangeDenied = () => asked().kind === "absolute" && !!me.data && !canViews();
  const range = (): TimeRange => (rangeDenied() ? DEFAULT_RANGE : asked());
  const hours = () => rangeHours(range());
  // A preset's window is fixed by its key, not the clock, so a tick never
  // changes a query key.
  const seriesWindowMs = () => {
    const h = hours();
    if (h !== undefined) return h * 3600_000;
    const r = resolveRange(range(), 0);
    return r.to - r.from;
  };
  // An absolute range reaching back past a day steps in whole hours: the
  // views read answers the hours past the raw samples from the hourly trends
  // only at a whole-hour step, where a finer one would read them as empty.
  const seriesStepS = () => {
    const r = range();
    return r.kind === "absolute" ? rangeStepSeconds(seriesWindowMs(), r.from < Date.now() - 86400_000 || undefined) : rangeStepSeconds(seriesWindowMs());
  };
  const seriesColumn = () => {
    const r = range();
    return r.kind === "preset" ? `Last ${r.preset}` : "In range";
  };
  // The charts' right edge: now for a preset, the range's end otherwise.
  const seriesNow = () => (hours() !== undefined ? historyNow() : resolveRange(range(), 0).to);
  // One series query per declared metric, alive only on Overview, where the
  // Data table draws every sparkline at once (#795 review).
  const seriesQueries = useQueries(() => ({
    queries: kpiMetrics().map((m) => {
      // The chart is the series whose latest value sits beside it: a bucket
      // is one instance (#1485), so a sampled row names the instance it shows.
      const instance = m.is_sampled ? m.instance : undefined;
      const h = hours();
      const step = seriesStepS();
      if (h === undefined) {
        const { from, to } = resolveRange(range(), 0);
        return {
          queryKey: metricBucketsBetweenKey("systems", id(), m.metric_type_name, from, to, step, instance),
          queryFn: () => metricBucketsBetween("systems", id(), m.metric_type_name, from, to, step, instance),
          // Not before the caller is known: the views read may be theirs to refuse.
          enabled: tab() === "overview" && !!me.data,
          // A range ending in the future still fills; one wholly past holds.
          refetchInterval: to > Date.now() ? SERIES_POLL_MS : false,
        };
      }
      return {
        queryKey: metricBucketsKey("systems", id(), m.metric_type_name, h, step, instance),
        queryFn: () => metricBuckets("systems", id(), m.metric_type_name, h, step, instance),
        enabled: tab() === "overview",
        // A day's sparkline does not visibly move in five seconds; the latest
        // value beside it rides the metrics read, which does.
        refetchInterval: SERIES_POLL_MS,
      };
    }),
  }));
  const metricSeriesData = () => kpiMetrics().map((_, i) => seriesQueries[i]?.data);
  const eventsQ = useQuery(() => ({ queryKey: systemEventsKey(id()), queryFn: () => systemEvents(id()), enabled: tab() === "activity", refetchInterval: LEAF_POLL_MS }));
  // The members' lines under the Logs filter (#1145): a severity threshold
  // and a source, applied by the read.
  const [logFilter, setLogFilter] = createSignal<LogFilter>({});
  const logsQ = useQuery(() => ({
    queryKey: systemLogsKey(id(), logFilter()),
    queryFn: () => systemLogs(id(), logFilter()),
    enabled: tab() === "activity",
    refetchInterval: LEAF_POLL_MS,
    placeholderData: (prev: SystemLogs | undefined) => prev,
  }));
  const logLines = () => logsQ.data?.logs ?? [];
  // The history's now: the coarse clock, but never behind the report's own
  // last edge, which a poll can bring in between two ticks (a stretch that
  // opened seconds ago would otherwise have a negative length and vanish).
  const historyNow = createMemo(() => {
    const t = health.data?.transitions ?? [];
    return Math.max(coarse(), t.length ? Date.parse(t[t.length - 1].ts) : 0);
  });
  const healthSpans = createMemo(() =>
    timelineSpans((health.data?.transitions ?? []).map((t) => ({ ts: t.ts, value: t.health })), health.data?.health ?? null, historyNow()),
  );
  const uptime = createMemo(() => uptimePct(healthSpans()));
  const stretches = createMemo(() => stretchesOf(healthSpans(), historyNow()));
  // A good uptime wears the muted healthy green (#1067); one that should
  // worry someone wears the trouble hue.
  const uptimeTone = () => {
    const u = uptime();
    if (u === null) return "none";
    const v = Number(u);
    return v >= 99 ? "good" : v >= 95 ? "warn" : "bad";
  };
  const [openStretch, setOpenStretch] = createSignal<number | null>(null);
  const windowAlarms = createMemo(() => {
    const byComponent = new Map<string, NonNullable<typeof alarmHistory.data>>();
    for (const a of alarmHistory.data ?? []) byComponent.set(a.component_id, [...(byComponent.get(a.component_id) ?? []), a]);
    const members: MemberAlarms[] = memberIds().map((m) => ({
      component: m.name,
      componentId: m.componentId,
      alarms: byComponent.get(m.componentId) ?? [],
    }));
    return memberAlarmRows(members, historyNow(), 30 * 24, scale().levels);
  });
  const otherAlarms = createMemo(() => {
    const inStretch = new Set(stretches().flatMap((inc) => stretchReasons(inc, windowAlarms()).map((r) => r.id)));
    return windowAlarms().filter((r) => !inStretch.has(r.id));
  });
  const comps = useQuery(() => ({ queryKey: COMPONENTS_KEY, queryFn: listComponents }));
  const prods = useQuery(() => ({ queryKey: PRODUCTS_KEY, queryFn: listProducts }));
  const ctypes = useQuery(() => ({ queryKey: COMPONENT_TYPES_KEY, queryFn: listComponentTypes }));

  // Each member's own health, one lookup shared by the component rows and the
  // map, so the two never disagree: the polled members read first, the fleet
  // view's dot as the fallback before the first poll lands.
  const componentHealth = createMemo(() => {
    const dots = new Map((system()?.dots ?? []).map((d) => [d.component, d] as const));
    const polled = new Map((membersQ.data ?? []).map((m) => [m.component_id, m.component_health] as const));
    return (cid: string) => healthOf(polled.get(cid) ?? dots.get(cid)?.health);
  });
  // Each member's label, icon, product, alarm and health: one context shared
  // by the component rows and the map (#1074), so a marker reads exactly like
  // its row.
  const memberCtx = createMemo((): MemberContext => {
    const comp = new Map((comps.data ?? []).map((c) => [c.id, c] as const));
    const prod = new Map((prods.data ?? []).map((p) => [p.name, p] as const));
    const types = new Map((ctypes.data ?? []).map((t) => [t.name, t] as const));
    const alarmOf = new Map(alarms().filter((a) => a.componentId).map((a) => [a.componentId!, a.message] as const));
    return {
      health: componentHealth(),
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
    };
  });
  const members = createMemo(() => {
    const b = bodyOf();
    return b ? memberModel(b, memberCtx()) : { rows: [], groups: [] };
  });
  const componentCount = () => (system()?.dots ?? []).length;

  // The ambient stack (the page's own, or the stack this blade sits on):
  // every member, alarm and map marker opens the component's blade on it
  // (ADR-0129's altitude rule), stacking wherever the body is.
  const blades = useBlades();
  const openComponent = (cid: string) => blades.push({ kind: "component", id: cid });
  const { destructive, err } = useEntityDelete({
    kindLabel: "system",
    id: props.id,
    name: () => systemsList.data?.find((s) => s.id === props.id)?.name,
    actions: () => systemsList.data?.find((s) => s.id === props.id)?.actions ?? [],
    remove: (sid) => deleteSystem(sid),
    onDeleted: () => (props.host === "blade" ? blades.close() : navigate("/explore")),
    invalidate: () => Promise.all([qc.invalidateQueries({ queryKey: [...SYSTEMS_KEY] }), qc.invalidateQueries({ queryKey: [...FLEET_VIEW_KEY] })]),
  });
  const pending = () => view.isPending || health.isPending || declared.isPending;

  return (
    <DetailGate
      noun="system"
      missing={!!view.data && !view.isFetching && !system()}
      pending={pending()}
      error={health.error ? describeError(health.error) : null}
      retrying={health.isFetching}
      onRetry={() => { void health.refetch(); void declared.refetch(); }}
    >
      <div data-testid="system-detail" data-host={props.host} class="@container flex flex-col gap-3">
        <Show when={err()}>
          <div role="alert" class="alert alert-error alert-soft text-sm"><span>{err()}</span></div>
        </Show>
          <Show when={locationId()}>
            {(pid) => <LocationCard locationId={pid()} showName={!sole()} path={props.host === "blade"} />}
          </Show>
          <section data-testid="system-card" class="card overflow-hidden border border-base-300 bg-base-200 p-0">
            <div data-testid="system-header" class="flex flex-wrap items-center gap-x-3 gap-y-1 border-b border-base-300 px-4 py-3 text-sm">
              <Eyebrow label="System" hint="The logical group of components that makes this location work, and the thing Omniglass monitors: its health is what the components filling its slots add up to." />
              <HealthBadge health={health.data?.health} size="sm" />
              <Show when={health.data && sinceOf(health.data, now())}>
                {(sc) => <span data-testid="since-line" class="tabular-nums text-base-content/70">since {fmtTime(sc().ts)} · {durationText(sc().ms)}</span>}
              </Show>
              <Show when={standard()}>
                <span class="text-base-content/30">·</span>
                <span data-testid="standard" class="text-base-content/70">{standardRow() ? entityLabel(standardRow()!) : standard()}</span>
              </Show>
              <span class="text-base-content/30">·</span>
              <span data-testid="component-count" class="tabular-nums text-base-content/70">{componentCount()} {componentCount() === 1 ? "component" : "components"}</span>
              {/* Slot arithmetic speaks only when hardware is MISSING: a
                  deployed room fills every slot (#785), and a down occupant
                  is a failure the alarms explain, not a gap. */}
              <Show when={strip() && strip()!.empty > 0}>
                <span class="text-base-content/30">·</span>
                <span data-testid="slots-short" class="tabular-nums text-incomplete">{strip()!.filled} of {strip()!.total} slots filled</span>
              </Show>
            </div>
            <TabRail
              tabs={tabs()}
              state={tabState}
              guard={leave}
              // The range governs Overview's charts. Activity's reads keep
              // their own fixed windows (#2304's follow-up), so the control
              // is not offered where it would be ignored.
              end={tab() === "overview" ? <TimeRangePicker range={range()} onChange={setRange} allowCustom={canViews()} /> : undefined}
            />
            <Show when={tab() === "configure"}>
              {/* A sole system's location has no page of its own: it configures
                  here, first, as the page reads (its card sits above the
                  system's), in its own section with its own Edit (#872). A
                  location shared by several systems keeps its own view and is
                  configured there. */}
              <Show when={sole() && locationId()} keyed>
                {(pid) => (
                  <section class="border-b border-base-300">
                    <div class="px-4 pt-4"><Eyebrow label="Location" hint="Where this system sits: the location's own name, type, parent, properties and tags. Its edit is its own, beside the system's." /></div>
                    <ConfigureFace kind="location" id={pid} editValue="location" testid="configure-face-location" guard={leave} host={props.host} />
                  </section>
                )}
              </Show>
              {/* Named only beside the location's section; alone it needs no name. */}
              <Show when={sole() && locationId()}>
                <div class="px-4 pt-4"><Eyebrow label="System" /></div>
              </Show>
              {/* The host keys the body on the id: another system is another
                  face, so the edit slot and drafts never carry over (#854). */}
              <ConfigureFace kind="system" id={id()} guard={leave} host={props.host} destructive={destructive} />
            </Show>
        <Show when={tab() === "activity"}>
          <section data-testid="history-tab" class="flex flex-col gap-4 p-4">
            <div class="flex flex-wrap items-stretch gap-3">
              <div data-testid="uptime-kpi" class="flex min-w-36 flex-col justify-between gap-1 rounded-box border border-base-300 bg-base-100 p-3.5">
                <Eyebrow label="Uptime" hint="The healthy share of the recorded window: the health KPI over time, the number a status page leads with. The timeline beside it is the same record drawn out." />
                <span
                  class="font-mono text-3xl tabular-nums"
                  classList={{
                    "text-healthy": uptimeTone() === "good",
                    "text-warning": uptimeTone() === "warn",
                    "text-error": uptimeTone() === "bad",
                  }}
                >
                  {uptime() ?? "–"}<span class="text-lg">%</span>
                </span>
                <span class="text-[10px] text-base-content/45">of the last 30 days</span>
              </div>
              <div class="flex min-w-60 flex-1 flex-col justify-center rounded-box border border-base-300 bg-base-100 p-3.5" data-testid="health-history-full">
                <HealthHistory compact transitions={health.data?.transitions ?? []} health={health.data?.health} />
                <div class="relative mt-1 h-2">
                  <For each={windowAlarms()}>
                    {(inc) => (
                      <span
                        data-testid={`alarm-marker-${inc.id}`}
                        class="absolute top-0 h-0 w-0 -translate-x-1/2 border-x-4 border-b-6 border-x-transparent"
                        classList={{ "border-b-error": scale().outage(inc.severity), "border-b-warning": !scale().outage(inc.severity) }}
                        style={{ left: `${markerX(inc.raisedAt, historyNow(), 30 * 24 * 3600_000) * 100}%` }}
                        title={`${inc.message} · ${inc.component}`}
                      />
                    )}
                  </For>
                </div>
              </div>
            </div>

            <div>
              <Eyebrow label="Health history" hint="One entry per contiguous stretch away from healthy, however many health changes it contained, ongoing first. Expand an entry for the transitions inside it and the alarms that explain them." />
              <Show when={stretches().length > 0} fallback={<p class="mt-2 text-sm text-base-content/50">Nothing in this window. The system held healthy the whole way.</p>}>
                <ul data-testid="stretch-list" class="mt-2 flex flex-col gap-2">
                  <For each={stretches()}>
                    {(inc, idx) => {
                      const reasons = () => stretchReasons(inc, windowAlarms());
                      const open = () => openStretch() === idx();
                      return (
                        <li
                          data-testid={`stretch-${idx()}`}
                          class="rounded-box border"
                          classList={{
                            "border-error/45 bg-error/5": inc.worst === "outage",
                            "border-warning/45 bg-warning/5": inc.worst === "degraded",
                            "border-incomplete/45": inc.worst === "incomplete",
                            "border-base-300": inc.worst === "healthy",
                          }}
                        >
                          <button type="button" class="flex w-full cursor-pointer flex-wrap items-baseline gap-x-3 gap-y-1 px-3 py-2 text-left text-sm" onClick={() => setOpenStretch(open() ? null : idx())}>
                            <HealthBadge health={inc.worst} size="xs" />
                            <span class="font-medium">
                              {inc.worst === "incomplete" ? "Commissioning" : inc.worst === "outage" ? "Outage" : "Degraded"}
                              {inc.ongoing ? " · ongoing" : ""}
                            </span>
                            <span class="text-xs tabular-nums text-base-content/60">
                              {fmtTime(new Date(inc.from).toISOString())}
                              {" → "}
                              {inc.ongoing ? "now" : fmtTime(new Date(inc.to).toISOString())}
                              {" · "}
                              {durationText((inc.ongoing ? now() : inc.to) - inc.from)}
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
                                      <HealthBadge health={sp.value} size="xs" />
                                      <span class="tabular-nums">{fmtTime(new Date(sp.from).toISOString())}</span>
                                      <span class="text-base-content/45">held {durationText(sp.to - sp.from)}</span>
                                    </li>
                                  )}
                                </For>
                              </ul>
                              </Show>
                              <Show
                                when={reasons().length > 0}
                                fallback={<p class="mt-2 text-xs text-base-content/50">No alarm overlaps this stretch: a commissioning gap (a slot nobody staffed), not a failure.</p>}
                              >
                                <ul class="mt-2 flex flex-col gap-1">
                                  <For each={reasons()}>
                                    {(r) => (
                                      <li class="flex flex-wrap items-baseline gap-x-2 text-xs">
                                        <SeverityBadge name={r.severity} size="xs" />
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
                        <SeverityBadge name={r.severity} size="xs" />
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
                  {(e) => {
                    // Each row owns its toggle: a rule's fire and clear events
                    // carry the same alarm, and opening one opens only that row.
                    const eventTimelines = createTimelineToggles();
                    return (
                      <li class="flex flex-col gap-1.5 px-3 py-2">
                        <div class="flex flex-wrap items-baseline gap-x-2 gap-y-0.5">
                          <span class="text-xs tabular-nums text-base-content/50">{fmtTime(e.ts)}</span>
                          <span class="font-mono text-xs text-base-content/70">{e.owner}</span>
                          <span class="font-mono text-xs">{e.key}</span>
                          <span class="min-w-0 flex-1 truncate text-base-content/80">{e.message}</span>
                          {/* A rule's event names the rule and carries the alarm it moved (#951). */}
                          <Show when={e.derived_by_rule}>{(rule) => <RuleLink name={rule()} id={e.derived_by_rule_id} />}</Show>
                          <Show when={e.alarm_id && e.owner_kind === "component" && e.owner_id}>{eventTimelines.button(e.alarm_id!, e.message)}</Show>
                        </div>
                        <Show when={e.alarm_id && e.owner_kind === "component" && e.owner_id && eventTimelines.isOpen(e.alarm_id)}>
                          <AlarmTimeline component={e.owner_id} alarmId={e.alarm_id!} id={eventTimelines.panelId(e.alarm_id!)} label={eventTimelines.label(e.message)} />
                        </Show>
                      </li>
                    );
                  }}
                </For>
              </ul>
            </Show>
          </section>
        </Show>
        <Show when={tab() === "activity"}>
          <section data-testid="logs-tab" class="flex flex-col gap-2 p-4">
            <Eyebrow label="Logs" hint="The members' raw log lines merged newest first, each naming the component that wrote it. The last 24 hours, capped; an outpost's own logs live on the outpost." />
            <LogFilters filter={logFilter()} sources={logsQ.data?.sources ?? []} onChange={setLogFilter} />
            <Show
              when={logLines().length > 0}
              fallback={<p class="text-sm text-base-content/50">{logFilter().severity || logFilter().source ? "No line matches the filter in the window." : "No lines in the window."}</p>}
            >
              <div class="overflow-x-auto rounded-box border border-base-300 bg-base-100 p-2 font-mono text-[11.5px] leading-relaxed">
                <For each={logLines()}>
                  {(l) => (
                    <div class="whitespace-pre" classList={{ "text-error": l.severity === "error" || l.severity === "critical" || l.severity === "alert" || l.severity === "emergency", "text-warning": l.severity === "warning", "text-base-content/70": !l.severity || l.severity === "info" || l.severity === "notice" || l.severity === "debug" }}>
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
                      component, impairing which slot, since when. */}
                  <Show when={alarms().length > 0}>
                    <section data-testid="alarm-strip" class="flex flex-col gap-1.5 rounded-box border p-3" classList={{ "border-error/40 bg-error/5": inOutage(), "border-warning/40 bg-warning/5": !inOutage() }}>
                      <h2 class="eyebrow">Why</h2>
                      <For each={alarms()}>
                        {(a) => (
                          <div class="flex flex-col gap-1.5">
                            {/* The controls sit in one right-aligned group, so
                                every row's buttons line up whatever the
                                message's width. */}
                            <div class="flex flex-wrap items-baseline gap-x-2 gap-y-0.5 text-sm">
                              <SeverityBadge name={a.severity} />
                              <span>{a.message}</span>
                              <Show when={a.componentId} fallback={<span class="font-mono text-xs text-base-content/60">{a.component}</span>}>
                                {(cid) => <button type="button" class="cursor-pointer font-mono text-xs text-base-content/80 hover:underline" onClick={() => openComponent(cid())}>{a.component}</button>}
                              </Show>
                              <span class="text-xs text-base-content/50">impairs {a.slotLabel} · {durationText(now() - Date.parse(a.raisedAt))}</span>
                              <Show when={a.acknowledged}>
                                <span class="text-xs text-base-content/50">
                                  · acknowledged{a.acknowledgedBy ? ` by ${a.acknowledgedBy}` : ""}{a.acknowledgedAt ? ` ${fmtTime(a.acknowledgedAt)}` : ""}
                                </span>
                              </Show>
                              <span data-testid="alarm-controls" class="ml-auto flex shrink-0 items-center gap-1">
                                <AlarmActions
                                  componentId={a.componentId ?? ""}
                                  alarm={{ id: a.id, message: a.message, component: a.component, raised_at: a.raisedAt, acknowledged: a.acknowledged }}
                                  show={{ ack: !!a.componentId, troubleshoot: true, timeline: a.componentId ? timelines : undefined }}
                                />
                              </span>
                            </div>
                            <Show when={a.componentId && timelines.isOpen(a.id)}>
                              <AlarmTimeline component={a.componentId!} alarmId={a.id} id={timelines.panelId(a.id)} label={timelines.label(a.message)} />
                            </Show>
                          </div>
                        )}
                      </For>
                    </section>
                  </Show>
                  <section class="flex flex-col gap-2">
                    <Eyebrow label="Components" hint="The pieces of this system, each with the slot it fills. A slot gets its own header only where it says something a column cannot: more than one wanted, a shortfall, or nobody staffing it yet." />
                    <MemberRows rows={members().rows} groups={members().groups} onOpen={openComponent} />
                  </section>
                  <Show when={mapDecl()}>
                    {(decl) => (
                      // SystemMap pads itself; the negative margin lines its
                      // eyebrow up with the sections around it.
                      <div class="-m-4">
                        <SystemMap decl={decl()} markers={mapMarkers(decl(), z())} member={memberCtx()} onOpen={openComponent} />
                      </div>
                    )}
                  </Show>
        <Show when={kpiMetrics().length > 0}>
          <section data-testid="data-tab" class="flex flex-col gap-3">
            <Eyebrow label="Data" hint="Every metric the standard declares, stacked: the page's time range as a sparkline beside the latest value. Click a row for the full chart. Raw samples, capped; a series still on its contract default has nothing to chart yet." />
            <Show when={rangeDenied()}>
              <p data-testid="range-denied" role="status" class="text-xs text-warning">
                This link's custom range is read through views, which needs view:read. Showing the last 24 hours instead.
              </p>
            </Show>
            <div class="overflow-x-auto rounded-box border border-base-300">
              <table class="table table-sm">
                <thead>
                  <tr>
                    <th>Metric</th>
                    <th>{seriesColumn()}</th>
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
                            <TimeseriesChart spark buckets={metricSeriesData()[i()]?.buckets ?? []} resolutionMs={seriesStepS() * 1000} now={seriesNow()} windowMs={seriesWindowMs()} />
                          </td>
                          <td class="text-right font-mono tabular-nums">{m.value === null || m.value === undefined ? "" : String(m.value)}</td>
                          <td class="text-right text-[10px] text-base-content/45">
                            <Show when={m.source_rule && m.source_rule_id} fallback={m.is_sampled ? "sampled" : "contract default"}>
                              <span onClick={(e) => e.stopPropagation()}><CalcRuleLink name={m.source_rule!} id={m.source_rule_id!} /></span>
                            </Show>
                          </td>
                        </tr>
                        <Show when={seriesPick() === m.metric_type_name}>
                          <tr>
                            <td colspan="4" class="bg-base-200/40">
                              <TimeseriesChart buckets={metricSeriesData()[i()]?.buckets ?? []} resolutionMs={seriesStepS() * 1000} now={seriesNow()} windowMs={seriesWindowMs()} />
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
          <Show when={sole() && hasBeneath() && locationId()}>
            {(pid) => (
              <section data-testid="beneath" class="flex flex-col gap-2">
                <Eyebrow label={`Beneath ${location() ? entityLabel(location()!) : "this location"}`} hint="The locations inside this one, as Explore draws them, counted by the systems they hold." />
                <OutlineWorkspace rootId={pid()} host={props.host} />
              </section>
            )}
          </Show>
      </div>
    </DetailGate>
  );
}

// A blade's own time range (#2423): the same shape useTimeRange gives a page,
// held in a signal, so a range chosen in a blade never touches the address.
function bladeRange(): { range: () => TimeRange; setRange: (r: TimeRange) => void } {
  const [range, setRange] = createSignal<TimeRange>(DEFAULT_RANGE);
  return { range, setRange };
}
