import { For, Show, createMemo, createSignal } from "solid-js";
import { useQuery } from "@tanstack/solid-query";
import { ChevronRight, Plus, Sliders } from "./icons";
import Button from "./Button";
import { rel } from "../lib/format";
import { INTERFACES_KEY, listInterfaces } from "../lib/interfaces";
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

// The Interfaces panel on the component detail: an interface belongs to its
// component, so it surfaces here (not a top-level tab). One row per interface (an
// interface is usable or not): the endpoint, an availability strip built from the
// verdict's transition history, and a 4-state verdict pill derived at read time.
// Expanding a row reveals the layered-gate breakdown (ping L3, port L4) plus a
// "why" reason line when the interface is down. The verdict/strip/reason are
// read-only (derived from real fields, no invented latency or history); the panel
// header carries the "Add interface" affordance and each row a "Manage" affordance,
// both wired by the component detail into its shared blade stack (create + the
// read-edit-save detail blade). Those write through the interfaces API and refresh
// this panel. The management affordances render only when the component detail
// passes their callbacks (gated on interface:create / interface:read).

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

function InterfaceRow(p: { iface: ReachInterface; manageId?: string; onManage?: (id: string) => void }) {
  const [open, setOpen] = createSignal(false);
  const word = createMemo(() => verdictWord(p.iface.verdict));
  const pill = createMemo(() => PILL[word()]);
  const canManage = () => !!(p.manageId && p.onManage);
  return (
    <div class="overflow-hidden">
      <div class="flex items-stretch">
        <button
          type="button"
          class="flex min-w-0 flex-1 items-center gap-3 px-3 py-2.5 text-left hover:bg-base-content/5"
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
        <Show when={canManage()}>
          <button
            type="button"
            class="flex shrink-0 items-center px-2.5 text-base-content/40 hover:bg-base-content/5 hover:text-base-content"
            title="Manage interface"
            aria-label={`Manage ${p.iface.interface}`}
            onClick={() => p.onManage!(p.manageId!)}
          >
            <Sliders size={15} />
          </button>
        </Show>
      </div>
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

export default function ReachabilityPanel(p: {
  name: string;
  // Present -> the panel header shows "Add interface", opening a create surface for
  // this component (the caller gates on interface:create).
  onAdd?: () => void;
  // Present -> each row that maps to a known interface shows "Manage", opening that
  // interface's detail blade by id (the caller gates on interface:read).
  onOpenInterface?: (id: string) => void;
}) {
  const q = useQuery(() => ({ queryKey: REACHABILITY_KEY(p.name), queryFn: () => getReachability(p.name) }));
  const ifaces = createMemo(() => q.data?.interfaces ?? []);
  // Only load the interface list when the caller can open interface details; map an
  // interface's name (the reachability row key) to its surrogate id so a row can
  // open its detail blade.
  const interfaces = useQuery(() => ({ queryKey: INTERFACES_KEY, queryFn: () => listInterfaces(), enabled: !!p.onOpenInterface }));
  const idByName = createMemo(() => {
    const m = new Map<string, string>();
    for (const it of interfaces.data ?? []) if (it.component === p.name) m.set(it.name, it.id);
    return m;
  });
  return (
    <div class="flex flex-col gap-1.5">
      <div class="flex items-center gap-2">
        <span class="eyebrow">Interfaces</span>
        <Show when={ifaces().length}>
          <span class="text-[11px] text-base-content/40">
            {ifaces().length} interface{ifaces().length === 1 ? "" : "s"}
          </span>
        </Show>
        <span class="flex-1" />
        <Show when={p.onAdd}>
          <Button size="xs" intent="quiet" icon={Plus} onClick={() => p.onAdd!()}>Add interface</Button>
        </Show>
      </div>
      <Show
        when={ifaces().length}
        fallback={
          <div class="rounded-box border border-dashed border-base-300 px-3 py-4 text-center text-[12px] text-base-content/45">
            <Show when={q.isLoading} fallback="No interfaces on this component yet.">
              Loading interfaces…
            </Show>
          </div>
        }
      >
        <div class="divide-y divide-base-300 overflow-hidden rounded-box border border-base-300">
          <For each={ifaces()}>{(iface) => <InterfaceRow iface={iface} manageId={idByName().get(iface.interface)} onManage={p.onOpenInterface} />}</For>
        </div>
      </Show>
    </div>
  );
}
