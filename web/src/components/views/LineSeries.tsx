import { For, Show, createMemo, type Accessor } from "solid-js";
import { requireRole, useViewRows, type FieldMapping, type ViewResult } from "../../lib/views";
import ViewEmpty from "./ViewEmpty";
import ViewError, { describeViewError } from "./ViewError";

// LineSeries: a numeric series over time, one line per series instance. Plain
// inline SVG rather than a charting dependency, because the whole job is
// mapping (time, value) into a viewport, and a library would bring a rendering
// model that fights the fine-grained one everything else here uses.
//
// A row whose value column is not a finite number is a GAP, not a zero:
// sample-history carries both sample lanes, so a categorical reading has no
// number. Plotting it at the axis would invent a measurement, and drawing
// straight through it would invent every measurement in between, which is
// worse: an outage would render as a clean line an operator reads as healthy.
// A gap therefore breaks the line into separate segments.

const VIEW_W = 600;
const VIEW_H = 160;
const PAD = 8;

// A bounded palette rather than a fading opacity ramp: past the fourth series
// an opacity ramp reaches zero and the line is simply not drawn, with nothing
// saying so. Colours cycle, which repeats rather than disappears.
const STROKES = [
  "var(--color-primary, currentColor)",
  "var(--color-secondary, currentColor)",
  "var(--color-accent, currentColor)",
  "var(--color-info, currentColor)",
  "var(--color-warning, currentColor)",
  "var(--color-success, currentColor)",
];

type Point = { x: number; y: number };

export default function LineSeries(props: {
  result: Accessor<ViewResult | undefined>;
  mapping: FieldMapping;
  error?: Accessor<unknown>;
}) {
  const { rows } = useViewRows(props.result);
  const at = createMemo<{ time: number; value: number; series: number } | Error | null>(() => {
    const r = props.result();
    if (!r) return null;
    try {
      return {
        time: requireRole(r, props.mapping, "time"),
        value: requireRole(r, props.mapping, "value"),
        series: props.mapping.series ? (r.columns ?? []).findIndex((c) => c.name === props.mapping.series) : -1,
      };
    } catch (err) {
      return err instanceof Error ? err : new Error(String(err));
    }
  });
  const failure = () => props.error?.() ?? (at() instanceof Error ? at() : undefined);

  // The plot: group by series, break each series at its gaps, and scale both
  // axes across the whole result so the lines share one frame.
  const plot = createMemo(() => {
    const idx = at();
    if (!idx || idx instanceof Error) return [];
    const points: { key: string; at: number; value: number | null }[] = [];
    for (const row of rows()) {
      const t = Date.parse(String(row.cells[idx.time]));
      if (Number.isNaN(t)) continue;
      const raw = row.cells[idx.value];
      const value = typeof raw === "number" && Number.isFinite(raw) ? raw : null;
      points.push({ key: idx.series >= 0 ? String(row.cells[idx.series]) : "series", at: t, value });
    }
    const plotted = points.filter((p) => p.value !== null);
    if (plotted.length === 0) return [];
    const times = plotted.map((p) => p.at);
    const values = plotted.map((p) => p.value as number);
    const [t0, t1] = [Math.min(...times), Math.max(...times)];
    const [v0, v1] = [Math.min(...values), Math.max(...values)];
    const spanT = t1 - t0;
    const spanV = v1 - v0;
    // A degenerate span (one point, or every value equal) centres rather than
    // pinning the line to an edge, where a constant series would be
    // indistinguishable from a series of zeros.
    const place = (p: { at: number; value: number }): Point => ({
      x: spanT === 0 ? VIEW_W / 2 : PAD + ((p.at - t0) / spanT) * (VIEW_W - 2 * PAD),
      y: spanV === 0 ? VIEW_H / 2 : VIEW_H - PAD - ((p.value - v0) / spanV) * (VIEW_H - 2 * PAD),
    });

    // Split each series into contiguous runs: a gap ends the current run.
    const byKey = new Map<string, Point[][]>();
    for (const p of points) {
      const runs = byKey.get(p.key) ?? [[]];
      if (p.value === null) {
        if (runs[runs.length - 1].length > 0) runs.push([]);
      } else {
        runs[runs.length - 1].push(place({ at: p.at, value: p.value }));
      }
      byKey.set(p.key, runs);
    }
    const out: { key: string; run: Point[]; seriesIndex: number }[] = [];
    let seriesIndex = 0;
    for (const [key, runs] of byKey) {
      for (const run of runs) {
        if (run.length > 0) out.push({ key, run, seriesIndex });
      }
      seriesIndex++;
    }
    return out;
  });

  return (
    <Show when={!failure()} fallback={<ViewError message={describeViewError(failure())} />}>
      <Show when={at()} fallback={<ViewEmpty message="Loading..." />}>
        <Show when={plot().length > 0} fallback={<ViewEmpty />}>
          <svg viewBox={`0 0 ${VIEW_W} ${VIEW_H}`} class="w-full h-40" role="img" aria-label="series">
            <For each={plot()}>
              {(segment) => (
                <Show
                  when={segment.run.length > 1}
                  fallback={
                    // A lone reading has no line to draw, so it renders as a
                    // point rather than as a blank frame.
                    <circle
                      cx={segment.run[0].x}
                      cy={segment.run[0].y}
                      r="3"
                      fill={STROKES[segment.seriesIndex % STROKES.length]}
                      data-series={segment.key}
                    />
                  }
                >
                  <polyline
                    fill="none"
                    stroke={STROKES[segment.seriesIndex % STROKES.length]}
                    stroke-width="2"
                    data-series={segment.key}
                    points={segment.run.map((p) => `${p.x.toFixed(1)},${p.y.toFixed(1)}`).join(" ")}
                  />
                </Show>
              )}
            </For>
          </svg>
        </Show>
      </Show>
    </Show>
  );
}
