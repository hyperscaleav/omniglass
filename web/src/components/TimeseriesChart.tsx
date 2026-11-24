import { For, Show, createMemo, createSignal, onCleanup } from "solid-js";
import { bucketLayout, chartLayout, nearestByTime, timeAtX, xAtTime, type ChartBucket, type ChartPoint, type ChartSample } from "../lib/chart";
import { useCursor } from "../lib/cursor";
import { fmtClock, fmtTime } from "../lib/format";

// The timeseries chart (#794): one series, drawn from chartLayout's pure
// geometry. Single series, so no legend (the row or title names it); a 2px
// line over a faint area fill, recessive grid, and the newest sample's value
// floating centered above its dot, intersecting the line (#795 review).
// `spark` renders the dense table form: the same geometry, no chrome.
//
// Three ways to give it a series (#1269): raw `samples` drawn as a line, or as
// steps in `mode="step"` (a property holds its value until it changes); or the
// bucketed read's `buckets`, drawn as the min/max band under the avg line so a
// one-sample spike survives any zoom.
//
// The crosshair rides the shared cursor (#2424, lib/cursor): under a
// CursorProvider every chart reads one instant, so hovering one draws the
// line and its own nearest value at that instant on all of them. The
// keyboard drives it too: focus lands on the newest point, the arrows step
// this chart's points, Home and End jump, Escape clears.
export default function TimeseriesChart(props: {
  samples?: ChartSample[];
  buckets?: ChartBucket[];
  resolutionMs?: number;
  mode?: "line" | "step";
  // The value in force when the window opens (#1488), step mode only: drawn
  // from the left edge to the first change.
  carriedIn?: number;
  now: number;
  windowMs: number;
  unit?: string;
  spark?: boolean;
  // `fluid` draws at the width it is given instead of scaling a 640-wide
  // frame (#2424): in a narrow card a scaled frame shrinks its text below
  // reading size, so a fluid chart measures itself and lays out at 1:1.
  fluid?: boolean;
}) {
  const [measured, setMeasured] = createSignal<number | undefined>(undefined);
  const W = () => (props.spark ? 160 : (props.fluid && measured()) || 640);
  const observe = (el: SVGSVGElement) => {
    if (!props.fluid || typeof ResizeObserver === "undefined") return;
    const ro = new ResizeObserver(([e]) => setMeasured(Math.round(e.contentRect.width) || undefined));
    ro.observe(el);
    onCleanup(() => ro.disconnect());
  };
  const H = () => (props.spark ? 36 : 170);
  const frame = createMemo(() => ({
    width: W(),
    height: H(),
    padLeft: props.spark ? 3 : 44,
    padRight: props.spark ? 6 : 16,
    padY: props.spark ? 5 : 20,
    now: props.now,
    windowMs: props.windowMs,
  }));
  const layout = createMemo(() => {
    if (props.buckets) return bucketLayout(props.buckets, { ...frame(), resolutionMs: props.resolutionMs ?? 3600_000 });
    return chartLayout(props.samples ?? [], { ...frame(), mode: props.mode, carriedIn: props.carriedIn });
  });
  const endNearEdge = () => layout().endLabel.x > W() - 60;
  const last = () => (layout().linePath ? layout().endDot : undefined);
  const tip = (p: ChartPoint) =>
    p.min !== undefined && p.max !== undefined
      ? `${fmtV(p.value)}${props.unit ?? ""} avg, ${fmtV(p.min)} to ${fmtV(p.max)} (${p.count} sample${p.count === 1 ? "" : "s"}) · ${fmtTime(p.ts)}`
      : `${fmtV(p.value)}${props.unit ?? ""} · ${fmtTime(p.ts)}`;
  const fmtV = (v: number) => (Math.abs(v) >= 100 ? v.toFixed(0) : v.toFixed(1));
  // The crosshair: at the shared cursor's instant, the sample nearest it, an
  // exact time and value where a sample exists, never an interpolation (#795
  // review). A bucketed series has no point further than a bucket from the
  // cursor, so its gap reads as a gap; a raw one snaps as it always has.
  const cursor = useCursor();
  const hover = createMemo(() => {
    const t = cursor.at();
    if (t === undefined || props.spark) return null;
    const reach = props.buckets ? (props.resolutionMs ?? 3600_000) : Infinity;
    return { x: xAtTime(t, frame()), point: nearestByTime(layout().points, t, reach) };
  });
  // The handler reads the svg off the event it is attached to (the retired band canvas
  // pattern): no ref, no initialization-order question for a reader or an
  // analyzer to puzzle over.
  const onMove = (e: MouseEvent & { currentTarget: SVGSVGElement }) => {
    if (props.spark) return;
    const r = e.currentTarget.getBoundingClientRect();
    if (r.width <= 0) return;
    cursor.set(timeAtX(((e.clientX - r.left) / r.width) * W(), frame()));
  };
  const onKey = (e: KeyboardEvent) => {
    const pts = layout().points;
    if (pts.length === 0) return;
    const at = cursor.at();
    const here = at === undefined ? pts.length - 1 : pts.indexOf(nearestByTime(pts, at)!);
    const step: Record<string, number | undefined> = {
      ArrowLeft: Math.max(here - 1, 0),
      ArrowRight: Math.min(here + 1, pts.length - 1),
      Home: 0,
      End: pts.length - 1,
    };
    if (e.key === "Escape") {
      cursor.set(undefined);
      return;
    }
    const next = step[e.key];
    if (next === undefined) return;
    e.preventDefault();
    cursor.set(pts[next].t);
  };
  const onFocus = () => {
    const pts = layout().points;
    if (cursor.at() === undefined && pts.length > 0) cursor.set(pts[pts.length - 1].t);
  };
  return (
    <Show
      when={layout().linePath !== ""}
      fallback={
        <Show when={!props.spark} fallback={<span class="text-[10px] text-base-content/40">no samples</span>}>
          <p class="text-sm text-base-content/50">No samples in the window.</p>
        </Show>
      }
    >
      <svg
        data-testid={props.spark ? "sparkline" : "timeseries-chart"}
        viewBox={`0 0 ${W()} ${H()}`}
        ref={observe}
        class={props.spark ? "h-9 w-40" : `w-full ${props.fluid ? "" : "max-w-3xl "}rounded-field focus:outline-none focus-visible:ring-2 focus-visible:ring-primary/50`}
        role="img"
        aria-label={hover()?.point ? `The series over the window, newest at the right. At the cursor: ${tip(hover()!.point!)}` : "The series over the window, newest at the right"}
        tabindex={props.spark ? undefined : 0}
        onMouseMove={onMove}
        onMouseLeave={() => cursor.set(undefined)}
        onFocus={onFocus}
        // Focus moving to another chart (a click on it) keeps the instant the
        // pointer set there; leaving the charts clears it.
        onBlur={(e) => {
          const next = e.relatedTarget as Element | null;
          if (!next?.closest?.("[data-testid=timeseries-chart]")) cursor.set(undefined);
        }}
        aria-keyshortcuts={props.spark ? undefined : "ArrowLeft ArrowRight Home End Escape"}
        onKeyDown={onKey}
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
        <Show when={layout().bandPath}>
          <path data-testid="chart-band" d={layout().bandPath} class="fill-primary/25" />
        </Show>
        <Show when={layout().areaPath}>
          <path d={layout().areaPath} class="fill-primary/10" />
        </Show>
        <path d={layout().linePath} class="stroke-primary" stroke-width={props.spark ? "1.5" : "2"} fill="none" stroke-linejoin="round" />
        <Show when={!props.spark}>
          <For each={layout().points}>
            {(p) => (
              <circle cx={p.x} cy={p.y} r="5" class="fill-transparent hover:fill-primary/40">
                <title>{tip(p)}</title>
              </circle>
            )}
          </For>
        </Show>
        <Show when={hover()}>
          {(h) => (
            <g data-testid="chart-crosshair">
              <line data-testid="cursor-line" x1={h().x} x2={h().x} y1="12" y2={H() - 20} class="stroke-base-content/35" stroke-width="1" stroke-dasharray="3 2" />
              <Show when={h().point}>
                {(p) => (
                  <>
                    <circle cx={p().x} cy={p().y} r="4" class="fill-primary stroke-base-100" stroke-width="1.5" />
                    <text
                      data-testid="cursor-value"
                      // Beside the line, on the side with room: a readout
                      // centered on a line near an edge runs off the frame.
                      x={h().x < W() / 2 ? h().x + 6 : h().x - 6}
                      y="10"
                      text-anchor={h().x < W() / 2 ? "start" : "end"}
                      class="fill-base-content"
                      font-size="10"
                      style={{ "font-variant-numeric": "tabular-nums" }}
                    >
                      {tip(p())}
                    </text>
                  </>
                )}
              </Show>
            </g>
          )}
        </Show>
        <Show when={last()}>
          {(p) => (
            <>
              <circle data-testid="chart-end-dot" cx={p().x} cy={p().y} r={props.spark ? "2.5" : "3.5"} class="fill-primary stroke-base-100" stroke-width={props.spark ? "1" : "2"} />
              <Show when={!props.spark}>
                <text
                  // Near the right edge the label ends at the frame rather
                  // than centering past it, so a unit is never cut off.
                  x={endNearEdge() ? W() - 2 : layout().endLabel.x}
                  y={layout().endLabel.y}
                  class="fill-base-content"
                  font-size="11"
                  text-anchor={endNearEdge() ? "end" : "middle"}
                  style={{ "font-variant-numeric": "tabular-nums" }}
                >
                  {fmtV(layout().endValue ?? 0)}
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
