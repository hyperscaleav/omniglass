import { For, Show, type JSX } from "solid-js";
import { A } from "@solidjs/router";
import { useQuery } from "@tanstack/solid-query";
import InfoTip from "../components/InfoTip";
import { can, useMe } from "../lib/auth";
import { describeError, fmtTime } from "../lib/format";
import { LEAF_POLL_MS } from "../lib/component_leaf";
import { SIMULATION_KEY, getSimulation, sidebarPollMs, simulatorConnected } from "../lib/simulation";
import {
  PLATFORM_SIM_KEY,
  deviceReceived,
  gapName,
  gapTone,
  platformSimulation,
  ratioText,
  retryPlatformView,
  simStateNote,
  sortDevices,
  sortGaps,
  wiredText,
  type GapTone,
  type PlatformSimGap,
  type PlatformSimNode,
} from "../lib/platformhealth";

// Platform health's simulation section (#1647): with a simulator connected,
// what each device saw against what the platform collected. The simulator is
// the one witness outside the platform, so it is the one place the page can
// count a loss rather than infer one: each gap is a hop a request or a batch
// travels, read as a loss ratio over a sliding window the server brackets each
// minute (internal/closedloop), 0 healthy.
//
// The section keys on the nav's "simulator" condition, the same one the
// sidebar's Simulation door waits on, so they appear and disappear together.

const ROW_TONE: Record<GapTone, string> = {
  fault: "bg-error/5",
  expected: "bg-info/5",
  restarted: "bg-base-content/5",
  calm: "",
};

const RATIO_TONE: Record<GapTone, string> = {
  fault: "text-error font-semibold",
  expected: "text-info font-semibold",
  restarted: "text-base-content/50",
  calm: "",
};

const READING_BADGE: Record<string, string> = {
  loss: "bg-base-content/10 text-base-content/70 border-transparent",
  impaired: "badge-soft badge-info",
  restarted: "badge-soft badge-warning",
  "not observed by the sim": "bg-base-content/5 text-base-content/50 border-transparent",
};

export default function PlatformSimulation() {
  const me = useMe();
  const fleet = useQuery(() => ({
    queryKey: SIMULATION_KEY,
    queryFn: getSimulation,
    enabled: can(me.data, "simulation", "read"),
    refetchInterval: (q) => sidebarPollMs(q.state.data),
    retry: false,
  }));
  const on = () => simulatorConnected(me.data, fleet.data);
  const sim = useQuery(() => ({
    queryKey: PLATFORM_SIM_KEY,
    queryFn: platformSimulation,
    enabled: on(),
    retry: retryPlatformView,
    refetchInterval: LEAF_POLL_MS,
  }));
  const minutes = () => Math.round((sim.data?.window_seconds ?? 600) / 60);
  return (
    <Show when={on()}>
      <section class="flex flex-col gap-4" data-testid="platform-simulation">
        <div class="flex flex-col gap-1">
          <h2 class="text-lg font-semibold">With a simulator</h2>
          <p class="max-w-3xl text-sm text-base-content/60">
            The simulator counts what each device saw; the platform counts what it collected. Each gap below is one hop a request or a
            batch travels, read as a loss ratio over the last {minutes()} minutes: what was lost at the hop over what went into it, 0 healthy.
            An impaired device loses traffic on purpose, so its gap reads as expected, not as a fault.
          </p>
        </div>
        <Show when={sim.error}>
          <div role="alert" class="alert alert-soft alert-warning text-sm">
            {(sim.error as { status?: number }).status === 404
              ? "The simulation read needs a fleet-wide component read; yours reaches only part of the fleet."
              : describeError(sim.error)}
          </div>
        </Show>
        <Show when={sim.data}>
          {(v) => (
            <For each={v().nodes} fallback={<div class="text-sm text-base-content/50">No node has a simulator checking in yet.</div>}>
              {(n) => <SimNode node={n} />}
            </For>
          )}
        </Show>
      </section>
    </Show>
  );
}

