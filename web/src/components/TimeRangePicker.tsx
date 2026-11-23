import { For, Show, createSignal, createUniqueId } from "solid-js";
import { Popover } from "@kobalte/core/popover";
import { Clock } from "./icons";
import Button from "./Button";
import { PRESETS, rangeLabel, type TimeRange } from "../lib/time_range";

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
  const customOk = () => fromMs() !== undefined && toMs() !== undefined && fromMs()! < toMs()!;
  const pick = (r: TimeRange) => {
    props.onChange(r);
    setOpen(false);
  };
  const isPreset = (key: string) => props.range.kind === "preset" && props.range.preset === key;

  return (
    <Popover
      placement="bottom-end"
      gutter={6}
      open={open()}
      onOpenChange={(o) => {
        if (o) seed();
        setOpen(o);
      }}
    >
      <Popover.Trigger data-testid="time-range-trigger" class="btn btn-quiet btn-xs gap-1.5 font-normal" aria-label={`Time range: ${rangeLabel(props.range)}`}>
        <Clock size={13} />
        <span class="tabular-nums">{rangeLabel(props.range)}</span>
      </Popover.Trigger>
      <Popover.Portal>
        <Popover.Content
          data-testid="time-range-panel"
          // Focus lands on the range in force, not the first preset, so the
          // ring never reads as a second selection.
          onOpenAutoFocus={(e) => {
            const checked = (e.currentTarget as HTMLElement | null)?.querySelector<HTMLElement>('[aria-checked="true"]');
            if (checked) {
              e.preventDefault();
              checked.focus();
            }
          }}
          class="z-50 flex w-72 max-w-[calc(100vw-1rem)] flex-col gap-2 rounded-box border border-base-300 bg-base-100 p-2 text-sm shadow-2xl focus:outline-none"
        >
          <p class="menu-title px-2 pt-1 pb-0 text-[10.5px]">Time range · every chart on this page</p>
          <ul role="radiogroup" aria-label="Preset ranges" class="flex flex-col">
            <For each={PRESETS}>
              {(p) => (
                <li>
                  <button
                    type="button"
                    role="radio"
                    aria-checked={isPreset(p.key) ? "true" : "false"}
                    data-testid={`time-range-preset-${p.key}`}
                    class="flex w-full cursor-pointer items-center justify-between rounded-field px-2 py-1.5 text-left hover:bg-base-content/5"
                    classList={{ "font-medium text-primary": isPreset(p.key) }}
                    onClick={() => pick({ kind: "preset", preset: p.key })}
                  >
                    {p.label}
                    <span class="font-data text-[10.5px] text-base-content/45">{p.key}</span>
                  </button>
                </li>
              )}
            </For>
          </ul>
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
                <p class="text-xs text-warning">The range must end after it starts.</p>
              </Show>
              <Button type="submit" intent="action" size="xs" class="self-end" disabled={!customOk()}>Apply</Button>
            </form>
          </Show>
        </Popover.Content>
      </Popover.Portal>
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
