import { For, Show, createMemo, createSignal } from "solid-js";
import { useQuery } from "@tanstack/solid-query";
import { A } from "@solidjs/router";
import { ChevronRight, Plus, Sliders } from "./icons";
import Button from "./Button";
import SessionPill from "./SessionPill";
import ReachLadder from "./ReachLadder";
import SessionNotice from "./SessionNotice";
import WebhookEndpointRow from "./WebhookEndpointRow";
import EndpointDetail from "./EndpointDetail";
import { AvailabilityStrip, PILL } from "./EndpointGate";
import { CLOCK_TICK_MS, createClock } from "../lib/clock";
import { endpointNotice, silentDatagram, NO_ANSWER_CAUSES } from "../lib/session";
import { entityLabel } from "../lib/entities";
import { ENDPOINTS_KEY, listEndpoints } from "../lib/endpoints";
import { REACHABILITY_KEY, getReachability, endpointWord, pendingNotice, type ReachEndpoint } from "../lib/reachability";

// The Endpoints panel on the component detail: an endpoint belongs to its
// component, so it surfaces here (not a top-level tab). One row per endpoint (an
// endpoint is usable or not): the endpoint, an availability strip built from the
// state's transition history, and a 4-state state pill derived at read time.
// Expanding a row reveals the layered-gate breakdown (ping L3, port L4) plus a
// "why" reason line when the endpoint is down. The state/strip/reason are
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
// with Retry and the way into the endpoint's own edit. A refusal the
// reachability probe met on an endpoint holding no session shows the same
// notice (#1110): the gate, not the session, is what it keys on. The ladder's chips are
// buttons (each teaches its rung), so they sit beside the row's expand toggle,
// never inside it.

function EndpointRow(p: { iface: ReachEndpoint; component: string; manageId?: string; onManage?: (id: string) => void }) {
  const [open, setOpen] = createSignal(false);
  const now = createClock(CLOCK_TICK_MS);
  const word = createMemo(() => endpointWord(p.iface, now()));
  const pill = createMemo(() => (word() === "down" && silentDatagram(p.iface) ? { cls: "badge-warning", label: "no answer" } : PILL[word()]));
  const canManage = () => !!(p.manageId && p.onManage);
  return (
    <div class="overflow-hidden">
      {/* The hover is the row's, not the toggle's: the ladder and pills sit
          beside the toggle, so a hover on the toggle alone lit half the row. */}
      <div class="flex items-stretch hover:bg-base-content/5">
        <button
          type="button"
          class="flex min-w-0 flex-1 items-center gap-3 px-3 py-2.5 text-left"
          onClick={() => setOpen((v) => !v)}
          aria-expanded={open()}
        >
          <span class={`shrink-0 text-base-content/40 transition-transform ${open() ? "rotate-90" : ""}`}><ChevronRight size={14} /></span>
          <div class="flex w-52 shrink-0 flex-col gap-0.5">
            {/* The label over the derived name, through the one renderer. This
                panel is where two endpoints of one transport sit beside each
                other, so it is where the label earns its place (#613). */}
            <span class="flex min-w-0 items-center gap-1.5">
              <span class="truncate text-sm">{entityLabel({ name: p.iface.endpoint, label: p.iface.label })}</span>
              {/* A skipped verification is the operator's explicit choice,
                  and it stays visible on the row (#1035), beside the label so
                  the address keeps its whole width. */}
              <Show when={p.iface.tls?.mode === "skip"}>
                <span class="badge badge-soft badge-warning badge-xs shrink-0" title="TLS verification is skipped: anything answering at this address is trusted">unverified</span>
              </Show>
            </span>
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
            session pill, then the state pill, close the row. */}
        <div class="flex shrink-0 items-center gap-2 pr-3">
          {/* Fixed slots, so the rungs, the pills and their ages line up down
              the panel whatever each row holds (a tcp row has no login rung,
              a probe endpoint no session). */}
          <div class="w-72"><ReachLadder iface={p.iface} /></div>
          <div class="w-36"><SessionPill iface={p.iface} now={now()} /></div>
          <span class={`badge badge-soft badge-sm shrink-0 ${pill().cls}`} title={pill().label === "no answer" ? NO_ANSWER_CAUSES : undefined}>{pill().label}</span>
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
      {/* The notice beneath the row: a refusal, whatever met it (the
          reachability probe, a poll, a held session, #1110), or a down
          session. A row with nothing to say carries no slot. */}
      <Show when={endpointNotice(p.iface, now())}>
        <div data-testid="endpoint-notice-slot" class="px-3 pb-2.5">
          <SessionNotice
            iface={p.iface}
            component={p.component}
            now={now()}
            onFix={canManage() ? () => p.onManage!(p.manageId!) : undefined}
            fixLabel="Open endpoint"
          />
        </div>
      </Show>
      {/* No state yet (#981): the reason, without expanding the row, and
          the way to fix it. */}
      <Show when={pendingNotice(p.iface)}>
        {(n) => (
          <div data-testid="endpoint-pending-notice" class="px-3 pb-2.5 text-[12px] text-base-content/60">
            {n().text}{" "}
            <A href={n().href} class="link link-primary whitespace-nowrap font-medium">{n().linkLabel}</A>
          </div>
        )}
      </Show>
      {/* The expanded row is the endpoint's whole story, the very body its
          drawer shows (#2436). */}
      <Show when={open() && p.iface.id}>
        <div class="border-t border-base-300 bg-base-200/40 px-3 py-3">
          <EndpointDetail id={p.iface.id!} host="row" />
        </div>
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
                {(iface) => (
                  // A push endpoint (#1024) has no ladder to draw: its own row.
                  <Show
                    when={iface().webhook}
                    fallback={<EndpointRow iface={iface()} component={p.name} manageId={idByName().get(iface().endpoint)} onManage={p.onOpenEndpoint} />}
                  >
                    <WebhookEndpointRow iface={iface()} manageId={idByName().get(iface().endpoint)} onManage={p.onOpenEndpoint} />
                  </Show>
                )}
              </Show>
            )}
          </For>
        </div>
      </Show>
    </div>
  );
}
