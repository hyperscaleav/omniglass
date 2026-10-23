import { For, Show, createMemo, createSignal } from "solid-js";
import { useQuery } from "@tanstack/solid-query";
import { ChevronRight, Plus, Sliders } from "./icons";
import Button from "./Button";
import StateStrip from "./StateStrip";
import SessionPill from "./SessionPill";
import ReachLadder from "./ReachLadder";
import SessionNotice from "./SessionNotice";
import { rel } from "../lib/format";
import { CLOCK_TICK_MS, createClock } from "../lib/clock";
import { entityLabel } from "../lib/entities";
import { ENDPOINTS_KEY, listEndpoints } from "../lib/endpoints";
import {
  REACHABILITY_KEY,
  getReachability,
  endpointWord,
  lastSeenAt,
  segments,
  uptime,
  reason,
  layerWord,
  type ReachEndpoint,
  type VerdictWord,
} from "../lib/reachability";

// The Endpoints panel on the component detail: an endpoint belongs to its
// component, so it surfaces here (not a top-level tab). One row per endpoint (an
// endpoint is usable or not): the endpoint, an availability strip built from the
// verdict's transition history, and a 4-state verdict pill derived at read time.
// Expanding a row reveals the layered-gate breakdown (ping L3, port L4) plus a
// "why" reason line when the endpoint is down. The verdict/strip/reason are
// read-only (derived from real fields, no invented latency or history); the panel
// header carries the "Add endpoint" affordance and each row a "Manage" affordance,
// both wired by the component detail into its shared blade stack (create + the
// read-edit-save detail blade). Those write through the endpoints API and refresh
// this panel. The management affordances render only when the component detail
// passes their callbacks (gated on endpoint:create / endpoint:read).
//
// A held session (#1059) rides the row too: the reachability ladder (ping,
// transport, login, the layered gate visible), the session pill, and for a
// rejected or down session the reason beneath the row without expanding it,
// with Retry and the way into the endpoint's own edit. The ladder's chips are
// buttons (each teaches its rung), so they sit beside the row's expand toggle,
// never inside it.

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
// hint. A single-value strip (or none) still reads correctly. The bar itself is the
// shared StateStrip primitive, the same one the health history draws.
function AvailabilityStrip(p: { iface: ReachEndpoint }) {
  const now = createClock(CLOCK_TICK_MS);
  const segs = createMemo(() => segments(p.iface.history, p.iface.verdict, now()));
  const up = createMemo(() => uptime(p.iface.history, p.iface.verdict, now()));
  return (
    <StateStrip segments={segs()} tone={segClass} title="Availability over the recent window">
      <Show when={up() !== null} fallback={<span class="text-[11px] text-base-content/40">no data</span>}>
        <span class="w-12 shrink-0 text-right text-[11px] tabular-nums text-base-content/60">{up()}% up</span>
      </Show>
    </StateStrip>
  );
}

// GateBreakdown lists one line per probe layer (dot + word + timing detail) and
// the "why" reason line for a down endpoint. It is the pedagogical payoff: the
// verdict is the AND of the layers, shown as the layers.
function GateBreakdown(p: { iface: ReachEndpoint }) {
  const now = createClock(CLOCK_TICK_MS);
  const why = createMemo(() => reason(p.iface, now()));
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
        <span class="text-base-content/60">endpoint {verdictOk() ? "up" : "down"}</span>
      </div>
      <Show when={why()}>
        <p class="mt-0.5 rounded-md bg-warning/10 px-2 py-1.5 text-[11.5px] text-base-content/70">{why()}</p>
      </Show>
    </div>
  );
}

