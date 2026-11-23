import { For, Show, createEffect, createMemo } from "solid-js";
import { createStore } from "solid-js/store";
import { useQuery } from "@tanstack/solid-query";
import Button from "./Button";
import Eyebrow from "./Eyebrow";
import { Zap } from "./icons";
import { useMe } from "../lib/auth";
import { ENDPOINTS_KEY, listEndpoints } from "../lib/endpoints";
import { DRIVERS_KEY, listDrivers } from "../lib/drivers";
import { METRICS_KEY, listMetricTypes, type MetricRow } from "../lib/metric_types";
import { classifyPollFailure, pollNames, readSample, runEndpointPoll, testPollRefusal, type PollFailure, type PollRun } from "../lib/poll_test";

// PollTest (#2427): the operator's proof that collection works. One button per
// poll the endpoint's driver declares, and Test all, run the poll now through
// the outpost that holds the endpoint and draw what came back in place: each
// sample read with its value and unit and the round trip, or the refusal
// with its real cause. Offered only to a caller holding endpoint:run, and only
// for an endpoint with a driver that has polls (a bare probe has none to run).
//
// Takes the endpoint's id and reads the rest off the shared caches, so a page
// mounts it with one line.

type Result =
  | { state: "pending" }
  | { state: "ok"; run: PollRun }
  | { state: PollFailure["kind"]; failure: PollFailure };

// A host that walks the operator through a test (the add-a-component flow,
// #2460) asks for autoRun, which runs every poll once as soon as the endpoint
// and its driver are read, and onResult, which hears each poll settle so it
// can say which step a failure is fixed on.
export type PollOutcome = { ok: true; run: PollRun } | { ok: false; failure: PollFailure };

export default function PollTest(props: { endpoint: string; autoRun?: boolean; onResult?: (poll: string, outcome: PollOutcome) => void }) {
  const me = useMe();
  const permitted = () => testPollRefusal(me.data) === null;
  const endpoints = useQuery(() => ({ queryKey: ENDPOINTS_KEY, queryFn: () => listEndpoints(), enabled: permitted() }));
  const drivers = useQuery(() => ({ queryKey: DRIVERS_KEY, queryFn: listDrivers, enabled: permitted() }));
  const metrics = useQuery(() => ({ queryKey: METRICS_KEY, queryFn: listMetricTypes, enabled: permitted() }));
  const ep = createMemo(() => endpoints.data?.find((e) => e.id === props.endpoint));
  const polls = createMemo(() => pollNames(drivers.data?.find((d) => d.name === ep()?.driver)));
  const [results, setResults] = createStore<Record<string, Result | undefined>>({});
  const busy = () => polls().some((p) => results[p]?.state === "pending");

  async function test(poll: string) {
    // An endpoint on no outpost is known from its row: nothing to send.
    if (!ep()?.outpost) {
      setResults(poll, { state: "no-outpost", failure: { kind: "no-outpost", title: "No outpost collects this endpoint", reason: "Place the endpoint on an outpost, and the outpost can run its polls." } });
      return;
    }
    setResults(poll, { state: "pending" });
    try {
      const run = await runEndpointPoll(props.endpoint, poll);
      setResults(poll, { state: "ok", run });
      props.onResult?.(poll, { ok: true, run });
    } catch (e) {
      const failure = classifyPollFailure(e);
      setResults(poll, { state: failure.kind, failure });
      props.onResult?.(poll, { ok: false, failure });
    }
  }

  // The outpost holds one lane per endpoint and a run waits for it, so the
  // polls go in turn rather than beside each other.
  async function testAll() {
    for (const p of polls()) await test(p);
  }

  let ran = false;
  createEffect(() => {
    if (!props.autoRun || ran || !ep() || polls().length === 0) return;
    ran = true;
    void testAll();
  });

  return (
    <Show when={permitted() && polls().length > 0}>
      <section data-testid="poll-test" class="flex flex-col gap-2">
        <div class="flex flex-wrap items-center gap-2">
          <Eyebrow
            label="Test"
            hint="Runs a poll now, through the outpost that holds this endpoint, and shows what it read. The samples land as a scheduled run's do; the schedule is unchanged. A gated endpoint refuses the run and names why."
          />
          <For each={polls()}>
            {(p) => (
              <Button size="xs" intent="quiet" icon={Zap} label={`Test ${p}`} loading={results[p]?.state === "pending"} disabled={busy()} onClick={() => void test(p)}>
                {p}
              </Button>
            )}
          </For>
          <Show when={polls().length > 1}>
            <Button size="xs" intent="action" icon={Zap} loading={busy()} disabled={busy()} onClick={() => void testAll()}>
              Test all
            </Button>
          </Show>
        </div>
        <For each={polls()}>
          {(p) => (
            <Show when={results[p]}>
              {(r) => <ResultCard poll={p} result={r()} metrics={metrics.data ?? []} />}
            </Show>
          )}
        </For>
      </section>
    </Show>
  );
}

