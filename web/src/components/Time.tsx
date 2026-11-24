import { Show } from "solid-js";
import Tooltip from "./ui/Tooltip";
import { fmtTime, rel } from "../lib/format";

// An instant as the console prints it everywhere (#2447): the relative age
// ("4m ago") with the absolute time and the exact instant on hover and on
// keyboard focus. `absolute` makes the text itself the absolute time, for a cell
// where "4m ago" tells nothing (a before/after pair in the audit change table).
// A value that is not an instant is shown as it came, with no tooltip.
export default function Time(props: { iso: string; now?: number; absolute?: boolean; class?: string }) {
  const valid = () => !isNaN(new Date(props.iso).getTime());
  return (
    <Show when={valid()} fallback={<span class={props.class}>{props.iso}</span>}>
      <Tooltip
        as="span"
        tabIndex={0}
        class={`cursor-default tabular-nums ${props.class ?? ""}`}
        content={
          <span class="flex flex-col gap-0.5">
            <span>{fmtTime(props.iso)}</span>
            <span class="font-data text-[11px] text-base-content/60">{props.iso}</span>
          </span>
        }
      >
        {props.absolute ? fmtTime(props.iso) : rel(props.iso, props.now)}
      </Tooltip>
    </Show>
  );
}
