import { For, Show, createMemo, createSignal, onCleanup, type JSX } from "solid-js";
import { A } from "@solidjs/router";
import { Popover } from "@kobalte/core/popover";
import { useQueries, useQuery } from "@tanstack/solid-query";
import Button from "./Button";
import Eyebrow from "./Eyebrow";
import AlarmTimeline from "./AlarmTimeline";
import { ChevronDown, ChevronRight, Sliders } from "./icons";
import { useCursor } from "../lib/cursor";
import { createClock } from "../lib/clock";
import { fmtTime } from "../lib/format";
import { durationText } from "../lib/timeline";
import { nearestByTime } from "../lib/chart";
import { rangeLabel, rangeStepSeconds, resolveRange, type TimeRange } from "../lib/time_range";
import { useSeverity } from "../lib/severity";
import { EVENT_RULES_KEY, listEventRules } from "../lib/event_rules";
import { effectiveMetrics, effectiveMetricsKey } from "../lib/component_metrics";
import { systemMetrics, systemMetricsKey } from "../lib/system_metrics";
import { ownerProperties, ownerPropertiesKey } from "../lib/owner_properties";
import { systemMembers, systemMembersKey } from "../lib/members";
import { metricBucketsBetween, metricBucketsBetweenKey } from "../lib/series";
import { LOG_SEVERITIES, type LogSeverity } from "../lib/logs";
import type { Command } from "../lib/commands";
import {
  alarmsOver,
  alarmsOverKey,
  commandsOver,
  commandsOverKey,
  eventsOver,
  eventsOverKey,
  healthOver,
  healthOverKey,
  logsOver,
  logsOverKey,
  propertiesOver,
  propertiesOverKey,
  PROPERTY_KEYS_MAX,
  type TimelineEvent,
  type TimelineKind,
  type TimelineLog,
} from "../lib/timeline_reads";
import {
  alarmSpans,
  alarmTip,
  changedPropertyKeys,
  clusterMarkers,
  cursorStep,
  healthSpans,
  markersInRange,
  metricLine,
  propertyBands,
  ruleInputs,
  spanAt,
  valueSpanTip,
  xOf,
  type AlarmSpan,
  type Cluster,
  type LaneFrame,
  type TimeWindow,
  type ValueSpan,
} from "../lib/timeline_lanes";

// TimelineOverlay (#2425): one component's or one system's history on one time
// axis. Lanes stack top down: health (a system adds a row per member), every
// alarm raised in range as its own span, the properties and metrics the
// operator picks (by default the ones the alarming rules watch, and the
// properties that changed), then events, log lines and commands as markers. It
// reads the page's range and shares the page's cursor (lib/cursor), so the
// crosshair crosses every lane and any metrics grid under the same provider,
// and the readout above the lanes says what each lane holds at that instant.
// Hovering a mark says what it is; clicking opens it below: an alarm's history,
// the event, log or command rows of a marker (a crowd of them is one counted
// marker). The derivation is lib/timeline_lanes, pure; this draws it.
//
// Plain props and no page logic, like MetricsGrid: a page mounts it with its
// own kind, id and range.

const HEALTH_FILL: Record<string, string> = {
  healthy: "var(--og-healthy)",
  incomplete: "var(--og-incomplete)",
  degraded: "var(--color-warning)",
  outage: "var(--color-error)",
};
const healthFill = (v: string) => HEALTH_FILL[v] ?? "color-mix(in oklch, var(--color-base-content) 20%, transparent)";

const SEVERITY_FILL: Record<string, string> = {
  gray: "color-mix(in oklch, var(--color-base-content) 45%, transparent)",
  blue: "var(--color-info)",
  green: "var(--color-success)",
  yellow: "var(--color-warning)",
  orange: "var(--og-average)",
  red: "var(--color-error)",
  darkred: "var(--og-disaster)",
};

const LOG_RANK: Record<string, number> = Object.fromEntries(LOG_SEVERITIES.map((s, i) => [s, i]));
const logFill = (sev?: string) => {
  const r = sev ? LOG_RANK[sev] : undefined;
  if (r === undefined) return "color-mix(in oklch, var(--color-base-content) 45%, transparent)";
  if (r <= LOG_RANK.error) return "var(--color-error)";
  if (r === LOG_RANK.warning) return "var(--color-warning)";
  return "color-mix(in oklch, var(--color-base-content) 45%, transparent)";
};

const FRAME: LaneFrame = { width: 640, padLeft: 0, padRight: 0 };
const ALARM_ROWS_SHOWN = 8;
const CLUSTER_GAP_PX = 10;

type Selected =
  | { kind: "alarm"; span: AlarmSpan }
  | { kind: "events"; rows: TimelineEvent[] }
  | { kind: "logs"; rows: TimelineLog[] }
  | { kind: "commands"; rows: Command[] };

type Group = "health" | "alarms" | "properties" | "metrics" | "events" | "logs" | "commands";

