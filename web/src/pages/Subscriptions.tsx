import { For, Show, createMemo, createSignal, type JSX } from "solid-js";
import { useNavigate } from "@solidjs/router";
import Page from "../components/Page";
import Breadcrumb from "../components/Breadcrumb";
import Button from "../components/Button";
import Drawer from "../components/Drawer";
import Eyebrow from "../components/Eyebrow";
import FieldRow from "../components/FieldRow";
import KVStacked from "../components/KVStacked";
import InfoTip from "../components/InfoTip";
import ExpressionField from "../components/ExpressionField";
import SeverityBadge from "../components/SeverityBadge";
import { IdentityCell } from "../components/IdentityCell";
import { Bell, Check, Pencil, Plus, RotateCcw, Save, Trash, X, Zap } from "../components/icons";
import { can, useMe } from "../lib/auth";
import { createIdentity, entityLabel } from "../lib/entities";
import { describeError, fmtTime, rel } from "../lib/format";
import { useFormActions } from "../lib/formactions";
import {
  CHANNELS,
  SUBSCRIPTION_CAP,
  TRANSITIONS,
  draftErrors,
  draftFrom,
  emptyDraft,
  matchSummary,
  testSubscription,
  toggled,
  useSubscriptionWrites,
  useSubscriptions,
  whenError,
  type Channel,
  type RuleMatch,
  type Subscription,
  type SubscriptionDraft,
  type Transition,
} from "../lib/subscriptions";

// My alarm subscriptions (#1355, epic #1341): the signed-in person's own action
// rules, each telling them alone, on the channels they pick, when an alarm they
// can see is raised or cleared and its when holds. Create and edit in a Drawer,
// pause and resume in place, delete with a confirm. The teaching surface is the
// drawer's test: it judges the draft's on and when against the person's own
// most recent alarms and shows which ones it would have told them about, so the
// predicate is learned against real data before it is saved. Every route is
// gated by notification:subscribe; without it the page says so and offers no
// write.
const HINTS = {
  page: "Rules you write for yourself: when an alarm you can see is raised or cleared and the rule's when holds, you are told on the channels you pick. You are only ever told about alarms on components you can read.",
  list: "You are only ever told about alarms on components you can read, judged at the moment the alarm moves, before the when runs.",
  on: "Which edges of an alarm tell you: when it is raised, when it clears, or both.",
  when: "A predicate over the alarm's facts (component, place, severity_rank, message, rule and more). Empty always holds: every alarm you can see tells you.",
  channels: "Inbox puts an entry in your console inbox (the bell in the top bar). Email sends to the address in your directory entry.",
  enabled: "A paused subscription queues nothing; resuming it tells you from the next transition on, never about what it missed.",
  test: "Judges this draft against your most recent alarms on components you can read, each edge the draft fires on. It writes and sends nothing.",
  noEmail: "You have no email in the directory, so email would reach no one. An administrator sets it.",
};

type Note = { tone: "success" | "error"; text: string } | null;

