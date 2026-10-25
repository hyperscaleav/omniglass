import { For, Show, createEffect, createMemo, createSignal, createUniqueId, type JSX } from "solid-js";
import { useQuery } from "@tanstack/solid-query";
import Button from "./Button";
import Drawer from "./Drawer";
import Eyebrow from "./Eyebrow";
import InfoTip from "./InfoTip";
import KVStacked from "./KVStacked";
import IssueCommandForm from "./IssueCommandForm";
import { ChevronDown, ChevronRight, Zap } from "./icons";
import {
  PHASE_WORDS,
  boundCommandTypes,
  commandSteps,
  commandsPollMs,
  componentCommands,
  componentCommandsKey,
  inFlight,
  intendedText,
  observedText,
  offsetWords,
  phaseBadge,
  phaseHint,
  windowWords,
  type Command,
} from "../lib/commands";
import { ENDPOINTS_KEY, listEndpoints } from "../lib/endpoints";
import { DRIVERS_KEY, listDrivers } from "../lib/drivers";
import { can, useMe } from "../lib/auth";
import { CLOCK_TICK_MS, createClock } from "../lib/clock";
import { describeError, fmtTime, rel } from "../lib/format";

// CommandsPanel (#1155): a component's commands, newest first, each where it
// stands: in flight (queued for its node, delivered, awaiting the reply or the
// observation that settles it, with its window counting down) or terminal
// (settled, failed, timed out, with why). A row opens in place to the whole
// arc: each transition with its time, the evidence that decided it (the
// component's reply, masked of credentials, or the observed sample), and the
// endpoint, node and driver that carried it. It polls every second while
// anything is in flight and at the view's cadence at rest. With command:issue
// and a driver that binds a command, the panel offers Issue a command; the
// issue's toast opens the new command's row here (`open`). `component` is
// the address the reads take (the uuid the view is addressed by), `name` what
// the operator reads.
export default function CommandsPanel(props: { component: string; name: string; componentId?: string; open?: number }): JSX.Element {
  const me = useMe();
  const q = useQuery(() => ({
    queryKey: componentCommandsKey(props.component),
    queryFn: () => componentCommands(props.component),
    refetchInterval: (query) => commandsPollMs(query.state.data as Command[] | undefined),
  }));
  const canIssue = () => can(me.data, "command", "issue");
  const endpoints = useQuery(() => ({ queryKey: ENDPOINTS_KEY, queryFn: listEndpoints, enabled: canIssue() && !!props.componentId }));
  const drivers = useQuery(() => ({ queryKey: DRIVERS_KEY, queryFn: listDrivers, enabled: canIssue() && !!props.componentId }));
  const bound = createMemo(() => (props.componentId ? boundCommandTypes(props.componentId, endpoints.data ?? [], drivers.data ?? []) : []));
  const [issuing, setIssuing] = createSignal(false);

  const now = createClock(CLOCK_TICK_MS);
  const prefix = createUniqueId();
  const [opened, setOpened] = createSignal<ReadonlySet<number>>(new Set());
  const isOpen = (id: number) => opened().has(id);
  const toggle = (id: number) =>
    setOpened((prev) => {
      const next = new Set(prev);
      if (!next.delete(id)) next.add(id);
      return next;
    });
  // A row asked for by address (the issue's toast) opens, and scrolls into
  // view once it has rendered.
  createEffect(() => {
    const id = props.open;
    if (!id || !q.data?.some((c) => c.id === id)) return;
    setOpened((prev) => (prev.has(id) ? prev : new Set(prev).add(id)));
    queueMicrotask(() => document.getElementById(`command-row-${prefix}-${id}`)?.scrollIntoView?.({ block: "nearest" }));
  });
  // Rows keyed by id, never by the objects a poll hands back, so an open row
  // and its focus survive the next read.
  const ids = createMemo(() => (q.data ?? []).map((c) => c.id), undefined, {
    equals: (a, b) => a.length === b.length && a.every((x, i) => x === b[i]),
  });
  const byId = createMemo(() => new Map((q.data ?? []).map((c) => [c.id, c])));
  const flying = createMemo(() => (q.data ?? []).filter(inFlight).length);

  return (
    <section data-testid="commands-panel" class="flex flex-col gap-1.5">
      <Eyebrow
        label="Commands"
        hint="What this component was told, and where each command stands: queued for its node, delivered, awaiting the reply or the observation that settles it, then settled, failed, or timed out when its settle window closed with no evidence."
        right={
          <span class="flex items-center gap-2">
            <Show when={flying()}>
              <span class="text-[11px] text-base-content/50" data-testid="commands-in-flight">{flying()} in flight</span>
            </Show>
            <Show when={canIssue() && bound().length > 0}>
              <Button size="xs" intent="action" icon={Zap} onClick={() => setIssuing(true)}>Issue a command</Button>
            </Show>
          </span>
        }
      />
      <Show when={q.error}>
        <div role="alert" class="alert alert-error alert-soft py-1.5 text-sm">
          <span>{describeError(q.error)}</span>
          <Button size="xs" intent="quiet" loading={q.isFetching} onClick={() => void q.refetch()}>Retry</Button>
        </div>
      </Show>
      <Show when={q.isPending}>
        <div class="rounded-box border border-base-300 px-3 py-3 text-[11.5px] text-base-content/45">Reading the commands…</div>
      </Show>
      <Show when={q.data}>
        <Show
          when={ids().length}
          fallback={
            <div class="rounded-box border border-base-300 px-3 py-3 text-sm text-base-content/55" data-testid="commands-empty">
              <Show
                when={canIssue() && endpoints.data && drivers.data && bound().length === 0}
                fallback={<>No commands issued to this component yet.</>}
              >
                No commands issued to this component yet, and none can reach it: no driver attached to its endpoints binds a command.
              </Show>
            </div>
          }
        >
          <ol class="divide-y divide-base-300 rounded-box border border-base-300" aria-label={`Commands issued to ${props.name}`}>
            <For each={ids()}>
              {(id) => (
                <Show when={byId().get(id)}>
                  {(c) => (
                    <li id={`command-row-${prefix}-${id}`} data-testid={`command-${id}`} class="flex flex-col gap-1.5 px-3 py-2">
                      <CommandRow c={c()} now={now()} open={isOpen(id)} controls={`command-detail-${prefix}-${id}`} onToggle={() => toggle(id)} />
                      <Show when={isOpen(id)}>
                        <CommandDetail c={c()} id={`command-detail-${prefix}-${id}`} />
                      </Show>
                    </li>
                  )}
                </Show>
              )}
            </For>
          </ol>
        </Show>
      </Show>
      <Drawer open={issuing()} onClose={() => setIssuing(false)} title={`Issue a command to ${props.name}`}>
        <IssueCommandForm component={props.component} name={props.name} componentId={props.componentId} types={bound()} close={() => setIssuing(false)} />
      </Drawer>
    </section>
  );
}