export default function TimelineOverlay(props: { kind: TimelineKind; id: string; range: TimeRange }): JSX.Element {
  // A minute's tick slides a preset, and every lane's key moves with it, so
  // each lane re-reads once a minute without a poll of its own; an absolute
  // range holds still and is read once.
  const now = createClock(60_000);
  const win = createMemo<TimeWindow>(() => resolveRange(props.range, Math.floor(now() / 60_000) * 60_000), resolveRange(props.range, now()), {
    equals: (a, b) => a.from === b.from && a.to === b.to,
  });
  const isComponent = () => props.kind === "components";
  const scale = useSeverity();

  // The plot's width, measured, so a drawer and a page draw at 1:1.
  const [width, setWidth] = createSignal(FRAME.width);
  const frame = createMemo<LaneFrame>(() => ({ ...FRAME, width: width() }));
  const measure = (el: HTMLElement) => {
    if (typeof ResizeObserver === "undefined") return;
    const ro = new ResizeObserver(([e]) => setWidth(Math.max(Math.round(e.contentRect.width), 120)));
    ro.observe(el);
    onCleanup(() => ro.disconnect());
  };
  const x = (t: number) => xOf(t, win(), frame());

  // ---- reads ---------------------------------------------------------------
  const healthQ = useQuery(() => ({
    queryKey: healthOverKey(props.kind, props.id, win().from, win().to),
    queryFn: () => healthOver(props.kind, props.id, win().from, win().to),
  }));
  const alarmsQ = useQuery(() => ({
    queryKey: alarmsOverKey(props.kind, props.id, win().from, win().to),
    queryFn: () => alarmsOver(props.kind, props.id, win().from, win().to),
  }));
  const membersQ = useQuery(() => ({
    queryKey: systemMembersKey(props.id),
    queryFn: () => systemMembers(props.id),
    enabled: !isComponent(),
  }));
  const members = () => (isComponent() ? [] : (membersQ.data ?? []));
  const memberHealthQ = useQueries(() => ({
    queries: members().map((m) => ({
      queryKey: healthOverKey("components", m.component_id, win().from, win().to),
      queryFn: () => healthOver("components", m.component_id, win().from, win().to),
      })),
  }));
  // A component's own commands; a system's are its members'.
  const commandOwners = () => (isComponent() ? [props.id] : members().map((m) => m.component_id));
  const commandsQ = useQueries(() => ({
    queries: commandOwners().map((c) => ({
      queryKey: commandsOverKey(c, win().from, win().to),
      queryFn: () => commandsOver(c, win().from, win().to),
      })),
  }));
  const eventsQ = useQuery(() => ({
    queryKey: eventsOverKey(props.kind, props.id, win().from, win().to),
    queryFn: () => eventsOver(props.kind, props.id, win().from, win().to),
  }));
  const [logSeverity, setLogSeverity] = createSignal<LogSeverity>("warning");
  const logsQ = useQuery(() => ({
    queryKey: logsOverKey(props.kind, props.id, win().from, win().to, logSeverity()),
    queryFn: () => logsOver(props.kind, props.id, win().from, win().to, logSeverity()),
  }));
  const rulesQ = useQuery(() => ({ queryKey: EVENT_RULES_KEY, queryFn: listEventRules, staleTime: 60_000 }));
  const propsListQ = useQuery(() => ({
    queryKey: ownerPropertiesKey(isComponent() ? "component" : "system", props.id),
    queryFn: () => ownerProperties(isComponent() ? "component" : "system", props.id),
  }));
  const metricsListQ = useQuery(() => ({
    queryKey: isComponent() ? effectiveMetricsKey(props.id) : systemMetricsKey(props.id),
    queryFn: () => (isComponent() ? effectiveMetrics(props.id) : systemMetrics(props.id)),
  }));

  // ---- lanes ---------------------------------------------------------------
  const health = createMemo(() =>
    healthSpans(healthQ.data?.transitions ?? [], win(), { carriedIn: healthQ.data?.carriedIn, current: healthQ.data?.health, now: Date.now() }),
  );
  const memberRows = createMemo(() =>
    members().map((m, i) => {
      const d = memberHealthQ[i]?.data;
      return { name: m.component, id: m.component_id, spans: healthSpans(d?.transitions ?? [], win(), { carriedIn: d?.carriedIn, current: d?.health ?? m.component_health, now: Date.now() }) };
    }),
  );
  const alarms = createMemo(() => alarmSpans(alarmsQ.data?.alarms ?? [], win(), Date.now()));
  const [allAlarms, setAllAlarms] = createSignal(false);
  const shownAlarms = () => (allAlarms() ? alarms() : alarms().slice(-ALARM_ROWS_SHOWN));
  const inputs = createMemo(() =>
    ruleInputs(
      alarms(),
      (rulesQ.data ?? []).map((r) => ({ id: r.id, name: r.name, sample_lane: r.sample_lane, sample_key: r.sample_key })),
    ),
  );

  // The effective properties, and whatever an alarming rule watches even when
  // it is no declared property (a per-endpoint signal such as reachability).
  const propertyKeys = createMemo(() => [...new Set([...(propsListQ.data ?? []).map((p) => p.property_type_name), ...inputs().properties])].sort().slice(0, PROPERTY_KEYS_MAX), [], {
    equals: (a, b) => a.join() === b.join(),
  });
  const propsQ = useQuery(() => ({
    queryKey: propertiesOverKey(props.kind, props.id, propertyKeys(), win().from, win().to),
    queryFn: () => propertiesOver(props.kind, props.id, propertyKeys(), win().from, win().to),
    enabled: propertyKeys().length > 0,
  }));

  const propertySeries = createMemo(() => propsQ.data ?? []);
  const [pickedProps, setPickedProps] = createSignal<string[] | undefined>(undefined);
  const shownProps = createMemo(() => {
    const picked = pickedProps();
    if (picked) return picked;
    return [...new Set([...inputs().properties, ...changedPropertyKeys(propertySeries(), win())])].filter((k) => propertyKeys().includes(k));
  });
  const propertyRows = createMemo(() =>
    shownProps().flatMap((key) =>
      propertySeries()
        .filter((s) => s.key === key)
        .map((s) => ({
          key,
          label: s.instance ? `${key} [${s.instance}]` : key,
          bands: propertyBands(s.points ?? [], s.carried_in, win(), Date.now()),
        })),
    ),
  );

  const metricNames = createMemo(() => [...new Set([...(metricsListQ.data ?? []).filter((m) => m.from_contract || m.is_sampled).map((m) => m.metric_type_name), ...inputs().metrics])]);
  const [pickedMetrics, setPickedMetrics] = createSignal<string[] | undefined>(undefined);
  const shownMetrics = createMemo(() => pickedMetrics() ?? inputs().metrics.filter((k) => metricNames().includes(k)), [], {
    equals: (a, b) => a.join() === b.join(),
  });
  // An absolute range reaching back past a day steps in whole hours, which the
  // hourly trends answer (#2304).
  const stepS = () => rangeStepSeconds(win().to - win().from, props.range.kind === "absolute" && win().from < Date.now() - 86400_000 ? true : undefined);
  const metricQ = useQueries(() => ({
    queries: shownMetrics().map((m) => ({
      queryKey: metricBucketsBetweenKey(props.kind, props.id, m, win().from, win().to, stepS()),
      queryFn: () => metricBucketsBetween(props.kind, props.id, m, win().from, win().to, stepS()),
      })),
  }));
  const METRIC_H = 36;
  const metricLines = createMemo(() =>
    shownMetrics().map((name, i) => ({
      name,
      unit: (metricsListQ.data ?? []).find((m) => m.metric_type_name === name)?.unit,
      layout: metricLine(metricQ[i]?.data?.buckets ?? [], win(), { ...frame(), height: METRIC_H, padY: 4 }, stepS() * 1000),
    })),
  );

  const commands = createMemo(() => commandsQ.flatMap((q) => q.data?.commands ?? []));
  const commandsMore = () => commandsQ.some((q) => q.data?.more);
  const eventClusters = createMemo(() => clusterMarkers(markersInRange(eventsQ.data?.events ?? [], (e) => e.ts, win()), win(), frame(), CLUSTER_GAP_PX));
  const logClusters = createMemo(() => clusterMarkers(markersInRange(logsQ.data?.logs ?? [], (l) => l.ts, win()), win(), frame(), CLUSTER_GAP_PX));
  const commandClusters = createMemo(() => clusterMarkers(markersInRange(commands(), (c) => c.issued_at, win()), win(), frame(), CLUSTER_GAP_PX));

  // ---- interaction ---------------------------------------------------------
  const cursor = useCursor();
  const [hovered, setHovered] = createSignal<string | undefined>(undefined);
  const [selected, setSelected] = createSignal<Selected | undefined>(undefined);
  const [collapsed, setCollapsed] = createSignal<Set<Group>>(new Set());
  const isOpen = (g: Group) => !collapsed().has(g);
  const toggle = (g: Group) => {
    const next = new Set(collapsed());
    if (next.has(g)) next.delete(g);
    else next.add(g);
    setCollapsed(next);
  };
  const cursorX = () => {
    const t = cursor.at();
    return t === undefined || t < win().from || t > win().to ? undefined : x(t);
  };
  const onMove = (e: MouseEvent & { currentTarget: Element }) => {
    const r = e.currentTarget.getBoundingClientRect();
    if (r.width <= 0) return;
    const f = Math.min(Math.max((e.clientX - r.left) / r.width, 0), 1);
    cursor.set(win().from + f * (win().to - win().from));
  };
  const onKey = (e: KeyboardEvent) => {
    if (e.key === "Escape") {
      cursor.set(undefined);
      return;
    }
    const dir = e.key === "ArrowLeft" ? -1 : e.key === "ArrowRight" ? 1 : 0;
    if (!dir) return;
    e.preventDefault();
    cursor.set(cursorStep(cursor.at(), win(), dir));
  };
  const fmt = (iso: string) => fmtTime(iso);
  const spanTip = (s: ValueSpan) => valueSpanTip(s, fmt, Date.now());
  const clusterTip = (c: Cluster<unknown>, noun: string, one: (item: never) => string) =>
    c.items.length === 1 ? one(c.items[0] as never) : `${c.items.length} ${noun} from ${fmt(new Date(c.from).toISOString())} to ${fmt(new Date(c.to).toISOString())}`;
  const eventTip = (e: TimelineEvent) => `${e.key} · ${e.message || "event"} · ${fmt(e.ts)}`;
  const logTip = (l: TimelineLog) => `${l.severity ?? "log"} · ${l.message} · ${fmt(l.ts)}`;
  const commandTip = (c: Command) => `${c.command_type} on ${c.component} · ${c.status} · issued ${fmt(c.issued_at)}${c.issuer ? ` by ${c.issuer}` : ""}`;

  // What every lane reads at the cursor: the instant, the health in force, the
  // alarms raised, and each shown metric's nearest bucket.
  const readout = createMemo(() => {
    const t = cursor.at();
    if (t === undefined || t < win().from || t > win().to) return undefined;
    const parts = [fmt(new Date(t).toISOString())];
    const h = spanAt(health(), t);
    if (h) parts.push(`health ${h.value}`);
    const up = alarms().filter((a) => t >= a.from && t <= a.to);
    // Each alarm raised at the instant, and how long it was raised in all.
    if (up.length === 0) parts.push("no alarm raised");
    for (const a of up.slice(-3)) parts.push(`${isComponent() ? "" : `${a.component} `}${a.rule ?? a.message} ${a.clearedAt === undefined ? `raised ${durationText(a.duration)} so far` : `raised for ${durationText(a.duration)}`}`);
    if (up.length > 3) parts.push(`${up.length - 3} more alarms raised`);
    for (const r of propertyRows()) {
      const b = spanAt(r.bands, t);
      if (b) parts.push(`${r.label} ${b.value}`);
    }
    for (const m of metricLines()) {
      const p = nearestByTime(m.layout.points, t, stepS() * 1000);
      if (p) parts.push(`${m.name} ${fmtV(p.value)}${m.unit ? ` ${m.unit}` : ""}`);
    }
    return parts.join(" · ");
  });

  const sevFill = (name: string) => SEVERITY_FILL[scale().levels.find((l) => l.name === name)?.color ?? "gray"] ?? SEVERITY_FILL.gray;

  // ---- drawing helpers -----------------------------------------------------
  // A lane row: its label, and a plot strip that carries the crosshair.
  const Row = (p: { label: JSX.Element; height: number; testid?: string; children?: JSX.Element; title?: string }) => (
    <div class="flex items-stretch" data-testid={p.testid}>
      <div class="flex w-36 shrink-0 items-center gap-1 truncate pr-2 text-[11px] text-base-content/70" title={p.title} style={{ height: `${p.height}px` }}>
        {p.label}
      </div>
      <svg class="block min-w-0 flex-1 overflow-visible" height={p.height} viewBox={`0 0 ${width()} ${p.height}`} preserveAspectRatio="none" onMouseMove={onMove}>
        {p.children}
        <Show when={cursorX() !== undefined}>
          <line data-testid="timeline-cursor" x1={cursorX()} x2={cursorX()} y1="0" y2={p.height} class="stroke-base-content/45" stroke-width="1" stroke-dasharray="3 2" pointer-events="none" />
        </Show>
      </svg>
    </div>
  );

  const Header = (p: { group: Group; label: string; count?: number; right?: JSX.Element; hint: string }) => (
    <div class="mt-2 flex items-center gap-1 border-b border-base-300 pb-0.5">
      <button
        type="button"
        class="flex cursor-pointer items-center gap-1 text-[11px] font-medium uppercase tracking-wide text-base-content/60 hover:text-base-content"
        aria-expanded={isOpen(p.group)}
        data-testid={`timeline-lane-${p.group}`}
        title={p.hint}
        onClick={() => toggle(p.group)}
      >
        <Show when={isOpen(p.group)} fallback={<ChevronRight size={12} />}>
          <ChevronDown size={12} />
        </Show>
        {p.label}
        <Show when={p.count !== undefined}>
          <span class="font-normal normal-case tabular-nums text-base-content/45">{p.count}</span>
        </Show>
      </button>
      <span class="ml-auto flex items-center gap-1">{p.right}</span>
    </div>
  );

  const Spans = (p: { spans: ValueSpan[]; height: number; fill: (v: string) => string; labels?: boolean }) => (
    <For each={p.spans}>
      {(s) => {
        const w = () => Math.max(x(s.to) - x(s.from), 1);
        return (
          <g onMouseEnter={() => setHovered(spanTip(s))} onMouseLeave={() => setHovered(undefined)}>
            <rect x={x(s.from)} y={2} width={w()} height={p.height - 4} rx="2" style={{ fill: p.fill(s.value) }}>
              <title>{spanTip(s)}</title>
            </rect>
            <Show when={p.labels && w() > s.value.length * 6 + 8}>
              <text x={x(s.from) + 4} y={p.height / 2 + 3.5} font-size="10" class="fill-base-content" pointer-events="none">
                {s.value}
              </text>
            </Show>
          </g>
        );
      }}
    </For>
  );

  const Markers = <T,>(p: { clusters: Cluster<T>[]; height: number; fill: (c: Cluster<T>) => string; tip: (c: Cluster<T>) => string; onPick: (c: Cluster<T>) => void; shape: "dot" | "diamond" | "triangle" }) => (
    <For each={p.clusters}>
      {(c) => {
        const cy = p.height / 2;
        const many = () => c.items.length > 1;
        const pick = () => p.onPick(c);
        return (
          <g
            role="button"
            tabindex="0"
            aria-label={p.tip(c)}
            class="cursor-pointer outline-none focus-visible:[&>*:first-child]:stroke-primary"
            onMouseEnter={() => setHovered(p.tip(c))}
            onMouseLeave={() => setHovered(undefined)}
            onFocus={() => {
              setHovered(p.tip(c));
              cursor.set((c.from + c.to) / 2);
            }}
            onClick={pick}
            onKeyDown={(e) => {
              if (e.key === "Enter" || e.key === " ") {
                e.preventDefault();
                pick();
              }
            }}
          >
            <title>{p.tip(c)}</title>
            <Show
              when={many()}
              fallback={
                p.shape === "dot" ? (
                  <circle cx={c.x} cy={cy} r="4" style={{ fill: p.fill(c) }} class="stroke-base-100" stroke-width="1.5" />
                ) : p.shape === "diamond" ? (
                  <rect x={c.x - 3.5} y={cy - 3.5} width="7" height="7" transform={`rotate(45 ${c.x} ${cy})`} style={{ fill: p.fill(c) }} class="stroke-base-100" stroke-width="1.5" />
                ) : (
                  <path d={`M${c.x - 5},${cy + 4} L${c.x + 5},${cy + 4} L${c.x},${cy - 5} Z`} style={{ fill: p.fill(c) }} class="stroke-base-100" stroke-width="1.5" />
                )
              }
            >
              <circle cx={c.x} cy={cy} r="8" style={{ fill: p.fill(c) }} class="stroke-base-100" stroke-width="1.5" />
              <text x={c.x} y={cy + 3.5} text-anchor="middle" font-size="9" class="fill-base-100 font-medium" pointer-events="none">
                {c.items.length > 99 ? "99+" : c.items.length}
              </text>
            </Show>
            {/* A hit target wider than the mark. */}
            <rect x={c.x - 9} y={0} width="18" height={p.height} fill="transparent" />
          </g>
        );
      }}
    </For>
  );

  const axisTicks = createMemo(() => {
    const span = win().to - win().from;
    const long = span > 2 * 86400_000;
    return [0, 0.25, 0.5, 0.75, 1].map((f) => {
      const t = win().from + f * span;
      const d = new Date(t);
      return {
        x: x(t),
        anchor: (f === 0 ? "start" : f === 1 ? "end" : "middle") as "start" | "end" | "middle",
        text: long ? d.toLocaleDateString(undefined, { month: "short", day: "numeric" }) : d.toLocaleTimeString(undefined, { hour: "2-digit", minute: "2-digit" }),
      };
    });
  });

  const loading = () => healthQ.isLoading || alarmsQ.isLoading;
  const failed = () => [healthQ, alarmsQ, eventsQ, logsQ].some((q) => q.isError) || commandsQ.some((q) => q.isError);

  return (
    <section data-testid="timeline-overlay" class="@container flex flex-col gap-1">
      <Eyebrow
        label="Timeline"
        hint="Everything this recorded over the page's time range on one axis: health, each alarm from raise to clear, the properties and metrics you pick, events, log lines and commands. Hover a lane, or focus the lanes and use the arrow keys, to read every lane at one instant; click a mark to open it."
        right={<span class="text-xs text-base-content/50">{rangeLabel(props.range)}</span>}
      />
      <p data-testid="timeline-readout" class="min-h-[1.25rem] truncate text-[11.5px] tabular-nums text-base-content/75" aria-live="polite" title={hovered() ?? readout()}>
        {hovered() ?? readout() ?? <span class="text-base-content/45">Hover the lanes to read them at one instant.</span>}
      </p>
      <Show when={failed()}>
        <div role="alert" class="alert alert-warning alert-soft py-1 text-xs">
          <span>Some lanes could not be read.</span>
          <Button size="xs" intent="quiet" onClick={() => void Promise.all([healthQ.refetch(), alarmsQ.refetch(), eventsQ.refetch(), logsQ.refetch(), ...commandsQ.map((q) => q.refetch())])}>
            Retry
          </Button>
        </div>
      </Show>
      <div
        class="flex flex-col rounded-field focus:outline-none focus-visible:ring-2 focus-visible:ring-primary/50"
        tabindex="0"
        role="group"
        aria-label="Timeline lanes; the arrow keys move the cursor, Escape clears it"
        aria-keyshortcuts="ArrowLeft ArrowRight Escape"
        onKeyDown={onKey}
        onMouseLeave={() => {
          cursor.set(undefined);
          setHovered(undefined);
        }}
      >
        {/* The measured plot column: an empty row the width of every strip. */}
        <div class="flex">
          <div class="w-36 shrink-0" />
          <div class="min-w-0 flex-1" ref={measure} />
        </div>

        <Header group="health" label="Health" hint="The health in force over the range, from the recorded edges: one change, one boundary." />
        <Show when={isOpen("health")}>
          <Row label={<span class="font-medium text-base-content/80">{isComponent() ? "This component" : "This system"}</span>} height={16} testid="timeline-health-row">
            <Spans spans={health()} height={16} fill={healthFill} labels />
          </Row>
          <For each={memberRows()}>
            {(m) => (
              <Row label={<A href={`/components/${m.id}`} class="link-hover truncate">{m.name}</A>} height={11} title={m.name} testid="timeline-member-row">
                <Spans spans={m.spans} height={11} fill={healthFill} />
              </Row>
            )}
          </For>
          <Show when={!loading() && health().length === 0}>
            <p class="pl-36 text-[11px] text-base-content/45">No health recorded in this range.</p>
          </Show>
        </Show>

        <Header
          group="alarms"
          label="Alarms"
          count={alarms().length}
          hint="Each alarm raised during the range, its own row, from raise to clear (or to now while still raised), in its severity's color."
          right={
            <Show when={alarms().length > ALARM_ROWS_SHOWN}>
              <Button size="xs" intent="quiet" onClick={() => setAllAlarms(!allAlarms())}>
                {allAlarms() ? `Newest ${ALARM_ROWS_SHOWN}` : `All ${alarms().length}`}
              </Button>
            </Show>
          }
        />
        <Show when={isOpen("alarms")}>
          <Show when={alarms().length > 0} fallback={<p class="pl-36 text-[11px] text-base-content/45">{loading() ? "Reading…" : "No alarm raised in this range."}</p>}>
            <For each={shownAlarms()}>
              {(a) => {
                const tip = () => alarmTip(a, fmt);
                const w = () => Math.max(x(a.to) - x(a.from), 2);
                const open = () => {
                  setSelected({ kind: "alarm", span: a });
                };
                return (
                  <Row label={<span class="truncate">{isComponent() ? (a.rule ?? a.message) : `${a.component} · ${a.rule ?? a.message}`}</span>} title={a.message} height={14} testid="timeline-alarm-row">
                    <g
                      role="button"
                      tabindex="0"
                      aria-label={tip()}
                      data-testid="timeline-alarm-span"
                      class="cursor-pointer outline-none"
                      onMouseEnter={() => setHovered(tip())}
                      onMouseLeave={() => setHovered(undefined)}
                      onFocus={() => {
                        setHovered(tip());
                        cursor.set(a.from);
                      }}
                      onClick={open}
                      onKeyDown={(e) => {
                        if (e.key === "Enter" || e.key === " ") {
                          e.preventDefault();
                          open();
                        }
                      }}
                    >
                      <title>{tip()}</title>
                      <rect x={x(a.from)} y={3} width={w()} height={8} rx={a.open ? 0 : 2} style={{ fill: sevFill(a.severity) }} classList={{ "opacity-60": a.acknowledged }} />
                      <Show when={w() > 40}>
                        <text x={x(a.from) + 4} y={10} font-size="8.5" class="fill-base-100 font-medium" pointer-events="none">
                          {durationText(a.duration)}
                        </text>
                      </Show>
                      <rect x={x(a.from) - 2} y={0} width={w() + 4} height={14} fill="transparent" />
                    </g>
                  </Row>
                );
              }}
            </For>
          </Show>
          <Show when={alarmsQ.data?.more}>
            <p class="pl-36 text-[11px] text-warning">The read stops at 500 alarms; narrow the range to see the rest.</p>
          </Show>
        </Show>

        <Header
          group="properties"
          label="Properties"
          count={propertyRows().length}
          hint="The value each picked property held over the range, a band per value. By default: the ones an alarming rule watches and the ones that changed."
          right={<KeyPicker label="Properties" all={propertyKeys()} shown={shownProps()} onChange={setPickedProps} />}
        />
        <Show when={isOpen("properties")}>
          <For each={propertyRows()} fallback={<p class="pl-36 text-[11px] text-base-content/45">No property changed in this range; pick one to show it.</p>}>
            {(r) => (
              <Row label={<span class="truncate font-data">{r.label}</span>} title={r.label} height={16} testid="timeline-property-row">
                <Spans
                  spans={r.bands}
                  height={16}
                  labels
                  fill={(v) => `color-mix(in oklch, var(--color-base-content) ${r.bands.findIndex((b) => b.value === v) % 2 ? 16 : 8}%, transparent)`}
                />
              </Row>
            )}
          </For>
        </Show>

        <Header
          group="metrics"
          label="Metrics"
          count={metricLines().length}
          hint="Each picked metric as its line over the range (the band is each bucket's low to high). By default: the metrics an alarming rule watches."
          right={<KeyPicker label="Metrics" all={metricNames()} shown={shownMetrics()} onChange={setPickedMetrics} />}
        />
        <Show when={isOpen("metrics")}>
          <For each={metricLines()} fallback={<p class="pl-36 text-[11px] text-base-content/45">No metric picked; pick one to lay it on the timeline.</p>}>
            {(m) => (
              <Row label={<span class="truncate font-data">{m.name}{m.unit ? ` (${m.unit})` : ""}</span>} title={m.name} height={METRIC_H} testid="timeline-metric-row">
                <path d={m.layout.bandPath} class="fill-primary/20" />
                <path d={m.layout.linePath} class="stroke-primary" stroke-width="1.5" fill="none" stroke-linejoin="round" />
                <Show when={m.layout.linePath === ""}>
                  <text x={4} y={METRIC_H / 2 + 3} font-size="10" class="fill-base-content/45">No data in range</text>
                </Show>
              </Row>
            )}
          </For>
        </Show>

        <Header group="events" label="Events" count={eventsQ.data?.events.length} hint="Each event recorded in range; a crowd of them is one marker with its count." />
        <Show when={isOpen("events")}>
          <Row label={<span class="text-base-content/50">{eventsQ.isLoading ? "Reading…" : eventsQ.data?.more ? "newest 3000" : ""}</span>} height={20} testid="timeline-events-row">
            <Markers
              clusters={eventClusters()}
              height={20}
              shape="diamond"
              fill={() => "var(--color-info)"}
              tip={(c) => clusterTip(c, "events", eventTip)}
              onPick={(c) => setSelected({ kind: "events", rows: c.items })}
            />
          </Row>
        </Show>

        <Header
          group="logs"
          label="Logs"
          count={logsQ.data?.logs.length}
          hint="Each log line at or above the severity picked here, colored by its severity."
          right={
            <select
              class="select select-xs w-auto"
              aria-label="Log severity at least"
              value={logSeverity()}
              onChange={(e) => setLogSeverity(e.currentTarget.value as LogSeverity)}
            >
              <For each={LOG_SEVERITIES}>{(s) => <option value={s}>{s} and worse</option>}</For>
            </select>
          }
        />
        <Show when={isOpen("logs")}>
          <Row label={<span class="text-base-content/50">{logsQ.isLoading ? "Reading…" : logsQ.data?.more ? "newest 3000" : ""}</span>} height={20} testid="timeline-logs-row">
            <Markers
              clusters={logClusters()}
              height={20}
              shape="dot"
              fill={(c) => logFill([...c.items].sort((a, b) => (LOG_RANK[a.severity ?? ""] ?? 9) - (LOG_RANK[b.severity ?? ""] ?? 9))[0]?.severity)}
              tip={(c) => clusterTip(c, "log lines", logTip)}
              onPick={(c) => setSelected({ kind: "logs", rows: c.items })}
            />
          </Row>
        </Show>

        <Header group="commands" label="Commands" count={commands().length} hint="Each command issued in range; open one to follow its arc from issue to settle." />
        <Show when={isOpen("commands")}>
          <Row label={<span class="text-base-content/50">{commandsMore() ? "newest 500 each" : ""}</span>} height={20} testid="timeline-commands-row">
            <Markers
              clusters={commandClusters()}
              height={20}
              shape="triangle"
              fill={(c) => (c.items.some((x) => x.status === "failed" || x.status === "timed-out") ? "var(--color-error)" : "var(--color-primary)")}
              tip={(c) => clusterTip(c, "commands", commandTip)}
              onPick={(c) => setSelected({ kind: "commands", rows: c.items })}
            />
          </Row>
        </Show>

        <div class="mt-1 flex">
          <div class="w-36 shrink-0" />
          <svg class="block min-w-0 flex-1 overflow-visible" height="14" viewBox={`0 0 ${width()} 14`} preserveAspectRatio="none">
            <line x1="0" x2={width()} y1="0.5" y2="0.5" class="stroke-base-content/15" />
            <For each={axisTicks()}>
              {(t) => (
                <text x={t.x} y="11" text-anchor={t.anchor} font-size="9.5" class="fill-base-content/50" style={{ "font-variant-numeric": "tabular-nums" }}>
                  {t.text}
                </text>
              )}
            </For>
          </svg>
        </div>
      </div>

      <Show when={selected()}>
        {(sel) => <Detail sel={sel()} onClose={() => setSelected(undefined)} fmt={fmt} />}
      </Show>
    </section>
  );
}

