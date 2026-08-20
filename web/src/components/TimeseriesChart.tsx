import { For, Show, createMemo } from "solid-js";
import { chartLayout, type ChartSample } from "../lib/chart";
import { fmtTime } from "../lib/format";

// The timeseries chart (#794): one series, drawn from chartLayout's pure
// geometry. Single series, so no legend (the title names it); a 2px line
// over a faint area fill, recessive grid, the newest point emphasized with
// its value; per-point hover carries the timestamp and value as the native
// title. Built for the Data tab and reused by dashboards later.
export default function TimeseriesChart(props: { samples: ChartSample[]; now: number; windowMs: number; unit?: string }) {
  const W = 640;
  const H = 170;
  const layout = createMemo(() => chartLayout(props.samples, { width: W, height: H, padLeft: 44, padRight: 16, padY: 20, now: props.now, windowMs: props.windowMs }));
  const last = () => layout().points[layout().points.length - 1];
  const fmtV = (v: number) => (Math.abs(v) >= 100 ? v.toFixed(0) : v.toFixed(1));
  return (
    <Show when={layout().points.length > 0} fallback={<p class="text-sm text-base-content/50">No samples in the window.</p>}>
      <svg data-testid="timeseries-chart" viewBox={`0 0 ${W} ${H}`} class="w-full max-w-3xl" role="img" aria-label="The series over the window, newest at the right">
        <For each={layout().yTicks}>
          {(t) => {
            const y = () => 20 + (1 - (t - layout().yMin) / (layout().yMax - layout().yMin)) * (H - 40);
            return (
              <>
                <line x1="44" x2={W - 16} y1={y()} y2={y()} class="stroke-base-content/10" stroke-width="1" />
                <text x="38" y={y() + 3} text-anchor="end" class="fill-base-content/40" font-size="9">{fmtV(t)}</text>
              </>
            );
          }}
        </For>
        <path d={layout().areaPath} class="fill-primary/10" />
        <path d={layout().linePath} class="stroke-primary" stroke-width="2" fill="none" stroke-linejoin="round" />
        <For each={layout().points}>
          {(p) => (
            <circle cx={p.x} cy={p.y} r="5" class="fill-transparent hover:fill-primary/40">
              <title>{`${fmtV(p.value)}${props.unit ?? ""} · ${fmtTime(p.ts)}`}</title>
            </circle>
          )}
        </For>
        <Show when={last()}>
          {(p) => (
            <>
              <circle cx={p().x} cy={p().y} r="3.5" class="fill-primary stroke-base-100" stroke-width="2" />
              <text x={Math.min(p().x, W - 60)} y={Math.max(p().y - 8, 12)} class="fill-base-content" font-size="11" text-anchor="end" style={{ "font-variant-numeric": "tabular-nums" }}>
                {fmtV(p().value)}
                {props.unit ?? ""}
              </text>
            </>
          )}
        </Show>
      </svg>
    </Show>
  );
}
