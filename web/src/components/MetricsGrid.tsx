import { For, Show, createMemo } from "solid-js";
import { useQueries, useQuery } from "@tanstack/solid-query";
import Button from "./Button";
import Eyebrow from "./Eyebrow";
import TimeseriesChart from "./TimeseriesChart";
import { CalcRuleLink } from "./RuleLink";
import { CursorProvider } from "../lib/cursor";
import { effectiveMetrics, effectiveMetricsKey } from "../lib/component_metrics";
import { systemMetrics, systemMetricsKey } from "../lib/system_metrics";
import { SERIES_POLL_MS, metricBuckets, metricBucketsBetween, metricBucketsBetweenKey, metricBucketsKey } from "../lib/series";
import { rangeLabel, type TimeRange } from "../lib/time_range";
import { createClock } from "../lib/clock";
import { LEAF_POLL_MS, vitalValueText } from "../lib/component_leaf";
import { entityLabel } from "../lib/entities";
import { gridWindow, hasData } from "../lib/metrics_grid";

// MetricsGrid (#2424): every effective metric of a component or a system
// charted over the page's range, one card each (name, unit, the current
// value, the chart), all on one shared cursor, so hovering one chart reads
// every metric at that instant. A metric with nothing sampled in the range
// keeps its card, marked, rather than vanishing: an empty series is a fact.
//
// Plain props and no page logic: a page mounts it with its own kind, id and
// range, and the detail-body refactor or a dashboard moves it unchanged. The
// grid answers its container's width (container queries), not the window's,
// so the same grid is one column in a drawer and two or three on a page.
// refused is whether a read failed for want of a permission (a 403 problem).
const refused = (err: unknown) => (err as { status?: number } | null)?.status === 403;

