import { For, Match, Show, Switch, createMemo, createSignal } from "solid-js";
import { useQuery } from "@tanstack/solid-query";
import { rel } from "../lib/format";
import { formatAttributes } from "../lib/events";
import { LOGS_KEY, getLogs, severityVariant, type ComponentLog } from "../lib/logs";

// LogsPanel: a component's recent raw log lines (ADR-0066, the ingest lane). Where
// the events panel above shows discrete typed occurrences, this shows the untyped
// firehose a rule may later derive events from; most lines never become events. The
// panel reads that stream (newest first, the last 24 hours, capped) so the operator
// can see what a component has been logging. Read-only: every field on a row (ts,
// severity, facility, source, message, attributes, labels) is a real API value,
// nothing derived. Severity rides a colored badge, the message carries the row,
// source/facility/correlation sit on a secondary line, and a structured payload
// (when present) opens as a compact JSON snippet.

// LogRow renders one log line: the relative time, a severity badge, the message,
// then a secondary metadata line and an optional attributes disclosure.
function LogRow(p: { line: ComponentLog }) {
  const [open, setOpen] = createSignal(false);
  const attrs = createMemo(() => formatAttributes(p.line.attributes));
  const labels = createMemo(() => formatAttributes(p.line.labels));
  return (
    <div class="flex flex-col gap-1 px-3 py-2.5">
      <div class="flex items-baseline gap-2">
        <span class="w-14 shrink-0 text-[11px] tabular-nums text-base-content/45" title={p.line.ts}>{rel(p.line.ts)}</span>
        <span class={`badge badge-sm shrink-0 font-data text-[10px] ${severityVariant(p.line.severity)}`}>{p.line.severity || "log"}</span>
        <span class="min-w-0 flex-1 text-sm text-base-content/80">{p.line.message}</span>
      </div>
      <div class="flex flex-wrap items-center gap-2 pl-[4.5rem] text-[11px] text-base-content/45">
        <Show when={p.line.source}>
          <span class="font-data">{p.line.source}</span>
        </Show>
        <Show when={p.line.facility}>
          <span>· {p.line.facility}</span>
        </Show>
        <Show when={p.line.correlation_id}>
          <span class="font-data text-base-content/35" title="Correlation id">⤳ {p.line.correlation_id}</span>
        </Show>
        <Show when={attrs() || labels()}>
          <button
            type="button"
            class="link text-[11px] text-base-content/50 hover:text-base-content"
            onClick={() => setOpen((v) => !v)}
            aria-expanded={open()}
          >
            {open() ? "hide fields" : "fields"}
          </button>
        </Show>
      </div>
      <Show when={open()}>
        <div class="ml-[4.5rem] flex flex-col gap-1">
          <Show when={attrs()}>
            <pre class="overflow-x-auto rounded-md bg-base-200/60 px-2 py-1.5 font-data text-[11px] text-base-content/70">{attrs()}</pre>
          </Show>
          <Show when={labels()}>
            <pre class="overflow-x-auto rounded-md bg-base-200/40 px-2 py-1.5 font-data text-[11px] text-base-content/60" title="labels">{labels()}</pre>
          </Show>
        </div>
      </Show>
    </div>
  );
}

export default function LogsPanel(p: { name: string }) {
  const q = useQuery(() => ({ queryKey: LOGS_KEY(p.name), queryFn: () => getLogs(p.name) }));
  const lines = createMemo(() => q.data?.logs ?? []);
  return (
    <div class="flex flex-col gap-1.5">
      <div class="flex items-center gap-2">
        <span class="eyebrow">Logs</span>
        <Show when={lines().length}>
          <span class="text-[11px] text-base-content/40">{lines().length} in the last 24h</span>
        </Show>
      </div>
      <p class="text-[12px] text-base-content/50">
        Raw log lines the component emitted, the ingest lane, untyped and unfiltered, that a rule may later derive events from. Newest first, last 24 hours.
      </p>
      <Show
        when={lines().length}
        fallback={
          <div class="rounded-box border border-dashed border-base-300 px-3 py-4 text-center text-[12px] text-base-content/45">
            <Switch fallback="No log lines in the last 24 hours.">
              <Match when={q.isLoading}>Loading logs…</Match>
              <Match when={q.isError}>Could not load logs.</Match>
            </Switch>
          </div>
        }
      >
        <div class="divide-y divide-base-300 overflow-hidden rounded-box border border-base-300">
          <For each={lines()}>{(line) => <LogRow line={line} />}</For>
        </div>
      </Show>
    </div>
  );
}
