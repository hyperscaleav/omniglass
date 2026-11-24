import { For, Match, Show, Switch, createMemo, createSignal } from "solid-js";
import { useQuery } from "@tanstack/solid-query";
import { EVENTS_KEY, getEvents, eventQuery, type ComponentEvents } from "../lib/events";
import { LOGS_KEY, OUTPOST_LOGS_KEY, getLogs, getOutpostLogs, type ComponentLog, type ComponentLogs, type OutpostLogs } from "../lib/logs";
import { systemEvents, systemEventsKey, systemLogs, systemLogsKey, type SystemEvent, type SystemLog, type SystemLogs } from "../lib/system_activity";
import { EMPTY_FILTER, fromEvent, fromLog, isFiltered, type Occurrence, type OccurrenceFilter } from "../lib/occurrence";
import Eyebrow from "./Eyebrow";
import OccurrenceFilters from "./OccurrenceFilters";
import OccurrenceRow from "./OccurrenceRow";

// OccurrenceList is the one list of events or log lines on the system and the
// component pages (#2437): a section head (title, count, info tip), the one
// filter bar, and a row per occurrence through OccurrenceRow. It owns its read
// and the filter, narrowed server-side, so the same list serves a component's
// events, a system's members' events, either's logs, and an outpost's self-logs
// (which take no filter). A system's list labels each row with the member that
// wrote it.
export type OccurrenceScope = { type: "component" | "system" | "outpost"; name: string };

// The query caches each lane's own response, so the list shares its cache entries
// with every other reader of the same read; the rows are normalized from it.
type Raw = ComponentEvents | SystemEvent[] | ComponentLogs | SystemLogs | OutpostLogs;

function read(kind: "events" | "logs", scope: OccurrenceScope, f: OccurrenceFilter): Promise<Raw> {
  if (kind === "events") {
    const ef = { key: f.key, source: f.source, q: f.q };
    return scope.type === "system" ? systemEvents(scope.name, ef) : getEvents(scope.name, ef);
  }
  const lf = { severity: f.severity, source: f.source, q: f.q };
  if (scope.type === "system") return systemLogs(scope.name, lf);
  if (scope.type === "outpost") return getOutpostLogs(scope.name);
  return getLogs(scope.name, lf);
}

function normalize(kind: "events" | "logs", scope: OccurrenceScope, raw: Raw | undefined): { rows: Occurrence[]; sources: string[] } {
  if (!raw) return { rows: [], sources: [] };
  if (kind === "events") {
    const events = Array.isArray(raw) ? raw : "events" in raw ? raw.events : [];
    return { rows: events.map((e, i) => fromEvent(e, i, scope.name)), sources: [] };
  }
  const r = raw as { logs?: (ComponentLog | SystemLog)[]; sources?: string[] };
  return { rows: (r.logs ?? []).map(fromLog), sources: r.sources ?? [] };
}

function keyFor(kind: "events" | "logs", scope: OccurrenceScope, f: OccurrenceFilter): readonly unknown[] {
  if (kind === "events") return scope.type === "system" ? systemEventsKey(scope.name, eventQuery(f)) : EVENTS_KEY(scope.name, eventQuery(f));
  if (scope.type === "outpost") return OUTPOST_LOGS_KEY(scope.name);
  return scope.type === "system" ? systemLogsKey(scope.name, f) : LOGS_KEY(scope.name, f);
}

export default function OccurrenceList(p: {
  kind: "events" | "logs";
  scope: OccurrenceScope;
  title?: string;
  hint: string;
  // How often to re-read, for a page that polls.
  refetchMs?: number;
}) {
  const [filter, setFilter] = createSignal<OccurrenceFilter>(EMPTY_FILTER);
  const filterable = () => p.scope.type !== "outpost";
  const q = useQuery(() => ({
    queryKey: [...keyFor(p.kind, p.scope, filter())],
    queryFn: () => read(p.kind, p.scope, filter()),
    refetchInterval: p.refetchMs,
    // A changed filter keeps the last answer on screen until the new one lands,
    // so the list does not flash empty between them.
    placeholderData: (prev: Raw | undefined) => prev,
  }));
  const view = createMemo(() => normalize(p.kind, p.scope, q.data));
  const rows = () => view().rows;
  const noun = () => (p.kind === "events" ? "event" : "line");
  const what = () => (p.kind === "events" ? "events" : "log lines");
  return (
    <section data-testid={p.kind === "events" ? "events-tab" : "logs-tab"} class="flex min-w-0 flex-col gap-2">
      <Eyebrow
        label={p.title ?? (p.kind === "events" ? "Events" : "Logs")}
        hint={p.hint}
        right={
          <Show when={rows().length}>
            <span class="text-[11px] text-base-content/40">{rows().length} in the last 24h</span>
          </Show>
        }
      />
      <Show when={filterable()}>
        <OccurrenceFilters kind={p.kind} filter={filter()} sources={view().sources} onChange={setFilter} />
      </Show>
      <Show
        when={rows().length}
        fallback={
          <div class="rounded-box border border-dashed border-base-300 px-3 py-4 text-center text-[12px] text-base-content/45">
            <Switch fallback={isFiltered(filter()) ? `No ${noun()} matches the filter in the last 24 hours.` : `No ${what()} in the last 24 hours.`}>
              <Match when={q.isLoading}>Loading {what()}…</Match>
              <Match when={q.isError}>Could not load {what()}.</Match>
            </Switch>
          </div>
        }
      >
        <ul class="divide-y divide-base-300 overflow-hidden rounded-box border border-base-300">
          <For each={rows()}>{(o) => <OccurrenceRow o={o} showOwner={p.scope.type === "system"} />}</For>
        </ul>
      </Show>
    </section>
  );
}
