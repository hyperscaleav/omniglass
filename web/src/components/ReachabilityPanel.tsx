import { For, Show, createMemo, createSignal } from "solid-js";
import { useQuery } from "@tanstack/solid-query";
import { ChevronRight } from "./icons";
import { rel } from "../lib/format";
import {
  REACHABILITY_KEY,
  getReachability,
  verdictWord,
  segments,
  uptime,
  reason,
  layerWord,
  type ReachInterface,
  type VerdictWord,
} from "../lib/reachability";

// The Reachability panel on the component detail: one row per interface (an
// interface is usable or not). Each row shows the interface endpoint, an
// availability strip built from the verdict's transition history, and a 4-state
// verdict pill derived at read time. Expanding a row reveals the layered-gate
// breakdown (ping L3, port L4) plus a "why" reason line when the interface is
// down. The rows are read-only (verdict/strip/reason derived from real fields, no
// invented latency or history); the header carries the 5d "Add check" affordance,
// which authors a valid interface + poll task for this component.

// The pill hue and label per derived verdict word.
const PILL: Record<VerdictWord, { cls: string; label: string }> = {
  responding: { cls: "badge-success", label: "responding" },
  down: { cls: "badge-error", label: "down" },
  stale: { cls: "badge-warning", label: "stale" },
  unknown: { cls: "badge-ghost", label: "unknown" },
};

function segClass(value: string): string {
  return value === "up" ? "bg-success" : "bg-error";
}

// AvailabilityStrip renders the up/down segments as flex weights, with an uptime
// hint. A single-value strip (or none) still reads correctly.
function AvailabilityStrip(p: { iface: ReachInterface }) {
  const now = Date.now();
  const segs = createMemo(() => segments(p.iface.history, p.iface.verdict, now));
  const up = createMemo(() => uptime(p.iface.history, p.iface.verdict, now));
  return (
    <div class="flex items-center gap-2">
      <div class="flex h-2 flex-1 overflow-hidden rounded-full bg-base-300" title="Availability over the recent window">
        <For each={segs()}>
          {(s) => <div class={segClass(s.value)} style={{ flex: `${Math.max(s.weight, 0.001)} 1 0%` }} />}
        </For>
      </div>
      <Show when={up() !== null} fallback={<span class="text-[11px] text-base-content/40">no data</span>}>
        <span class="w-12 shrink-0 text-right text-[11px] tabular-nums text-base-content/60">{up()}% up</span>
      </Show>
    </div>
  );
}

// GateBreakdown lists one line per probe layer (dot + word + timing detail) and
// the "why" reason line for a down interface. It is the pedagogical payoff: the
// verdict is the AND of the layers, shown as the layers.
function GateBreakdown(p: { iface: ReachInterface }) {
  const now = Date.now();
  const why = createMemo(() => reason(p.iface, now));
  const verdictOk = createMemo(() => p.iface.verdict?.value === "up");
  return (
    <div class="flex flex-col gap-1.5 border-t border-base-300 bg-base-200/40 px-3 py-2.5 text-[12px]">
      <For each={p.iface.layers}>
        {(l) => {
          const up = l.value >= 1;
          return (
            <div class="flex items-center gap-2">
              <span class={`inline-block size-2 shrink-0 rounded-full ${up ? "bg-success" : "bg-error"}`} />
              <span class="w-10 shrink-0 uppercase tracking-wide text-base-content/45 text-[10px]">{l.layer}</span>
              <span class="font-data text-base-content/70">{l.check}</span>
              <span class="text-base-content/50">{layerWord(l)}</span>
              <Show when={l.detail}>
                <span class="text-base-content/40">· {l.detail}</span>
              </Show>
            </div>
          );
        }}
      </For>
      <div class="flex items-center gap-2">
        <span class={`inline-block size-2 shrink-0 rounded-full ${verdictOk() ? "bg-success" : "bg-error"}`} />
        <span class="w-10 shrink-0 uppercase tracking-wide text-base-content/45 text-[10px]">verdict</span>
        <span class="text-base-content/60">interface {verdictOk() ? "up" : "down"}</span>
      </div>
      <Show when={why()}>
        <p class="mt-0.5 rounded-md bg-warning/10 px-2 py-1.5 text-[11.5px] text-base-content/70">{why()}</p>
      </Show>
    </div>
  );
}

function InterfaceRow(p: { iface: ReachInterface }) {
  const [open, setOpen] = createSignal(false);
  const word = createMemo(() => verdictWord(p.iface.verdict));
  const pill = createMemo(() => PILL[word()]);
  return (
    <div class="overflow-hidden">
      <button
        type="button"
        class="flex w-full items-center gap-3 px-3 py-2.5 text-left hover:bg-base-content/5"
        onClick={() => setOpen((v) => !v)}
        aria-expanded={open()}
      >
        <span class={`shrink-0 text-base-content/40 transition-transform ${open() ? "rotate-90" : ""}`}><ChevronRight size={14} /></span>
        <div class="flex w-52 shrink-0 flex-col gap-0.5">
          <span class="truncate text-sm">{p.iface.interface}</span>
          <span class="truncate font-data text-[11px] text-base-content/50">
            {p.iface.type}
            <Show when={p.iface.endpoint}> · {p.iface.endpoint}</Show>
          </span>
        </div>
        <div class="min-w-0 flex-1">
          <AvailabilityStrip iface={p.iface} />
        </div>
        <span class={`badge badge-soft badge-sm shrink-0 ${pill().cls}`}>{pill().label}</span>
      </button>
      <Show when={open()}>
        <GateBreakdown iface={p.iface} />
        <Show when={p.iface.node}>
          <div class="border-t border-base-300 bg-base-200/40 px-3 py-1.5 text-[11px] text-base-content/45">
            probed by <span class="font-data text-base-content/60">{p.iface.node}</span>
            <Show when={p.iface.verdict}>
              {" "}· last checked {rel(p.iface.verdict!.ts)}
            </Show>
          </div>
        </Show>
      </Show>
    </div>
  );
}

export default function ReachabilityPanel(p: { name: string }) {
  const q = useQuery(() => ({ queryKey: REACHABILITY_KEY(p.name), queryFn: () => getReachability(p.name) }));
  const ifaces = createMemo(() => q.data?.interfaces ?? []);
  return (
    <div class="flex flex-col gap-1.5">
      <div class="flex items-center gap-2">
        <span class="eyebrow">Reachability</span>
        <Show when={ifaces().length}>
          <span class="text-[11px] text-base-content/40">
            {ifaces().length} interface{ifaces().length === 1 ? "" : "s"}
          </span>
        </Show>
      </div>
      <Show
        when={ifaces().length}
        fallback={
          <div class="rounded-box border border-dashed border-base-300 px-3 py-4 text-center text-[12px] text-base-content/45">
            <Show when={q.isLoading} fallback="No reachability checks on this component yet.">
              Loading reachability…
            </Show>
          </div>
        }
      >
        <div class="divide-y divide-base-300 overflow-hidden rounded-box border border-base-300">
          <For each={ifaces()}>{(iface) => <InterfaceRow iface={iface} />}</For>
        </div>
      </Show>
    </div>
  );
}