export default function Subscriptions(): JSX.Element {
  const me = useMe();
  const navigate = useNavigate();
  const subs = useSubscriptions();
  const writes = useSubscriptionWrites();
  const canSubscribe = () => can(me.data, "notification", "subscribe");
  const rows = () => subs.data ?? [];
  const atCap = () => rows().length >= SUBSCRIPTION_CAP;

  // The drawer: null closed, "new" creating, else the rule being edited.
  const [editing, setEditing] = createSignal<Subscription | "new" | null>(null);
  const [note, setNote] = createSignal<Note>(null);
  const [busy, setBusy] = createSignal<string | null>(null);

  async function toggleEnabled(s: Subscription) {
    setBusy(s.name);
    setNote(null);
    try {
      await writes.setEnabled(s.name, !s.enabled);
    } catch (e) {
      setNote({ tone: "error", text: describeError(e) });
    }
    setBusy(null);
  }
  async function remove(s: Subscription) {
    if (!window.confirm(`Delete the subscription "${entityLabel(s)}"? What it already sent stays in your inbox.`)) return;
    setBusy(s.name);
    setNote(null);
    try {
      await writes.remove(s.name);
    } catch (e) {
      setNote({ tone: "error", text: describeError(e) });
    }
    setBusy(null);
  }

  const title = (
    <span class="flex items-center gap-2">
      Alarm subscriptions
      <InfoTip text={HINTS.page} label="Alarm subscriptions" />
    </span>
  );

  return (
    <Page
      title={title}
      breadcrumb={<Breadcrumb crumbs={[{ key: "profile", label: "Profile", onClick: () => navigate("/profile") }, { key: "subscriptions", label: "Alarm subscriptions" }]} />}
      actions={
        <Show when={canSubscribe() && rows().length > 0}>
          <Button intent="action" icon={Plus} disabled={atCap()} title={atCap() ? `You own the most subscriptions a person may (${SUBSCRIPTION_CAP})` : undefined} onClick={() => setEditing("new")}>
            New subscription
          </Button>
        </Show>
      }
    >
      <Show when={me.data && !canSubscribe()}>
        <div role="alert" class="alert alert-warning alert-soft text-sm" data-testid="subs-denied">
          <span>
            Your role does not include <span class="font-data">notification:subscribe</span>, so you cannot subscribe yourself to alarms. An administrator grants it.
          </span>
        </div>
      </Show>

      <Show when={canSubscribe()}>
        <Show when={note()}>
          {(n) => (
            <div role="alert" class="alert alert-soft text-sm" classList={{ "alert-error": n().tone === "error", "alert-success": n().tone === "success" }}>
              <span>{n().text}</span>
            </div>
          )}
        </Show>

        <Show when={subs.isPending}>
          <div class="card border border-base-300 bg-base-200 p-4" aria-busy="true" data-testid="subs-loading">
            <div class="skeleton mb-4 h-8 w-full" />
            <For each={[0, 1, 2]}>{() => <div class="skeleton mb-2 h-10 w-full" />}</For>
          </div>
        </Show>

        <Show when={subs.error && !subs.data}>
          <div role="alert" class="alert alert-error alert-soft flex items-center justify-between" data-testid="subs-error">
            <span>Could not read your subscriptions: {describeError(subs.error)}</span>
            <Button icon={RotateCcw} loading={subs.isFetching} onClick={() => void subs.refetch()}>Retry</Button>
          </div>
        </Show>

        <Show when={subs.data && rows().length === 0}>
          <section class="card border border-base-300 bg-base-200" data-testid="subs-empty">
            <div class="card-body items-center gap-3 p-10 text-center">
              <span class="text-base-content/30"><Bell size={32} /></span>
              <div class="flex items-center gap-1.5">
                <h2 class="text-base font-semibold">No subscriptions yet</h2>
                <InfoTip text={HINTS.page} label="No subscriptions yet" />
              </div>
              <Button intent="action" icon={Plus} onClick={() => setEditing("new")}>New subscription</Button>
            </div>
          </section>
        </Show>

        <Show when={rows().length > 0}>
          <section class="card overflow-hidden border border-base-300 bg-base-200 p-0" data-testid="subs-list">
            <div class="flex items-center gap-2 border-b border-base-300 px-4 py-2.5">
              <Eyebrow label="Your subscriptions" hint={HINTS.list} />
              <span class="font-data text-xs tnum text-base-content/50">
                {rows().length} / {SUBSCRIPTION_CAP}
              </span>
            </div>
            <div class="overflow-x-auto">
              <table class="table table-sm w-full">
                <thead>
                  <tr>
                    <th>Name</th>
                    <th>On</th>
                    <th>When</th>
                    <th>Channels</th>
                    <th>Status</th>
                    <th class="w-px"><span class="sr-only">Actions</span></th>
                  </tr>
                </thead>
                <tbody>
                  <For each={rows()}>
                    {(s) => (
                      <tr data-testid="sub-row" data-name={s.name}>
                        <td class="max-w-64"><IdentityCell entity={s} /></td>
                        <td>
                          <div class="flex flex-wrap gap-1">
                            <For each={s.on}>{(t) => <span class={TRANSITION_PILL[t] ?? PILL_NEUTRAL}>{t}</span>}</For>
                          </div>
                        </td>
                        <td class="max-w-80">
                          <Show when={s.when} fallback={<span class="text-xs text-base-content/40">every alarm you can see</span>}>
                            <code class="block truncate font-data text-xs" title={s.when}>{s.when}</code>
                          </Show>
                        </td>
                        <td>
                          <div class="flex flex-wrap gap-1">
                            <For each={s.channels}>{(c) => <span class={`${PILL_NEUTRAL} font-data`}>{c}</span>}</For>
                          </div>
                        </td>
                        <td>
                          <span class={s.enabled ? "badge badge-sm badge-healthy" : PILL_NEUTRAL} data-testid="sub-status">
                            {s.enabled ? "active" : "paused"}
                          </span>
                        </td>
                        <td>
                          <div class="flex items-center justify-end gap-1">
                            <Button
                              size="xs"
                              intent={s.enabled ? "warn" : "ok"}
                              loading={busy() === s.name}
                              disabled={busy() !== null}
                              onClick={() => void toggleEnabled(s)}
                              label={`${s.enabled ? "Pause" : "Resume"} ${entityLabel(s)}`}
                            >
                              {s.enabled ? "Pause" : "Resume"}
                            </Button>
                            <Button size="xs" square icon={Pencil} title={`Edit ${entityLabel(s)}`} disabled={busy() !== null} onClick={() => setEditing(s)} />
                            <Button size="xs" square intent="danger" icon={Trash} title={`Delete ${entityLabel(s)}`} disabled={busy() !== null} onClick={() => void remove(s)} />
                          </div>
                        </td>
                      </tr>
                    )}
                  </For>
                </tbody>
              </table>
            </div>
          </section>
        </Show>
      </Show>

      <Drawer
        open={editing() !== null}
        onClose={() => setEditing(null)}
        title={editing() === "new" ? "New subscription" : `Edit ${editing() ? entityLabel(editing() as Subscription) : ""}`}
        wide
      >
        <Show when={editing()} keyed>
          {(target) => (
            <SubscriptionForm
              rule={target === "new" ? undefined : target}
              hasEmail={!!me.data?.human?.email}
              onDone={(saved) => {
                setEditing(null);
                setNote({ tone: "success", text: `Saved ${entityLabel(saved)}.` });
              }}
              onCancel={() => setEditing(null)}
            />
          )}
        </Show>
      </Drawer>
    </Page>
  );
}

