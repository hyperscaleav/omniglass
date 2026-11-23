import { For, Show, createMemo, createUniqueId, onCleanup } from "solid-js";
import { useNavigate, useSearchParams } from "@solidjs/router";
import { useQueries, useQuery, useQueryClient } from "@tanstack/solid-query";
import TabRail from "./TabRail";
import ConfigureFace from "./ConfigureFace";
import EventsPanel from "./EventsPanel";
import LogsPanel from "./LogsPanel";
import CommandsPanel from "./CommandsPanel";
import DependenciesPanel from "./DependenciesPanel";
import IdentitiesPanel from "./IdentitiesPanel";
import HealthBadge from "./HealthBadge";
import Eyebrow from "./Eyebrow";
import DetailGate from "./DetailGate";
import LocationCard from "./LocationCard";
import AlarmActions from "./AlarmActions";
import { SnoozedBadge } from "./SnoozeButton";
import AlarmTimeline, { createTimelineToggles } from "./AlarmTimeline";
import SeverityBadge from "./SeverityBadge";
import SimulationSection from "./SimulationSection";
import { SimFaultChipFor } from "./SimFaultChip";
import ReachLadder from "./ReachLadder";
import SessionPill from "./SessionPill";
import SessionNotice from "./SessionNotice";
import PollTest from "./PollTest";
import InfoTip from "./InfoTip";
import { ChevronRight } from "./icons";
import { CalcRuleLink } from "./RuleLink";
import { useBlades } from "../lib/blades";
import { createLeaveGuard } from "../lib/leave_guard";
import { createTabState, type DetailHost } from "../lib/tabstate";
import { useEntityDelete } from "../lib/entity_delete";
import { useSeverity } from "../lib/severity";
import { ladderObserved } from "../lib/session";
import { FLEET_VIEW_KEY, fleetView } from "../lib/fleet";
import { COMPONENTS_KEY, deleteComponent, listComponents, type Component as FleetComponent } from "../lib/components";
import { componentSystemsKey, componentSystems } from "../lib/members";
import { REACHABILITY_KEY, getReachability, lastSeenAt, type ReachEndpoint } from "../lib/reachability";
import { componentAlarmsRead, componentAlarmsReadKey, splitAlarms } from "../lib/alarms";
import { lifecycleRows } from "../lib/lifecycle";
import { effectiveProperties, effectivePropertiesKey } from "../lib/component_properties";
import { effectiveMetrics, effectiveMetricsKey } from "../lib/component_metrics";
import { durationText } from "../lib/timeline";
import { getOutpost, outpostKey } from "../lib/outposts";
import { PRODUCTS_KEY, listProducts } from "../lib/products";
import { LEAF_OUTPOST_POLL_MS, LEAF_POLL_MS, collectionState, dotHealth, identityRows, instanceName, leafAlarmSince, membershipRows, spreadSummary, vitalRows, vitalValueText } from "../lib/component_leaf";
import { entityLabel } from "../lib/entities";
import { CLOCK_TICK_MS, createClock } from "../lib/clock";
import { createToggles } from "../lib/toggles";
import { systemSlots, systemSlotsKey, SYSTEM_SLOTS_PREFIX } from "../lib/system_slots";
import { SYSTEM_HEALTH_PREFIX, SYSTEM_HEALTHS_KEY } from "../lib/health";
import { isRoomOf, slotsOf } from "../lib/detail";
import { can, useMe } from "../lib/auth";
import { describeError, fmtTime } from "../lib/format";

