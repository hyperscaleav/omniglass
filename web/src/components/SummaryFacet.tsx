import { For, type JSX } from "solid-js";
import Donut, { type Segment } from "./Donut";

// SummaryFacet: a donut + a clickable legend, where each segment is a filter
// facet (the summary widget board's health/type/site cards). Clicking a legend
// row or arc toggles that facet (onSelect), and active() reflects the chip
// state, so a summary card and the filter stay in sync.
export default function SummaryFacet(props: {
  title: string;
  segments: Segment[];
  center?: JSX.Element;
  onSelect: (key: string) => void;
  active: (key: string) => boolean;
  label?: (s: Segment) => string;
}) {
  return (
    <div class="card border border-base-300 bg-base-200">
      <div class="og-pad">
        <div class="eyebrow mb-3.5">{props.title}</div>
        <div class="flex items-center gap-[18px]">
          <Donut segments={props.segments} onSelect={props.onSelect} active={props.active} center={props.center} />
          <ul class="m-0 flex flex-1 list-none flex-col gap-px p-0 text-sm">
            <For each={props.segments.filter((s) => s.value > 0)}>
              {(s) => (
                <li>
                  <button
                    class="flex w-full items-center gap-2.5 rounded-field px-[7px] py-1 text-left text-sm"
                    classList={{ "bg-base-content/10 font-semibold": props.active(s.key), "hover:bg-base-content/5": !props.active(s.key) }}
                    onClick={() => props.onSelect(s.key)}
                  >
                    <span class="size-2.5 flex-none rounded-[3px]" style={{ background: s.color }} />
                    <span class="flex-1 overflow-hidden text-ellipsis whitespace-nowrap">{props.label ? props.label(s) : s.label ?? s.key}</span>
                    <span class="tnum text-base-content/50">{s.value}</span>
                  </button>
                </li>
              )}
            </For>
          </ul>
        </div>
      </div>
    </div>
  );
}
