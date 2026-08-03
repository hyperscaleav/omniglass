import { For, Show, createMemo, type Accessor } from "solid-js";
import { requireRole, useViewRows, type FieldMapping, type ViewResult } from "../../lib/views";
import ViewEmpty from "./ViewEmpty";

// LineSeries: a numeric series over time, one polyline per series instance.
// Plain inline SVG rather than a charting dependency, because the whole job is
// mapping (time, value) into a viewport, and a library would bring a rendering
// model that fights the fine-grained one everything else here uses.
//
// A row whose value column is null is SKIPPED, not read as zero: sample-history
// carries both sample lanes, so a categorical reading has no number, and
// plotting it at the axis would invent a measurement.

const VIEW_W = 600;
const VIEW_H = 160;
const PAD = 8;

type Point = { x: number; y: number };

export default function LineSeries(props: { result: Accessor<ViewResult | undefined>; mapping: FieldMapping }) {
  const { rows } = useViewRows(props.result);
  const at = createMemo(() => {
    const r = props.result();
    if (!r) return null;
    return {
      time: requireRole(r, props.mapping, "time"),
      value: requireRole(r, props.mapping, "value"),
      series: r.columns.findIndex((c) => c.name === props.mapping.series),
    };
  });

  // The plot: group by series, scale each axis across the whole result so the
  // lines share one frame and can be compared.
  const plot = createMemo(() => {
    const idx = at();
    if (!idx) return [];
    const points: { key: string; at: number; value: number }[] = [];
    for (const row of rows()) {
      const raw = row.cells[idx.value];
      if (typeof raw !== "number") continue;
      const t = Date.parse(String(row.cells[idx.time]));
      if (Number.isNaN(t)) continue;
      points.push({ key: idx.series >= 0 ? String(row.cells[idx.series]) : "series", at: t, value: raw });
    }
    if (points.length === 0) return [];
    const times = points.map((p) => p.at);
    const values = points.map((p) => p.value);
    const [t0, t1] = [Math.min(...times), Math.max(...times)];
    const [v0, v1] = [Math.min(...values), Math.max(...values)];
    const spanT = t1 - t0 || 1;
    const spanV = v1 - v0 || 1;
    const byKey = new Map<string, Point[]>();
    for (const p of points) {
      const x = PAD + ((p.at - t0) / spanT) * (VIEW_W - 2 * PAD);
      // SVG y grows downward, so a higher value sits closer to the top.
      const y = VIEW_H - PAD - ((p.value - v0) / spanV) * (VIEW_H - 2 * PAD);
      byKey.set(p.key, [...(byKey.get(p.key) ?? []), { x, y }]);
    }
    return [...byKey.entries()].map(([key, pts]) => ({ key, pts }));
  });

  return (
    <Show when={at()} fallback={<ViewEmpty message="Loading..." />}>
      <Show when={plot().length > 0} fallback={<ViewEmpty />}>
        <svg viewBox={`0 0 ${VIEW_W} ${VIEW_H}`} class="w-full h-40" role="img" aria-label="series">
          <For each={plot()}>
            {(line, i) => (
              <polyline
                fill="none"
                stroke="currentColor"
                stroke-width="2"
                opacity={1 - i() * 0.25}
                points={line.pts.map((p) => `${p.x.toFixed(1)},${p.y.toFixed(1)}`).join(" ")}
              />
            )}
          </For>
        </svg>
      </Show>
    </Show>
  );
}