const PILL_NEUTRAL = "badge badge-sm badge-ghost";
const TRANSITION_PILL: Record<string, string> = {
  raised: "badge badge-sm badge-soft badge-warning",
  cleared: "badge badge-sm badge-healthy",
};

// The drawer body: renders inside the Drawer so it binds the drawer's rail.
// Its test judges the draft's on and when, saved or not.
function SubscriptionForm(p: {
  rule?: Subscription;
  hasEmail: boolean;
  onDone: (s: Subscription) => void;
  onCancel: () => void;
}): JSX.Element {
  const writes = useSubscriptionWrites();
  const start = p.rule ? draftFrom(p.rule) : emptyDraft();
  const ident = createIdentity({ display: start.label, name: p.rule?.name });
  const [on, setOn] = createSignal<Transition[]>(start.on);
  const [when, setWhen] = createSignal(start.when);
  const [channels, setChannels] = createSignal<Channel[]>(start.channels);
  const [enabled, setEnabled] = createSignal(start.enabled);
  // The expression field's verdict; an empty when always holds and needs none.
  const [whenOk, setWhenOk] = createSignal<boolean | undefined>(undefined);
  const [whenRefused, setWhenRefused] = createSignal<string | null>(null);
  const [nameRefused, setNameRefused] = createSignal<string | null>(null);
  const [saveErr, setSaveErr] = createSignal<string | null>(null);
  const [saving, setSaving] = createSignal(false);
  const [touched, setTouched] = createSignal(false);

  const draft = (): SubscriptionDraft => ({ name: ident.name(), label: ident.display(), on: on(), when: when(), channels: channels(), enabled: enabled() });
  const errors = createMemo(() => draftErrors(draft()));
  const whenReady = () => !when().trim() || whenOk() === true;
  const valid = () => Object.keys(errors()).length === 0 && whenReady();

  async function save() {
    setTouched(true);
    if (!valid()) return;
    setSaving(true);
    setSaveErr(null);
    setWhenRefused(null);
    setNameRefused(null);
    try {
      const saved = p.rule ? await writes.update(p.rule.name, draft()) : await writes.create(draft());
      p.onDone(saved);
    } catch (e) {
      const w = whenError(e);
      const status = (e as { status?: number } | null)?.status;
      if (w) setWhenRefused(w);
      else if (status === 409 && !p.rule) setNameRefused(describeError(e));
      else setSaveErr(describeError(e));
    }
    setSaving(false);
  }

  useFormActions().bind({
    submitLabel: p.rule ? "Save" : "Subscribe",
    submitIcon: p.rule ? Save : Plus,
    submit: () => void save(),
    busy: saving,
    disabled: () => !valid(),
    cancel: p.onCancel,
  });

  const fieldErr = (k: "name" | "on" | "channels") => (
    <Show when={(touched() || k !== "name" || ident.name()) && errors()[k]}>
      {(m) => <span class="text-[11px] text-error">{m()}</span>}
    </Show>
  );

  return (
    <form class="flex flex-col gap-4" onSubmit={(e) => { e.preventDefault(); void save(); }}>
      <div class="grid gap-3 sm:grid-cols-2">
        <FieldRow bind="label" info="A friendly name for this subscription; optional.">
          <input class="input input-bordered w-full" value={ident.display()} onInput={(e) => ident.setDisplay(e.currentTarget.value)} placeholder="Outages at HQ" />
        </FieldRow>
        <Show
          when={!p.rule}
          fallback={<KVStacked bind="name" mono value={p.rule!.name} hint="How the API and CLI address this subscription; it cannot be changed." />}
        >
          <div class="flex flex-col gap-1">
            <FieldRow bind="name" info="How the API and CLI address this subscription: lowercase letters, digits and hyphens, unique among your own.">
              <input
                class="input input-bordered w-full font-data"
                classList={{ "input-error": !!(touched() && errors().name) || !!nameRefused() }}
                value={ident.name()}
                onInput={(e) => { ident.setName(e.currentTarget.value); setNameRefused(null); }}
                aria-required="true"
              />
            </FieldRow>
            {fieldErr("name")}
            <Show when={nameRefused()}>{(m) => <span class="text-[11px] text-error">{m()}</span>}</Show>
          </div>
        </Show>
      </div>

      <div class="flex flex-col gap-1.5">
        <Eyebrow label="Tell me when it is" hint={HINTS.on} />
        <div class="flex gap-4" role="group" aria-label="Tell me when it is">
          <For each={TRANSITIONS}>
            {(t) => (
              <label class="flex items-center gap-2 text-sm">
                <input type="checkbox" class="checkbox checkbox-sm" checked={on().includes(t)} onChange={() => setOn((cur) => toggled(cur, t, TRANSITIONS))} />
                <span>{t}</span>
              </label>
            )}
          </For>
        </div>
        {fieldErr("on")}
      </div>

      <div class="flex flex-col gap-1">
        <FieldRow label="When" info={HINTS.when}>
          <ExpressionField
            site="action"
            keyless
            sampleKey=""
            label="When"
            placeholder="severity_rank >= 40"
            value={when()}
            onInput={(v) => { setWhen(v); setWhenRefused(null); }}
            onCheck={setWhenOk}
          />
        </FieldRow>
        <Show when={!when().trim()}>
          <span class="text-[11px] text-base-content/50" data-testid="when-empty">Empty: every alarm you can see tells you.</span>
        </Show>
        <Show when={whenRefused()}>{(m) => <span class="text-[11px] text-error" data-testid="when-refused">{m()}</span>}</Show>
      </div>

      <div class="flex flex-col gap-1.5">
        <Eyebrow label="Tell me by" hint={HINTS.channels} />
        <div class="flex gap-4" role="group" aria-label="Tell me by">
          <For each={CHANNELS}>
            {(c) => (
              <label class="flex items-center gap-2 text-sm">
                <input type="checkbox" class="checkbox checkbox-sm" checked={channels().includes(c)} onChange={() => setChannels((cur) => toggled(cur, c, CHANNELS))} />
                <span>{c}</span>
              </label>
            )}
          </For>
        </div>
        <Show when={channels().includes("email") && !p.hasEmail}>
          <span class="text-[11px] text-warning" data-testid="no-email">{HINTS.noEmail}</span>
        </Show>
        {fieldErr("channels")}
      </div>

      <FieldRow label="Active" info={HINTS.enabled}>
        <input type="checkbox" class="toggle toggle-sm" checked={enabled()} onChange={(e) => setEnabled(e.currentTarget.checked)} />
      </FieldRow>

      <Show when={saveErr()}>
        {(m) => (
          <div role="alert" class="alert alert-error alert-soft text-sm">
            <span>{m()}</span>
          </div>
        )}
      </Show>

      <TestPanel on={on()} when={when()} ready={on().length > 0 && whenReady()} />
    </form>
  );
}

