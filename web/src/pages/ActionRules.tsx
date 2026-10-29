import { For, Show, createContext, createMemo, createSignal, useContext, type JSX } from "solid-js";
import { useQuery, useQueryClient } from "@tanstack/solid-query";
import FlatList, { type FlatColumn } from "../components/FlatList";
import Drawer from "../components/Drawer";
import BladeTitle from "../components/BladeTitle";
import FieldRow from "../components/FieldRow";
import KVStacked from "../components/KVStacked";
import Eyebrow from "../components/Eyebrow";
import InfoTip from "../components/InfoTip";
import ExpressionField from "../components/ExpressionField";
import RecentNotifications from "../components/RecentNotifications";
import { identityColumn } from "../components/IdentityCell";
import { Pencil, Plus, Save, Trash } from "../components/icons";
import { createIdentity, entityLabel } from "../lib/entities";
import { useFormActions } from "../lib/formactions";
import { bindSelectValue } from "../lib/selectvalue";
import { can, canFleet, fleetView, useMe } from "../lib/auth";
import { OFFICIAL_LOCK, registryLock } from "../lib/catalog";
import { describeError } from "../lib/format";
import { type BladeDef, createBladeController, useBlades, useBladeEdit } from "../lib/blades";
import { PILL_NEUTRAL, TRANSITIONS, TRANSITION_PILL, toggled, whenError, type Transition } from "../lib/subscriptions";
import { NOTIFICATION_TARGETS_KEY, listNotificationTargets } from "../lib/notification_targets";
import { NOTIFICATION_TYPES_KEY, listNotificationTypes } from "../lib/notification_types";
import {
  ACTION_RULES_KEY,
  actionRuleDraftErrors,
  actionRuleDraftFrom,
  createActionRule,
  createActionRuleBody,
  deleteActionRule,
  emptyActionRuleDraft,
  listActionRules,
  updateActionRule,
  updateActionRuleBody,
  type ActionRule,
  type ActionRuleDraft,
} from "../lib/action_rules";

// Action rules (Catalog > Actions > Action rules, #1346): when a message about
// an alarm is sent. On an alarm's raise, clear or both, when the rule's
// predicate (the "action" expression site, over the alarm facts) holds, it
// queues one notification of its type to each of its targets. The form writes
// the predicate in the console's one expression editor, checked as it is
// typed with the site's facts listed; the blade shows what the rule recently
// sent. A rule rides rule:* like an event rule: any rule:read lists them, and
// every write is judged on the caller's all-scope permissions (canFleet). A
// person's own subscriptions are theirs alone and live on their profile.

export const ACTION_RULE_HINTS = {
  on: "Which edges of an alarm send: when it is raised, when it clears, or both. Acknowledging an alarm never sends anything.",
  when: "A predicate over the alarm's facts (component, place, severity, severity_rank, message, rule and more). Empty always holds: every alarm transition sends. severity_rank >= 40 is high and above.",
  targets: "Where it sends: one notification per target, per transition. A disabled target is skipped.",
  type: "What the message says. The official one for the transition (alarm-raised, alarm-cleared) unless you pick your own from Notifications.",
  optOut: "Whether a member of a group this rule targets may opt themselves out of it. Off by default, so an on-call page cannot be opted out of.",
  enabled: "A disabled rule queues nothing on a transition; what it queued before still delivers. Enabled again, it acts from the next transition.",
  recent: "What this rule sent lately, newest first, about components you can read, picked from the newest 500 notifications.",
  official: "Official rules ship with Omniglass and update with it, so they are read-only.",
} as const;

const LIST_POLL_MS = 10_000;

function statusPill(enabled: boolean): JSX.Element {
  return enabled
    ? <span class="badge badge-soft badge-success badge-sm">enabled</span>
    : <span class={PILL_NEUTRAL}>disabled</span>;
}

function TransitionPills(p: { on: string[] | null | undefined }): JSX.Element {
  return (
    <span class="flex flex-wrap gap-1">
      <For each={p.on ?? []}>{(t) => <span class={TRANSITION_PILL[t] ?? PILL_NEUTRAL}>{t}</span>}</For>
    </span>
  );
}

function TargetChips(p: { targets: string[] | null | undefined }): JSX.Element {
  return (
    <span class="flex flex-wrap gap-1">
      <For each={p.targets ?? []}>{(t) => <span class={`${PILL_NEUTRAL} font-data`}>{t}</span>}</For>
    </span>
  );
}

type RulesUi = { openEdit: (name: string) => void };
const ActionRulesUiContext = createContext<RulesUi>();