// CommandRow is a command's one line: when, what to which value, its phase
// (an InfoTip saying what the phase means for it), and, in flight, its window;
// once terminal, why.
function CommandRow(props: { c: Command; now: number; open: boolean; controls: string; onToggle: () => void }): JSX.Element {
  const want = () => intendedText(props.c);
  return (
    <div class="flex flex-wrap items-center gap-x-2 gap-y-0.5 text-sm">
      <span class="w-14 shrink-0 text-[11px] tabular-nums text-base-content/45" title={fmtTime(props.c.issued_at)}>{rel(props.c.issued_at, props.now)}</span>
      <span class="font-data">{props.c.command_type}</span>
      <Show when={want()}>
        <span class="text-[11.5px] text-base-content/45">to</span>
        <span class="font-data">{want()}</span>
      </Show>
      <InfoTip text={phaseHint(props.c)} label={`${props.c.command_type}: ${PHASE_WORDS[props.c.phase]}`} class={`badge badge-sm ${phaseBadge(props.c.phase)}`}>
        <span data-testid="command-phase">{PHASE_WORDS[props.c.phase]}</span>
      </InfoTip>
      {/* Once terminal, why, on the line; an open row's detail says it whole
          instead of twice. */}
      <Show when={inFlight(props.c)} fallback={<span class="min-w-0 flex-1 truncate text-[11.5px] text-base-content/55">{props.open ? "" : props.c.reason}</span>}>
        <span class="min-w-0 flex-1 text-[11px] tabular-nums text-base-content/45">{windowWords(props.c, props.now)}</span>
      </Show>
      <Button
        square
        size="xs"
        icon={props.open ? ChevronDown : ChevronRight}
        label={`Details of ${props.c.command_type} command ${props.c.id}`}
        title="Open this command's arc: each transition with its time, the evidence that decided it, and what carried it"
        expanded={props.open}
        controls={props.controls}
        onClick={() => props.onToggle()}
      />
    </div>
  );
}

