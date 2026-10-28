import { For, Show, createMemo, type JSX } from "solid-js";
import { useQuery } from "@tanstack/solid-query";
import Button from "./Button";
import { Clock } from "./icons";
import { ALARM_TIMELINE_POLL_MS, ageWords, alarmTimeline, alarmTimelineKey, timelineWhat, timelineWho } from "../lib/alarms";
import { describeError, fmtTime } from "../lib/format";
import { createToggles } from "../lib/toggles";

// AlarmTimeline is one alarm's history, opened in place under its row (#950):
// the events that carry the alarm (an event rule's fire event, which opened
// it, and its clear event, which resolved it) merged by time with its audit
// rows (the raise, each acknowledgement, the clear). An event is what the
// rule saw; an audit row is what was done to the alarm, and by whom. It
// renders only while its row is open, so a closed row costs no read; while
// open it refreshes on the alarm list's cadence (paused while the tab is
// hidden), and an acknowledge or clear refreshes it at once. Every age on it
// is read against one clock, taken when its entries land, so the order the
// ages say is the order the lines are in.
export default function AlarmTimeline(props: { component: string; alarmId: string; id?: string; label: string }): JSX.Element {
  const q = useQuery(() => ({
    queryKey: alarmTimelineKey(props.component, props.alarmId),
    queryFn: () => alarmTimeline(props.component, props.alarmId),
    refetchInterval: ALARM_TIMELINE_POLL_MS,
    refetchOnWindowFocus: false,
  }));
  const now = createMemo(() => (q.data, Date.now()));
  return (
    <div id={props.id} class="flex flex-col gap-1 rounded-box bg-base-200/40 px-2.5 py-2">
      <Show when={q.error}>
        <div role="alert" class="alert alert-error alert-soft py-1 text-xs"><span>{describeError(q.error)}</span></div>
      </Show>
      <Show when={q.isLoading}>
        <span class="text-[11px] text-base-content/45">Reading the timeline…</span>
      </Show>
      <Show when={q.data}>
        {(entries) => (
          <Show
            when={entries().length}
            fallback={<span class="text-[11px] text-base-content/45">Nothing recorded about this alarm yet.</span>}
          >
            <ol class="flex flex-col gap-1" aria-label={props.label}>
              <For each={entries()}>
                {(e) => (
                  <li class="grid grid-cols-[5.5rem_3.25rem_minmax(0,1fr)] items-baseline gap-x-2 text-[11.5px]">
                    <span class="tabular-nums text-base-content/45" title={fmtTime(e.at)}>{ageWords(e.at, now())}</span>
                    <span
                      class={`badge badge-xs w-full justify-center ${e.kind === "event" ? "badge-outline badge-info" : "badge-ghost"}`}
                      title={e.kind === "event" ? "An event the rule wrote: what it saw" : "An audit row: what was done to the alarm"}
                    >
                      {e.kind}
                    </span>
                    <span class="flex min-w-0 flex-wrap items-baseline gap-x-2 gap-y-0.5">
                      <span class="font-data text-base-content/85">{timelineWhat(e)}</span>
                      <span class="text-base-content/55">by {timelineWho(e)}</span>
                      <Show when={e.instance}>
                        <span class="text-base-content/45">on <span class="font-data">{e.instance}</span></span>
                      </Show>
                      <Show when={e.message}>
                        <span class="min-w-0 truncate text-base-content/45">{e.message}</span>
                      </Show>
                    </span>
                  </li>
                )}
              </For>
            </ol>
          </Show>
        )}
      </Show>
    </div>
  );
}

// createTimelineToggles is the open state of a list's alarm timelines, by
// alarm id, and the disclosure each row carries: every alarm row that opens
// its history (the Why strips, the alarms panel, a rule's open alarms, an
// Activity event that carries an alarm) opens it the same way, in place under
// the row, and any number can be open at once. A drawer is for forms; a row's
// history is read where the row is. The toggle is named by what the alarm
// says (its message), and points at the panel it opens (aria-controls).
export function createTimelineToggles() {
  const { isOpen, toggle, panelId } = createToggles("alarm-timeline");
  const label = (what: string) => `Timeline of ${what || "this alarm"}`;
  const button = (id: string, what: string) => (
    <Button
      square
      size="xs"
      icon={Clock}
      label={label(what)}
      title="Open this alarm's timeline: what the rule saw, and what was done to it, in order"
      expanded={isOpen(id)}
      controls={panelId(id)}
      onClick={() => toggle(id)}
    />
  );
  return { isOpen, toggle, button, panelId, label };
}
