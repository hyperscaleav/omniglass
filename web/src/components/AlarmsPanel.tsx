import { For, Show, createMemo, createSignal, type JSX } from "solid-js";
import { useQuery, useQueryClient } from "@tanstack/solid-query";
import Button from "./Button";
import { Check, Plus, Siren, X } from "./icons";
import { describeError, rel } from "../lib/format";
import { CAPABILITIES_KEY, listCapabilities } from "../lib/capabilities";
import {
  clearAlarm,
  componentAlarms,
  componentAlarmsKey,
  raiseAlarm,
  splitAlarms,
  type Alarm,
  type Severity,
} from "../lib/alarms";

// AlarmsPanel: what is wrong with this component, and what was.
//
// An alarm is where estate health starts. On its own it is just a recorded
// condition; what gives it reach is the CAPABILITIES it names. A system role that
// requires one of them can no longer be filled by this component, so the role drops
// below its quorum and the role's impact becomes the system's verdict. Raising an
// alarm with no capabilities is legal and deliberate: it is recorded, and no
// verdict moves.
//
// Clearing keeps the row rather than deleting it, so the panel shows recently
// cleared alarms below the active ones: "what was wrong, and when" is the thing an
// operator needs weeks later, and a fix that erases its own evidence is worse than
// no record at all.
//
// Writes are immediate, like the tag and capability panels, so there is no Save of
// its own; the caller passes canUpdate (the component detail computes it as "in
// edit mode AND holding component:update"), which keeps view read-only per the
// console invariant.

const SEVERITY: Record<string, string> = { critical: "badge-error", warning: "badge-warning", info: "badge-info" };
const severityBadge = (s: string) => SEVERITY[s] ?? "badge-ghost";

const SEVERITIES: { value: Severity; label: string }[] = [
  { value: "info", label: "info (recorded, nothing is worse for it)" },
  { value: "warning", label: "warning (degraded, still working)" },
  { value: "critical", label: "critical (this component is out)" },
];

function CapabilityChips(props: { alarm: Alarm }) {
  return (
    <div class="flex flex-wrap items-center gap-1.5">
      <span class="text-[10.5px] uppercase tracking-wide text-base-content/40">degrades</span>
      <Show
        when={(props.alarm.capabilities ?? []).length}
        fallback={<span class="text-[11px] italic text-base-content/40">nothing: it reaches no role</span>}
      >
        <For each={props.alarm.capabilities ?? []}>
          {(c) => <span class="badge badge-ghost badge-sm font-data">{c}</span>}
        </For>
      </Show>
    </div>
  );
}

export default function AlarmsPanel(props: { component: string; canUpdate: boolean }): JSX.Element {
  const qc = useQueryClient();
  const key = () => componentAlarmsKey(props.component);
  const q = useQuery(() => ({
    queryKey: key(),
    queryFn: () => componentAlarms(props.component),
    refetchOnWindowFocus: false,
  }));
  const catalog = useQuery(() => ({ queryKey: CAPABILITIES_KEY, queryFn: listCapabilities }));

  const split = createMemo(() => splitAlarms(q.data ?? []));
  const active = () => split().active;
  const cleared = () => split().cleared;

  const [err, setErr] = createSignal<string | null>(null);
  const [busy, setBusy] = createSignal(false);
  const [severity, setSeverity] = createSignal<Severity>("warning");
  const [message, setMessage] = createSignal("");
  const [picked, setPicked] = createSignal<string[]>([]);
  const [adding, setAdding] = createSignal("");

  // The catalog minus what this draft already names, so a capability cannot be
  // added twice.
  const addable = createMemo(() => {
    const taken = new Set(picked());
    return [...(catalog.data ?? [])]
      .filter((c) => !taken.has(c.id))
      .sort((a, b) => a.display_name.localeCompare(b.display_name));
  });

  async function run(write: () => Promise<void>, after?: () => void) {
    setBusy(true);
    setErr(null);
    try {
      await write();
      await qc.invalidateQueries({ queryKey: key() });
      after?.();
    } catch (e) {
      // The server's refusal (an unknown capability is a 422) names the problem, so
      // it is shown as sent.
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
          capabilities: picked().length ? picked() : undefined,
        }).then(() => undefined),
      () => {
        setMessage("");
        setPicked([]);
        setAdding("");
        setSeverity("warning");
      },
    );

  const clear = (id: string) => run(() => clearAlarm(props.component, id));

  return (
    <div class="flex flex-col gap-2">
      <div class="flex items-baseline justify-between gap-2">
        <span class="eyebrow">Alarms</span>
        <span class="shrink-0 text-[10.5px] text-base-content/40">what is wrong here, and what it takes away</span>
      </div>
      <p class="text-[11px] text-base-content/50">
        An alarm records a condition on this component and names the capabilities it degrades. A system role requiring
        one of them can no longer be filled here, which is how a fault on a box becomes a verdict on a room. An alarm
        naming no capability is recorded and reaches nothing.
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
              Nothing is wrong here: this component has no active alarm, so it takes no capability away from any role.
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
                <CapabilityChips alarm={a} />
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
          </div>

          <div class="flex flex-wrap items-center gap-1.5">
            <span class="text-[10.5px] uppercase tracking-wide text-base-content/40">degrades</span>
            <Show
              when={picked().length}
              fallback={<span class="text-[11px] italic text-base-content/40">nothing yet: it would reach no role</span>}
            >
              <For each={picked()}>
                {(c) => (
                  <span class="badge badge-outline badge-sm gap-1 font-data">
                    {c}
                    <button
                      type="button"
                      class="ml-0.5 inline-flex opacity-60 hover:opacity-100"
                      aria-label={`Do not degrade ${c}`}
                      onClick={() => setPicked((p) => p.filter((x) => x !== c))}
                    >
                      <X size={11} />
                    </button>
                  </span>
                )}
              </For>
            </Show>
          </div>

          <div class="flex items-center gap-1.5">
            <select
              class="select select-bordered select-sm min-w-0 flex-1"
              aria-label="Capability this alarm degrades"
              value={adding()}
              onChange={(e) => setAdding(e.currentTarget.value)}
            >
              <option value="">Add a capability it degrades…</option>
              <For each={addable()}>{(c) => <option value={c.id}>{c.display_name} ({c.id})</option>}</For>
            </select>
            <Button
              square
              size="sm"
              icon={Plus}
              label="Add capability to alarm"
              title="Add"
              disabled={busy() || !adding()}
              onClick={() => {
                const id = adding();
                if (!id) return;
                setPicked((p) => [...p, id]);
                setAdding("");
              }}
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
