import { For, Match, Show, Switch, createMemo, createSignal } from "solid-js";
import { useQuery } from "@tanstack/solid-query";
import { rel } from "../lib/format";
import { EVENTS_KEY, getEvents, formatAttributes, type ComponentEvent } from "../lib/events";

// EventsPanel: a component's recent log-kind observations. Where the reachability
// strip above renders sampled values (a metric or a state read on a cadence), the
// event sink collects discrete occurrences: syslog lines, traps, and the like, each
// a message stamped when it happened rather than a value averaged over a window.
// This panel reads that stream (newest first, the last 24 hours, capped) so the
// operator can see what a component has been saying. Read-only: every field on a row
// (ts, key, message, source, instance, attributes) is a real API value, nothing
// derived or invented. The key is shown as a subtle mono badge, the message carries
// the row, source/instance/provenance sit on a secondary line, and a structured
// payload (when present) opens as a compact JSON snippet.

// EventRow renders one occurrence: the relative time, the log's key as a mono badge,
// the message, then a secondary metadata line and an optional attributes disclosure.
function EventRow(p: { ev: ComponentEvent }) {
  const [open, setOpen] = createSignal(false);
  const attrs = createMemo(() => formatAttributes(p.ev.attributes));
  return (
    <div class="flex flex-col gap-1 px-3 py-2.5">
      <div class="flex items-baseline gap-2">
        <span class="w-14 shrink-0 text-[11px] tabular-nums text-base-content/45" title={p.ev.ts}>{rel(p.ev.ts)}</span>
        <span class="badge badge-ghost badge-sm shrink-0 font-data text-[10px]">{p.ev.key}</span>
        <span class="min-w-0 flex-1 text-sm text-base-content/80">{p.ev.message}</span>
      </div>
      <div class="flex flex-wrap items-center gap-2 pl-[4.5rem] text-[11px] text-base-content/45">
        <Show when={p.ev.source}>
          <span class="font-data">{p.ev.source}</span>
        </Show>
        <Show when={p.ev.instance}>
          <span>· {p.ev.instance}</span>
        </Show>
        <span class="uppercase tracking-wide text-[10px] text-base-content/35">{p.ev.provenance}</span>
        <Show when={attrs()}>
          <button
            type="button"
            class="link text-[11px] text-base-content/50 hover:text-base-content"
            onClick={() => setOpen((v) => !v)}
            aria-expanded={open()}
          >
            {open() ? "hide attributes" : "attributes"}
          </button>
        </Show>
      </div>
      <Show when={open() && attrs()}>
        <pre class="ml-[4.5rem] overflow-x-auto rounded-md bg-base-200/60 px-2 py-1.5 font-data text-[11px] text-base-content/70">{attrs()}</pre>
      </Show>
    </div>
  );
}

export default function EventsPanel(p: { name: string }) {
  const q = useQuery(() => ({ queryKey: EVENTS_KEY(p.name), queryFn: () => getEvents(p.name) }));
  const events = createMemo(() => q.data?.events ?? []);
  return (
    <div class="flex flex-col gap-1.5">
      <div class="flex items-center gap-2">
        <span class="eyebrow">Events</span>
        <Show when={events().length}>
          <span class="text-[11px] text-base-content/40">{events().length} in the last 24h</span>
        </Show>
      </div>
      <p class="text-[12px] text-base-content/50">
        Log-kind observations (syslog lines, traps, discrete occurrences) routed to the event sink, as opposed to the sampled metric and state values above. Newest first, last 24 hours.
      </p>
      <Show
        when={events().length}
        fallback={
          <div class="rounded-box border border-dashed border-base-300 px-3 py-4 text-center text-[12px] text-base-content/45">
            <Switch fallback="No events in the last 24 hours.">
              <Match when={q.isLoading}>Loading events…</Match>
              <Match when={q.isError}>Could not load events.</Match>
            </Switch>
          </div>
        }
      >
        <div class="divide-y divide-base-300 overflow-hidden rounded-box border border-base-300">
          <For each={events()}>{(ev) => <EventRow ev={ev} />}</For>
        </div>
      </Show>
    </div>
  );
}