// ComponentDetail (#637, reoriented in #872, one body for both hosts since
// #2423): a component's whole detail body, rendered by its page
// (`/components/:id`, host "page") and by the fleet blade (host "blade") alike,
// so a blade shows everything the page shows. The host decides only what a
// host must: where the tab lives (the page's ?tab=, the blade's own signal),
// whether Edit answers the address's ?edit=1, and where a Delete leaves to.
// Sections lay out by container query, never the viewport, so the same body
// reads as two columns on a page and one in a blade.
//
// A component is a piece of a system. Its location is a card of context (its
// own, or its system's standing in for one it does not have, and the card says
// which); then the component itself: health, since-when (what took it down),
// and its three tabs. Overview reads why, what it is, the systems it serves and
// the slot it fills in each, its vitals, and collection in context: the one
// place an outpost appears to an operator, because the distinction that matters
// is stated here plainly (a healthy outpost with a stale sample means the
// component or the path, not collection).
//
// Seams for the work that lands on this body: the Overview's section grid
// (#1836, #2424), the Activity tab's occurrence rows (#2425, #2437), the
// Collection section's endpoint rows (#2436), the Configure face (#2439).

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

export default function ComponentDetail(props: { id: string; host: DetailHost }) {
  const id = () => props.id;
  const navigate = useNavigate();
  const qc = useQueryClient();
  const me = useMe();
  // The ambient stack: the page's own, or the stack this blade sits on, so a
  // nested open stacks on whichever host it is in.
  const blades = useBlades();
  const leave = createLeaveGuard();
  // In a blade, closing or popping it asks the same guard a tab switch does.
  if (props.host === "blade") onCleanup(blades.guard({ kind: "component", id: props.id }, leave));
  // Three tabs (#826): Overview, Activity, Configure. A legacy ?tab=events
  // address lands on Activity, which absorbed it.
  const leafTabs = createMemo(() => [
    { key: "overview", label: "Overview" },
    { key: "activity", label: "Activity" },
    ...(can(me.data, "component", "update") || can(me.data, "component", "delete") ? [{ key: "configure", label: "Configure" }] : []),
  ]);
  const tabState = createTabState(props.host, leafTabs, { legacy: { events: "activity" } });
  const leafTab = tabState.active;
  // A command asked for by address (an issue's toast opens its row): a page's
  // fact; a blade has no address of its own.
  const openCommand = props.host === "page" ? useCommandParam() : () => undefined;

  const view = useQuery(() => ({ queryKey: FLEET_VIEW_KEY, queryFn: fleetView }));
  const components = useQuery(() => ({ queryKey: COMPONENTS_KEY, queryFn: listComponents }));
  const products = useQuery(() => ({ queryKey: PRODUCTS_KEY, queryFn: listProducts }));
  const component = createMemo<FleetComponent | undefined>(() => (components.data ?? []).find((c) => c.id === id()));

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
  // would keep reading fresh after its outpost died.
  const pageNow = createClock(CLOCK_TICK_MS);
  const scale = useSeverity();
  // Each alarm row opens its timeline in place (#950).
  const timelines = createTimelineToggles();
  const snoozeSurface = createUniqueId();
  const activeAlarms = createMemo(() => splitAlarms(alarmsQ.data?.alarms ?? [], scale().levels).active);
  // The strip reads red when any active alarm is at an outage level (the
  // catalog's flag, #917), amber when they only degrade.
  const inOutage = createMemo(() => activeAlarms().some((a) => scale().outage(a.severity)));
  const since = createMemo(() => leafAlarmSince(alarmsQ.data?.alarms ?? [], pageNow(), scale().levels));
  // The component's own health rides on its alarms read, polled; the fleet
  // view's dot (read once, for the location and the path) stands in until then.
  const componentHealth = createMemo(() => alarmsQ.data?.health ?? (view.data ? dotHealth(view.data, component()?.id ?? "") : null));
  const identity = createMemo(() => identityRows(propsQ.data ?? []));
  const lifecycle = createMemo(() => lifecycleRows(propsQ.data ?? [], metricsQ.data ?? []));
  const vitals = createMemo(() => vitalRows(metricsQ.data ?? []));
  // The vitals rows are kept by metric name, never by the objects a poll
  // hands back (each poll is fresh), so an opened row and its tooltip survive
  // the next poll, the rule the collection rows below follow (#1059).
  const vitalKeys = createMemo(() => vitals().map((v) => v.name), undefined, {
    equals: (a, b) => a.length === b.length && a.every((x, i) => x === b[i]),
  });
  const vitalByKey = createMemo(() => new Map(vitals().map((v) => [v.name, v])));
  const vitalSpreads = createToggles("vital-instances");

  const product = createMemo(() => (products.data ?? []).find((p) => p.name === component()?.product));
  const rows = createMemo(() => (view.data && memberships.data ? membershipRows(memberships.data, view.data) : []));
  // The outpost behind each endpoint, read one by one: a component has an outpost
  // or two, the fleet has many, and only these are on the card.
  const outpostNames = createMemo(() => [...new Set((reach.data?.endpoints ?? []).map((e) => e.outpost).filter((n): n is string => !!n))], undefined, {
    equals: (a, b) => a.length === b.length && a.every((x, i) => x === b[i]),
  });
  const outpostReads = useQueries(() => ({
    queries: outpostNames().map((n) => ({ queryKey: outpostKey(n), queryFn: () => getOutpost(n), refetchInterval: LEAF_OUTPOST_POLL_MS })),
  }));
  const outpostByName = createMemo(() => new Map(outpostReads.filter((q) => q.data).map((q) => [q.data!.name, q.data!])));
  // The collection rows are kept by endpoint id, never by the objects a poll
  // hands back (each poll is fresh: last_seen moves), so a row's ladder
  // tooltip and a retry waiting on its session survive the next poll (#1059).
  const reachKey = (it: ReachEndpoint, i: number) => it.id ?? `${it.endpoint}#${i}`;
  const reachKeys = createMemo(() => (reach.data?.endpoints ?? []).map(reachKey), undefined, {
    equals: (a, b) => a.length === b.length && a.every((x, i) => x === b[i]),
  });
  const reachByKey = createMemo(() => new Map((reach.data?.endpoints ?? []).map((it, i) => [reachKey(it, i), it])));
  // The slot this component fills in each system it serves: a membership
  // says THAT it belongs; the system's declared slots say what it does there.
  const slotReads = useQueries(() => ({
    queries: rows().filter((r) => r.systemId).map((r) => ({
      queryKey: systemSlotsKey(r.systemId!),
      queryFn: () => systemSlots(r.systemId!),
      staleTime: 30_000,
    })),
  }));
  const slotsIn = (systemId: string | null) => {
    if (!systemId) return [];
    const i = rows().filter((r) => r.systemId).findIndex((r) => r.systemId === systemId);
    const data = i >= 0 ? slotReads[i]?.data : undefined;
    return data ? slotsOf(component()?.name ?? "", data) : [];
  };

  const primary = createMemo(() => rows().find((r) => r.primary) ?? rows()[0]);
  // Its own location, or (when it has none) its primary system's standing in.
  const systemLocation = createMemo(() => {
    const sid = primary()?.systemId;
    return sid ? (view.data?.systems ?? []).find((s) => s.id === sid)?.location ?? null : null;
  });
  const locationId = () => component()?.location_id ?? systemLocation();
  const provenance = () => (component()?.location_id ? "set here" : systemLocation() ? "from its system" : undefined);

  // Delete leaves by the host's own way out. A blade is page-level, so it may
  // sit over a system workspace whose slot cards and health read their own
  // keys: refresh those too, or the deleted component's card stays.
  const { destructive, err } = useEntityDelete({
    kindLabel: "component",
    id: props.id,
    name: () => component()?.name,
    actions: () => component()?.actions ?? [],
    remove: (cid) => deleteComponent(cid),
    onDeleted: () => (props.host === "blade" ? blades.close() : navigate("/explore")),
    invalidate: () =>
      Promise.all([
        qc.invalidateQueries({ queryKey: [...COMPONENTS_KEY] }),
        qc.invalidateQueries({ queryKey: [...FLEET_VIEW_KEY] }),
        qc.invalidateQueries({ queryKey: [...SYSTEM_SLOTS_PREFIX] }),
        qc.invalidateQueries({ queryKey: [...SYSTEM_HEALTH_PREFIX] }),
        qc.invalidateQueries({ queryKey: [...SYSTEM_HEALTHS_KEY] }),
      ]),
  });
  // A blade has no gate of its own above it: the body says pending and miss.
  const pending = () => view.isPending || components.isPending;

  return (
    <DetailGate
      noun="component"
      missing={!pending() && !component() && !components.isFetching && !components.error}
      pending={pending()}
      error={components.error && !component() ? describeError(components.error) : null}
      retrying={components.isFetching}
      onRetry={() => { void components.refetch(); }}
    >
      <div data-testid="component-detail" data-host={props.host} class="@container flex flex-col gap-3">
        <Show when={err()}>
          <div role="alert" class="alert alert-error alert-soft text-sm"><span>{err()}</span></div>
        </Show>
        {/* In the room a system it serves makes, the location and the system
            are one thing, named under Systems it serves. A blade has no
            breadcrumb, so its card says where the location sits. */}
        <Show when={locationId() && !(view.data && isRoomOf(view.data, locationId(), rows().map((r) => r.systemId)))}>
          <LocationCard locationId={locationId()!} showName provenance={provenance()} path={props.host === "blade"} />
        </Show>
          <section data-testid="component-card" class="card overflow-hidden border border-base-300 bg-base-200 p-0">
            {/* State, age, identity: the header every detail view opens with.
                A component has no transitions read, so since-when is what
                took it down: the worst active alarm's age (#786). */}
            <div data-testid="leaf-header" class="flex flex-wrap items-center gap-x-3 gap-y-1 border-b border-base-300 px-4 py-3 text-sm">
              <Eyebrow label="Component" hint="A piece of a system. Its health is what its own checks and alarms say; the systems it serves read it through the slots it fills." />
              <HealthBadge health={componentHealth() ?? undefined} size="sm" />
              <Show when={since()}>
                {(sc) => <span data-testid="since-line" class="tabular-nums text-base-content/70">since {fmtTime(sc().ts)} · {durationText(sc().ms)}</span>}
              </Show>
              <SimFaultChipFor componentId={component()?.id ?? id()} name={component()?.name} />
              <span class="text-base-content/30">·</span>
              <span class="font-data text-xs text-base-content/60">{component()?.name}</span>
            </div>
            <TabRail tabs={leafTabs()} state={tabState} guard={leave} />
            <Show when={leafTab() === "configure"}>
              {/* The host keys the body on the id, so another component is
                  another face, never this one's drafts (#854). */}
              <ConfigureFace kind="component" id={id()} guard={leave} host={props.host} destructive={destructive} />
            </Show>
            <Show when={leafTab() === "activity"}>
              <div data-testid="activity-tab" class="flex flex-col gap-5 p-4">
                {/* What it was told and where each command stands (#1155),
                    above what it has been saying. */}
                <Show when={can(me.data, "command", "read")}>
                  <CommandsPanel component={id()} name={component()?.name ?? id()} componentId={component()?.id} open={openCommand()} productDriver={product()?.driver} onAddEndpoint={can(me.data, "endpoint", "create") ? () => blades.push({ kind: "endpoint-create", id: component()?.id ?? id() }) : undefined} />
                </Show>
                <EventsPanel name={id()} />
                {/* The component's own words beside its occurrences (#1145):
                    the log lines its driver read and its listeners left
                    unclaimed. */}
                <LogsPanel name={id()} />
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
                          {/* The message takes the slack and the controls sit in
                              one right-aligned group, so every row's buttons
                              line up whatever its message's width. */}
                          <div class="flex flex-wrap items-baseline gap-x-2 gap-y-0.5 text-sm">
                            <SeverityBadge name={a.severity} />
                            <span class="min-w-0 flex-1">{a.message}</span>
                            <Show when={a.raised_by_rule}>
                              {(rule) => <span class="shrink-0 text-[11px] text-base-content/45">by rule <span class="font-data">{rule()}</span></span>}
                            </Show>
                            <span class="shrink-0 text-xs text-base-content/50">
                              {durationText(pageNow() - Date.parse(a.raised_at))} · {a.acknowledged ? "acknowledged" : "unacknowledged"}
                            </span>
                            <span data-testid="alarm-controls" class="ml-auto flex shrink-0 items-center gap-1">
                              <SnoozedBadge alarm={a} />
                              <AlarmActions componentId={component()?.id ?? id()} alarm={a} surface={snoozeSurface} show={{ ack: true, snooze: true, troubleshoot: true, command: true, timeline: timelines }} />
                            </span>
                          </div>
                          <Show when={timelines.isOpen(a.id)}>
                            <AlarmTimeline component={component()?.id ?? id()} alarmId={a.id} id={timelines.panelId(a.id)} label={timelines.label(a.message)} />
                          </Show>
                        </div>
                      )}
                    </For>
                  </section>
                </Show>
                <div class="grid grid-cols-1 gap-3 @3xl:grid-cols-2">
              <section data-testid="leaf-identity" class="rounded-box border border-base-300 bg-base-100 p-3.5 text-sm">
                <Eyebrow label="What it is" />
                {/* Rows align on their first baseline (#1379), as the
                    Vitals below do: a monospace driver or model number
                    beside the card's text label. */}
                <dl class="mt-2 grid grid-cols-[6rem_1fr] items-baseline gap-x-3 gap-y-1">
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
                  <Show when={lifecycle().length > 0}>
                    <section data-testid="leaf-lifecycle" class="rounded-box border border-base-300 bg-base-100 p-3.5 text-sm">
                      <Eyebrow label="Lifecycle" hint="Install, warranty, support and end-of-life dates, and what replacing it costs. Set on the product, overridden here where this one differs." />
                      <dl class="mt-2 grid grid-cols-[8rem_1fr] items-baseline gap-x-3 gap-y-1">
                        <For each={lifecycle()}>
                          {(row) => (
                            <>
                              <dt class="text-base-content/50">{row.label}</dt>
                              <dd class="font-mono text-xs" data-lifecycle={row.name}>{row.text}</dd>
                            </>
                          )}
                        </For>
                      </dl>
                    </section>
                  </Show>
                  <section data-testid="leaf-memberships" class="rounded-box border border-base-300 bg-base-100 p-3.5 text-sm">
                    <Eyebrow label="Systems it serves" hint="Each system this component belongs to, and the slot it fills there. The primary is the system answered when a question names none." />
                    <Show when={rows().length > 0} fallback={<p class="mt-2 text-base-content/60">Not in any system yet.</p>}>
                      <ul class="mt-2 divide-y divide-base-300 rounded-field border border-base-300">
                        <For each={rows()}>
                          {(row) => (
                            <li class="flex flex-wrap items-center gap-x-2 gap-y-0.5 px-3 py-1.5">
                              <Show when={row.systemId} fallback={<span>{row.label}</span>}>
                                <button type="button" class="cursor-pointer hover:underline" onClick={() => blades.push({ kind: "system", id: row.systemId! })}>{row.label}</button>
                              </Show>
                              <For each={slotsIn(row.systemId)}>
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
                <Eyebrow label="Vitals" hint="The latest sample per series; a dot marks the component speaking, not a contract default standing in. A metric several endpoints collect shows the newest of them and says which." />
                {/* Rows align on their first baseline (#1379): the label is
                    the card's text and the value smaller monospace, and a
                    stretched row would start each at the top, lifting the
                    value above its label. */}
                <dl class="mt-2 grid grid-cols-[minmax(7rem,max-content)_minmax(5rem,max-content)_1fr] items-baseline gap-x-3 gap-y-1">
                  <For each={vitalKeys()}>
                    {(key) => (
                      <Show when={vitalByKey().get(key)}>
                        {(row) => (
                          <>
                            <dt class="text-base-content/50">{row().label}</dt>
                            <dd data-testid={`vital-${key}`} class="flex flex-wrap items-baseline gap-x-1.5 gap-y-0.5 font-mono text-xs">
                              <span title={vitalValueText(row().value) !== String(row().value) ? String(row().value) : undefined}>
                                {vitalValueText(row().value)}
                                <Show when={row().unit}>
                                  {" "}
                                  <span data-testid="vital-unit" class="text-base-content/50">{row().unit}</span>
                                </Show>
                              </span>
                              <Show when={row().sampled} fallback={<span class="text-[10px] text-base-content/40">contract default</span>}>
                                {/* Centered on the value's digits, not on its line box,
                                    which rests the dot on the baseline (#1379). */}
                                <span class="h-1.5 w-1.5 flex-none -translate-y-px self-center rounded-full bg-success" title="live series" />
                              </Show>
                              <Show when={row().sourceRuleId}>
                                <CalcRuleLink name={row().sourceRule!} id={row().sourceRuleId!} />
                              </Show>
                            </dd>
                            {/* Several endpoints collect it (#1020): the row
                                shows the newest, says which and of how many,
                                and opens to each one's latest. Its own column,
                                so the captions align whatever the values' widths. */}
                            <dd data-testid={`vital-spread-${key}`} class="flex items-baseline">
                              <Show when={row().spread}>
                                {(sp) => (
                                  <span class="inline-flex items-center gap-1 self-center text-[11px] text-base-content/55">
                                    <button
                                      type="button"
                                      class="inline-flex cursor-pointer items-center gap-0.5 hover:text-base-content"
                                      aria-expanded={vitalSpreads.isOpen(key)}
                                      aria-controls={vitalSpreads.panelId(key)}
                                      onClick={() => vitalSpreads.toggle(key)}
                                    >
                                      <span class={`text-base-content/40 transition-transform ${vitalSpreads.isOpen(key) ? "rotate-90" : ""}`}><ChevronRight size={11} /></span>
                                      {spreadSummary(sp())}
                                    </button>
                                    <InfoTip
                                      label={`which endpoint ${row().label} shows`}
                                      text="This metric has more than one live series here, one for each endpoint that collects it (or the component as a whole). The row shows the newest sample across them and names the one it came from; open it for each one's latest. An endpoint since deleted is left out."
                                    />
                                  </span>
                                )}
                              </Show>
                            </dd>
                            <Show when={row().spread && vitalSpreads.isOpen(key) ? row().spread : undefined}>
                              {(sp) => (
                                <dd class="col-span-2 col-start-2 mb-1">
                                  <ul id={vitalSpreads.panelId(key)} data-testid={`vital-instances-${key}`} class="flex flex-col gap-0.5 border-l border-base-300 pl-2.5 text-xs">
                                    <For each={sp().instances}>
                                      {(it) => (
                                        <li class="flex items-baseline gap-2">
                                          <span class="min-w-14 text-base-content/60" classList={{ "font-medium text-base-content/85": it.instance === sp().shown }}>{instanceName(it.instance)}</span>
                                          <span class="font-data" title={vitalValueText(it.value) !== String(it.value) ? String(it.value) : undefined}>
                                            {vitalValueText(it.value)}
                                            <Show when={row().unit}>{" "}<span class="text-base-content/50">{row().unit}</span></Show>
                                          </span>
                                          <span class="text-[11px] text-base-content/45">{ageWords(it.ts, pageNow())}</span>
                                        </li>
                                      )}
                                    </For>
                                  </ul>
                                </dd>
                              )}
                            </Show>
                          </>
                        )}
                      </Show>
                    )}
                  </For>
                </dl>
              </section>
            </Show>
            {/* Seam: each endpoint row below is what #2436's shared
                endpoint body takes over. */}
            <section data-testid="leaf-collection" class="rounded-box border border-base-300 bg-base-100 p-3.5 text-sm">
              <Eyebrow label="Collection" hint="How this component is read: each endpoint, the outpost collecting it, and the ladder it climbs bottom up (a ping, informational only; the transport answering, the gate; then the login). A held session's state sits beside it. A stale sample under a healthy outpost points at the component or the path, not at collection." />
              <Show when={reach.data && (reach.data.endpoints ?? []).length > 0} fallback={<p class="text-base-content/60">No endpoint declared yet, so nothing collects from this component.</p>}>
                <ul class="mt-1 flex flex-col gap-1.5">
                  <For each={reachKeys()}>
                    {(key) => (
                      <Show when={reachByKey().get(key)}>
                        {(iface) => {
                          const state = () => collectionState(iface(), iface().outpost ? outpostByName().get(iface().outpost!) : undefined, pageNow());
                          return (
                            <li class="flex flex-col gap-1.5">
                              <div class="flex flex-wrap items-center gap-2 text-xs">
                                <span class="min-w-14 font-medium">{iface().endpoint}</span>
                                {/* The ladder (#1059), so component-versus-path
                                    reads at a glance and the layered gate is
                                    visible: the ping answers the path, the
                                    transport answers the service, the login
                                    answers the credential. */}
                                {/* A push endpoint (#1024) has no ladder and
                                    no session: a sender posts to it on the
                                    server, and it reads last heard. */}
                                <Show
                                  when={iface().webhook}
                                  fallback={
                                    <>
                                      <Show when={ladderObserved(iface())}>
                                        <ReachLadder iface={iface()} />
                                      </Show>
                                      <SessionPill iface={iface()} now={pageNow()} />
                                    </>
                                  }
                                >
                                  <span class="text-base-content/60">hosted by the server</span>
                                </Show>
                                <Show when={iface().outpost}>
                                  <span class="text-base-content/60">via {iface().outpost}</span>
                                </Show>
                                <Show when={lastSeenAt(iface())}>
                                  {(ts) => <span class="text-base-content/50">{iface().webhook ? "last heard" : "last seen"} {ageWords(ts(), pageNow())}</span>}
                                </Show>
                                <span data-testid={`collection-${iface().endpoint}`} class="rounded border border-base-content/20 px-1.5 py-0.5">
                                  {
                                    {
                                      collecting: "collecting",
                                      "component-or-path": "stale sample; the outpost is healthy, so check the component or the path",
                                      "outpost-offline": "outpost offline",
                                      "no-outpost": "no outpost collecting it",
                                      down: "component reports down",
                                      rejected: "polls stopped",
                                      "session-down": "polls failing",
                                      unknown: "no sample yet",
                                      listening: "listening",
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
                              <Show when={iface().id}>{(eid) => <PollTest endpoint={eid()} />}</Show>
                            </li>
                          );
                        }}
                      </Show>
                    )}
                  </For>
                </ul>
              </Show>
            </section>
            {/* What it needs and what needs it (#2428). */}
            <Show when={component()}>
              {(c) => (
                <DependenciesPanel
                  component={c().id}
                  canUpdate={can(me.data, "component", "update") && (Array.isArray(c().actions) ? c().actions!.includes("update") : true)}
                />
              )}
            </Show>
            {/* A component that speaks for many owners (a conduit, #1389):
                the identities it fans out and the ones it heard unbound. It
                renders nothing on a component that speaks only for itself. */}
            <Show when={component()}>
              {(c) => (
                <IdentitiesPanel
                  component={c().id}
                  canUpdate={can(me.data, "component", "update") && (Array.isArray(c().actions) ? c().actions!.includes("update") : true)}
                />
              )}
            </Show>
            {/* A simulated component says so, with its live hits (#890). */}
            <Show when={component()}>{(c) => <SimulationSection componentId={c().id} />}</Show>
              </div>
            </Show>
          </section>
      </div>
    </DetailGate>
  );
}

function useCommandParam(): () => number | undefined {
  const [search] = useSearchParams();
  return () => {
    const raw = Array.isArray(search.command) ? search.command[0] : search.command;
    const n = raw ? Number(raw) : NaN;
    return Number.isInteger(n) && n > 0 ? n : undefined;
  };
}
