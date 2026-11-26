import { For, Show, createEffect, createMemo, createSignal, on as onChange, type JSX, type Signal } from "solid-js";
import { A } from "@solidjs/router";
import { useQuery } from "@tanstack/solid-query";
import InfoTip from "../components/InfoTip";
import KeyedFor from "../components/KeyedFor";
import Pager from "../components/Pager";
import PlatformSection from "../components/PlatformSection";
import { deviceStateText, devicePlaceText, filterDevices, pageSlice, simulationSummary } from "../lib/platform_sections";
import { can, useMe } from "../lib/auth";
import { describeError, fmtTime } from "../lib/format";
import { LEAF_POLL_MS } from "../lib/component_leaf";
import { SIMULATION_KEY, getSimulation, sidebarPollMs, simulatorConnected } from "../lib/simulation";
import {
  PLATFORM_SIM_KEY,
  deviceReceived,
  gapName,
  gapProtocolText,
  gapTone,
  platformSimulation,
  ratioText,
  retryPlatformView,
  simStateNote,
  sortDevices,
  sortGaps,
  windowText,
  type GapTone,
  type PlatformSimGap,
  type PlatformSimOutpost,
} from "../lib/platformhealth";
import Tooltip from "../components/ui/Tooltip";

// Platform health's simulation section (#1647): with a simulator connected,
// what each device saw against what the platform collected. The simulator is
// the one witness outside the platform, so it is the one place the page can
// count a loss rather than infer one: each gap is a hop a request or a batch
// travels, read as a loss ratio over a sliding window the server brackets each
// minute, a lab faster (internal/closedloop), 0 healthy.
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

export default function PlatformSimulation(props: { open: boolean; onOpenChange: (open: boolean) => void }) {
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
  return (
    <Show when={on()}>
      <div data-testid="platform-simulation">
        <PlatformSection
          testid="section-simulation"
          label="With a simulator"
          summary={simulationSummary(sim.error ? undefined : sim.data?.outposts)}
          open={props.open}
          onOpenChange={props.onOpenChange}
        >
          <div class="flex flex-col gap-4">
            <div class="flex items-center gap-1.5 text-xs text-base-content/60">
              <span>What each device saw against what the platform collected, over the last {windowText(sim.data?.window_seconds)}.</span>
              <InfoTip
                label="the simulation section"
                text="The simulator counts what each device saw; the platform counts what it collected. Each gap is one hop a request or a batch travels, read as a loss ratio: what was lost at the hop over what went into it, 0 healthy. An impaired device loses traffic on purpose, so its gap reads as expected, not as a fault."
              />
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
                <KeyedFor each={v().outposts} by={(n) => n.outpost} fallback={<div class="text-sm text-base-content/50">No outpost has a simulator checking in yet.</div>}>
                  {(n) => <SimOutpost outpost={n()} />}
                </KeyedFor>
              )}
            </Show>
          </div>
        </PlatformSection>
      </div>
    </Show>
  );
}

function SimOutpost(props: { outpost: PlatformSimOutpost }) {
  const note = () => simStateNote(props.outpost);
  // The device table's filter and page live here, above the window's state: a
  // window that flaps to warming up or restarted unmounts the table, and the
  // operator's filter must be there when the figures come back.
  const filter = createSignal("");
  const paging = createSignal(0);
  return (
    <div class="card border border-base-300 bg-base-100" data-testid={`platform-sim-${props.outpost.outpost}`} data-state={props.outpost.state}>
      <div class="card-body gap-4 p-5">
        <div class="flex flex-wrap items-baseline justify-between gap-2">
          <div class="flex items-baseline gap-2">
            <span class="font-semibold">{props.outpost.outpost}</span>
            <span class="badge badge-sm bg-base-content/10 text-base-content/70 border-transparent" data-testid="platform-sim-state">{props.outpost.state}</span>
          </div>
          <Show when={props.outpost.start && props.outpost.end}>
            <span class="text-xs text-base-content/50">{fmtTime(props.outpost.start!)} to {fmtTime(props.outpost.end!)}</span>
          </Show>
        </div>
        <Show when={props.outpost.error}>
          <div role="alert" class="alert alert-soft alert-error text-sm">The last bracket failed: {props.outpost.error}</div>
        </Show>
        <Show when={note()}>
          {(t) => <div role="note" class="text-sm text-base-content/60" data-testid="platform-sim-note">{t()}</div>}
        </Show>
        <div class="grid grid-cols-[repeat(auto-fit,minmax(170px,1fr))] gap-3">
          <Figure label="Batches landed" tip="Telemetry receipts the server landed for this outpost over the window, counted by landed_at, with the margin before it and the bracket after it." value={note() ? null : String(props.outpost.receipts.landed)} />
          <Figure label="Sim in flight, high" tip="The most requests the outpost's simulator held at once over every protocol: the simulator's own count." value={note() ? null : String(props.outpost.in_flight_high)} />
        </div>
        <Show when={!note()}>
          <GapTable gaps={props.outpost.gaps} />
          <ProtocolTable outpost={props.outpost} />
          <DeviceTable outpost={props.outpost} query={filter} page={paging} />
        </Show>
      </div>
    </div>
  );
}