function SimNode(props: { node: PlatformSimNode }) {
  const note = () => simStateNote(props.node);
  return (
    <div class="card border border-base-300 bg-base-100" data-testid={`platform-sim-${props.node.node}`} data-state={props.node.state}>
      <div class="card-body gap-4 p-5">
        <div class="flex flex-wrap items-baseline justify-between gap-2">
          <div class="flex items-baseline gap-2">
            <span class="font-semibold">{props.node.node}</span>
            <span class="badge badge-sm bg-base-content/10 text-base-content/70 border-transparent" data-testid="platform-sim-state">{props.node.state}</span>
          </div>
          <Show when={props.node.start && props.node.end}>
            <span class="text-xs text-base-content/50">{fmtTime(props.node.start!)} to {fmtTime(props.node.end!)}</span>
          </Show>
        </div>
        <Show when={props.node.error}>
          <div role="alert" class="alert alert-soft alert-error text-sm">The last bracket failed: {props.node.error}</div>
        </Show>
        <Show when={note()}>
          {(t) => <div role="note" class="text-sm text-base-content/60" data-testid="platform-sim-note">{t()}</div>}
        </Show>
        <div class="grid grid-cols-[repeat(auto-fit,minmax(170px,1fr))] gap-3">
          <Figure label="Batches landed" tip="Telemetry receipts the server landed for this node over the window, counted by landed_at, with the margin before it and the bracket after it." value={note() ? null : String(props.node.receipts.landed)} />
          <Figure label="Sim in flight, high" tip="The most requests the node's simulator held at once over every protocol: the simulator's own count." value={note() ? null : String(props.node.in_flight_high)} />
          <Figure label="Queue depth" tip="The node's dispatch queue depth. The node does not report it to the server yet." value={wiredText(props.node.queue_depth)} muted={props.node.queue_depth == null} />
          <Figure label="Busy workers" tip="The node's busy-worker high water. The node does not report it to the server yet." value={wiredText(props.node.busy_workers)} muted={props.node.busy_workers == null} />
        </div>
        <Show when={!note()}>
          <GapTable gaps={props.node.gaps} />
          <ProtocolTable node={props.node} />
          <DeviceTable node={props.node} />
        </Show>
      </div>
    </div>
  );
}

// Figure is one small figure with its source in an (i) tooltip (ADR-0127). A
// null value is a window with no figures yet: no number, a dash.
function Figure(props: { label: string; tip: string; value: string | null; muted?: boolean }) {
  return (
    <div class="flex flex-col gap-0.5 rounded-box bg-base-200 px-3 py-2">
      <div class="flex items-center gap-1 text-xs text-base-content/60">
        {props.label}
        <InfoTip text={props.tip} label={props.label} />
      </div>
      <div
        class="tnum text-lg font-semibold"
        classList={{ "text-base-content/30": props.value == null || props.muted }}
        data-figure={props.label}
      >
        {props.value ?? "-"}
      </div>
    </div>
  );
}

function GapTable(props: { gaps: PlatformSimGap[] | null }) {
  return (
    <div class="overflow-x-auto rounded-box border border-base-300">
      <table class="table table-sm" data-testid="platform-sim-gaps">
        <thead>
          <tr>
            <th>Gap</th>
            <th>Protocol</th>
            <th class="text-right">
              <span class="inline-flex items-center gap-1">
                Loss ratio
                <InfoTip text="Lost at the hop over what went into it, from the simulator's and the node's counters at the window's two ends. 0 is healthy." label="Loss ratio" />
              </span>
            </th>
            <th class="text-right">Lost / in</th>
            <th>Reading</th>
            <th>Masked by</th>
          </tr>
        </thead>
        <tbody>
          <For each={sortGaps(props.gaps)}>
            {(g) => {
              const tone = gapTone(g);
              return (
                <tr class={ROW_TONE[tone]} data-testid="platform-sim-gap" data-gap={g.gap} data-protocol={g.protocol} data-tone={tone} data-ratio={g.ratio ?? ""}>
                  <td class="whitespace-nowrap">{gapName(g.gap)}</td>
                  <td>{g.protocol || <span class="text-base-content/40">batches</span>}</td>
                  <td class={`tnum text-right ${RATIO_TONE[tone]}`}>{ratioText(g.ratio)}</td>
                  <td class="tnum whitespace-nowrap text-right text-base-content/60">{g.count} / {g.in}</td>
                  <td><span class={`badge badge-sm ${READING_BADGE[g.reading] ?? ""}`}>{g.reading}</span></td>
                  <td class="text-xs text-base-content/60">
                    <Show when={g.masked_by} fallback={<span class="text-base-content/30">none</span>}>
                      {g.masked > 0 ? `${g.masked} in ${g.masked_by}` : g.masked_by}
                    </Show>
                  </td>
                </tr>
              );
            }}
          </For>
        </tbody>
      </table>
    </div>
  );
}

