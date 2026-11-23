import { Show, createMemo, createSignal } from "solid-js";
import { A, useSearchParams } from "@solidjs/router";
import { keepPreviousData, useQuery } from "@tanstack/solid-query";
import FlatList, { type FlatColumn } from "../components/FlatList";
import Button from "../components/Button";
import SeverityBadge from "../components/SeverityBadge";
import AcknowledgeButton from "../components/AcknowledgeButton";
import { SnoozeControl, SnoozedBadge } from "../components/SnoozeButton";
import TroubleshootButton from "../components/TroubleshootButton";
import AlarmTimeline from "../components/AlarmTimeline";
import KVStacked from "../components/KVStacked";
import { FLEET_ALARMS_PAGE, fleetAlarms, fleetAlarmsKey, type Alarm } from "../lib/alarms";
import { alarmFilterKeys, chipsFromParams, paramsFromChips, queryFromChips } from "../lib/alarmqueue";
import { LEAF_POLL_MS } from "../lib/component_leaf";
import { can, useMe } from "../lib/auth";
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
// Live means a poll: no alarm stream exists and the ingest tail does not carry
// rule-derived transitions, so the queue refetches at the leaf cadence, and an
// acknowledge or snooze refreshes it at once (FLEET_ALARMS_PREFIX).

const ACTIONS_SURFACE = "alarm-queue";

export default function Alarms() {
  const me = useMe();
  const scale = useSeverity();
  const [params, setParams] = useSearchParams();
  const chips = createMemo(() => chipsFromParams(params));
  const setChips = (next: Chip[]) => {
    setPages(1);
    setParams(paramsFromChips(next), { replace: true });
  };
  const [pages, setPages] = createSignal(1);

  const queue = useQuery(() => ({
    queryKey: fleetAlarmsKey(queryFromChips(chips()), pages()),
    queryFn: () => fleetAlarms(queryFromChips(chips()), pages()),
    refetchInterval: LEAF_POLL_MS,
    // A new filter or page keeps the rows in view until the new ones land.
    placeholderData: keepPreviousData,
  }));
  const rows = () => queue.data?.alarms ?? [];
  const canAck = () => can(me.data, "alarm", "acknowledge");
  const canSnooze = () => can(me.data, "alarm", "snooze");
  const now = () => (queue.dataUpdatedAt, Date.now());

  const columns: FlatColumn<Alarm>[] = [
    { key: "severity", label: "Severity", width: "110px", cell: (a) => <SeverityBadge name={a.severity} /> },
    {
      key: "component", label: "Component", cell: (a) => (
        <span class="flex flex-col gap-0.5">
          <A href={`/components/${a.component_id}`} class="font-data text-sm hover:underline" onClick={(e) => e.stopPropagation()}>{a.component}</A>
          <span class="truncate text-xs text-base-content/55" title={a.message}>{a.message}</span>
        </span>
      ),
    },
    { key: "system", label: "System", cell: (a) => <span class="font-data text-xs text-base-content/70">{(a.systems ?? []).join(", ")}</span> },
    { key: "location", label: "Location", cell: (a) => <span class="font-data text-xs text-base-content/70" title={a.location_path}>{a.location}</span> },
    {
      key: "raised", label: "Raised", width: "110px", cell: (a) => (
        <span class="whitespace-nowrap text-xs tnum text-base-content/60" title={fmtTime(a.raised_at)}>{rel(a.raised_at, now())}</span>
      ),
    },
    {
      key: "state", label: "State", width: "170px", cell: (a) => (
        <span class="flex flex-wrap items-center gap-1">
          <Show when={a.acknowledged} fallback={<span class="badge badge-ghost badge-xs" title="Nobody has recorded seeing this alarm">new</span>}>
            <span class="badge badge-soft badge-success badge-xs" title={a.acknowledged_by ? `Acknowledged by ${a.acknowledged_by}` : "Acknowledged"}>acked</span>
          </Show>
          <SnoozedBadge alarm={a} />
        </span>
      ),
    },
    {
      key: "actions", label: "", width: "250px", cell: (a) => (
        // The row opens its drawer on click; the actions are their own targets.
        <span class="flex items-center justify-end gap-1" onClick={(e) => e.stopPropagation()} data-testid="alarm-actions">
          <AcknowledgeButton component={a.component} alarm={a} />
          <SnoozeControl component={a.component} alarm={a} surface={ACTIONS_SURFACE} allowed={canSnooze()} />
          <TroubleshootButton alarm={a} />
        </span>
      ),
    },
  ];
  // A viewer sees state and no actions: the column is not drawn at all.
  const shownColumns = () => (canAck() || canSnooze() || can(me.data, "assistant", "use") ? columns : columns.filter((c) => c.key !== "actions"));

  return (
    <div class="h-[calc(100dvh-9rem)] min-h-96" data-testid="alarm-queue">
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
          empty: "No active alarms in your scope.",
          rowId: (a) => a.id,
          detail: (a) => ({
            title: <span class="font-data text-sm">{a.component}: {a.message}</span>,
            wide: true,
            body: (
              <div class="flex flex-col gap-4">
                <div class="grid grid-cols-2 gap-3 text-sm">
                  <KVStacked label="Severity" value={<SeverityBadge name={a.severity} />} />
                  <KVStacked label="Raised" value={<span class="tnum text-xs">{fmtTime(a.raised_at)}</span>} />
                  <KVStacked label="Location" value={<span class="font-data text-xs">{a.location_path || "unplaced"}</span>} />
                  <KVStacked label="Systems" value={<span class="font-data text-xs">{(a.systems ?? []).join(", ") || "none"}</span>} />
                </div>
                <AlarmTimeline component={a.component} alarmId={a.id} label={a.message} />
              </div>
            ),
          }),
          footer: ({ shown, total, filtering }) => (
            <>
              <span data-testid="alarm-count">
                {shown}
                <Show when={filtering}> of {total} loaded</Show> active
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
  );
}
