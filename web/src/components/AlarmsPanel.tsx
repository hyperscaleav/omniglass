import { For, Show, createMemo, createSignal, type JSX } from "solid-js";
import { useQuery, useQueryClient } from "@tanstack/solid-query";
import Button from "./Button";
import { Check, Eye, Siren } from "./icons";
import { describeError, rel } from "../lib/format";
import {
  acknowledgeAlarm,
  clearAlarm,
  componentAlarms,
  componentAlarmsKey,
  raiseAlarm,
  splitAlarms,
  unacknowledgedCount,
  type Severity,
} from "../lib/alarms";

// AlarmsPanel: what is wrong with this component, and what was.
//
// An alarm is where fleet health starts, and it impairs its component's own
// verdict wholesale (#626): any active alarm degrades it, a critical one is an
// outage. A system role the component fills no longer counts it toward quorum
// while it is down, so the role can drop below quorum and its impact becomes
// the system's verdict.
//
// Clearing keeps the row rather than deleting it, so the panel shows recently
// cleared alarms below the active ones: "what was wrong, and when" is the thing an
// operator needs weeks later, and a fix that erases its own evidence is worse than
// no record at all.
//
// Writes are immediate, like the tag panel, so there is no Save of
// its own; the caller passes canUpdate (the component detail computes it as "in
// edit mode AND holding component:update"), which keeps view read-only per the
// console invariant.
//
// ACKNOWLEDGING is deliberately outside that gate, on canAcknowledge alone. Edit
// mode exists to protect the component's own data from an accidental write, and
// an acknowledgement writes none of it: it records that the reader looked, leaves
// the alarm exactly as raised, and recomputes no health. The server says the same
// thing by gating it on its own permission with its own scope rather than on
// component:update (ADR-0109). Making an operator enter an editing mode, which is
// also where the raise and clear controls live, in order to say "I have seen this"
// would put a triage act behind the guard for a different kind of act.

const SEVERITY: Record<string, string> = { critical: "badge-error", warning: "badge-warning", info: "badge-info" };
const severityBadge = (s: string) => SEVERITY[s] ?? "badge-ghost";

const SEVERITIES: { value: Severity; label: string }[] = [
  { value: "info", label: "info (recorded, nothing is worse for it)" },
  { value: "warning", label: "warning (degraded, still working)" },
  { value: "critical", label: "critical (this component is out)" },
];

