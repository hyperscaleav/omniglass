import { For, Match, Show, Switch, createEffect, createMemo, createSignal, type JSX } from "solid-js";
import { A } from "@solidjs/router";
import { useQuery } from "@tanstack/solid-query";
import SimulationSection from "./SimulationSection";
import PipelineFlow from "./PipelineFlow";
import ReachabilityPanel from "./ReachabilityPanel";
import AlarmsPanel from "./AlarmsPanel";
import CommandsPanel from "./CommandsPanel";
import EventsPanel from "./EventsPanel";
import HealthBadge from "./HealthBadge";
import RulePreviewPanel from "./RulePreviewPanel";
import Eyebrow from "./Eyebrow";
import { can, useMe } from "../lib/auth";
import { entityLabel } from "../lib/entities";
import { effectiveMetrics, effectiveMetricsKey } from "../lib/component_metrics";
import { componentSystems, componentSystemsKey } from "../lib/members";
import { PLATFORM_VIEW_KEY, platformRefusal, platformView, retryPlatformView } from "../lib/platformhealth";
import { emptyDraft, type RuleDraft, type RuleOp } from "../lib/event_rules";
import { componentAlarms, componentAlarmsKey } from "../lib/alarms";
import type { LiveKind } from "../lib/lessons";

// LessonLive renders a lesson step's live element (#2450): an existing console
// component over the simulated component the lesson is walked on, so the
// learner sees the real engine against real (simulated) data, never a picture.
// Plain props, so the Learn page is its only coupling.
export default function LessonLive(props: { kind: LiveKind; componentId: string; componentName: string }): JSX.Element {
  const me = useMe();
  return (
    <div data-testid={`lesson-live-${props.kind}`} class="min-w-0">
      <Switch>
        <Match when={props.kind === "simulation"}>
          <SimulationSection componentId={props.componentId} />
        </Match>
        <Match when={props.kind === "samples"}>
          <Samples componentId={props.componentId} />
        </Match>
        <Match when={props.kind === "pipeline"}>
          <Pipeline />
        </Match>
        <Match when={props.kind === "reach"}>
          <ReachabilityPanel name={props.componentId} />
        </Match>
        <Match when={props.kind === "rule-preview"}>
          <ThresholdPreview componentId={props.componentId} />
        </Match>
        <Match when={props.kind === "alarms"}>
          <PollAlarms componentId={props.componentId} />
          <AlarmsPanel
            component={props.componentId}
            canUpdate={false}
            canAcknowledge={can(me.data, "alarm", "acknowledge")}
            canSnooze={can(me.data, "alarm", "snooze")}
          />
        </Match>
        <Match when={props.kind === "health"}>
          <Systems componentId={props.componentId} />
        </Match>
        <Match when={props.kind === "commands"}>
          <CommandsPanel component={props.componentId} name={props.componentName} componentId={props.componentId} />
        </Match>
        <Match when={props.kind === "activity"}>
          <EventsPanel name={props.componentId} />
        </Match>
      </Switch>
    </div>
  );
}

// PollAlarms keeps the alarms panel's own read fresh while a lesson waits for
// an alarm to land: a second observer of the panel's query key, polling, so
// the panel (which a component page refreshes on its own) lists an alarm the
// moment the rule opens it. It renders nothing.
function PollAlarms(props: { componentId: string }): JSX.Element {
  useQuery(() => ({
    queryKey: componentAlarmsKey(props.componentId),
    queryFn: () => componentAlarms(props.componentId),
    refetchInterval: 3_000,
  }));
  return null;
}

// Samples: the metrics the component owns now, each with its latest value, as
// the component's vitals read them.
function Samples(props: { componentId: string }): JSX.Element {
  const q = useQuery(() => ({
    queryKey: effectiveMetricsKey(props.componentId),
    queryFn: () => effectiveMetrics(props.componentId),
    refetchInterval: 5_000,
  }));
  const sampled = () => (q.data ?? []).filter((m) => m.is_sampled);
  return (
    <section class="rounded-box border border-base-300 bg-base-100 p-3.5 text-sm">
      <Eyebrow label="Its samples now" hint="Each metric the component owns and its latest sample. A metric its product declares but that has no sample yet is left out." />
      <Show when={!q.isLoading} fallback={<span class="skeleton mt-2 block h-16 w-full" aria-hidden="true" />}>
        <Show when={sampled().length > 0} fallback={<p class="mt-2 text-base-content/60">No samples yet: collection has not polled it.</p>}>
          <dl class="mt-2 grid grid-cols-[minmax(8rem,auto)_1fr] items-baseline gap-x-4 gap-y-1">
            <For each={sampled().slice(0, 10)}>
              {(m) => (
                <>
                  <dt class="truncate text-base-content/70">{entityLabel({ name: m.metric_type_name, label: m.label })}</dt>
                  <dd data-testid="lesson-sample" class="font-data tnum">
                    {sampleText(m.value ?? m.sample_value)}
                    <Show when={m.unit}><span class="ml-1 text-base-content/50">{m.unit}</span></Show>
                    <Show when={m.instance}><span class="ml-2 text-base-content/50">{m.instance}</span></Show>
                  </dd>
                </>
              )}
            </For>
          </dl>
        </Show>
      </Show>
    </section>
  );
}

