import { For, Show, type JSX } from "solid-js";

// A ring chart. segments: { key, value, color }[]. Optional onSelect/active make
// each arc a facet (the SummaryFacet board uses this to drive the filter).
export type Segment = { key: string; value: number; color: string; label?: string };

export default function Donut(props: {
  segments: Segment[];
  size?: number;
  thickness?: number;
  center?: JSX.Element;
  onSelect?: (key: string) => void;
  active?: (key: string) => boolean;
}) {
  const size = () => props.size ?? 116;
  const thickness = () => props.thickness ?? 13;
  const cx = () => size() / 2;
  const r = () => (size() - thickness()) / 2;
  const circ = () => 2 * Math.PI * r();
  const anyActive = () => !!props.active && props.segments.some((s) => props.active!(s.key));

  const arcs = () => {
    const segs = props.segments.filter((s) => s.value > 0);
    const total = segs.reduce((sum, s) => sum + s.value, 0);
    let acc = 0;
    return segs.map((seg) => {
      const len = (total > 0 ? seg.value / total : 0) * circ();
      const a = { seg, dash: `${len} ${circ() - len}`, offset: -acc };
      acc += len;
      return a;
    });
  };

  return (
    <div style={{ position: "relative", display: "inline-flex", width: `${size()}px`, height: `${size()}px` }}>
      <svg width={size()} height={size()} viewBox={`0 0 ${size()} ${size()}`} style={{ transform: "rotate(-90deg)" }}>
        <circle cx={cx()} cy={cx()} r={r()} fill="none" stroke="var(--color-base-300)" stroke-width={thickness()} />
        <For each={arcs()}>
          {(a) => (
            <circle
              cx={cx()}
              cy={cx()}
              r={r()}
              fill="none"
              stroke={a.seg.color}
              stroke-width={props.active && props.active(a.seg.key) ? thickness() + 3 : thickness()}
              stroke-dasharray={a.dash}
              stroke-dashoffset={a.offset}
              stroke-linecap="butt"
              style={{
                cursor: props.onSelect ? "pointer" : "default",
                transition: "stroke-width .15s ease",
                opacity: props.active && !props.active(a.seg.key) && anyActive() ? 0.4 : 1,
              }}
              onClick={() => props.onSelect?.(a.seg.key)}
            />
          )}
        </For>
      </svg>
      <Show when={props.center}>
        <div style={{ position: "absolute", inset: 0, display: "flex", "flex-direction": "column", "align-items": "center", "justify-content": "center", "text-align": "center", "line-height": 1.15, "pointer-events": "none" }}>
          {props.center}
        </div>
      </Show>
    </div>
  );
}