export default function MetricsGrid(props: { kind: "components" | "systems"; id: string; range: TimeRange }) {
  const metricsQ = useQuery(() => ({
    queryKey: props.kind === "components" ? effectiveMetricsKey(props.id) : systemMetricsKey(props.id),
    queryFn: () => (props.kind === "components" ? effectiveMetrics(props.id) : systemMetrics(props.id)),
    refetchInterval: LEAF_POLL_MS,
  }));
  // Every metric declared or reporting, sampled or not.
  const metrics = createMemo(() => (metricsQ.data ?? []).filter((m) => m.from_contract || m.is_sampled));
  // Cards are kept by metric name, never by the objects a poll hands back
  // (each poll is fresh): a card, its chart and a focused chart's focus
  // survive the next poll, as the leaf's vitals rows do (#1059).
  const names = createMemo(() => metrics().map((m) => m.metric_type_name), [], {
    equals: (a, b) => a.length === b.length && a.every((n, i) => n === b[i]),
  });
  const byName = createMemo(() => new Map(metrics().map((m) => [m.metric_type_name, m])));
  // A minute's tick: a chart over an hour or more moves too little in a second
  // to redraw on every one, and the series polls on its own slower clock.
  const now = createClock(60_000);
  const win = createMemo(() => gridWindow(props.range, now()));
  const seriesQ = useQueries(() => ({
    queries: metrics().map((m) => {
      // The chart is the series whose current value sits beside it: a bucket
      // is one instance (#1485), so a sampled metric names the instance shown.
      const instance = m.is_sampled ? m.instance : undefined;
      const w = win();
      if (w.hours === undefined) {
        // An absolute range wholly in the past holds still, so it is read
        // once; one whose end is still ahead keeps polling, as a preset does.
        return {
          queryKey: metricBucketsBetweenKey(props.kind, props.id, m.metric_type_name, w.from, w.to, w.stepSeconds, instance),
          queryFn: () => metricBucketsBetween(props.kind, props.id, m.metric_type_name, w.from, w.to, w.stepSeconds, instance),
          refetchInterval: w.to > Date.now() ? SERIES_POLL_MS : (false as const),
        };
      }
      const hours = w.hours;
      return {
        queryKey: metricBucketsKey(props.kind, props.id, m.metric_type_name, hours, w.stepSeconds, instance),
        queryFn: () => metricBuckets(props.kind, props.id, m.metric_type_name, hours, w.stepSeconds, instance),
        refetchInterval: SERIES_POLL_MS,
      };
    }),
  }));
  return (
    <Show when={metrics().length > 0}>
      <section data-testid="metrics-grid" class="@container flex flex-col gap-2">
        <Eyebrow
          label="Metrics"
          hint="Every metric this declares or reports, charted over the page's time range: the band is each bucket's low to high, the line its average. Hover a chart, or focus it and use the arrow keys, to read every metric at the same instant."
          right={<span class="text-xs text-base-content/50">{rangeLabel(props.range)}</span>}
        />
        <div class="grid grid-cols-1 gap-3 @2xl:grid-cols-2 @6xl:grid-cols-3">
          <CursorProvider>
            <For each={names()}>
              {(name, i) => {
                // The last row read under this name: a dropped metric's card
                // may read once more before it is disposed.
                let last = byName().get(name)!;
                const m = () => (last = byName().get(name) ?? last);
                const q = () => seriesQ[i()];
                const buckets = () => q()?.data?.buckets ?? [];
                // A preset's key is fixed while its window slides, so the chart's
                // right edge is when its data was read, not the minute clock's
                // last tick: a poll cannot return buckets past the edge it draws.
                const edge = () => (win().hours === undefined ? win().to : q()?.dataUpdatedAt || win().to);
                return (
                  <article data-testid={`metric-card-${name}`} class="flex min-w-0 flex-col gap-1 rounded-box border border-base-300 bg-base-100 p-3">
                    <header class="flex items-baseline gap-2">
                      <h3 class="min-w-0 truncate text-sm font-medium" title={name}>{entityLabel({ name: name, label: m().label })}</h3>
                      <Show when={m().unit}>
                        <span class="text-xs text-base-content/50">{m().unit}</span>
                      </Show>
                      <span class="ml-auto flex items-baseline gap-1.5">
                        <Show when={m().source_rule && m().source_rule_id}>
                          <CalcRuleLink name={m().source_rule!} id={m().source_rule_id!} />
                        </Show>
                        <Show when={!m().is_sampled}>
                          <span class="text-[10px] text-base-content/45">contract default</span>
                        </Show>
                        <Show when={m().value !== null && m().value !== undefined}>
                          <span data-testid="metric-current" class="font-mono text-sm tabular-nums">{vitalValueText(m().value)}</span>
                        </Show>
                      </span>
                    </header>
                    <Show
                      when={q()?.data}
                      fallback={
                        <div class="flex h-[170px] flex-col items-center justify-center gap-2 text-xs text-base-content/45">
                          <Show when={q()?.isError} fallback="Reading…">
                            <Show
                              when={!(props.range.kind === "absolute" && refused(q()?.error))}
                              // A custom range reads through the views binding, which
                              // needs view:read; a link can carry one to a caller the
                              // picker offers none to, and no Retry would help them.
                              fallback={<span>A custom range needs the view:read permission.</span>}
                            >
                            <span>Could not read this series.</span>
                            <Button size="xs" intent="quiet" loading={q()?.isFetching} onClick={() => void q()?.refetch()}>Retry</Button>
                            </Show>
                          </Show>
                        </div>
                      }
                    >
                      <Show
                        when={hasData(buckets())}
                        fallback={
                          <div data-testid="metric-no-data" class="flex h-[170px] items-center justify-center rounded-field border border-dashed border-base-300 text-xs text-base-content/50">
                            No data in range
                          </div>
                        }
                      >
                        <TimeseriesChart
                          fluid
                          buckets={buckets()}
                          resolutionMs={win().stepSeconds * 1000}
                          now={edge()}
                          windowMs={win().to - win().from}
                          unit={m().unit ? ` ${m().unit}` : ""}
                        />
                      </Show>
                    </Show>
                  </article>
                );
              }}
            </For>
          </CursorProvider>
        </div>
      </section>
    </Show>
  );
}