function Pipeline(): JSX.Element {
  const q = useQuery(() => ({ queryKey: PLATFORM_VIEW_KEY, queryFn: platformView, retry: retryPlatformView, refetchInterval: 10_000 }));
  return (
    <Show when={!q.error} fallback={<p class="text-sm text-base-content/60">{platformRefusal(q.error)}</p>}>
      <Show when={q.data?.pipeline} fallback={<span class="skeleton block h-24 w-full" aria-hidden="true" />}>
        {(p) => <PipelineFlow pipeline={p()} />}
      </Show>
    </Show>
  );
}

// Systems: the systems this component serves, each with its health now.
function Systems(props: { componentId: string }): JSX.Element {
  const q = useQuery(() => ({
    queryKey: componentSystemsKey(props.componentId),
    queryFn: () => componentSystems(props.componentId),
    refetchInterval: 5_000,
  }));
  return (
    <section class="rounded-box border border-base-300 bg-base-100 p-3.5 text-sm">
      <Eyebrow label="Systems it serves" hint="Each system this component fills a slot in, with the system's health now. An alarm on the component moves these." />
      <Show when={!q.isLoading} fallback={<span class="skeleton mt-2 block h-10 w-full" aria-hidden="true" />}>
        <Show when={(q.data ?? []).length > 0} fallback={<p class="mt-2 text-base-content/60">This component serves no system.</p>}>
          <ul class="mt-2 flex flex-col gap-1">
            <For each={q.data ?? []}>
              {(m) => (
                <li data-testid="lesson-system" class="flex items-center gap-3">
                  <A href={`/systems/${m.system_id}`} class="link link-hover truncate">{m.system}</A>
                  <span class="ml-auto"><HealthBadge system={m.system_id} size="sm" /></span>
                </li>
              )}
            </For>
          </ul>
        </Show>
      </Show>
    </section>
  );
}

const OPS: { op: RuleOp; words: string }[] = [
  { op: "gt", words: "above" },
  { op: "lt", words: "below" },
];

// ThresholdPreview: a threshold on one of the component's own metrics, run as
// a candidate event rule over the last day of its samples by the same preview
// the rule form uses. Nothing is saved.
function ThresholdPreview(props: { componentId: string }): JSX.Element {
  const q = useQuery(() => ({ queryKey: effectiveMetricsKey(props.componentId), queryFn: () => effectiveMetrics(props.componentId) }));
  const metrics = createMemo(() => [...new Set((q.data ?? []).filter((m) => m.is_sampled).map((m) => m.metric_type_name))].sort());
  const [picked, setPicked] = createSignal("");
  const [op, setOp] = createSignal<RuleOp>("gt");
  const [value, setValue] = createSignal("0");
  // The first metric is latched as the choice once one is known, so a metric
  // sampled for the first time later (a fresh lab) cannot reorder the list
  // under the learner and silently change the draft, which clears a preview.
  createEffect(() => {
    if (!picked() && metrics().length) setPicked(metrics()[0]);
  });
  const metric = () => picked();
  const draft = (): RuleDraft => ({
    ...emptyDraft(),
    sampleKey: metric(),
    lane: "metric",
    fireOp: op(),
    fireValue: value(),
    components: [props.componentId],
  });
  const ready = () => !!metric() && value().trim() !== "" && Number.isFinite(Number(value()));
  return (
    <section class="flex flex-col gap-3 rounded-box border border-base-300 bg-base-100 p-3.5 text-sm">
      <Show when={!q.isLoading} fallback={<span class="skeleton block h-10 w-full" aria-hidden="true" />}>
        <Show when={metrics().length > 0} fallback={<p class="text-base-content/60">This component reports no metric yet, so there is nothing to preview.</p>}>
          <div class="flex flex-wrap items-end gap-2">
            <label class="flex flex-col gap-1">
              <span class="text-xs text-base-content/60">Fire when</span>
              <select aria-label="Metric" class="select select-sm font-data" value={metric()} onChange={(e) => setPicked(e.currentTarget.value)}>
                <For each={metrics()}>{(m) => <option value={m}>{m}</option>}</For>
              </select>
            </label>
            <label class="flex flex-col gap-1">
              <span class="text-xs text-base-content/60">reads</span>
              <select aria-label="Comparison" class="select select-sm" value={op()} onChange={(e) => setOp(e.currentTarget.value as RuleOp)}>
                <For each={OPS}>{(o) => <option value={o.op}>{o.words}</option>}</For>
              </select>
            </label>
            <label class="flex flex-col gap-1">
              <span class="text-xs text-base-content/60">this value</span>
              <input aria-label="Threshold" class="input input-sm w-28 font-data" inputmode="decimal" value={value()} onInput={(e) => setValue(e.currentTarget.value)} />
            </label>
          </div>
          <RulePreviewPanel draft={draft()} ready={ready()} />
        </Show>
      </Show>
    </section>
  );
}

// sampleText renders a sample's value, whatever its shape, as one line.
function sampleText(v: unknown): string {
  if (v === undefined || v === null) return "none";
  if (typeof v === "number") return Number.isInteger(v) ? String(v) : v.toFixed(2);
  return typeof v === "string" ? v : JSON.stringify(v);
}
