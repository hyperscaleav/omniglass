import { For, Show, createMemo, createSignal } from "solid-js";
import { useQuery } from "@tanstack/solid-query";
import { ChevronRight, Plus, Sliders } from "./icons";
import Button from "./Button";
import StateStrip from "./StateStrip";
import SessionPill from "./SessionPill";
import ReachLadder from "./ReachLadder";
import SessionNotice from "./SessionNotice";
import InfoTip from "./InfoTip";
import WebhookEndpointRow from "./WebhookEndpointRow";
import { rel } from "../lib/format";
import { certTone, daysLeftWord, expiryDay, shortKey, tlsMode, type ReachTLS, type Tone } from "../lib/tls";
import { CLOCK_TICK_MS, createClock } from "../lib/clock";
import { endpointNotice } from "../lib/session";
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
  requestRung,
  requestLayer,
  requestHeld,
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
// with Retry and the way into the endpoint's own edit. A refusal the
// reachability probe met on an endpoint holding no session shows the same
// notice (#1110): the gate, not the session, is what it keys on. The ladder's chips are
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

// TONE_DOT is the dot a tls row's tone wears, the layer rows' own dots.
const TONE_DOT: Record<Tone, string> = {
  ok: "bg-success",
  warn: "bg-warning",
  bad: "bg-error",
  unknown: "border border-base-content/35",
};

// TlsRows are a tls endpoint's lines in the breakdown (#1035), in the layer
// rows' idiom: the handshake the transport rung ran and its time, how long the
// certificate has, and how the endpoint verifies it, each teaching itself on
// hover. The pinned key rides its row in full on the title.
function TlsRows(p: { tls: ReachTLS; responsive?: boolean; refused: boolean }) {
  const mode = createMemo(() => tlsMode(p.tls.mode));
  const days = () => p.tls.days_left ?? undefined;
  // Under a certificate gate the read withholds the last accepted
  // certificate's facts (the probe connects only), so the row names the
  // refusal instead of reading as nothing accepted yet.
  const tone = (): Tone => (p.refused ? "bad" : certTone(days()));
  return (
    <>
      {/* The row follows the latest probe (endpoint-responsive): a handshake
          time is shown only beside an answer, since the stored one may be
          an earlier probe's; beneath a certificate gate no handshake runs. */}
      <div class="flex items-center gap-2">
        <span class={`inline-block size-2 shrink-0 rounded-full ${p.refused || p.responsive === undefined ? TONE_DOT.unknown : p.responsive ? "bg-success" : "bg-error"}`} />
        <span class="w-16 shrink-0 uppercase tracking-wide text-base-content/45 text-[10px]">tls</span>
        <span class="font-data text-base-content/70">handshake</span>
        <span class="text-base-content/50">
          {p.refused ? "not tried beneath the gate" : p.responsive === undefined ? "not answered yet" : p.responsive ? "answered" : "not answered"}
        </span>
        <Show when={!p.refused && p.responsive && p.tls.handshake_ms !== undefined}>
          <span class="text-base-content/40">· {p.tls.handshake_ms!.toFixed(1)} ms</span>
        </Show>
        <InfoTip label="the tls handshake" text="The transport rung for tls: the node opens the port, then runs the TLS handshake and judges the certificate the component offers. A certificate it refuses holds this rung, and nothing above it is tried." />
      </div>
      <div class="flex items-center gap-2" data-testid="tls-cert" data-tone={tone()}>
        <span class={`inline-block size-2 shrink-0 rounded-full ${TONE_DOT[tone()]}`} />
        <span class="w-16 shrink-0 uppercase tracking-wide text-base-content/45 text-[10px]">cert</span>
        <Show
          when={days() !== undefined}
          fallback={
            <span class="text-base-content/50">{p.refused ? "the offered certificate is refused" : "no certificate accepted yet"}</span>
          }
        >
          <span class="text-base-content/70">{daysLeftWord(days()!)}</span>
        </Show>
        <Show when={p.tls.expires}>
          <span class="text-base-content/40">· expires {expiryDay(p.tls.expires!)}</span>
        </Show>
        <InfoTip label="the certificate's days left" text="Days from the latest probe to the certificate's not-after (tls-certificate-days-left), the number a threshold rule reads; the not-after itself is tls-certificate-expiry. Amber inside a month, red once expired." />
      </div>
      <div class="flex items-center gap-2">
        <span class={`inline-block size-2 shrink-0 rounded-full ${TONE_DOT[mode().tone]}`} />
        <span class="w-16 shrink-0 uppercase tracking-wide text-base-content/45 text-[10px]">trust</span>
        <span class={mode().tone === "warn" ? "text-warning" : "text-base-content/70"}>{mode().word}</span>
        <Show when={p.tls.key}>
          <span class="font-data text-base-content/40" title={p.tls.key}>· {shortKey(p.tls.key!)}</span>
        </Show>
        <InfoTip label="how the endpoint verifies" text={mode().hint} />
      </div>
    </>
  );
}