// clockTime is a moment to the second, the issue's time on the arc.
const clockTime = (iso: string) => new Date(iso).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit", second: "2-digit" });

// CommandDetail is a command's whole arc, opened in place under its row: the
// issue's time, then each later transition as an offset from it.
function CommandDetail(props: { c: Command; id: string }): JSX.Element {
  const steps = () => commandSteps(props.c);
  const observed = () => observedText(props.c);
  return (
    <div id={props.id} class="flex flex-col gap-2.5 rounded-box bg-base-200/40 px-3 py-2.5" data-testid="command-detail">
      <ol class="flex flex-wrap items-center gap-x-1.5 gap-y-1 text-[11.5px]" aria-label="The command's transitions">
        <For each={steps()}>
          {(s, i) => (
            <li class="flex items-center gap-1.5" data-step={s.key}>
              <Show when={i() > 0}>
                <span class="text-base-content/25" aria-hidden="true">›</span>
              </Show>
              <span classList={{ "text-base-content/85": s.reached, "text-base-content/35": !s.reached }}>{s.label}</span>
              <Show when={s.reached && s.at}>
                <span class="tabular-nums text-base-content/45" title={fmtTime(s.at!)}>
                  {i() === 0 ? clockTime(s.at!) : offsetWords(props.c.issued_at, s.at!)}
                </span>
              </Show>
            </li>
          )}
        </For>
      </ol>
      <Show when={props.c.reason}>
        <p class="text-[12px] text-base-content/75" data-testid="command-reason">{props.c.reason}</p>
      </Show>
      <div class="grid grid-cols-2 gap-x-4 gap-y-2 md:grid-cols-4">
        <KVStacked label="Settles from" hint="How its driver binding says it settles: the component's reply, or the next observation of what it sets." value={props.c.settle_mode === "none" ? "nothing (it sets no value)" : props.c.settle_mode} />
        <KVStacked label="Window" hint="How long it waits for its evidence before it times out." value={`${props.c.settle_window_seconds}s`} mono />
        <Show when={props.c.endpoint}>
          <KVStacked label="Endpoint" hint="The endpoint it rides: the one a node took it over, else the one a node would." value={props.c.endpoint} mono />
        </Show>
        <Show when={props.c.node}>
          <KVStacked label="Node" value={props.c.node} mono />
        </Show>
        <Show when={props.c.driver}>
          <KVStacked label="Driver" hint="The driver whose command binding renders it and reads its reply." value={props.c.driver} mono />
        </Show>
        <Show when={props.c.target}>
          <KVStacked label="Sets" value={props.c.target} mono />
        </Show>
        <KVStacked label="Issued by" value={props.c.issuer || "a purged principal"} />
      </div>
      <Show when={props.c.reply}>
        <div class="flex flex-col gap-1">
          <Eyebrow label="Reply" hint="The component's answer, as its node reported it, with any credential masked." />
          <pre class="overflow-x-auto whitespace-pre-wrap break-all rounded-md bg-base-200/60 px-2 py-1.5 font-data text-[11px] text-base-content/75" data-testid="command-reply">{props.c.reply}</pre>
        </div>
      </Show>
      <Show when={observed()}>
        <KVStacked label="Observed" hint="The sample of what it sets that decided it." value={<span class="font-data text-sm">{observed()} <span class="text-[11px] text-base-content/45">at {fmtTime(props.c.observed!.at)}</span></span>} />
      </Show>
      <Show when={props.c.exec_error}>
        <KVStacked label="Execution error" value={<span class="font-data text-[12px] text-error">{props.c.exec_error}</span>} />
      </Show>
    </div>
  );
}