const fmtV = (v: number) => (Math.abs(v) >= 100 ? v.toFixed(0) : v.toFixed(1));

// KeyPicker chooses which properties or metrics a lane shows.
function KeyPicker(props: { label: string; all: string[]; shown: string[]; onChange: (keys: string[]) => void }): JSX.Element {
  const toggle = (k: string) => {
    const set = new Set(props.shown);
    if (set.has(k)) set.delete(k);
    else set.add(k);
    props.onChange(props.all.filter((x) => set.has(x)));
  };
  return (
    <Popover placement="bottom-end" gutter={6}>
      <Popover.Trigger class="btn btn-quiet btn-xs gap-1 font-normal" aria-label={`Pick ${props.label.toLowerCase()}`} disabled={props.all.length === 0}>
        <Sliders size={12} />
        Pick
      </Popover.Trigger>
      <Popover.Portal>
        <Popover.Content class="z-50 flex max-h-80 w-64 max-w-[calc(100vw-1rem)] flex-col gap-0.5 overflow-auto rounded-box border border-base-300 bg-base-100 p-2 text-sm shadow-2xl focus:outline-none">
          <p class="menu-title px-1 pt-0 pb-1 text-[10.5px]">{props.label} on the timeline</p>
          <For each={props.all}>
            {(k) => (
              <label class="flex cursor-pointer items-center gap-2 rounded-field px-1 py-1 hover:bg-base-content/5">
                <input type="checkbox" class="checkbox checkbox-xs" checked={props.shown.includes(k)} onChange={() => toggle(k)} />
                <span class="truncate font-data text-xs">{k}</span>
              </label>
            )}
          </For>
        </Popover.Content>
      </Popover.Portal>
    </Popover>
  );
}