export default function ActionRules(): JSX.Element {
  const me = useMe();
  const qc = useQueryClient();
  const rules = useQuery(() => ({ queryKey: ACTION_RULES_KEY, queryFn: listActionRules, refetchInterval: LIST_POLL_MS }));
  const rows = () => (rules.data ?? []).slice().sort((a, b) => a.name.localeCompare(b.name));
  const canCreate = () => canFleet(me.data, "rule", "create");
  const blades = createBladeController();
  const [editing, setEditing] = createSignal<string | null>(null);
  const editRow = () => (rules.data ?? []).find((r) => r.name === editing());
  const backToBlade = (name: string) => {
    setEditing(null);
    blades.push({ kind: "action_rule", id: name });
  };

  const columns: FlatColumn<ActionRule>[] = [
    identityColumn<ActionRule>(),
    { key: "on", label: "On", width: "140px", cell: (r) => <TransitionPills on={r.on} /> },
    {
      key: "when",
      label: "When",
      width: "240px",
      sortVal: (r) => r.when ?? "",
      cell: (r) => (
        <Show when={r.when} fallback={<span class="text-[12px] text-base-content/40">every alarm</span>}>
          <code class="block truncate font-data text-[12px]" title={r.when}>{r.when}</code>
        </Show>
      ),
    },
    { key: "targets", label: "Sends to", width: "200px", sortVal: (r) => (r.targets ?? []).join(","), cell: (r) => <TargetChips targets={r.targets} /> },
    { key: "enabled", label: "Status", width: "96px", sortVal: (r) => String(r.enabled), cell: (r) => statusPill(r.enabled) },
  ];

  return (
    <ActionRulesUiContext.Provider value={{ openEdit: (name) => { blades.close(); setEditing(name); } }}>
      <div class="flex min-h-full flex-col gap-4">
        <FlatList<ActionRule>
          config={{
            entity: { name: "action rule", plural: "action rules" },
            rows,
            loading: () => rules.isPending,
            error: () => rules.error,
            filterKeys: [
              { key: "name", type: "string", hint: "substring", get: (r) => `${entityLabel(r)} ${r.name}`, values: () => [] },
              { key: "target", type: "string", hint: "exact", get: (r) => (r.targets ?? []).join(" "), values: () => [...new Set(rows().flatMap((r) => r.targets ?? []))] },
              { key: "enabled", type: "string", hint: "exact", get: (r) => (r.enabled ? "enabled" : "disabled"), values: () => ["enabled", "disabled"] },
            ],
            filterPlaceholder: "filter action rules by name, target…",
            columns,
            empty: "No action rules. An action rule sends a message about an alarm to its targets when the alarm is raised or cleared and its when holds.",
            rowId: (r) => r.name,
            blades: { registry: { action_rule: actionRuleBlade }, rootKind: "action_rule", controller: blades },
            railExtra: () => (
              <span class="mr-1 inline-flex items-center gap-1.5 text-[12px] text-base-content/50">
                What an action rule does
                <InfoTip
                  label="action rules"
                  text="On an alarm's raise or clear, when its when holds over the alarm's facts, an action rule queues one notification per target, in the transition's own transaction; the server then delivers each one, retrying a receiver that is down. Each delivery joins the alarm's timeline."
                />
              </span>
            ),
            create: canCreate()
              ? { label: "New action rule", can: canCreate, body: (ctx) => <ActionRuleForm mode="create" onSaved={ctx.select} /> }
              : undefined,
          }}
        />
      </div>
      <Show when={editing() ? editRow() : undefined}>
        {(r) => (
          <Drawer open={true} onClose={() => backToBlade(r().name)} title={<span>Edit {entityLabel(r())}</span>}>
            <ActionRuleForm
              mode="edit"
              rule={r()}
              onSaved={async (saved) => {
                await qc.invalidateQueries({ queryKey: ACTION_RULES_KEY });
                backToBlade(saved.name);
              }}
            />
          </Drawer>
        )}
      </Show>
    </ActionRulesUiContext.Provider>
  );
}

function useActionRuleLookup(name: string): { row: () => ActionRule | undefined; loaded: () => boolean } {
  const rules = useQuery(() => ({ queryKey: ACTION_RULES_KEY, queryFn: listActionRules }));
  return { row: () => (rules.data ?? []).find((r) => r.name === name), loaded: () => rules.isSuccess };
}

function ActionRuleBladeTitle(p: { name: string }): JSX.Element {
  const row = useActionRuleLookup(p.name).row;
  return <BladeTitle row={row} fallback={p.name} />;
}

export const actionRuleBlade: BladeDef = {
  Title: (p) => <ActionRuleBladeTitle name={p.id} />,
  Body: (p) => <ActionRuleBladeBody name={p.id} />,
};