// GateBreakdown lists one line per probe layer (dot + word + timing detail) and
// the "why" reason line for a down endpoint. It is the pedagogical payoff: the
// verdict is the AND of the layers, shown as the layers.
function GateBreakdown(p: { iface: ReachEndpoint }) {
  const now = createClock(CLOCK_TICK_MS);
  const why = createMemo(() => reason(p.iface, now()));
  const verdictOk = createMemo(() => p.iface.verdict?.value === "up");
  // A request rung a refusal holds: the component answered with a refusal
  // retrying cannot fix, so beneath the gate nothing is sent (#1037, #821).
  const requestGated = () => requestHeld(p.iface);
  return (
    <div class="flex flex-col gap-1.5 border-t border-base-300 bg-base-200/40 px-3 py-2.5 text-[12px]">
      {/* The request layer (udp) is drawn as its own row below, beside the
          gate it is, not as a probe layer. */}
      <For each={p.iface.layers.filter((l) => l.layer !== "request")}>
        {(l) => {
          const up = l.value >= 1;
          return (
            <div class="flex items-center gap-2">
              <span class={`inline-block size-2 shrink-0 rounded-full ${up ? "bg-success" : "bg-error"}`} />
              <span class="w-16 shrink-0 uppercase tracking-wide text-base-content/45 text-[10px]">{l.layer}</span>
              <span class="font-data text-base-content/70">{l.check}</span>
              <span class="text-base-content/50">{layerWord(l)}</span>
              <Show when={l.detail}>
                <span class="text-base-content/40">· {l.detail}</span>
              </Show>
            </div>
          );
        }}
      </For>
      <Show when={requestRung(p.iface) !== undefined}>
        {/* The transport rung of an endpoint with no port to open (udp):
            the driver's request answered, the layer its verdict follows. */}
        <div class="flex items-center gap-2" data-testid="request-rung">
          <span class={`inline-block size-2 shrink-0 rounded-full ${requestGated() ? TONE_DOT.unknown : requestRung(p.iface) ? "bg-success" : "bg-error"}`} />
          <span class="w-16 shrink-0 uppercase tracking-wide text-base-content/45 text-[10px]">rung</span>
          <span class="font-data text-base-content/70">{p.iface.transport === "snmp" ? "get" : "request"}</span>
          <span class="text-base-content/50">{requestGated() ? "refused, not sent beneath the gate" : requestRung(p.iface) ? "answered" : "not answered"}</span>
          <Show when={!requestGated() && requestRung(p.iface) && requestLayer(p.iface)?.detail}>
            <span class="text-base-content/40">· {requestLayer(p.iface)!.detail}</span>
          </Show>
          <InfoTip
            label="the request rung"
            text="The transport rung where an open port cannot be detected (udp, snmp): the node sends the driver's named request (for snmp, a get of a standard OID with the endpoint's community) and an answer proves the protocol. Silence reads not answered (timed out), and a port the host reports closed reads refused; a v2c agent drops a wrong community silently, so it reads not answered too. With no port to open, this rung is the endpoint's verdict."
          />
        </div>
      </Show>
      <Show when={p.iface.tls}>
        {(tls) => (
          <TlsRows
            tls={tls()}
            responsive={p.iface.responsive ? p.iface.responsive.value === "up" : undefined}
            refused={p.iface.rejection?.class === "certificate"}
          />
        )}
      </Show>
      <div class="flex items-center gap-2">
        <span class={`inline-block size-2 shrink-0 rounded-full ${verdictOk() ? "bg-success" : "bg-error"}`} />
        <span class="w-16 shrink-0 uppercase tracking-wide text-base-content/45 text-[10px]">verdict</span>
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
            session pill, then the verdict pill, close the row. */}
        <div class="flex shrink-0 items-center gap-2 pr-3">
          {/* Fixed slots, so the rungs, the pills and their ages line up down
              the panel whatever each row holds (a tcp row has no login rung,
              a probe endpoint no session). */}
          <div class="w-72"><ReachLadder iface={p.iface} /></div>
          <div class="w-36"><SessionPill iface={p.iface} now={now()} /></div>
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