// The teaching surface: run the draft against your own recent alarms and see,
// alarm by alarm and edge by edge, whether it would have told you.
function TestPanel(p: { on: Transition[]; when: string; ready: boolean }): JSX.Element {
  const [matches, setMatches] = createSignal<RuleMatch[] | null>(null);
  const [tested, setTested] = createSignal<string | null>(null);
  const [err, setErr] = createSignal<string | null>(null);
  const [running, setRunning] = createSignal(false);
  const key = () => JSON.stringify([p.on, p.when.trim()]);
  const stale = () => tested() !== null && tested() !== key();
  const summary = () => matchSummary(matches() ?? []);

  async function run() {
    setRunning(true);
    setErr(null);
    const k = key();
    try {
      setMatches(await testSubscription({ on: p.on, when: p.when }));
      setTested(k);
    } catch (e) {
      setMatches(null);
      setTested(null);
      setErr(whenError(e) ?? describeError(e));
    }
    setRunning(false);
  }

  return (
    <section class="flex flex-col gap-2 rounded-box border border-base-300 p-3" data-testid="sub-test">
      <Eyebrow
        label="Test against my recent alarms"
        hint={HINTS.test}
        right={
          <Button size="xs" icon={Zap} loading={running()} disabled={!p.ready} onClick={() => void run()}>
            {matches() ? "Test again" : "Test"}
          </Button>
        }
      />
      <Show when={err()}>{(m) => <span class="text-[11px] text-error" role="alert">{m()}</span>}</Show>
      <Show when={matches()}>
        {(ms) => (
          <>
            <div class="flex flex-wrap items-center gap-2 text-xs text-base-content/70" data-testid="sub-test-summary">
              <span>
                <span class="font-data tnum font-semibold text-base-content">{summary().matched}</span> of{" "}
                <span class="font-data tnum">{ms().length}</span> would have told you
              </span>
              <Show when={summary().errored > 0}>
                <span class="badge badge-sm badge-soft badge-error">{summary().errored} could not be judged</span>
              </Show>
              <Show when={stale()}>
                <span class="badge badge-sm badge-ghost" title="The draft changed since this test ran">stale</span>
              </Show>
            </div>
            <Show
              when={ms().length > 0}
              fallback={<span class="text-[11px] text-base-content/50">You have no recent alarms on components you can read.</span>}
            >
              <ul class="flex flex-col divide-y divide-base-300 rounded-box border border-base-300" classList={{ "opacity-60": stale() }}>
                <For each={ms()}>{(m) => <MatchRow match={m} />}</For>
              </ul>
            </Show>
          </>
        )}
      </Show>
    </section>
  );
}

