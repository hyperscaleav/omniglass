import { For, Show, createMemo, createSignal } from "solid-js";
import Page from "../components/Page";
import { A, useSearchParams } from "@solidjs/router";
import SegmentedControl from "../components/ui/SegmentedControl";
import AlarmDuration from "../components/AlarmDuration";
import { keepPreviousData, useQuery } from "@tanstack/solid-query";
import FlatList, { type FlatColumn } from "../components/FlatList";
import Button from "../components/Button";
import SeverityBadge from "../components/SeverityBadge";
import AlarmActions from "../components/AlarmActions";
import { SnoozedBadge } from "../components/SnoozeButton";
import AlarmTimeline from "../components/AlarmTimeline";
import KVStacked from "../components/KVStacked";
import { FLEET_ALARMS_PAGE, alarmDuration, fleetAlarms, fleetAlarmsKey, type Alarm } from "../lib/alarms";
import { alarmFilterKeys, chipsFromParams, paramsFromChips, queryFromChips, stateFromParams, type AlarmState } from "../lib/alarmqueue";
import { LEAF_POLL_MS } from "../lib/component_leaf";
import { can, useMe } from "../lib/auth";
import Time from "../components/Time";
import { fmtTime, rel } from "../lib/format";
import { useSeverity } from "../lib/severity";
import type { Chip } from "../lib/predicate";

// Alarms (#2333): the fleet's alarm queue, the page an operator triages from.
// Every active alarm the caller may read, worst severity first and newest first
// within a level, one row each, with where it is (component, systems, location),
// since when, and where it stands (acknowledged, snoozed). It wears the shared
// list stack (FlatList over ListShell and FilterBar, as the audit trail does).
//
// The filter is the address. Severity, location (a subtree), ack and snoozed
// are chips, and the chips are `?severity=&location=&ack=&snoozed=`, so the
// page handed to a colleague or to omnibot (pageSubject) reads as it was left.
// The chips narrow the queue on the server, where it is read, so a filter holds
// over a queue longer than the pages loaded; a page is FLEET_ALARMS_PAGE rows
// and "Load more" asks for the next.
//
// The state (#2426) is held in the address too, `state=active|cleared|all`: the
// queue an operator works, the history of what cleared (each row with when it
// cleared and how long it was raised), or both. A cleared row opens the same
// drawer with its history; an open row's Duration ticks.
//
// Live means a poll: no alarm stream exists and the ingest tail does not carry
// rule-derived transitions, so the queue refetches at the leaf cadence, and an
// acknowledge or snooze refreshes it at once (FLEET_ALARMS_PREFIX).

const ACTIONS_SURFACE = "alarm-queue";
// The row and its drawer offer the same actions: a drawer never offers less than the page.
const ROW_ACTIONS = { ack: true, snooze: true, troubleshoot: true, command: true };
// A cleared alarm has nothing left to acknowledge; a snooze still applies, since
// it is stored on the component and dedup key and quiets a flapping one.
const CLEARED_ACTIONS = { snooze: true, troubleshoot: true };
const actionsFor = (a: Alarm) => (a.cleared_at ? CLEARED_ACTIONS : ROW_ACTIONS);

const STATE_OPTIONS: { value: AlarmState; label: string }[] = [
  { value: "active", label: "Open" },
  { value: "cleared", label: "Cleared" },
  { value: "all", label: "All" },
];
const STATE_WORD: Record<AlarmState, string> = { active: "active", cleared: "cleared", all: "in all" };
const EMPTY: Record<AlarmState, string> = {
  active: "No active alarms in your scope.",
  cleared: "No cleared alarms in your scope.",
  all: "No alarms in your scope.",
};

const ms = (iso: string) => new Date(iso).getTime();