function ActionRuleBladeBody(p: { name: string }): JSX.Element {
  const qc = useQueryClient();
  const me = useMe();
  const blades = useBlades();
  const edit = useBladeEdit();
  const ui = useContext(ActionRulesUiContext);
  const lookup = useActionRuleLookup(p.name);
  const row = lookup.row;
  const [err, setErr] = createSignal<string | null>(null);
  const canUpdate = () => canFleet(me.data, "rule", "update");
  const canDelete = () => canFleet(me.data, "rule", "delete");

  async function toggleEnabled(r: ActionRule) {
    setErr(null);
    try {
      await updateActionRule(r.name, { enabled: !r.enabled });
      await qc.invalidateQueries({ queryKey: ACTION_RULES_KEY });
    } catch (e) {
      setErr(describeError(e));
    }
  }

  async function remove(r: ActionRule) {
    if (!confirm(`Delete action rule "${r.name}"? What it already queued still delivers, and keeps its name.`)) return;
    setErr(null);
    try {
      await deleteActionRule(r.name);
      blades.close();
      await qc.invalidateQueries({ queryKey: ACTION_RULES_KEY });
    } catch (e) {
      setErr(describeError(e));
    }
  }

  // An official rule is read-only for everyone; a custom one offers Edit (the
  // Drawer), the Disable / Enable switch and a kebab Delete, each on its own
  // all-scope permission (the Rules page's footer).
  edit.bind({
    primary: () => {
      const r = row();
      return r && !r.official && canUpdate() && ui ? { label: "Edit", icon: <Pencil size={15} />, onClick: () => ui.openEdit(r.name) } : undefined;
    },
    destructive: () => {
      const r = row();
      if (!r || r.official || !canUpdate()) return undefined;
      return r.enabled
        ? { label: "Disable", tone: "warn", onClick: () => void toggleEnabled(r) }
        : { label: "Enable", tone: "ok", onClick: () => void toggleEnabled(r) };
    },
    secondary: () => {
      const r = row();
      if (!r || r.official || !canDelete()) return [];
      return [{ label: "Delete", icon: <Trash size={15} />, tone: "danger", onClick: () => void remove(r) }];
    },
    locked: () => {
      const r = row();
      if (!r) return null;
      if (r.official) return OFFICIAL_LOCK;
      return canUpdate() ? null : registryLock(r, fleetView(me.data), "rule");
    },
  });

  return (
    <Show
      when={row()}
      fallback={
        <Show when={lookup.loaded()} fallback={<p class="text-sm text-base-content/40">Loading…</p>}>
          <p class="text-sm text-base-content/50">Action rule not found.</p>
        </Show>
      }
    >
      {(r) => (
        <div class="flex flex-col gap-5">
          <Show when={err()}>
            <div role="alert" class="alert alert-error alert-soft text-sm"><span>{err()}</span></div>
          </Show>
          <div class="grid grid-cols-2 gap-3 text-sm">
            <KVStacked bind="name" value={<span class="font-data">{r().name}</span>} />
            <KVStacked label="Status" hint={ACTION_RULE_HINTS.enabled} value={<span data-testid="action-rule-status">{statusPill(r().enabled)}</span>} />
            <KVStacked label="On" hint={ACTION_RULE_HINTS.on} value={<TransitionPills on={r().on} />} />
            <KVStacked
              label="Sends"
              hint={ACTION_RULE_HINTS.type}
              value={
                <Show when={r().notification_type} fallback={<span class="text-base-content/60">the official message</span>}>
                  <span class="font-data">{r().notification_type}</span>
                </Show>
              }
            />
            <KVStacked label="Opt-out" hint={ACTION_RULE_HINTS.optOut} value={r().opt_out_allowed ? "allowed" : "not allowed"} />
            <Show when={r().official}>
              <KVStacked label="Origin" hint={ACTION_RULE_HINTS.official} value={<span class="badge badge-ghost badge-sm">official</span>} />
            </Show>
          </div>
          <Show when={r().description}>
            <KVStacked label="Description" value={<span class="text-base-content/80">{r().description}</span>} />
          </Show>
          <section class="flex flex-col gap-2" aria-label="When">
            <Eyebrow label="When" hint={ACTION_RULE_HINTS.when} />
            <Show when={r().when} fallback={<span class="text-sm text-base-content/60" data-testid="action-rule-when">every alarm</span>}>
              <code class="break-all font-data text-[12px] text-base-content/80" data-testid="action-rule-when">{r().when}</code>
            </Show>
          </section>
          <section class="flex flex-col gap-2" aria-label="Sends to">
            <Eyebrow label="Sends to" hint={ACTION_RULE_HINTS.targets} />
            <TargetChips targets={r().targets} />
          </section>
          <Show when={can(me.data, "notification", "read")}>
            <RecentNotifications rule={r().name} hint={ACTION_RULE_HINTS.recent} />
          </Show>
        </div>
      )}
    </Show>
  );
}

