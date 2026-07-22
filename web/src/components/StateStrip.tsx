import { For, type JSX } from "solid-js";

// StateStrip is the console's one horizontal state-over-time strip: a rounded bar
// split into weighted segments, one per contiguous stretch in a single state, with
// an optional trailing hint. It is deliberately dumb about MEANING: the caller maps
// a state value to its tone class, so the same primitive draws interface
// availability (up / down) and estate health (healthy / degraded / outage) without
// either surface inventing a second timeline idiom.
//
// The segments come from lib/timeline's `spans`, which turns recorded EDGES into
// weights. Both halves are shared on purpose: a strip drawn from samples and a
// strip drawn from transitions look identical and mean different things, so the
// derivation and the drawing travel together.

export type StripSegment = {
  value: string;
  // Share of the window, 0..1. Rendered as a flex weight, floored so a very short
  // stretch is still a visible sliver rather than nothing.
  weight: number;
  // Per-segment hover text, e.g. how long that state lasted and when it began.
  title?: string;
};

export default function StateStrip(props: {
  segments: StripSegment[];
  // state value -> a background class (bg-success, bg-error, ...).
  tone: (value: string) => string;
  // Hover text for the bar as a whole (what window it covers).
  title?: string;
  // Height class; the default matches the interface rows.
  height?: string;
  // The trailing hint beside the bar (an uptime percentage, a change count).
  children?: JSX.Element;
}) {
  return (
    <div class="flex items-center gap-2">
      <div class={`flex ${props.height ?? "h-2"} flex-1 overflow-hidden rounded-full bg-base-300`} title={props.title}>
        <For each={props.segments}>
          {(s) => <div class={props.tone(s.value)} style={{ flex: `${Math.max(s.weight, 0.001)} 1 0%` }} title={s.title} />}
        </For>
      </div>
      {props.children}
    </div>
  );
}