export default function Alarms() {
  const me = useMe();
  const scale = useSeverity();
  const [params, setParams] = useSearchParams();
  const chips = createMemo(() => chipsFromParams(params));
  const state = createMemo(() => stateFromParams(params));
  const setChips = (next: Chip[]) => {
    setPages(1);
    setParams(paramsFromChips(next, state()), { replace: true });
  };
  const setState = (next: string) => {
    setPages(1);
    setParams(paramsFromChips(chips(), next as AlarmState), { replace: true });
  };
  const [pages, setPages] = createSignal(1);

  const queue = useQuery(() => ({
    queryKey: fleetAlarmsKey(queryFromChips(chips(), state()), pages()),
    queryFn: () => fleetAlarms(queryFromChips(chips(), state()), pages()),
    refetchInterval: LEAF_POLL_MS,
    // A new filter or page keeps the rows in view until the new ones land.
    placeholderData: keepPreviousData,
  }));
  const rows = () => queue.data?.alarms ?? [];
  const canAck = () => can(me.data, "alarm", "acknowledge");
  const canSnooze = () => can(me.data, "alarm", "snooze");
  // Reading dataUpdatedAt ties the relative times to each poll; it is never
  // later than the clock, so the value is the current time.
  const now = () => Math.max(queue.dataUpdatedAt, Date.now());

  const columns: FlatColumn<Alarm>[] = [
    { key: "severity", label: "Severity", width: "80px", cell: (a) => <SeverityBadge name={a.severity} /> },
    {
      key: "component", label: "Component", cell: (a) => (
        <span class="flex flex-col gap-0.5">
          <A href={`/components/${a.component_id}`} class="font-data text-sm hover:underline" onClick={(e) => e.stopPropagation()}>{a.component}</A>
          <span class="max-w-40 truncate text-xs text-base-content/55" title={a.message}>{a.message}</span>
        </span>
      ),
    },
    {
      key: "system", label: "System", cell: (a) => (
        <span class="font-data text-xs text-base-content/70">
          <For each={a.systems ?? []}>
            {(name, i) => (
              <>
                <Show when={i() > 0}>, </Show>
                <Show when={a.system_ids?.[i()]} fallback={name}>
                  {(id) => <A href={`/systems/${id()}`} class="hover:underline" onClick={(e) => e.stopPropagation()}>{name}</A>}
                </Show>
              </>
            )}
          </For>
        </span>
      ),
    },
    {
      key: "location", label: "Location", cell: (a) => (
        <span class="font-data text-xs text-base-content/70" title={a.location_path}>
          <Show when={a.location_id} fallback={a.location}>
            {(id) => <A href={`/locations/${id()}`} class="hover:underline" onClick={(e) => e.stopPropagation()}>{a.location}</A>}
          </Show>
        </span>
      ),
    },
    {
      key: "raised", label: "Raised", width: "84px", sortVal: (a) => ms(a.raised_at), cell: (a) => (
        // A fixed-width box: the age reads from the clock (9s, 17m ago), and its
        // width must not size the column, or the columns after it move between
        // two captures of the same page (the docs shot masks this cell).
        <Time iso={a.raised_at} now={now()} class="inline-block w-[8ch] whitespace-nowrap text-xs text-base-content/60" />
      ),
    },
    {
      key: "cleared", label: "Cleared", width: "84px", sortVal: (a) => (a.cleared_at ? ms(a.cleared_at) : Number.MAX_SAFE_INTEGER), cell: (a) => (
        <span class="inline-block w-[8ch] whitespace-nowrap text-xs tnum text-base-content/60" data-testid="alarm-cleared" title={a.cleared_at ? fmtTime(a.cleared_at) : "Still raised"}>
          <Show when={a.cleared_at} fallback="open">{(at) => rel(at(), now())}</Show>
        </span>
      ),
    },
    {
      key: "duration", label: "Duration", width: "80px", sortVal: (a) => alarmDuration(a, now()), cell: (a) => <AlarmDuration alarm={a} class="text-xs text-base-content/70" />,
    },
    {
      key: "state", label: "State", width: "160px", cell: (a) => (
        <span class="flex flex-wrap items-center gap-1">
          <Show when={a.acknowledged} fallback={<span class="badge badge-ghost badge-xs" title="Nobody has recorded seeing this alarm">new</span>}>
            <span class="badge badge-soft badge-success badge-xs whitespace-nowrap" title={a.acknowledged_by ? `Acknowledged by ${a.acknowledged_by}` : "Acknowledged"}>{a.acknowledged_by ? `acked by ${a.acknowledged_by}` : "acked"}</span>
          </Show>
          <SnoozedBadge alarm={a} />
        </span>
      ),
    },
    {
      key: "actions", label: "", width: "236px", cell: (a) => (
        // The row opens its drawer on click; the actions are their own targets.
        <span class="flex items-center justify-end gap-1" onClick={(e) => e.stopPropagation()} data-testid="alarm-actions">
          <AlarmActions componentId={a.component_id} alarm={a} surface={ACTIONS_SURFACE} show={actionsFor(a)} allow={{ snooze: canSnooze() }} />
        </span>
      ),
    },
  ];
  // A viewer sees state and no actions: the column is not drawn at all.
  const shownColumns = () => {
    let cols = canAck() || canSnooze() || can(me.data, "assistant", "use") ? columns : columns.filter((c) => c.key !== "actions");
    // The Cleared column is the history's: the open queue has nothing to show in it.
    if (state() === "active") cols = cols.filter((c) => c.key !== "cleared");
    return cols;
  };

  return (
    <Page route="/alarms" fill>
    <div class="min-h-0 flex-1" data-testid="alarm-queue">
      <FlatList<Alarm>
        config={{
          entity: { name: "alarm", plural: "alarms" },
          rows,
          loading: () => queue.isLoading,
          loadingRows: 8,
          fill: true,
          error: () => queue.error,
          filterKeys: () => alarmFilterKeys(chips, () => scale().levels.map((l) => l.name)),
          filterPlaceholder: "filter by severity, location, ack, snoozed, or a component",
          chips,
          onChips: setChips,
          get columns() { return shownColumns(); },
          get empty() { return EMPTY[state()]; },
          railExtra: () => (
            <SegmentedControl
              label="Alarm state"
              options={STATE_OPTIONS}
              value={state()}
              onChange={setState}
              size="xs"
              data-testid="alarm-state"
            />
          ),
          rowId: (a) => a.id,
          detail: (a) => ({
            title: <span class="font-data text-sm">{a.component}: {a.message}</span>,
            wide: true,
            body: (
              <div class="flex flex-col gap-4">
                <div class="grid grid-cols-2 gap-3 text-sm">
                  <KVStacked label="Severity" value={<SeverityBadge name={a.severity} />} />
                  <KVStacked label="Raised" value={<span class="text-xs"><Time iso={a.raised_at} now={now()} /></span>} />
                  <Show when={a.cleared_at}>
                    {(at) => <KVStacked label="Cleared" value={<span class="tnum text-xs">{fmtTime(at())}</span>} />}
                  </Show>
                  <KVStacked label={a.cleared_at ? "Lasted" : "Raised for"} value={<AlarmDuration alarm={a} class="text-xs" />} />
                  <KVStacked label="Location" value={<span class="font-data text-xs">{a.location_path || "unplaced"}</span>} />
                  <KVStacked label="Systems" value={<span class="font-data text-xs">{(a.systems ?? []).join(", ") || "none"}</span>} />
                </div>
                <div class="flex"><AlarmActions componentId={a.component_id} alarm={a} surface={`${ACTIONS_SURFACE}:drawer`} show={actionsFor(a)} allow={{ snooze: canSnooze() }} /></div>
                <AlarmTimeline component={a.component_id} alarmId={a.id} label={a.message} />
              </div>
            ),
          }),
          footer: ({ shown, total, filtering }) => (
            <>
              <span data-testid="alarm-count">
                {shown}
                <Show when={filtering}> of {total} loaded</Show> {STATE_WORD[state()]}
                <Show when={queue.data?.more}>, more below</Show>
              </span>
              <Show when={queue.data?.more}>
                <Button class="min-w-28 text-xs" disabled={queue.isFetching} onClick={() => setPages(pages() + 1)}>
                  {queue.isFetching ? "Loading…" : `Load ${FLEET_ALARMS_PAGE} more`}
                </Button>
              </Show>
            </>
          ),
        }}
      />
    </div>
    </Page>
  );
}