// ---- the form ----------------------------------------------------------------

// ActionRuleForm authors a platform rule, on create and on edit alike; a Drawer
// hosts it and draws its rail. The when is written in the expression editor at
// the action site (no sample key: it reads the alarm's fixed facts) and must
// compile before the rule saves; the targets and the notification type are
// picked from their registries. The name is fixed at create.
export function ActionRuleForm(p: { mode: "create" | "edit"; rule?: ActionRule; onSaved: (r: ActionRule) => void }): JSX.Element {
  const qc = useQueryClient();
  const me = useMe();
  const editing = p.mode === "edit";
  const start: ActionRuleDraft = p.rule ? actionRuleDraftFrom(p.rule) : emptyActionRuleDraft();
  const ident = createIdentity({ display: start.label, name: p.rule?.name });
  const [description, setDescription] = createSignal(start.description);
  const [on, setOn] = createSignal<Transition[]>(start.on);
  const [when, setWhen] = createSignal(start.when);
  const [targets, setTargets] = createSignal<string[]>(start.targets);
  const [type, setType] = createSignal(start.notificationType);
  const [optOut, setOptOut] = createSignal(start.optOutAllowed);
  const [enabled, setEnabled] = createSignal(start.enabled);
  const [whenOk, setWhenOk] = createSignal<boolean | undefined>(undefined);
  const [whenRefused, setWhenRefused] = createSignal<string | null>(null);
  const [touched, setTouched] = createSignal(false);
  const [busy, setBusy] = createSignal(false);
  const [formErr, setFormErr] = createSignal<string | null>(null);

  // The targets are admin-only to read; a rule writer who cannot read them
  // types the names instead.
  const canReadTargets = () => can(me.data, "notification_target", "read");
  const targetList = useQuery(() => ({ queryKey: NOTIFICATION_TARGETS_KEY, queryFn: listNotificationTargets, enabled: canReadTargets() }));
  const targetOptions = createMemo(() => {
    const known = (targetList.data ?? []).map((t) => t.name);
    // A name the rule holds that the list does not (yet) is kept, so a save
    // never drops it silently.
    return [...new Set([...known, ...targets()])].sort();
  });
  const types = useQuery(() => ({ queryKey: NOTIFICATION_TYPES_KEY, queryFn: listNotificationTypes }));
  const typeOptions = createMemo(() => [...(types.data ?? [])].sort((a, b) => a.name.localeCompare(b.name)));

  const draft = (): ActionRuleDraft => ({
    name: ident.name(), label: ident.display(), description: description(), on: on(), when: when(), targets: targets(),
    notificationType: type(), optOutAllowed: optOut(), enabled: enabled(),
  });
  const errors = createMemo(() => actionRuleDraftErrors(draft(), editing));
  const whenReady = () => !when().trim() || whenOk() === true;
  const valid = () => Object.keys(errors()).length === 0 && whenReady();
  // An error shows once the form is engaged (a name typed, a submit tried, or
  // an edit), so a fresh form opens with nothing in red.
  const shown = (k: "name" | "on" | "targets") => (
    <Show when={(touched() || !!ident.name() || editing) && errors()[k]}>
      {(m) => <span class="text-[11px] text-error">{m()}</span>}
    </Show>
  );

  useFormActions().bind({
    submitLabel: editing ? "Save action rule" : "Create action rule",
    submitIcon: editing ? Save : Plus,
    submit: () => void submit(),
    busy,
    disabled: () => !valid(),
  });

  async function submit() {
    setTouched(true);
    if (!valid()) return;
    setBusy(true);
    setFormErr(null);
    setWhenRefused(null);
    try {
      const saved = editing
        ? await updateActionRule(p.rule!.name, updateActionRuleBody(draft(), p.rule!))
        : await createActionRule(createActionRuleBody(draft()));
      await qc.invalidateQueries({ queryKey: ACTION_RULES_KEY });
      p.onSaved(saved);
    } catch (e) {
      const w = whenError(e);
      if (w) setWhenRefused(w);
      else setFormErr(describeError(e));
    }
    setBusy(false);
  }

  return (
    <form class="flex flex-col gap-4" onSubmit={(e) => { e.preventDefault(); void submit(); }}>
      <Show when={formErr()}>
        <div role="alert" class="alert alert-error alert-soft text-sm" data-testid="action-rule-form-error"><span>{formErr()}</span></div>
      </Show>
      <div class="grid gap-3 sm:grid-cols-2">
        <FieldRow bind="label" info="A friendly name for this rule; optional.">
          <input class="input input-bordered w-full" value={ident.display()} onInput={(e) => ident.setDisplay(e.currentTarget.value)} placeholder="Page ops" />
        </FieldRow>
        <Show
          when={!editing}
          fallback={<KVStacked bind="name" mono value={p.rule!.name} hint="How the API and CLI address this rule; it cannot be changed." />}
        >
          <div class="flex flex-col gap-1">
            <FieldRow bind="name" info="How the API and CLI address this rule: lowercase letters, digits and hyphens.">
              <input class="input input-bordered w-full font-data" value={ident.name()} onInput={(e) => ident.setName(e.currentTarget.value)} placeholder="page-ops" aria-required="true" />
            </FieldRow>
            {shown("name")}
          </div>
        </Show>
      </div>
      <FieldRow label="Description">
        <input class="input input-bordered w-full" value={description()} onInput={(e) => setDescription(e.currentTarget.value)} />
      </FieldRow>

      <div class="flex flex-col gap-1.5">
        <Eyebrow label="Send when it is" hint={ACTION_RULE_HINTS.on} />
        <div class="flex gap-4" role="group" aria-label="Send when it is">
          <For each={TRANSITIONS}>
            {(t) => (
              <label class="flex items-center gap-2 text-sm">
                <input type="checkbox" class="checkbox checkbox-sm" checked={on().includes(t)} onChange={() => setOn((cur) => toggled(cur, t, TRANSITIONS))} />
                <span>{t}</span>
              </label>
            )}
          </For>
        </div>
        {shown("on")}
      </div>

      <div class="flex flex-col gap-1">
        <FieldRow label="When" info={ACTION_RULE_HINTS.when}>
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
          <span class="text-[11px] text-base-content/50" data-testid="action-rule-when-empty">Empty: every alarm transition sends.</span>
        </Show>
        <Show when={whenRefused()}>{(m) => <span class="text-[11px] text-error" data-testid="action-rule-when-refused">{m()}</span>}</Show>
      </div>

      <div class="flex flex-col gap-1.5">
        <Eyebrow label="Send to" hint={ACTION_RULE_HINTS.targets} />
        <Show
          when={canReadTargets()}
          fallback={
            <input
              class="input input-bordered w-full font-data"
              aria-label="Send to"
              value={targets().join(", ")}
              placeholder="ops-chat, on-call-page"
              onInput={(e) => setTargets(e.currentTarget.value.split(/[\s,]+/).filter(Boolean))}
            />
          }
        >
          <Show when={targetOptions().length > 0} fallback={<span class="text-[12px] text-base-content/50">No targets yet: make one on Targets first.</span>}>
            <div class="flex flex-wrap gap-x-4 gap-y-2" role="group" aria-label="Send to">
              <For each={targetOptions()}>
                {(t) => (
                  <label class="flex items-center gap-2 text-sm">
                    <input type="checkbox" class="checkbox checkbox-sm" checked={targets().includes(t)} onChange={() => setTargets((cur) => toggled(cur, t, targetOptions()))} />
                    <span class="font-data">{t}</span>
                  </label>
                )}
              </For>
            </div>
          </Show>
        </Show>
        {shown("targets")}
      </div>

      <FieldRow label="Notification type" info={ACTION_RULE_HINTS.type}>
        <select ref={bindSelectValue(type, typeOptions)} class="select select-bordered w-full" onChange={(e) => setType(e.currentTarget.value)}>
          <option value="">The official message for the transition</option>
          <For each={typeOptions()}>{(t) => <option value={t.name}>{entityLabel(t) === t.name ? t.name : `${entityLabel(t)} (${t.name})`}</option>}</For>
        </select>
      </FieldRow>

      <div class="grid gap-3 sm:grid-cols-2">
        <FieldRow label="Members may opt out" info={ACTION_RULE_HINTS.optOut}>
          <input type="checkbox" class="toggle toggle-sm" checked={optOut()} onChange={(e) => setOptOut(e.currentTarget.checked)} />
        </FieldRow>
        <FieldRow label="Enabled" info={ACTION_RULE_HINTS.enabled}>
          <input type="checkbox" class="toggle toggle-sm" checked={enabled()} onChange={(e) => setEnabled(e.currentTarget.checked)} />
        </FieldRow>
      </div>
    </form>
  );
}
