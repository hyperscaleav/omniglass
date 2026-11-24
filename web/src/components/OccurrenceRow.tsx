import { For, Show, createMemo, createSignal, type JSX } from "solid-js";
import { fmtTime, rel } from "../lib/format";
import { formatAttributes } from "../lib/events";
import { arrivedApart, messageParts, type Occurrence } from "../lib/occurrence";
import AlarmTimeline, { createTimelineToggles } from "./AlarmTimeline";
import RuleLink from "./RuleLink";

// OccurrenceRow is the one row an event or a log line wears wherever it is listed
// (#2437): the relative time (the absolute on hover), a kind badge (an event's
// key, or a log's severity colored by level), the owner when the list spans
// several entities, and the message clamped to one line, so a long unbroken
// message never widens the row. Clicking the row opens its detail in place: the
// message wrapped in full, its source, correlation id, structured attributes, the
// rule that derived it and the alarm it moved. An internal id in a message (a task's
// 64-hex id) reads as a short labelled reference, never raw.

// Message renders the message with each internal id as a short labelled
// reference; the full id rides the reference's title.
function Message(p: { text: string }): JSX.Element {
  const parts = createMemo(() => messageParts(p.text));
  return (
    <For each={parts()}>
      {(part) =>
        "ref" in part ? (
          <span class="badge badge-ghost badge-xs align-baseline font-data" title={part.ref} data-testid="message-ref">
            {part.label} {part.short}
          </span>
        ) : (
          part.text
        )
      }
    </For>
  );
}

export default function OccurrenceRow(p: { o: Occurrence; showOwner?: boolean }): JSX.Element {
  const [open, setOpen] = createSignal(false);
  // Each row owns its timeline toggle: a rule's fire and clear events carry the
  // same alarm, and opening one opens only that row.
  const timelines = createTimelineToggles();
  const attrs = createMemo(() => formatAttributes(p.o.attributes));
  const labels = createMemo(() => formatAttributes(p.o.labels));
  const detailId = `occ-${Math.random().toString(36).slice(2, 9)}`;
  const facts = createMemo(() => {
    const o = p.o;
    const out: [string, string][] = [];
    // The source is the channel; the endpoint that collected the line usually
    // shares its name, so the source is shown only where it says something more.
    if (o.source && o.source !== o.instance) out.push(["Source", o.source]);
    if (o.instance) out.push(["Via", o.instance]);
    if (o.provenance) out.push(["Provenance", o.provenance]);
    if (o.origin) out.push(["Arrived", o.origin]);
    if (o.facility) out.push(["Facility", o.facility]);
    if (o.correlationId) out.push(["Correlation", o.correlationId]);
    if (arrivedApart(o)) out.push(["Received", fmtTime(o.receivedAt!)]);
    return out;
  });
  return (
    <li data-testid="occurrence-row" data-kind={p.o.kind} class="flex min-w-0 flex-col gap-1.5 px-3 py-1.5">
      <div class="flex min-w-0 items-center gap-2">
        <button
          type="button"
          class="flex min-w-0 flex-1 cursor-pointer items-center gap-2 text-left"
          aria-expanded={open()}
          aria-controls={detailId}
          onClick={() => setOpen((v) => !v)}
        >
          <span class="w-14 shrink-0 text-[11px] tabular-nums text-base-content/45" title={p.o.ts}>{rel(p.o.ts)}</span>
          <span class={`badge badge-sm max-w-40 shrink-0 truncate font-data text-[10px] ${p.o.badgeClass}`}>{p.o.badge}</span>
          <Show when={p.showOwner && p.o.owner}>
            <span class="max-w-32 shrink-0 truncate font-data text-[11px] text-base-content/60" title={p.o.owner}>{p.o.owner}</span>
          </Show>
          <span class="min-w-0 flex-1 truncate text-sm text-base-content/80" data-testid="occurrence-message"><Message text={p.o.message} /></span>
        </button>
        {/* A rule's event names the rule and carries the alarm it moved (#951):
            beside the row's button, never inside it. */}
        <Show when={p.o.rule}>{(r) => <RuleLink name={r().name} id={r().id} />}</Show>
        <Show when={p.o.alarm}>{(a) => timelines.button(a().id, p.o.message)}</Show>
      </div>
      <Show when={open()}>
        <div id={detailId} data-testid="occurrence-detail" class="flex min-w-0 flex-col gap-1.5 pl-[4.5rem] text-[12px]">
          <p class="whitespace-pre-wrap text-sm text-base-content/85 [overflow-wrap:anywhere]">{p.o.message}</p>
          <Show when={facts().length}>
            <dl class="flex flex-wrap gap-x-4 gap-y-0.5 text-[11px] text-base-content/55">
              <For each={facts()}>
                {([k, v]) => (
                  <div class="flex gap-1">
                    <dt class="text-base-content/40">{k}</dt>
                    <dd class="font-data [overflow-wrap:anywhere]">{v}</dd>
                  </div>
                )}
              </For>
            </dl>
          </Show>
          <Show when={attrs()}>
            <div class="flex flex-col gap-0.5">
              <span class="text-[10px] uppercase tracking-wide text-base-content/35">attributes</span>
              <pre class="overflow-x-auto rounded-md bg-base-200/60 px-2 py-1.5 font-data text-[11px] text-base-content/70">{attrs()}</pre>
            </div>
          </Show>
          <Show when={labels()}>
            <div class="flex flex-col gap-0.5">
              <span class="text-[10px] uppercase tracking-wide text-base-content/35">labels</span>
              <pre class="overflow-x-auto rounded-md bg-base-200/40 px-2 py-1.5 font-data text-[11px] text-base-content/60">{labels()}</pre>
            </div>
          </Show>
        </div>
      </Show>
      <Show when={p.o.alarm && timelines.isOpen(p.o.alarm.id)}>
        <div class="pl-[4.5rem]">
          <AlarmTimeline component={p.o.alarm!.component} alarmId={p.o.alarm!.id} id={timelines.panelId(p.o.alarm!.id)} label={timelines.label(p.o.message)} />
        </div>
      </Show>
    </li>
  );
}