// Detail is what a click opens under the lanes: an alarm's own history, or the
// rows a marker stands for.
function Detail(props: { sel: Selected; onClose: () => void; fmt: (iso: string) => string }): JSX.Element {
  const s = () => props.sel;
  return (
    <div data-testid="timeline-detail" class="mt-2 flex flex-col gap-1.5 rounded-box border border-base-300 bg-base-100 p-2.5">
      <div class="flex items-center gap-2">
        <span class="eyebrow">
          {s().kind === "alarm" ? "Alarm" : s().kind === "events" ? "Events" : s().kind === "logs" ? "Log lines" : "Commands"}
        </span>
        <Button size="xs" intent="quiet" class="ml-auto" onClick={props.onClose}>
          Close
        </Button>
      </div>
      <Show when={s().kind === "alarm" && (s() as { span: AlarmSpan }).span}>
        {(a) => (
          <>
            <p class="text-[12px] text-base-content/80">{alarmTip(a(), props.fmt)}</p>
            <AlarmTimeline component={a().componentId ?? a().component} alarmId={a().id} label={`Timeline of ${a().message}`} />
          </>
        )}
      </Show>
      <Show when={s().kind === "events" && (s() as { rows: TimelineEvent[] }).rows}>
        {(rows) => (
          <ul class="flex max-h-64 flex-col gap-0.5 overflow-auto text-[11.5px]">
            <For each={rows()}>
              {(e) => (
                <li class="flex gap-2">
                  <span class="shrink-0 tabular-nums text-base-content/50">{props.fmt(e.ts)}</span>
                  <span class="shrink-0 font-data">{e.key}</span>
                  <span class="min-w-0 truncate text-base-content/70">{e.message}</span>
                </li>
              )}
            </For>
          </ul>
        )}
      </Show>
      <Show when={s().kind === "logs" && (s() as { rows: TimelineLog[] }).rows}>
        {(rows) => (
          <ul class="flex max-h-64 flex-col gap-0.5 overflow-auto text-[11.5px]">
            <For each={rows()}>
              {(l) => (
                <li class="flex gap-2">
                  <span class="shrink-0 tabular-nums text-base-content/50">{props.fmt(l.ts)}</span>
                  <span class="shrink-0 font-data" style={{ color: logFill(l.severity) }}>{l.severity ?? "-"}</span>
                  <span class="min-w-0 truncate text-base-content/70" title={l.message}>{l.message}</span>
                </li>
              )}
            </For>
          </ul>
        )}
      </Show>
      <Show when={s().kind === "commands" && (s() as { rows: Command[] }).rows}>
        {(rows) => (
          <ul class="flex max-h-64 flex-col gap-0.5 overflow-auto text-[11.5px]">
            <For each={rows()}>
              {(c) => (
                <li class="flex items-baseline gap-2">
                  <span class="shrink-0 tabular-nums text-base-content/50">{props.fmt(c.issued_at)}</span>
                  <span class="shrink-0 font-data">{c.command_type}</span>
                  <span class="text-base-content/60">{c.status}</span>
                  <A class="link link-primary ml-auto shrink-0" href={`/components/${c.component_id}?tab=activity&command=${c.id}`}>
                    Open its arc
                  </A>
                </li>
              )}
            </For>
          </ul>
        )}
      </Show>
    </div>
  );
}