function MatchRow(p: { match: RuleMatch }): JSX.Element {
  const m = () => p.match;
  const verdict = () => (m().error ? "error" : m().matched ? "matched" : "missed");
  return (
    <li class="flex items-start gap-2 px-2.5 py-2 text-xs" data-testid="sub-test-match" data-verdict={verdict()}>
      <span
        class="mt-0.5 flex-none"
        classList={{ "text-healthy": verdict() === "matched", "text-base-content/30": verdict() === "missed", "text-error": verdict() === "error" }}
        title={verdict() === "matched" ? "Would have told you" : verdict() === "missed" ? "Would not have told you" : "Could not be judged"}
      >
        <Show when={verdict() === "matched"} fallback={<X size={14} />}>
          <Check size={14} />
        </Show>
      </span>
      <span class="sr-only">{verdict() === "matched" ? "Would have told you:" : verdict() === "missed" ? "Would not have told you:" : "Could not be judged:"}</span>
      <div class="flex min-w-0 flex-1 flex-col gap-0.5">
        <div class="flex flex-wrap items-center gap-x-2 gap-y-1">
          <span class="font-data font-medium">{m().component}</span>
          <SeverityBadge name={m().severity} size="xs" />
          <span class={TRANSITION_PILL[m().transition] ?? PILL_NEUTRAL}>{m().transition}</span>
          <span class="tnum text-base-content/50" title={fmtTime(m().at)}>{rel(m().at)}</span>
        </div>
        <Show when={m().message}>
          <span class="truncate text-base-content/60">{m().message}</span>
        </Show>
        <Show when={m().error}>
          <span class="text-error">{m().error}</span>
        </Show>
      </div>
    </li>
  );
}
