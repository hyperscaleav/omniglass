import { For, Show, createMemo, type JSX } from "solid-js";
import { useQuery } from "@tanstack/solid-query";
import { A } from "@solidjs/router";
import Collapsible from "./ui/Collapsible";
import Eyebrow from "./Eyebrow";
import KVStacked from "./KVStacked";
import SessionPill from "./SessionPill";
import ReachLadder from "./ReachLadder";
import SessionNotice from "./SessionNotice";
import PollTest from "./PollTest";
import { AvailabilityStrip, GateBreakdown, PILL } from "./EndpointGate";
import { ENDPOINTS_KEY, listEndpoints, endpointTarget } from "../lib/endpoints";
import { REACHABILITY_KEY, getReachability, endpointWord, lastSeenAt } from "../lib/reachability";
import { DRIVERS_KEY, listDrivers } from "../lib/drivers";
import { effectiveMetricsKey, effectiveMetrics } from "../lib/component_metrics";
import { effectivePropertiesKey, effectiveProperties } from "../lib/component_properties";
import { drawerHref } from "../lib/drawer_address";
import { LEAF_POLL_MS } from "../lib/component_leaf";
import { CLOCK_TICK_MS, createClock } from "../lib/clock";
import { ladder } from "../lib/session";
import { rel } from "../lib/format";
import { inputRows, paramRows, boundSecrets, pollRows } from "../lib/endpoint_detail";
import Tooltip from "./ui/Tooltip";

// EndpointDetail (#2436): the one body that tells an endpoint's whole story.
// The component page's endpoint row (expanded) and the endpoint drawer both
// mount it, so a drawer never shows less than the page: identity and target,
// the driver and its version, availability, the reach ladder and each rung's
// last result, the session and any gate reasons, parameters and inputs with
// where each came from, the NAMES of the secrets it is bound to, whether it is
// an out-of-band path, and each poll with its last read and a way to run it.
//
// An endpoint has no page, so the drawer is the one place it can be read whole;
// the row host and the blade host differ only in two things the host owns: the
// blade carries the session's notice (the row shows it above, always), and the
// blade can send the operator into the editor. Every field is marked
// data-field=<id> from lib/endpoint_detail's ENDPOINT_FIELDS, which the parity
// test reads in both hosts.

export type EndpointDetailHost = "row" | "blade";

function Section(p: { field: string; label: string; summary?: string; open?: boolean; children: JSX.Element }) {
  return (
    <section data-field={p.field} class="flex flex-col gap-1.5">
      <Collapsible
        defaultOpen={p.open ?? true}
        class="flex flex-col gap-1.5"
        title={
          <span class="flex items-baseline gap-2">
            <span class="eyebrow">{p.label}</span>
            <Show when={p.summary}>
              <span class="text-[11px] font-normal normal-case tracking-normal text-base-content/50">{p.summary}</span>
            </Show>
          </span>
        }
      >
        {p.children}
      </Collapsible>
    </section>
  );
}

const none = (text: string) => <span class="text-[12px] text-base-content/45">{text}</span>;