function ResultCard(props: { poll: string; result: Result; metrics: MetricRow[] }) {
  const r = () => props.result;
  return (
    <div data-testid="poll-result" data-poll={props.poll} data-state={r().state} class="rounded-box border border-base-300 bg-base-200/40 text-[12px]">
      {(() => {
        const res = r();
        if (res.state === "pending") {
          return (
            <div class="flex items-center gap-2 px-3 py-2 text-base-content/60">
              <span class="loading loading-spinner loading-xs" />
              <span class="font-data">{props.poll}</span>
              <span>running on the outpost</span>
            </div>
          );
        }
        if (res.state === "ok") return <Read run={res.run} metrics={props.metrics} />;
        return (
          <div role="alert" class="flex flex-col gap-0.5 px-3 py-2">
            <span class="flex items-center gap-2">
              <span class="inline-block size-2 shrink-0 rounded-full bg-error" />
              <span class="font-data">{props.poll}</span>
              <span class="font-medium">{res.failure.title}</span>
            </span>
            <span class="break-words pl-4 text-base-content/70 [overflow-wrap:anywhere]">{res.failure.reason}</span>
          </div>
        );
      })()}
    </div>
  );
}

function Read(props: { run: PollRun; metrics: MetricRow[] }) {
  const samples = () => props.run.samples ?? [];
  const faults = () => props.run.faults ?? [];
  const metricOf = (name: string) => props.metrics.find((m) => m.name === name);
  return (
    <div class="flex flex-col">
      <div class="flex flex-wrap items-center gap-x-2 px-3 py-2">
        <span class={`inline-block size-2 shrink-0 rounded-full ${samples().length ? "bg-success" : "bg-warning"}`} />
        <span class="font-data">{props.run.poll}</span>
        <span class="font-medium">
          {samples().length ? `read ${samples().length} sample${samples().length === 1 ? "" : "s"}` : "answered, but read no samples"}
        </span>
        <span class="text-base-content/50">
          in {props.run.took_ms} ms · via <span class="font-data">{props.run.outpost}</span>
        </span>
      </div>
      <Show when={samples().length}>
        <dl class="grid grid-cols-[minmax(0,auto)_1fr] gap-x-4 gap-y-0.5 border-t border-base-300 px-3 py-2">
          <For each={samples()}>
            {(s) => {
              const read = () => readSample(s, metricOf(s.name));
              return (
                <>
                  <dt class="font-data text-base-content/60">{read().name}</dt>
                  <dd class="font-data">
                    {read().text}
                    <Show when={read().unit}>
                      {" "}
                      <span class="text-base-content/50">{read().unit}</span>
                    </Show>
                    <Show when={read().lane}>
                      {" "}
                      <span class="badge badge-ghost badge-xs">{read().lane}</span>
                    </Show>
                  </dd>
                </>
              );
            }}
          </For>
        </dl>
      </Show>
      <Show when={faults().length}>
        <ul class="border-t border-base-300 px-3 py-2 text-warning">
          <For each={faults()}>{(f) => <li class="font-data">{f}</li>}</For>
        </ul>
      </Show>
    </div>
  );
}
