import { For, Show, createMemo } from "solid-js";
import StateStrip from "./StateStrip";
import InfoTip from "./InfoTip";
import { rel } from "../lib/format";
import { certTone, daysLeftWord, expiryDay, shortKey, tlsMode, type ReachTLS, type Tone } from "../lib/tls";
import { CLOCK_TICK_MS, createClock } from "../lib/clock";
import { type ReachWord, segments, uptime, reason, layerUp, layerWord, requestRung, requestLayer, requestHeld, type ReachEndpoint } from "../lib/reachability";

// The availability strip and the layered-gate breakdown of an endpoint: the
// parts of the endpoint's story that are drawn from its reachability read.
// They live here, not in the Endpoints panel, because the one EndpointDetail
// body (#2436) mounts them in both of its hosts.

// The pill hue and label per derived state word.
export const PILL: Record<ReachWord, { cls: string; label: string }> = {
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
export function AvailabilityStrip(p: { iface: ReachEndpoint }) {
  const now = createClock(CLOCK_TICK_MS);
  const segs = createMemo(() => segments(p.iface.history, p.iface.reachability, now()));
  const up = createMemo(() => uptime(p.iface.history, p.iface.reachability, now()));
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
        <InfoTip label="the tls handshake" text="The transport rung for tls: the outpost opens the port, then runs the TLS handshake and judges the certificate the component offers. A certificate it refuses holds this rung, and nothing above it is tried." />
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
// state is the AND of the layers, shown as the layers.
export function GateBreakdown(p: { iface: ReachEndpoint }) {
  const now = createClock(CLOCK_TICK_MS);
  const why = createMemo(() => reason(p.iface, now()));
  const reachable = createMemo(() => p.iface.reachability?.value === "up");
  // A request rung a refusal holds: the component answered with a refusal
  // retrying cannot fix, so beneath the gate nothing is sent (#1037, #821).
  const requestGated = () => requestHeld(p.iface);
  return (
    <div class="flex flex-col gap-1.5 text-[12px]">
      {/* The request layer (udp) is drawn as its own row below, beside the
          gate it is, not as a probe layer. */}
      <For each={p.iface.layers.filter((l) => l.layer !== "request")}>
        {(l) => {
          const up = layerUp(l);
          return (
            <div class="flex items-center gap-2">
              <span class={`inline-block size-2 shrink-0 rounded-full ${up ? "bg-success" : "bg-error"}`} />
              <span class="w-16 shrink-0 uppercase tracking-wide text-base-content/45 text-[10px]">{l.layer}</span>
              <span class="font-data text-base-content/70">{l.check}</span>
              <span class="text-base-content/50">{layerWord(l)}</span>
              <Show when={l.detail}>
                <span class="text-base-content/40">· {l.detail}</span>
              </Show>
              <Show when={l.ts}>
                <span class="text-base-content/40" title={l.ts}>· {rel(l.ts, now())}</span>
              </Show>
            </div>
          );
        }}
      </For>
      <Show when={requestRung(p.iface) !== undefined}>
        {/* The transport rung of an endpoint with no port to open (udp):
            the driver's request answered, the layer its state follows. */}
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
            text="The transport rung where an open port cannot be detected (udp, snmp): the outpost sends the driver's named request (for snmp, a get of a standard OID with the endpoint's community) and an answer proves the protocol. Silence reads not answered (timed out), and a port the host reports closed reads refused; a v2c agent drops a wrong community silently, so it reads not answered too. With no port to open, this rung is the endpoint's state."
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
        <span class={`inline-block size-2 shrink-0 rounded-full ${reachable() ? "bg-success" : "bg-error"}`} />
        <span class="min-w-16 shrink-0 uppercase tracking-wide text-base-content/45 text-[10px]">reachability</span>
        <span class="text-base-content/60">endpoint {reachable() ? "up" : "down"}</span>
      </div>
      <Show when={why()}>
        <p class="mt-0.5 rounded-md bg-warning/10 px-2 py-1.5 text-[11.5px] text-base-content/70">{why()}</p>
      </Show>
    </div>
  );
}

