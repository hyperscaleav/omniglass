import { For, Show, createSignal, createUniqueId } from "solid-js";
import Popover from "./ui/Popover";
import { Clock } from "./icons";
import Button from "./Button";
import { MAX_CUSTOM_MS, PRESETS, rangeLabel, type TimeRange } from "../lib/time_range";

// TimeRangePicker: the page's one time-range control (#2304), on the tab rail.
// The trigger names the range; its panel (a Kobalte popover, portaled so a
// card's overflow never clips it) offers the presets, which slide to now, and,
// where every read under it can honor one, a custom absolute range. A page
// whose reads only count hours back from now passes allowCustom={false}
// rather than offering a control that would be ignored.
//
// Controlled: the page holds the range (useTimeRange, the URL) and passes it
// in; a pick is reported through onChange.
export default function TimeRangePicker(props: { range: TimeRange; onChange: (r: TimeRange) => void; allowCustom?: boolean }) {
  const [open, setOpen] = createSignal(false);
  const [from, setFrom] = createSignal("");
  const [to, setTo] = createSignal("");
  const fromId = createUniqueId();
  const toId = createUniqueId();

  // Opening seeds the custom fields from the range in force: an absolute
  // range's own ends, a preset's span ending now.
  const seed = () => {
    const now = Date.now();
    const r = props.range;
    const span = r.kind === "absolute" ? r : { from: now - (PRESETS.find((p) => p.key === r.preset)?.ms ?? 0), to: now };
    setFrom(toLocalInput(span.from));
    setTo(toLocalInput(span.to));
  };
  const fromMs = () => fromLocalInput(from());
  const toMs = () => fromLocalInput(to());
  // Why a custom range cannot apply, or undefined when it can.
  const customProblem = () => {
    const f = fromMs();
    const t = toMs();
    if (f === undefined || t === undefined) return "Give both ends.";
    if (f >= t) return "The range must end after it starts.";
    if (t - f > MAX_CUSTOM_MS) return "A range spans at most two years.";
    return undefined;
  };
  const customOk = () => customProblem() === undefined;
  const pick = (r: TimeRange) => {
    props.onChange(r);
    setOpen(false);
  };
  const isPreset = (key: string) => props.range.kind === "preset" && props.range.preset === key;
  // One tab stop for the group (the range in force, else the first preset);
  // the arrows move between presets, wrapping, as a radio group does.
  const tabStop = (i: number) => (props.range.kind === "preset" ? isPreset(PRESETS[i].key) : i === 0);
  const onArrow = (e: KeyboardEvent, i: number) => {
    const step = e.key === "ArrowDown" || e.key === "ArrowRight" ? 1 : e.key === "ArrowUp" || e.key === "ArrowLeft" ? -1 : 0;
    if (!step) return;
    e.preventDefault();
    const radios = (e.currentTarget as HTMLElement).parentElement?.querySelectorAll<HTMLElement>('[role="radio"]');
    radios?.[(i + step + PRESETS.length) % PRESETS.length]?.focus();
  };

  return (
    <Popover class="btn btn-quiet btn-xs gap-1.5 font-normal"
      placement="bottom-end"
      gutter={6}
      open={open()}
      onOpenChange={(o) => {
        if (o) seed();
        setOpen(o);
      }}
      data-testid="time-range-trigger"
      aria-label={`Time range: ${rangeLabel(props.range)}`}
      contentLabel="Time range"
      contentClass="z-50 w-72 max-w-[calc(100vw-1rem)] rounded-box border border-base-300 bg-base-100 p-2 text-sm shadow-2xl focus:outline-none"
      // Focus lands on the range in force, not the first preset, so the
      // ring never reads as a second selection.
      initialFocus={(panel) => panel.querySelector<HTMLElement>('[aria-checked="true"]')}
      content={
        <div data-testid="time-range-panel" class="flex flex-col gap-2">
          <p class="menu-title px-2 pt-1 pb-0 text-[10.5px]">Time range · every chart on this page</p>
          <div role="radiogroup" aria-label="Preset ranges" class="flex flex-col">
            <For each={PRESETS}>
              {(p, i) => (
                  <button
                    type="button"
                    role="radio"
                    aria-checked={isPreset(p.key) ? "true" : "false"}
                    data-testid={`time-range-preset-${p.key}`}
                    class="flex w-full cursor-pointer items-center justify-between rounded-field px-2 py-1.5 text-left hover:bg-base-content/5"
                    classList={{ "font-medium text-primary": isPreset(p.key) }}
                    tabindex={tabStop(i()) ? 0 : -1}
                    onKeyDown={(e) => onArrow(e, i())}
                    onClick={() => pick({ kind: "preset", preset: p.key })}
                  >
                    {p.label}
                    <span class="font-data text-[10.5px] text-base-content/45">{p.key}</span>
                  </button>
              )}
            </For>
          </div>
          <Show when={props.allowCustom}>
            <form
              class="flex flex-col gap-1.5 border-t border-base-300 px-2 pt-2 pb-1"
              onSubmit={(e) => {
                e.preventDefault();
                if (customOk()) pick({ kind: "absolute", from: fromMs()!, to: toMs()! });
              }}
            >
              <span class="text-[10.5px] text-base-content/60">Custom range, your local time</span>
              <div class="grid grid-cols-[2.5rem_1fr] items-center gap-x-2 gap-y-1.5">
                <label for={fromId} class="text-xs text-base-content/70">From</label>
                <input id={fromId} type="datetime-local" class="input input-bordered input-xs w-full min-w-0 font-data" value={from()} onInput={(e) => setFrom(e.currentTarget.value)} />
                <label for={toId} class="text-xs text-base-content/70">To</label>
                <input id={toId} type="datetime-local" class="input input-bordered input-xs w-full min-w-0 font-data" value={to()} onInput={(e) => setTo(e.currentTarget.value)} />
              </div>
              <Show when={from() && to() && !customOk()}>
                <p class="text-xs text-warning">{customProblem()}</p>
              </Show>
              <Button type="submit" intent="action" size="xs" class="self-end" disabled={!customOk()}>Apply</Button>
            </form>
          </Show>
        </div>
      }
    >
      <Clock size={13} />
      <span class="tabular-nums">{rangeLabel(props.range)}</span>
    </Popover>
  );
}

const pad = (n: number) => String(n).padStart(2, "0");

// A datetime-local input speaks local wall time to the minute, no zone.
export function toLocalInput(ms: number): string {
  const d = new Date(ms);
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

export function fromLocalInput(s: string): number | undefined {
  if (!/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}$/.test(s)) return undefined;
  const t = new Date(s).getTime();
  return Number.isNaN(t) ? undefined : t;
}