function EndpointRow(p: { iface: ReachEndpoint; component: string; manageId?: string; onManage?: (id: string) => void }) {
  const [open, setOpen] = createSignal(false);
  const now = createClock(CLOCK_TICK_MS);
  const word = createMemo(() => endpointWord(p.iface, now()));
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
            {/* The label over the derived name, through the one renderer. This
                panel is where two endpoints of one transport sit beside each
                other, so it is where the label earns its place (#613). */}
            <span class="truncate text-sm">{entityLabel({ name: p.iface.endpoint, label: p.iface.label })}</span>
            <span class="truncate font-data text-[11px] text-base-content/50">
              {p.iface.transport}
              <Show when={p.iface.address}> · {p.iface.address}</Show>
            </span>
          </div>
          <div class="min-w-0 flex-1">
            <AvailabilityStrip iface={p.iface} />
          </div>
        </button>
        {/* The ladder (#1059): its rungs teach themselves on hover, so they
            are buttons of their own beside the toggle, never inside it. The
            session pill, then the verdict pill, close the row. */}
        <div class="flex shrink-0 items-center gap-2 pr-3">
          <ReachLadder iface={p.iface} />
          <SessionPill iface={p.iface} now={now()} />
          <span class={`badge badge-soft badge-sm shrink-0 ${pill().cls}`}>{pill().label}</span>
        </div>
        <Show when={canManage()}>
          <button
            type="button"
            class="flex shrink-0 items-center px-2.5 text-base-content/40 hover:bg-base-content/5 hover:text-base-content"
            title="Manage endpoint"
            aria-label={`Manage ${entityLabel({ name: p.iface.endpoint, label: p.iface.label })}`}
            onClick={() => p.onManage!(p.manageId!)}
          >
            <Sliders size={15} />
          </button>
        </Show>
      </div>
      <Show when={p.iface.session}>
        <div class="px-3 pb-2.5 empty:hidden">
          <SessionNotice
            iface={p.iface}
            component={p.component}
            now={now()}
            onFix={canManage() ? () => p.onManage!(p.manageId!) : undefined}
            fixLabel="Open endpoint"
          />
        </div>
      </Show>
      <Show when={open()}>
        <GateBreakdown iface={p.iface} />
        <Show when={p.iface.node}>
          <div class="border-t border-base-300 bg-base-200/40 px-3 py-1.5 text-[11px] text-base-content/45">
            probed by <span class="font-data text-base-content/60">{p.iface.node}</span>
            <Show when={lastSeenAt(p.iface)}>
              {(ts) => <>{" "}· last checked {rel(ts(), now())}</>}
            </Show>
          </div>
        </Show>
      </Show>
    </div>
  );
}

export default function ReachabilityPanel(p: {
  // The owning component's address (#627: the console addresses a component
  // by uuid now, since its name is scoped to placement and not reliably
  // unique; getReachability dual-accepts either, ADR-0062).
  name: string;
  // Present -> the panel header shows "Add endpoint", opening a create surface for
  // this component (the caller gates on endpoint:create).
  onAdd?: () => void;
  // Present -> each row that maps to a known endpoint shows "Manage", opening that
  // endpoint's detail blade by id (the caller gates on endpoint:read).
  onOpenEndpoint?: (id: string) => void;
}) {
  const q = useQuery(() => ({ queryKey: REACHABILITY_KEY(p.name), queryFn: () => getReachability(p.name) }));
  const ifaces = createMemo(() => q.data?.endpoints ?? []);
  // Rows are kept by endpoint id, never by the objects a read hands back: the
  // component's view polls this read and every poll returns fresh objects,
  // which would rebuild each row, closing an expanded one and dropping a retry
  // that waits on its session (#1059).
  const keyOf = (it: ReachEndpoint, i: number) => it.id ?? `${it.endpoint}#${i}`;
  const keys = createMemo(() => ifaces().map(keyOf), undefined, {
    equals: (a, b) => a.length === b.length && a.every((x, i) => x === b[i]),
  });
  const byKey = createMemo(() => new Map(ifaces().map((it, i) => [keyOf(it, i), it])));
  // Only load the endpoint list when the caller can open endpoint details; map an
  // endpoint's name (the reachability row key) to its surrogate id so a row can
  // open its detail blade. Matched by component_id, not the component name
  // (#627): p.name is now the component's own uuid, and two components can
  // legally share a name, so a name match could pull in a sibling's rows.
  const endpoints = useQuery(() => ({ queryKey: ENDPOINTS_KEY, queryFn: () => listEndpoints(), enabled: !!p.onOpenEndpoint }));
  const idByName = createMemo(() => {
    const m = new Map<string, string>();
    for (const it of endpoints.data ?? []) if (it.component_id === p.name) m.set(it.name, it.id);
    return m;
  });
  return (
    <div class="flex flex-col gap-1.5">
      <div class="flex items-center gap-2">
        <span class="eyebrow">Endpoints</span>
        <Show when={ifaces().length}>
          <span class="text-[11px] text-base-content/40">
            {ifaces().length} endpoint{ifaces().length === 1 ? "" : "s"}
          </span>
        </Show>
        <span class="flex-1" />
        <Show when={p.onAdd}>
          <Button intent="quiet" icon={Plus} onClick={() => p.onAdd!()}>Add endpoint</Button>
        </Show>
      </div>
      <Show
        when={ifaces().length}
        fallback={
          <div class="rounded-box border border-dashed border-base-300 px-3 py-4 text-center text-[12px] text-base-content/45">
            <Show when={q.isLoading} fallback="No endpoints on this component yet.">
              Loading endpoints…
            </Show>
          </div>
        }
      >
        <div class="divide-y divide-base-300 overflow-hidden rounded-box border border-base-300">
          <For each={keys()}>
            {(key) => (
              <Show when={byKey().get(key)}>
                {(iface) => <EndpointRow iface={iface()} component={p.name} manageId={idByName().get(iface().endpoint)} onManage={p.onOpenEndpoint} />}
              </Show>
            )}
          </For>
        </div>
      </Show>
    </div>
  );
}