function ProtocolTable(props: { node: PlatformSimNode }) {
  return (
    <div class="overflow-x-auto rounded-box border border-base-300">
      <table class="table table-sm" data-testid="platform-sim-protocols">
        <thead>
          <tr>
            <th>Protocol</th>
            <Head label="Received" tip="Requests the simulator received on the protocol over the window: the simulator's count, not the node's." />
            <Head label="Answered" tip="Requests the simulator answered over the window." />
            <Head label="p95 service" tip="The simulator's own service time, 95th percentile, in milliseconds; a dash when it timed nothing." />
            <Head label="Poll targets" tip="Enabled poll targets on the protocol, from the server's task table, each due at least twice over the window." />
            <Head label="In flight, high" tip="The most requests on the protocol the simulator held at once." />
          </tr>
        </thead>
        <tbody>
          <For each={props.node.protocols ?? []} fallback={<tr><td colSpan={6} class="text-base-content/50">No protocol drew a request over the window.</td></tr>}>
            {(p) => (
              <tr data-testid="platform-sim-protocol" data-protocol={p.protocol} data-received={p.received}>
                <td>{p.protocol}</td>
                <td class="tnum text-right">{p.received}</td>
                <td class="tnum text-right">{p.answered}</td>
                <td class="tnum text-right">{p.latency_ms.n > 0 ? `${Math.round(p.latency_ms.p95 * 10) / 10} ms` : "-"}</td>
                <td class="tnum text-right">{p.poll_targets}</td>
                <td class="tnum text-right">{p.in_flight_high}</td>
              </tr>
            )}
          </For>
        </tbody>
      </table>
    </div>
  );
}

function Head(props: { label: string; tip: string }): JSX.Element {
  return (
    <th class="text-right">
      <span class="inline-flex items-center gap-1">
        {props.label}
        <InfoTip text={props.tip} label={props.label} />
      </span>
    </th>
  );
}

function DeviceTable(props: { node: PlatformSimNode }) {
  return (
    <div class="overflow-x-auto rounded-box border border-base-300">
      <table class="table table-sm" data-testid="platform-sim-devices">
        <thead>
          <tr>
            <th>Device</th>
            <Head label="Received" tip="Requests the simulator saw for the device over the window, by protocol." />
            <th>State</th>
          </tr>
        </thead>
        <tbody>
          <For each={sortDevices(props.node.devices)} fallback={<tr><td colSpan={3} class="text-base-content/50">The simulator serves no device on this node.</td></tr>}>
            {(d) => (
              <tr data-testid="platform-sim-device" data-component={d.component_id} data-received={deviceReceived(d)} classList={{ "bg-warning/5": d.unhit }}>
                <td>
                  <A href={`/simulation?c=${encodeURIComponent(d.component_id)}`} class="font-medium hover:underline" title="Open the device's drawer on the Simulation page">
                    {d.name || d.component_id}
                  </A>
                </td>
                <td class="tnum text-right text-sm">
                  {Object.entries(d.received ?? {}).sort(([a], [b]) => a.localeCompare(b)).map(([p, n]) => `${p} ${n}`).join(", ") || "0"}
                </td>
                <td class="flex gap-1">
                  <Show when={d.impaired}><span class="badge badge-sm badge-soft badge-info">impaired</span></Show>
                  <Show when={d.unhit}><span class="badge badge-sm badge-soft badge-warning">due, never seen</span></Show>
                </td>
              </tr>
            )}
          </For>
        </tbody>
      </table>
    </div>
  );
}

