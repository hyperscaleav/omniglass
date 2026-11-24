import { For, Show, createSignal, onCleanup } from "solid-js";
import { LOG_SEVERITIES, type LogSeverity } from "../lib/logs";
import type { OccurrenceFilter } from "../lib/occurrence";

// OccurrenceFilters is the one filter bar for events and logs (#2437): free
// text and source on both, a severity threshold on logs (RFC 5424's eight
// levels, most severe first, so "warning" reads warning and everything worse;
// a line with no severity is left out once one is picked) and an event key on
// events. Each narrows the server read itself, so the capped window holds only
// what matches. A log source is picked from the sources the window carries (the
// read's own facet); an event source is typed, the event read carrying no facet.
const TEXT_DEBOUNCE_MS = 300;

export default function OccurrenceFilters(p: {
  kind: "events" | "logs";
  filter: OccurrenceFilter;
  sources?: string[];
  onChange: (f: OccurrenceFilter) => void;
}) {
  // A source the window no longer carries stays selectable while chosen, so the
  // control never shows a value it does not list.
  const sources = () => (p.filter.source && !(p.sources ?? []).includes(p.filter.source) ? [...(p.sources ?? []), p.filter.source] : (p.sources ?? []));
  // Typed fields commit after a pause (or on Enter / blur), not per keystroke.
  const [timer, setTimer] = createSignal<ReturnType<typeof setTimeout>>();
  onCleanup(() => clearTimeout(timer()));
  const typed = (field: "q" | "key" | "source", value: string, now = false) => {
    clearTimeout(timer());
    const commit = () => p.onChange({ ...p.filter, [field]: value });
    if (now) commit();
    else setTimer(setTimeout(commit, TEXT_DEBOUNCE_MS));
  };
  const text = (field: "q" | "key" | "source", label: string, placeholder: string, width: string) => (
    <div class="flex items-center gap-1.5">
      <span class="whitespace-nowrap" aria-hidden="true">{label}</span>
      <input
        class={`input input-bordered input-xs ${width}`}
        aria-label={label === "Contains" ? "Message contains" : label}
        placeholder={placeholder}
        value={p.filter[field] ?? ""}
        onInput={(e) => typed(field, e.currentTarget.value)}
        onChange={(e) => typed(field, e.currentTarget.value, true)}
      />
    </div>
  );
  return (
    <div data-testid="log-filters" class="flex flex-wrap items-center gap-x-3 gap-y-1.5 text-[11px] text-base-content/55">
      <Show when={p.kind === "logs"}>
        <div class="flex items-center gap-1.5">
          <span class="whitespace-nowrap" aria-hidden="true">At least</span>
          <select
            class="select select-bordered select-xs min-w-0"
            aria-label="Severity at least"
            value={p.filter.severity ?? ""}
            onChange={(e) => p.onChange({ ...p.filter, severity: e.currentTarget.value as LogSeverity | "" })}
          >
            <option value="">any severity</option>
            <For each={LOG_SEVERITIES}>{(l) => <option value={l}>{l}</option>}</For>
          </select>
        </div>
        <div class="flex items-center gap-1.5">
          <span class="whitespace-nowrap" aria-hidden="true">From</span>
          <select
            class="select select-bordered select-xs min-w-0"
            aria-label="Source"
            value={p.filter.source ?? ""}
            onChange={(e) => p.onChange({ ...p.filter, source: e.currentTarget.value })}
          >
            <option value="">any source</option>
            <For each={sources()}>{(s) => <option value={s}>{s}</option>}</For>
          </select>
        </div>
      </Show>
      <Show when={p.kind === "events"}>
        {text("key", "Key", "call-started", "w-36")}
        {text("source", "Source", "xapi", "w-28")}
      </Show>
      {text("q", "Contains", "message text", "w-40")}
    </div>
  );
}
