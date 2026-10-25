import { For } from "solid-js";
import { LOG_SEVERITIES, type LogFilter, type LogSeverity } from "../lib/logs";

// LogFilters: the two narrowings a component's or a system's log read takes
// (#1145). Severity is a threshold on RFC 5424's eight levels, most severe
// first, so "warning" reads warning and everything worse, the way a syslog
// collector filters; a line with no severity is left out once one is picked.
// Source is the channel a line arrived on, its choices the sources the window
// carries (the read's own facet), so an operator picks from what is there
// rather than guessing a name. Both are applied by the read itself, not the
// browser, so the capped window holds only what matches.
export default function LogFilters(p: {
  filter: LogFilter;
  sources: string[];
  onChange: (f: LogFilter) => void;
}) {
  // A source the window no longer carries stays selectable while chosen, so
  // the control never shows a value it does not list.
  const sources = () => (p.filter.source && !p.sources.includes(p.filter.source) ? [...p.sources, p.filter.source] : p.sources);
  return (
    <div data-testid="log-filters" class="flex flex-wrap items-center gap-x-3 gap-y-1.5 text-[11px] text-base-content/55">
      <label class="flex items-center gap-1.5">
        <span>At least</span>
        <select
          class="select select-bordered select-xs min-w-0"
          aria-label="Severity at least"
          value={p.filter.severity ?? ""}
          onChange={(e) => p.onChange({ ...p.filter, severity: e.currentTarget.value as LogSeverity | "" })}
        >
          <option value="">any severity</option>
          <For each={LOG_SEVERITIES}>{(l) => <option value={l}>{l}</option>}</For>
        </select>
      </label>
      <label class="flex items-center gap-1.5">
        <span>From</span>
        <select
          class="select select-bordered select-xs min-w-0"
          aria-label="Source"
          value={p.filter.source ?? ""}
          onChange={(e) => p.onChange({ ...p.filter, source: e.currentTarget.value })}
        >
          <option value="">any source</option>
          <For each={sources()}>{(s) => <option value={s}>{s}</option>}</For>
        </select>
      </label>
    </div>
  );
}