export default function AlarmsPanel(props: { component: string; canUpdate: boolean; canAcknowledge?: boolean }): JSX.Element {
  const qc = useQueryClient();
  const key = () => componentAlarmsKey(props.component);
  const q = useQuery(() => ({
    queryKey: key(),
    queryFn: () => componentAlarms(props.component),
    refetchOnWindowFocus: false,
  }));

  const split = createMemo(() => splitAlarms(q.data ?? []));
  const active = () => split().active;
  const cleared = () => split().cleared;
  // The queue an operator actually works: raised, and nobody has looked at it.
  const unseen = createMemo(() => unacknowledgedCount(q.data ?? []));

  const [err, setErr] = createSignal<string | null>(null);
  const [busy, setBusy] = createSignal(false);
  const [severity, setSeverity] = createSignal<Severity>("warning");
  const [message, setMessage] = createSignal("");

  async function run(write: () => Promise<void>, after?: () => void) {
    setBusy(true);
    setErr(null);
    try {
      await write();
      await qc.invalidateQueries({ queryKey: key() });
      after?.();
    } catch (e) {
      setErr(describeError(e));
    } finally {
      setBusy(false);
    }
  }

  const raise = () =>
    run(
      () =>
        raiseAlarm(props.component, {
          severity: severity(),
          message: message().trim() || undefined,
        }).then(() => undefined),
      () => {
        setMessage("");
        setSeverity("warning");
      },
    );

  const clear = (id: string) => run(() => clearAlarm(props.component, id));

  const acknowledge = (id: string) => run(() => acknowledgeAlarm(props.component, id).then(() => undefined));

  return (
    <div class="flex flex-col gap-2">
      <div class="flex items-baseline justify-between gap-2">
        <div class="flex items-baseline gap-2">
          <span class="eyebrow">Alarms</span>
          <Show when={unseen()}>
            <span class="badge badge-outline badge-warning badge-sm shrink-0" title="Raised, and nobody has recorded seeing it yet">
              {unseen()} unacknowledged
            </span>
          </Show>
        </div>
        <span class="shrink-0 text-[10.5px] text-base-content/40">what is wrong here, and what it takes down</span>
      </div>
      <p class="text-[11px] text-base-content/50">
        An alarm records a condition on this component and takes its own verdict down (any active alarm degrades it,
        a critical one is an outage). Any role it fills stops counting it toward quorum while it stays down, which is
        how a fault on a box becomes a verdict on a room.
      </p>
      <p class="text-[11px] text-base-content/50">
        Acknowledging is separate from clearing: it records that a human has seen the alarm and changes nothing else,
        so the component stays exactly as broken as it was. What is raised is a fact about the condition; who has
        looked is a fact about a person.
      </p>

      <Show when={err()}>
        <div role="alert" class="alert alert-error alert-soft py-1.5 text-xs"><span>{err()}</span></div>
      </Show>
      <Show when={q.error}>
        <div role="alert" class="alert alert-error alert-soft py-1.5 text-xs"><span>{describeError(q.error)}</span></div>
      </Show>

      <Show
        when={active().length}
        fallback={
          <Show when={!q.isLoading}>
            <p class="rounded-box border border-dashed border-base-300 px-3 py-4 text-center text-[12px] text-base-content/45">
              Nothing is wrong here: this component has no active alarm, so its own verdict is healthy.
            </p>
          </Show>
        }
      >
        <div class="divide-y divide-base-300 overflow-hidden rounded-box border border-base-300">
          <For each={active()}>
            {(a) => (
              <div class="flex flex-col gap-1.5 px-3 py-2.5">
                <div class="flex flex-wrap items-baseline gap-2">
                  <span class={`badge badge-soft badge-sm shrink-0 ${severityBadge(a.severity)}`}>{a.severity}</span>
                  <span class="min-w-0 flex-1 text-sm text-base-content/85">{a.message || "(no message)"}</span>
                  <span class="shrink-0 text-[11px] text-base-content/45" title={a.raised_at}>
                    raised {rel(a.raised_at)}
                  </span>
                  {/* Who has looked at this, if anybody. An unacknowledged alarm is
                      the one still waiting for a human, so it says so rather than
                      staying silent; an acknowledged one names the person and stays
                      just as raised, because acknowledging is not fixing. */}
                  <Show
                    when={a.acknowledged}
                    fallback={
                      <span class="badge badge-outline badge-warning badge-xs shrink-0" title="Nobody has recorded seeing this yet">
                        unacknowledged
                      </span>
                    }
                  >
                    <span class="shrink-0 text-[11px] text-base-content/45" title={a.acknowledged_at}>
                      seen by {a.acknowledged_by || "someone since removed"} {rel(a.acknowledged_at!)}
                    </span>
                  </Show>
                  <Show when={props.canAcknowledge && !a.acknowledged}>
                    <Button
                      square
                      size="xs"
                      icon={Eye}
                      label={`Acknowledge alarm ${a.id}`}
                      title="Record that you have seen this alarm. It stays raised."
                      disabled={busy()}
                      onClick={() => void acknowledge(a.id)}
                    />
                  </Show>
                  <Show when={props.canUpdate}>
                    <Button
                      square
                      size="xs"
                      icon={Check}
                      label={`Clear alarm ${a.id}`}
                      title="Clear this alarm"
                      disabled={busy()}
                      onClick={() => void clear(a.id)}
                    />
                  </Show>
                </div>
              </div>
            )}
          </For>
        </div>
      </Show>

      <Show when={props.canUpdate}>
        <div class="flex flex-col gap-2 rounded-box border border-dashed border-base-300 p-2.5">
          <span class="text-[10.5px] font-semibold uppercase tracking-wide text-base-content/50">Raise an alarm</span>
          <div class="flex flex-wrap items-center gap-1.5">
            <select
              class="select select-bordered select-sm w-full min-w-0 sm:w-auto sm:flex-none"
              aria-label="Alarm severity"
              value={severity()}
              onChange={(e) => setSeverity(e.currentTarget.value as Severity)}
            >
              <For each={SEVERITIES}>{(s) => <option value={s.value}>{s.label}</option>}</For>
            </select>
            <input
              class="input input-bordered input-sm min-w-0 flex-1"
              aria-label="Alarm message"
              placeholder="What is wrong, for whoever reads it later"
              value={message()}
              onInput={(e) => setMessage(e.currentTarget.value)}
            />
            <Button
              size="sm"
              intent="warn"
              icon={Siren}
              disabled={busy()}
              onClick={() => void raise()}
            >
              Raise alarm
            </Button>
          </div>
        </div>
      </Show>

      <Show when={cleared().length}>
        <div class="flex flex-col gap-1" role="group" aria-label="Recently cleared alarms">
          <div class="flex items-baseline gap-2">
            <span class="text-[10.5px] font-semibold uppercase tracking-wide text-base-content/50">Recently cleared</span>
            <span class="text-[10.5px] text-base-content/40">kept on purpose: the fix does not erase the fault</span>
          </div>
          <div class="divide-y divide-base-300 overflow-hidden rounded-box border border-dashed border-base-300">
            <For each={cleared()}>
              {(a) => (
                <div class="flex flex-wrap items-baseline gap-2 px-3 py-2 opacity-70">
                  <span class={`badge badge-outline badge-sm shrink-0 ${severityBadge(a.severity)}`}>{a.severity}</span>
                  <span class="min-w-0 flex-1 text-[12.5px] text-base-content/70">{a.message || "(no message)"}</span>
                  <span class="shrink-0 text-[11px] text-base-content/45">
                    raised {rel(a.raised_at)}
                    <Show when={a.cleared_at}> · cleared {rel(a.cleared_at!)}</Show>
                    {/* An incident that came and went with nobody looking is worth
                        seeing in the history: it is the one that got no attention. */}
                    <Show when={!a.acknowledged}> · never acknowledged</Show>
                  </span>
                </div>
              )}
            </For>
          </div>
        </div>
      </Show>
    </div>
  );
}