export default function EndpointDetail(props: {
  id: string;
  host: EndpointDetailHost;
  // The way into the editor, offered by the blade on a refused session.
  onFix?: () => void;
  fixLabel?: string;
}) {
  const now = createClock(CLOCK_TICK_MS);
  const endpoints = useQuery(() => ({ queryKey: ENDPOINTS_KEY, queryFn: () => listEndpoints() }));
  const ep = createMemo(() => endpoints.data?.find((x) => x.id === props.id));
  const drivers = useQuery(() => ({ queryKey: DRIVERS_KEY, queryFn: listDrivers }));
  const driver = createMemo(() => drivers.data?.find((d) => d.name === ep()?.driver));
  const cid = () => ep()?.component_id ?? "";
  const reach = useQuery(() => ({
    queryKey: REACHABILITY_KEY(cid()),
    queryFn: () => getReachability(cid()),
    enabled: !!cid(),
    refetchInterval: LEAF_POLL_MS,
  }));
  const held = createMemo(() => (reach.data?.endpoints ?? []).find((e) => e.id === props.id) ?? (reach.data?.endpoints ?? []).find((e) => !e.id && e.endpoint === ep()?.name));
  const metricsQ = useQuery(() => ({
    queryKey: effectiveMetricsKey(cid()),
    queryFn: () => effectiveMetrics(cid()),
    enabled: !!cid(),
    refetchInterval: LEAF_POLL_MS,
  }));

  const propsQ = useQuery(() => ({
    queryKey: effectivePropertiesKey(cid()),
    queryFn: () => effectiveProperties(cid()),
    enabled: !!cid(),
    refetchInterval: LEAF_POLL_MS,
  }));

  const inputs = createMemo(() => (ep() ? inputRows(ep()!, driver()?.spec) : []));
  const params = createMemo(() => (ep() ? paramRows(ep()!) : []));
  const secrets = createMemo(() => boundSecrets(inputs()));
  const polls = createMemo(() => pollRows(driver()?.spec, ep()?.name ?? "", metricsQ.data ?? [], propsQ.data ?? [], held() ? lastSeenAt(held()!) : null));
  const rungs = createMemo(() => (held() ? ladder(held()!) : []));
  const word = createMemo(() => (held() ? endpointWord(held()!, now()) : "unknown"));

  const names = (rows: { name: string }[]) => rows.map((r) => r.name).join(", ");

  return (
    <div data-testid="endpoint-detail" data-host={props.host} class="flex flex-col gap-4">
      <Show when={ep()} fallback={<p class="text-sm text-base-content/50">This endpoint is no longer available.</p>}>
        {(e) => (
          <>
            <section data-field="identity" class="grid grid-cols-2 gap-4">
              <KVStacked bind="label" value={e().label ? <span>{e().label}</span> : <span class="text-base-content/40">unset</span>} />
              <KVStacked bind="name" value={<span class="font-data">{e().name}</span>} />
              <KVStacked label="Type" value={<span class="badge badge-ghost badge-sm">{e().transport}</span>} />
              <KVStacked label="Component" value={e().component ? <span class="font-data">{e().component}</span> : <span class="text-base-content/40">server-hosted</span>} />
              <KVStacked
                label="Outpost"
                value={
                  e().outpost ? (
                    <span>
                      <span class="font-data">{e().outpost}</span>
                      <Show when={held() && lastSeenAt(held()!)}>
                        {(ts) => <span class="text-[11px] text-base-content/45"> · last checked {rel(ts(), now())}</span>}
                      </Show>
                    </span>
                  ) : (
                    <span class="text-base-content/40">unassigned</span>
                  )
                }
              />
              <KVStacked label="Target" value={endpointTarget(e()) ? <span class="font-data">{endpointTarget(e())}</span> : <span class="text-base-content/40">not set</span>} />
            </section>

            <section data-field="driver" class="flex flex-col gap-1">
              <Eyebrow label="Driver" hint="The driver this endpoint was attached through: it declares the inputs, the polls and the transport. A bare probe endpoint has none." />
              <Show when={e().driver} fallback={none("a bare probe endpoint: no driver")}>
                <span class="flex flex-wrap items-baseline gap-2 text-sm">
                  <A href={drawerHref("driver", e().driver!)} class="link link-primary font-data">{e().driver}</A>
                  <Show when={driver()?.version}>
                    <span class="font-data text-[11px] text-base-content/50">v{driver()!.version}</span>
                  </Show>
                </span>
              </Show>
            </section>

            <section data-field="availability" class="flex flex-col gap-1.5">
              <Eyebrow label="Availability" hint="Up and down over the recent window, drawn from the recorded transitions of the endpoint's reachability. The percentage is the share of that window it was up." />
              <Show when={held()} fallback={none("no reachability read yet")}>
                {(h) => (
                  <div class="flex items-center gap-3">
                    <div class="min-w-0 flex-1"><AvailabilityStrip iface={h()} /></div>
                    <span class={`badge badge-soft badge-sm shrink-0 ${PILL[word()].cls}`}>{PILL[word()].label}</span>
                  </div>
                )}
              </Show>
            </section>

            <Section
              field="ladder"
              label="Reach ladder"
              summary={rungs().length ? `${rungs().filter((r) => r.status === "pass").length} of ${rungs().length} rungs passing` : undefined}
            >
              <Show when={held()} fallback={none("nothing probed yet")}>
                {(h) => (
                  <div class="flex flex-col gap-2">
                    <ReachLadder iface={h()} />
                    <GateBreakdown iface={h()} />
                  </div>
                )}
              </Show>
            </Section>

            <section data-field="session" class="flex flex-col gap-2">
              <Eyebrow
                label="Connection"
                hint="The outpost checks an endpoint bottom up: a ping (informational only), the transport answering (the gate), then the login. A held session's state sits beside the ladder; a gate held for another reason (a reboot, an unreachable probe, an upstream gateway) is listed with when it began."
              />
              <Show when={held()} fallback={none("no session state yet")}>
                {(h) => (
                  <>
                    <div class="flex flex-wrap items-center gap-2">
                      <Show when={h().session} fallback={none("holds no session")}>
                        <SessionPill iface={h()} now={now()} />
                      </Show>
                    </div>
                    <Show when={props.host === "blade"}>
                      <SessionNotice iface={h()} component={e().component_id!} now={now()} onFix={props.onFix} fixLabel={props.fixLabel ?? "Edit endpoint"} />
                    </Show>
                    <Show when={h().gate_reasons?.length}>
                      <ul data-testid="gate-reasons" class="flex flex-col gap-1 text-[12px]">
                        <For each={h().gate_reasons}>
                          {(r) => (
                            <li class="rounded-md bg-warning/10 px-2 py-1.5">
                              <span class="font-data">{r.kind}</span>
                              <Show when={r.component}> <span class="text-base-content/60">via {r.component}</span></Show>
                              <span class="text-base-content/50"> · held since {rel(r.since, now())}</span>
                            </li>
                          )}
                        </For>
                      </ul>
                    </Show>
                  </>
                )}
              </Show>
            </section>

            <Section field="parameters" label="Parameters" summary={params().length ? `${params().length} · ${names(params())}` : "none"}>
              <Show when={params().length} fallback={none("no parameters set")}>
                <dl class="grid grid-cols-[minmax(0,auto)_1fr_auto] gap-x-4 gap-y-1 text-[12px]">
                  <For each={params()}>
                    {(r) => (
                      <>
                        <dt class="font-data text-base-content/60">{r.name}</dt>
                        <dd class="font-data break-all">{r.value}</dd>
                        <dd class="text-[11px] text-base-content/40">{r.provenance}</dd>
                      </>
                    )}
                  </For>
                </dl>
              </Show>
            </Section>

            <Section field="inputs" label="Inputs" summary={inputs().length ? `${inputs().length} · ${names(inputs())}` : "none"}>
              <Show when={inputs().length} fallback={none(e().driver ? "the driver declares no inputs" : "a bare probe endpoint takes none")}>
                <dl class="grid grid-cols-[minmax(0,auto)_1fr_auto] gap-x-4 gap-y-1 text-[12px]">
                  <For each={inputs()}>
                    {(r) => (
                      <>
                        <dt class="font-data text-base-content/60">
                          {r.name}
                          <Show when={r.required}><Tooltip as="span" tabIndex={0} class="text-error" content="required"> *</Tooltip></Show>
                        </dt>
                        <dd class="font-data break-all">
                          <Show when={r.value} fallback={<span class="text-base-content/40">not set</span>}>
                            {r.value}
                            <Show when={r.secret}> <span class="badge badge-ghost badge-xs">secret name</span></Show>
                          </Show>
                        </dd>
                        <dd class="text-[11px] text-base-content/40">{r.provenance}</dd>
                      </>
                    )}
                  </For>
                </dl>
              </Show>
            </Section>

            <section data-field="secrets" class="flex flex-col gap-1">
              <Eyebrow label="Secrets bound" hint="The names of the secrets this endpoint authenticates with. A value is never shown here; a secret's page says where it is used." />
              <Show when={secrets().length} fallback={none("no secret bound")}>
                <ul class="flex flex-wrap gap-1.5">
                  <For each={secrets()}>
                    {(s) => (
                      <Tooltip as="li" tabIndex={0} class="badge badge-ghost gap-1 font-data" content={`input ${s.input}${s.type ? `, ${s.type}` : ""}`}>
                        {s.secret}
                      </Tooltip>
                    )}
                  </For>
                </ul>
              </Show>
            </section>

            <section data-field="out-of-band" class="flex flex-col gap-1">
              <Eyebrow label="Out of band" hint="A management path to the device (a BMC, a management port) that answers through a reboot of it: a reboot's hold leaves this endpoint's gate open." />
              <span class="text-sm">{e().out_of_band ? "A management path: its gate stays open through a reboot." : "An ordinary endpoint: a reboot holds it."}</span>
            </section>

            <Section
              field="polls"
              label="Polls"
              summary={polls().length ? polls().map((p) => `${p.name} every ${p.every}`).join(", ") : "none"}
            >
              <Show when={polls().length} fallback={none(e().driver ? "the driver declares no polls" : "a bare probe endpoint has no polls")}>
                <ul class="flex flex-col divide-y divide-base-300 rounded-box border border-base-300">
                  <For each={polls()}>
                    {(p) => (
                      <li data-testid="poll-row" data-poll={p.name} class="flex flex-col gap-0.5 px-3 py-2 text-[12px]">
                        <span class="flex flex-wrap items-baseline gap-x-3">
                          <span class="font-data font-medium">{p.name}</span>
                          <span class="text-base-content/50">every {p.every}</span>
                          <Show when={p.lastRun}>{(ts) => <Tooltip as="span" tabIndex={0} class="text-base-content/50" content={ts()}>last run {rel(ts(), now())}</Tooltip>}</Show>
                          <Show when={p.duration}>{(d) => <span class="text-base-content/50">{Number(d().value.toFixed(1))} {d().unit} mean</span>}</Show>
                        </span>
                        <span class={p.reads.length ? "text-base-content/70" : "text-base-content/45"}>{p.result}</span>
                        <Show when={p.reads.length}>
                          <span class="font-data text-base-content/60">
                            {p.reads.map((r) => `${r.name} ${typeof r.value === "number" ? Number(r.value.toFixed(2)) : String(r.value)}${r.unit ? ` ${r.unit}` : ""}`).join(" · ")}
                          </span>
                        </Show>
                      </li>
                    )}
                  </For>
                </ul>
                <PollTest endpoint={e().id} />
              </Show>
            </Section>
          </>
        )}
      </Show>
    </div>
  );
}
