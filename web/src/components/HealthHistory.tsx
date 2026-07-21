import { For, Show, createMemo } from "solid-js";
import StateStrip from "./StateStrip";
import { fmtTime, rel } from "../lib/format";
import { durationText, share, spans, type Span } from "../lib/timeline";
import type { HealthTransition } from "../lib/health";

// HealthHistory renders the recorded EDGES: the moments this system or location
// changed verdict, and how long each state held.
//
// This is the durable requirement behind the whole health design. A live badge
// answers "is it broken now", which an operator can also get by walking into the
// room. The question a console has to answer weeks later is "when exactly did this
// go unhealthy, and for how long", and only a recorded edge can answer it: a sample
// says what a poller happened to see, an edge says what actually changed and when.
// So the strip is drawn from transitions, never from samples, and each stretch is
// labelled with its real duration rather than a bucket.
//
// The bar is the shared StateStrip, the same primitive the interface availability
// strip uses, drawn from the same lib/timeline spans. One timeline idiom in the
// console, two things measured with it.

const TONE: Record<string, string> = {
  healthy: "bg-success",
  degraded: "bg-warning",
  outage: "bg-error",
};
const tone = (v: string) => TONE[v] ?? "bg-base-300";

const PILL: Record<string, string> = {
  healthy: "badge-success",
  degraded: "badge-warning",
  outage: "badge-error",
};
const pill = (v: string) => PILL[v] ?? "badge-ghost";

export default function HealthHistory(props: {
  transitions: HealthTransition[];
  // The current verdict, so a window with no recorded change still draws as the
  // state it is in rather than as an empty bar.
  verdict?: string;
  // What the window covers, in the API's terms.
  window?: string;
}) {
  // Pinned at setup, like the availability strip: a strip whose "now" moved under
  // it would re-weight every segment on an unrelated re-render.
  const now = Date.now();
  const edges = () => props.transitions ?? [];
  const list = createMemo<Span<string>[]>(() =>
    spans(edges().map((t) => ({ ts: t.ts, value: t.verdict })), props.verdict ?? null, now),
  );
  // Newest first for the reading order: what it is now, then what it was before.
  const rows = createMemo(() => [...list()].reverse());
  const healthy = createMemo(() => share(list(), (v) => v === "healthy"));
  const changes = () => Math.max(edges().length - 1, 0);

  return (
    <div class="flex flex-col gap-2">
      <div class="flex items-baseline justify-between gap-2">
        <span class="eyebrow">History</span>
        <span class="shrink-0 text-[10.5px] text-base-content/40">{props.window ?? "the recorded edges, last 30 days"}</span>
      </div>
      <p class="text-[11px] text-base-content/50">
        One entry per change, never a sample. This is what answers "when exactly did it go unhealthy", read back weeks
        later.
      </p>

      <StateStrip segments={list()} tone={tone} height="h-2.5" title="Verdict over the recorded window">
        <Show when={healthy() !== null} fallback={<span class="text-[11px] text-base-content/40">no data</span>}>
          <span class="w-20 shrink-0 text-right text-[11px] tabular-nums text-base-content/60">{healthy()}% healthy</span>
        </Show>
      </StateStrip>

      <Show
        when={edges().length}
        fallback={
          <p class="rounded-box border border-dashed border-base-300 px-3 py-3 text-center text-[12px] text-base-content/45">
            No change recorded in this window. Nothing has moved since before it began.
          </p>
        }
      >
        <div class="divide-y divide-base-300 overflow-hidden rounded-box border border-base-300">
          <For each={rows()}>
            {(s, i) => {
              const current = () => i() === 0;
              return (
                <div class="flex flex-wrap items-baseline gap-2 px-3 py-2">
                  <span class={`badge badge-soft badge-sm shrink-0 ${pill(s.value)}`}>{s.value}</span>
                  <span class="text-[12px] text-base-content/70">
                    <Show when={current()} fallback="from">
                      since
                    </Show>{" "}
                    {fmtTime(new Date(s.from).toISOString())}
                  </span>
                  <span class="text-[11px] text-base-content/40">({rel(new Date(s.from).toISOString())})</span>
                  <span class="flex-1" />
                  <span class="tnum shrink-0 text-[11.5px] text-base-content/60">
                    <Show when={current()} fallback={`held ${durationText(s.to - s.from)}`}>
                      {durationText(s.to - s.from)} and counting
                    </Show>
                  </span>
                </div>
              );
            }}
          </For>
        </div>
        <span class="text-[11px] text-base-content/40">
          {changes()} change{changes() === 1 ? "" : "s"} recorded in this window.
        </span>
      </Show>
    </div>
  );
}
