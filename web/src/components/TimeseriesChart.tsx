import { For, Show, createMemo, createSignal } from "solid-js";
import { chartLayout, nearestPoint, type ChartPoint, type ChartSample } from "../lib/chart";
import { fmtTime } from "../lib/format";

// The timeseries chart (#794): one series, drawn from chartLayout's pure
// geometry. Single series, so no legend (the row or title names it); a 2px
// line over a faint area fill, recessive grid, and the newest sample's value
// floating centered above its dot, intersecting the line (#795 review).
// `spark` renders the dense table form: the same geometry, no chrome.
export default function TimeseriesChart(props: { samples: ChartSample[]; now: number; windowMs: number; unit?: string; spark?: boolean }) {
  const W = () => (props.spark ? 160 : 640);
  const H = () => (props.spark ? 36 : 170);
  const layout = createMemo(() =>
    chartLayout(props.samples, {
      width: W(),
      height: H(),
      padLeft: props.spark ? 3 : 44,
      padRight: props.spark ? 6 : 16,
      padY: props.spark ? 5 : 20,
      now: props.now,
      windowMs: props.windowMs,
    }),
  );
  const last = () => layout().points[layout().points.length - 1];
  const fmtV = (v: number) => (Math.abs(v) >= 100 ? v.toFixed(0) : v.toFixed(1));
  const fmtClock = (ts: string) => new Date(ts).toLocaleTimeString(undefined, { hour: "2-digit", minute: "2-digit" });
  // The crosshair: the sample nearest the pointer, an exact time and value
  // where a sample exists, never an interpolation (#795 review).
  const [hover, setHover] = createSignal<ChartPoint | null>(null);
  // The handler reads the svg off the event it is attached to (the retired band canvas
  // pattern): no ref, no initialization-order question for a reader or an
  // analyzer to puzzle over.
  const onMove = (e: MouseEvent & { currentTarget: SVGSVGElement }) => {
    if (props.spark) return;
    const r = e.currentTarget.getBoundingClientRect();
    if (r.width <= 0) return;
    setHover(nearestPoint(layout(), ((e.clientX - r.left) / r.width) * W()));
  };
  return (
    <Show
      when={layout().points.length > 0}
      fallback={
        <Show when={!props.spark} fallback={<span class="text-[10px] text-base-content/40">no samples</span>}>
          <p class="text-sm text-base-content/50">No samples in the window.</p>
        </Show>
      }
    >
      <svg
        data-testid={props.spark ? "sparkline" : "timeseries-chart"}
        viewBox={`0 0 ${W()} ${H()}`}
        class={props.spark ? "h-9 w-40" : "w-full max-w-3xl"}
        role="img"
        aria-label="The series over the window, newest at the right"
        onMouseMove={onMove}
        onMouseLeave={() => setHover(null)}
      >
        <Show when={!props.spark}>
          <For each={layout().yTicks}>
            {(t) => {
              const y = () => 20 + (1 - (t - layout().yMin) / (layout().yMax - layout().yMin)) * (H() - 40);
              return (
                <>
                  <line x1="44" x2={W() - 16} y1={y()} y2={y()} class="stroke-base-content/10" stroke-width="1" />
                  <text x="38" y={y() + 3} text-anchor="end" class="fill-base-content/40" font-size="9">{fmtV(t)}</text>
                </>
              );
            }}
          </For>
        </Show>
        <Show when={!props.spark}>
          <line x1="44" x2={W() - 16} y1={H() - 20} y2={H() - 20} class="stroke-base-content/15" stroke-width="1" />
          <For each={layout().xTicks}>
            {(t, i) => (
              <text
                x={t.x}
                y={H() - 7}
                text-anchor={i() === 0 ? "start" : i() === 3 ? "end" : "middle"}
                class="fill-base-content/40"
                font-size="9"
                style={{ "font-variant-numeric": "tabular-nums" }}
              >
                {fmtClock(t.ts)}
              </text>
            )}
          </For>
        </Show>
        <path d={layout().areaPath} class="fill-primary/10" />
        <path d={layout().linePath} class="stroke-primary" stroke-width={props.spark ? "1.5" : "2"} fill="none" stroke-linejoin="round" />
        <Show when={!props.spark}>
          <For each={layout().points}>
            {(p) => (
              <circle cx={p.x} cy={p.y} r="5" class="fill-transparent hover:fill-primary/40">
                <title>{`${fmtV(p.value)}${props.unit ?? ""} · ${fmtTime(p.ts)}`}</title>
              </circle>
            )}
          </For>
        </Show>
        <Show when={hover()}>
          {(p) => (
            <g data-testid="chart-crosshair">
              <line x1={p().x} x2={p().x} y1="12" y2={H() - 20} class="stroke-base-content/35" stroke-width="1" stroke-dasharray="3 2" />
              <circle cx={p().x} cy={p().y} r="4" class="fill-primary stroke-base-100" stroke-width="1.5" />
              <text
                x={Math.min(Math.max(p().x, 70), W() - 70)}
                y="10"
                text-anchor="middle"
                class="fill-base-content"
                font-size="10"
                style={{ "font-variant-numeric": "tabular-nums" }}
              >
                {`${fmtV(p().value)}${props.unit ?? ""} · ${fmtTime(p().ts)}`}
              </text>
            </g>
          )}
        </Show>
        <Show when={last()}>
          {(p) => (
            <>
              <circle cx={p().x} cy={p().y} r={props.spark ? "2.5" : "3.5"} class="fill-primary stroke-base-100" stroke-width={props.spark ? "1" : "2"} />
              <Show when={!props.spark}>
                <text
                  x={layout().endLabel.x}
                  y={layout().endLabel.y}
                  class="fill-base-content"
                  font-size="11"
                  text-anchor="middle"
                  style={{ "font-variant-numeric": "tabular-nums" }}
                >
                  {fmtV(p().value)}
                  {props.unit ?? ""}
                </text>
              </Show>
            </>
          )}
        </Show>
      </svg>
    </Show>
  );
}