// Figure is one small figure with its source in an (i) tooltip (ADR-0127). A
// null value is a window with no figures yet: no number, a dash.
function Figure(props: { label: string; tip: string; value: string | null }) {
  return (
    <div class="flex flex-col gap-0.5 rounded-box bg-base-200 px-3 py-2">
      <div class="flex items-center gap-1 text-xs text-base-content/60">
        {props.label}
        <InfoTip text={props.tip} label={props.label} />
      </div>
      <div
        class="tnum text-lg font-semibold"
        classList={{ "text-base-content/30": props.value == null }}
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
                <InfoTip text="Lost at the hop over what went into it, from the simulator's and the outpost's counters at the window's two ends. 0 is healthy." label="Loss ratio" />
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
                  <td classList={{ "text-base-content/40": !g.protocol }}>{gapProtocolText(g)}</td>
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

function ProtocolTable(props: { outpost: PlatformSimOutpost }) {
  return (
    <div class="overflow-x-auto rounded-box border border-base-300">
      <table class="table table-sm" data-testid="platform-sim-protocols">
        <thead>
          <tr>
            <th>Protocol</th>
            <Head label="Accepted" tip="Connections the simulator's listeners on the protocol accepted over the window; 0 for a connectionless protocol. A probe that only connects, or logs in and asks nothing, draws a connection and no request." />
            <Head label="Received" tip="Requests the simulator received on the protocol over the window: the simulator's count, not the outpost's." />
            <Head label="Answered" tip="Requests the simulator answered over the window." />
            <Head label="p95 service" tip="The simulator's own service time, 95th percentile, in milliseconds; a dash when it timed nothing." />
            <Head label="Poll targets" tip="Enabled poll targets on the protocol, from the server's task table, each due at least twice over the window." />
            <Head label="In flight, high" tip="The most requests on the protocol the simulator held at once." />
          </tr>
        </thead>
        <tbody>
          <For each={props.outpost.protocols ?? []} fallback={<tr><td colSpan={7} class="text-base-content/50">No protocol drew a connection or a request over the window.</td></tr>}>
            {(p) => (
              <tr data-testid="platform-sim-protocol" data-protocol={p.protocol} data-accepted={p.accepted} data-received={p.received} data-poll-targets={p.poll_targets}>
                <td>{p.protocol}</td>
                <td class="tnum text-right">{p.accepted}</td>
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

// PAGE is how many device rows the table renders at once: the simulator can
// serve thousands, so the table is a page of them behind a filter, never all.
const DEVICE_PAGE = 25;

function DeviceTable(props: { outpost: PlatformSimOutpost; query: Signal<string>; page: Signal<number> }) {
  const [query, setQuery] = props.query;
  const [page, setPage] = props.page;
  const all = createMemo(() => sortDevices(props.outpost.devices));
  const matched = createMemo(() => filterDevices(all(), query()));
  const rows = createMemo(() => pageSlice(matched(), page(), DEVICE_PAGE));
  createEffect(onChange(query, () => setPage(0), { defer: true }));
  return (
    <div class="flex flex-col gap-2">
      <div class="flex flex-wrap items-center justify-between gap-2">
        <input
          type="search"
          class="input input-bordered input-sm w-full max-w-xs"
          placeholder="filter by device, system or location"
          aria-label={`Filter ${props.outpost.outpost}'s devices by name, system or location`}
          value={query()}
          onInput={(e) => setQuery(e.currentTarget.value)}
          data-testid="platform-sim-device-filter"
        />
        <Pager total={matched().length} page={page()} size={DEVICE_PAGE} onPage={setPage} label={`${props.outpost.outpost} devices`} />
      </div>
      <div class="overflow-x-auto rounded-box border border-base-300">
        <table class="table table-sm" data-testid="platform-sim-devices">
          <thead>
            <tr>
              <th>Device</th>
              <th>System and location</th>
              <Head label="Received" tip="Requests the simulator saw for the device over the window, by protocol." />
              <th>State</th>
            </tr>
          </thead>
          <tbody>
            <For
              each={rows()}
              fallback={
                <tr>
                  <td colSpan={4} class="text-base-content/50">
                    {all().length === 0 ? "The simulator serves no device on this outpost." : "No device matches the filter."}
                  </td>
                </tr>
              }
            >
              {(d) => (
                <tr data-testid="platform-sim-device" data-component={d.component_id} data-received={deviceReceived(d)} classList={{ "bg-warning/5": d.unhit }}>
                  <td class="whitespace-nowrap align-middle">
                    <Tooltip as={A} href={`/simulation?c=${encodeURIComponent(d.component_id)}&tab=impairments`} class="font-medium hover:underline" content="Open the device's drawer on the Simulation page">
                      {d.component || d.component_id}
                    </Tooltip>
                  </td>
                  <td class="align-middle text-xs text-base-content/60" data-testid="platform-sim-device-place">{devicePlaceText(d)}</td>
                  <td class="tnum whitespace-nowrap text-right align-middle text-sm">
                    {Object.entries(d.received ?? {}).sort(([a], [b]) => a.localeCompare(b)).map(([p, n]) => `${p} ${n}`).join(", ") || "0"}
                  </td>
                  <td class="align-middle">
                    <div class="flex flex-wrap gap-1" data-testid="platform-sim-device-state">
                      <For each={deviceStateText(d)}>
                        {(t) => <span class={`badge badge-sm ${STATE_BADGE[t]}`}>{t}</span>}
                      </For>
                    </div>
                  </td>
                </tr>
              )}
            </For>
          </tbody>
        </table>
      </div>
    </div>
  );
}

const STATE_BADGE: Record<string, string> = {
  answering: "bg-base-content/5 text-base-content/60 border-transparent",
  impaired: "badge-soft badge-info",
  "due, never seen": "badge-soft badge-warning",
};
