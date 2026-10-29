import { For, Match, Show, Switch, createContext, createMemo, createSignal, useContext, type JSX } from "solid-js";
import { useQuery, useQueryClient } from "@tanstack/solid-query";
import FlatList, { type FlatColumn } from "../components/FlatList";
import Drawer from "../components/Drawer";
import BladeTitle from "../components/BladeTitle";
import FieldRow from "../components/FieldRow";
import KVStacked from "../components/KVStacked";
import Eyebrow from "../components/Eyebrow";
import InfoTip from "../components/InfoTip";
import Button from "../components/Button";
import RecentNotifications from "../components/RecentNotifications";
import { identityColumn } from "../components/IdentityCell";
import { Eye, EyeOff, Pencil, Plus, Save, Trash, Zap } from "../components/icons";
import { createIdentity, entityLabel } from "../lib/entities";
import { useFormActions } from "../lib/formactions";
import { bindSelectValue } from "../lib/selectvalue";
import { can, canFleet, fleetView, useMe } from "../lib/auth";
import { registryLock } from "../lib/catalog";
import { describeError } from "../lib/format";
import { type BladeDef, createBladeController, useBlades, useBladeEdit } from "../lib/blades";
import { toggled, PILL_NEUTRAL } from "../lib/subscriptions";
import { SECRETS_KEY, listSecrets } from "../lib/secrets";
import { GROUPS_KEY, listGroups } from "../lib/groups";
import { PRINCIPALS_KEY, listPrincipals, principalName } from "../lib/principals";
import { ACTION_RULES_KEY, listActionRules, usedBy } from "../lib/action_rules";
import {
  KIND_WORDS,
  NOTIFICATION_TARGETS_KEY,
  PEOPLE_CHANNELS,
  TARGET_KINDS,
  createNotificationTarget,
  createTargetBody,
  deleteNotificationTarget,
  destination,
  emptyTargetDraft,
  listNotificationTargets,
  revealTargetUrl,
  targetDraftErrors,
  targetDraftFrom,
  testTarget,
  testVerdict,
  testable,
  updateNotificationTarget,
  updateTargetBody,
  type NotificationTarget,
  type PeopleChannel,
  type TargetDraft,
  type TargetKind,
  type TargetTest,
} from "../lib/notification_targets";

// Notification targets (Catalog > Actions > Targets, #1346): where a message
// about an alarm goes. A webhook (its URL masked on every read, revealed whole
// under an audit row of its own, and optionally signed with a platform
// secret), a shared mailbox, a group or one person. The blade tests a webhook
// or a mailbox through the server's egress contract and shows what the
// receiver answered, names the action rules that send to the target, and
// lists what was recently sent to it. Every route is admin-only
// (notification_target:*, all-scope), so the page judges each affordance on
// the caller's all-scope permissions (canFleet).

export const TARGET_HINTS = {
  kind: "Webhook posts a signed JSON envelope to a URL (a chat or paging tool). Email sends to fixed addresses through the install's mail relay. Group tells every member of a group, read at each alarm; Person tells one person. A person is told only about alarms on components they can read.",
  url: "Masked to its scheme and host on every read, since a chat webhook's URL is often itself a credential. Reveal shows it whole and is audited. A private address is refused until Settings > notification > egress_allow names it.",
  urlEdit: "Leave blank to keep the stored URL. A new one is judged against the egress allow list as it is saved.",
  secret: "A platform hmac-key signs each body (X-Omniglass-Signature); a bearer-token is presented as Authorization: Bearer. None sends unsigned. Binding one takes the right to reveal it.",
  addresses: "The mailbox addresses it sends to, separated by commas or new lines, through the install's mail relay.",
  group: "Every human member, read at each alarm transition: add someone to the group and they are told next time.",
  person: "The one person it tells.",
  channels: "Inbox puts an entry in each person's console inbox (the bell in the top bar). Email sends to each person's directory email.",
  enabled: "A disabled target is skipped: a rule sending to it queues nothing for it. It can still be tested.",
  test: "Sends one synthetic message (transition test, about no alarm) exactly as a delivery would be sent, and shows what the receiver answered. It records nothing but its audit row.",
  usedBy: "The action rules that send here. A target a rule still sends to cannot be deleted.",
  recent: "What was sent to this target lately, newest first, about components you can read.",
} as const;

function statusPill(enabled: boolean): JSX.Element {
  return enabled
    ? <span class="badge badge-soft badge-success badge-sm">enabled</span>
    : <span class={PILL_NEUTRAL}>disabled</span>;
}

// The edit Drawer is the page's: a blade's Edit closes the stack and opens it,
// and saving or closing lands back on the target's blade (the Rules page's
// model, since a form never stacks over a blade).
type TargetsUi = { openEdit: (name: string) => void };
const TargetsUiContext = createContext<TargetsUi>();

export default function NotificationTargets(): JSX.Element {
  const me = useMe();
  const qc = useQueryClient();
  const targets = useQuery(() => ({ queryKey: NOTIFICATION_TARGETS_KEY, queryFn: listNotificationTargets }));
  const rows = () => (targets.data ?? []).slice().sort((a, b) => a.name.localeCompare(b.name));
  const canCreate = () => canFleet(me.data, "notification_target", "create");
  const blades = createBladeController();
  const [editing, setEditing] = createSignal<string | null>(null);
  const editRow = () => (targets.data ?? []).find((t) => t.name === editing());
  const backToBlade = (name: string) => {
    setEditing(null);
    blades.push({ kind: "notification_target", id: name });
  };

  const columns: FlatColumn<NotificationTarget>[] = [
    identityColumn<NotificationTarget>(),
    { key: "kind", label: "Kind", width: "96px", sortVal: (t) => t.kind, cell: (t) => <span class="text-[12px]">{KIND_WORDS[t.kind]}</span> },
    {
      key: "destination",
      label: "Destination",
      width: "280px",
      sortVal: destination,
      cell: (t) => <span class="block truncate font-data text-[12px] text-base-content/70" title={destination(t)}>{destination(t)}</span>,
    },
    { key: "enabled", label: "Status", width: "96px", sortVal: (t) => String(t.enabled), cell: (t) => statusPill(t.enabled) },
  ];

  return (
    <TargetsUiContext.Provider value={{ openEdit: (name) => { blades.close(); setEditing(name); } }}>
      <div class="flex min-h-full flex-col gap-4">
        <FlatList<NotificationTarget>
          config={{
            entity: { name: "notification target", plural: "notification targets" },
            rows,
            loading: () => targets.isPending,
            error: () => targets.error,
            filterKeys: [
              { key: "name", type: "string", hint: "substring", get: (t) => `${entityLabel(t)} ${t.name}`, values: () => [] },
              { key: "kind", type: "string", hint: "exact", get: (t) => t.kind, values: () => [...TARGET_KINDS] },
              { key: "enabled", type: "string", hint: "exact", get: (t) => (t.enabled ? "enabled" : "disabled"), values: () => ["enabled", "disabled"] },
            ],
            filterPlaceholder: "filter targets by name, kind…",
            columns,
            empty: "No notification targets. A target is where a message about an alarm goes: a chat webhook, a mailbox, a group or a person.",
            rowId: (t) => t.name,
            blades: { registry: { notification_target: targetBlade }, rootKind: "notification_target", controller: blades },
            railExtra: () => (
              <span class="mr-1 inline-flex items-center gap-1.5 text-[12px] text-base-content/50">
                What a target is
                <InfoTip
                  label="targets"
                  text="A target says where a message about an alarm goes. An action rule says when to send, and to which targets; a notification type says what the message reads."
                />
              </span>
            ),
            create: canCreate()
              ? { label: "New target", can: canCreate, body: (ctx) => <TargetForm mode="create" onSaved={ctx.select} /> }
              : undefined,
          }}
        />
      </div>
      <Show when={editing() ? editRow() : undefined}>
        {(t) => (
          <Drawer open={true} onClose={() => backToBlade(t().name)} title={<span>Edit {entityLabel(t())}</span>}>
            <TargetForm
              mode="edit"
              target={t()}
              onSaved={async (saved) => {
                await qc.invalidateQueries({ queryKey: NOTIFICATION_TARGETS_KEY });
                backToBlade(saved.name);
              }}
            />
          </Drawer>
        )}
      </Show>
    </TargetsUiContext.Provider>
  );
}

function useTargetLookup(name: string): { row: () => NotificationTarget | undefined; loaded: () => boolean } {
  const targets = useQuery(() => ({ queryKey: NOTIFICATION_TARGETS_KEY, queryFn: listNotificationTargets }));
  return { row: () => (targets.data ?? []).find((t) => t.name === name), loaded: () => targets.isSuccess };
}

function TargetBladeTitle(p: { name: string }): JSX.Element {
  const row = useTargetLookup(p.name).row;
  return <BladeTitle row={row} fallback={p.name} />;
}

export const targetBlade: BladeDef = {
  Title: (p) => <TargetBladeTitle name={p.id} />,
  Body: (p) => <TargetBladeBody name={p.id} />,
};

function ChannelPills(p: { channels: string[] | null | undefined }): JSX.Element {
  return (
    <span class="flex flex-wrap gap-1">
      <For each={p.channels ?? []}>{(c) => <span class={`${PILL_NEUTRAL} font-data`}>{c}</span>}</For>
    </span>
  );
}

function TargetBladeBody(p: { name: string }): JSX.Element {
  const qc = useQueryClient();
  const me = useMe();
  const blades = useBlades();
  const edit = useBladeEdit();
  const ui = useContext(TargetsUiContext);
  const lookup = useTargetLookup(p.name);
  const row = lookup.row;
  const rules = useQuery(() => ({ queryKey: ACTION_RULES_KEY, queryFn: listActionRules, enabled: can(me.data, "rule", "read") }));
  const senders = createMemo(() => usedBy(rules.data ?? [], p.name));
  const [err, setErr] = createSignal<string | null>(null);
  const canUpdate = () => canFleet(me.data, "notification_target", "update");
  const canDelete = () => canFleet(me.data, "notification_target", "delete");

  async function toggleEnabled(t: NotificationTarget) {
    setErr(null);
    try {
      await updateNotificationTarget(t.name, { enabled: !t.enabled });
      await qc.invalidateQueries({ queryKey: NOTIFICATION_TARGETS_KEY });
    } catch (e) {
      setErr(describeError(e));
    }
  }

  async function remove(t: NotificationTarget) {
    if (!confirm(`Delete target "${t.name}"? What was already sent to it stays on each alarm's timeline.`)) return;
    setErr(null);
    try {
      await deleteNotificationTarget(t.name);
      blades.close();
      await qc.invalidateQueries({ queryKey: NOTIFICATION_TARGETS_KEY });
    } catch (e) {
      setErr(describeError(e));
    }
  }

  edit.bind({
    primary: () => {
      const t = row();
      return t && canUpdate() && ui ? { label: "Edit", icon: <Pencil size={15} />, onClick: () => ui.openEdit(t.name) } : undefined;
    },
    destructive: () => {
      const t = row();
      if (!t || !canUpdate()) return undefined;
      return t.enabled
        ? { label: "Disable", tone: "warn", onClick: () => void toggleEnabled(t) }
        : { label: "Enable", tone: "ok", onClick: () => void toggleEnabled(t) };
    },
    secondary: () => {
      const t = row();
      if (!t || !canDelete()) return [];
      return [{ label: "Delete", icon: <Trash size={15} />, tone: "danger", onClick: () => void remove(t) }];
    },
    locked: () => {
      const t = row();
      if (!t || canUpdate()) return null;
      // A target carries no official flag: judged on the permission arm alone.
      return registryLock({ official: false }, fleetView(me.data), "notification_target");
    },
  });

  return (
    <Show
      when={row()}
      fallback={
        <Show when={lookup.loaded()} fallback={<p class="text-sm text-base-content/40">Loading…</p>}>
          <p class="text-sm text-base-content/50">Target not found.</p>
        </Show>
      }
    >
      {(t) => (
        <div class="flex flex-col gap-5">
          <Show when={err()}>
            <div role="alert" class="alert alert-error alert-soft text-sm"><span>{err()}</span></div>
          </Show>
          <div class="grid grid-cols-2 gap-3 text-sm">
            <KVStacked bind="name" value={<span class="font-data">{t().name}</span>} />
            <KVStacked label="Kind" hint={TARGET_HINTS.kind} value={<span data-testid="target-kind">{KIND_WORDS[t().kind]}</span>} />
            <KVStacked label="Status" hint={TARGET_HINTS.enabled} value={statusPill(t().enabled)} />
            <Show when={t().kind === "webhook"}>
              <KVStacked
                label="Signed with"
                hint={TARGET_HINTS.secret}
                value={
                  <Show when={t().secret} fallback={<span class="text-base-content/50">unsigned</span>}>
                    <span class="font-data">{t().secret}</span> <span class="text-[11px] text-base-content/50">({t().auth})</span>
                  </Show>
                }
              />
            </Show>
          </div>

          <section class="flex flex-col gap-2" aria-label="Destination">
            <Switch>
              <Match when={t().kind === "webhook"}>
                <Eyebrow label="URL" hint={TARGET_HINTS.url} />
                <RevealedUrl target={t()} />
              </Match>
              <Match when={t().kind === "email"}>
                <Eyebrow label="Addresses" hint={TARGET_HINTS.addresses} />
                <span class="flex flex-wrap gap-1">
                  <For each={t().addresses ?? []}>{(a) => <span class={`${PILL_NEUTRAL} font-data`}>{a}</span>}</For>
                </span>
              </Match>
              <Match when={t().kind === "principal_group" || t().kind === "principal"}>
                <div class="grid grid-cols-2 gap-3 text-sm">
                  <KVStacked
                    label={t().kind === "principal" ? "Person" : "Group"}
                    hint={t().kind === "principal" ? TARGET_HINTS.person : TARGET_HINTS.group}
                    value={<span class="font-data">{(t().kind === "principal" ? t().principal : t().principal_group) || "withheld"}</span>}
                  />
                  <KVStacked label="Channels" hint={TARGET_HINTS.channels} value={<ChannelPills channels={t().channels} />} />
                </div>
              </Match>
            </Switch>
          </section>

          <Show when={testable(t().kind) && canFleet(me.data, "notification_target", "test")}>
            <TestPanel target={t()} />
          </Show>

          <Show when={can(me.data, "rule", "read")}>
            <section class="flex flex-col gap-2" aria-label="Used by">
              <Eyebrow label={`Used by${rules.data ? ` (${senders().length})` : ""}`} hint={TARGET_HINTS.usedBy} />
              <Show when={senders().length > 0} fallback={<span class="text-[12px] text-base-content/40">No action rule sends here.</span>}>
                <span class="flex flex-wrap gap-1.5" data-testid="target-used-by">
                  <For each={senders()}>{(r) => <span class="badge badge-ghost badge-sm" title={r.name}>{entityLabel(r)}</span>}</For>
                </span>
              </Show>
            </section>
          </Show>

          <Show when={can(me.data, "notification", "read")}>
            <RecentNotifications target={t().name} hint={TARGET_HINTS.recent} />
          </Show>
        </div>
      )}
    </Show>
  );
}

// RevealedUrl shows the masked URL and, for a caller who may, reveals it whole:
// one audited :reveal per click, never on render. Hide drops it from memory.
function RevealedUrl(p: { target: NotificationTarget }): JSX.Element {
  const me = useMe();
  const [whole, setWhole] = createSignal<string | null>(null);
  const [busy, setBusy] = createSignal(false);
  const [err, setErr] = createSignal<string | null>(null);
  const canReveal = () => canFleet(me.data, "notification_target", "reveal");

  async function reveal() {
    setBusy(true);
    setErr(null);
    try {
      setWhole(await revealTargetUrl(p.target.name));
    } catch (e) {
      setErr(describeError(e));
    }
    setBusy(false);
  }

  return (
    <div class="flex flex-col gap-1">
      <div class="flex min-w-0 items-center gap-2">
        <code class="min-w-0 flex-1 break-all font-data text-[12px]" data-testid="target-url">{whole() ?? p.target.url}</code>
        <Show when={canReveal()}>
          <Show
            when={whole() === null}
            fallback={<Button size="xs" icon={EyeOff} onClick={() => setWhole(null)}>Hide</Button>}
          >
            <Button size="xs" icon={Eye} loading={busy()} onClick={() => void reveal()} title="Show the whole URL (audited)">Reveal</Button>
          </Show>
        </Show>
      </div>
      <Show when={err()}>{(m) => <span role="alert" class="text-[11px] text-error">{m()}</span>}</Show>
    </div>
  );
}

// TestPanel sends one synthetic message and shows what the receiver said: its
// status and, when it did not take the message, the reason (a refused
// address, an answer that was not 2xx, a certificate that does not verify).
function TestPanel(p: { target: NotificationTarget }): JSX.Element {
  const [result, setResult] = createSignal<TargetTest | null>(null);
  const [err, setErr] = createSignal<string | null>(null);
  const [running, setRunning] = createSignal(false);

  async function run() {
    setRunning(true);
    setErr(null);
    try {
      setResult(await testTarget(p.target.name));
    } catch (e) {
      setResult(null);
      setErr(describeError(e));
    }
    setRunning(false);
  }

  return (
    <section class="flex flex-col gap-2 rounded-box border border-base-300 p-3" data-testid="target-test">
      <Eyebrow
        label="Test"
        hint={TARGET_HINTS.test}
        right={
          <Button size="xs" icon={Zap} loading={running()} onClick={() => void run()}>
            {result() ? "Send again" : "Send a test"}
          </Button>
        }
      />
      <Show when={result()}>
        {(r) => (
          <div
            role="status"
            class="alert alert-soft text-sm"
            classList={{ "alert-success": r().ok, "alert-warning": !r().ok }}
            data-testid="target-test-result"
            data-ok={String(r().ok)}
          >
            <span class="min-w-0 [overflow-wrap:anywhere]">{testVerdict(r())}</span>
          </div>
        )}
      </Show>
      <Show when={err()}>{(m) => <span role="alert" class="text-[11px] text-error">{m()}</span>}</Show>
    </section>
  );
}

// ---- the form ----------------------------------------------------------------

// TargetForm creates a target or edits one; a Drawer hosts it and draws its
// rail. The kind decides the fields; kind, name and who a people target names
// are fixed at create, so an edit shows them as facts. Every rule is judged in
// TypeScript (lib/notification_targets) and gates the submit; what the egress
// contract admits is the server's call, said as its refusal.
export function TargetForm(p: { mode: "create" | "edit"; target?: NotificationTarget; onSaved: (t: NotificationTarget) => void }): JSX.Element {
  const qc = useQueryClient();
  const me = useMe();
  const start: TargetDraft = p.target ? targetDraftFrom(p.target) : emptyTargetDraft();
  const ident = createIdentity({ display: start.label, name: p.target?.name });
  const [kind, setKind] = createSignal<TargetKind>(start.kind);
  const [url, setUrl] = createSignal(start.url);
  const [secret, setSecret] = createSignal(start.secret);
  const [addresses, setAddresses] = createSignal(start.addresses);
  const [group, setGroup] = createSignal(start.principalGroup);
  const [person, setPerson] = createSignal(start.principal);
  const [channels, setChannels] = createSignal<PeopleChannel[]>(start.channels);
  const [enabled, setEnabled] = createSignal(start.enabled);
  const [touched, setTouched] = createSignal(false);
  const [busy, setBusy] = createSignal(false);
  const [formErr, setFormErr] = createSignal<string | null>(null);
  const editing = p.mode === "edit";

  // The pickers' collections, each read only where its kind needs it and the
  // caller may read it. A signing secret is a platform hmac-key or
  // bearer-token; any other secret is refused by the route.
  const secrets = useQuery(() => ({ queryKey: SECRETS_KEY, queryFn: listSecrets, enabled: kind() === "webhook" && can(me.data, "secret", "read") }));
  const signing = createMemo(() =>
    (secrets.data ?? []).filter((s) => s.owner_kind === "platform" && (s.secret_type === "hmac-key" || s.secret_type === "bearer-token")).sort((a, b) => a.name.localeCompare(b.name)),
  );
  const groups = useQuery(() => ({ queryKey: GROUPS_KEY, queryFn: listGroups, enabled: !editing && kind() === "principal_group" }));
  const people = useQuery(() => ({
    queryKey: [...PRINCIPALS_KEY, "human"],
    queryFn: () => listPrincipals("human"),
    enabled: !editing && kind() === "principal",
  }));
  const humans = createMemo(() => (people.data ?? []).filter((x) => x.human?.username).sort((a, b) => a.human!.username.localeCompare(b.human!.username)));

  const draft = (): TargetDraft => ({
    name: ident.name(), label: ident.display(), kind: kind(), url: url(), secret: secret(), addresses: addresses(),
    principalGroup: group(), principal: person(), channels: channels(), enabled: enabled(),
  });
  const errors = createMemo(() => targetDraftErrors(draft(), editing));
  const valid = () => Object.keys(errors()).length === 0;
  // An error shows once the form is engaged (a name typed, a submit tried, or
  // an edit), so a fresh form opens with nothing in red.
  const shown = (k: keyof ReturnType<typeof targetDraftErrors>) => (
    <Show when={(touched() || !!ident.name() || editing) && errors()[k]}>
      {(m) => <span class="text-[11px] text-error">{m()}</span>}
    </Show>
  );

  useFormActions().bind({
    submitLabel: editing ? "Save target" : "Create target",
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
    try {
      const saved = editing
        ? await updateNotificationTarget(p.target!.name, updateTargetBody(draft(), p.target!))
        : await createNotificationTarget(createTargetBody(draft()));
      await qc.invalidateQueries({ queryKey: NOTIFICATION_TARGETS_KEY });
      p.onSaved(saved);
    } catch (e) {
      setFormErr(describeError(e));
    }
    setBusy(false);
  }

  // A function, so each kind's branch renders its own copy of the boxes.
  const ChannelBoxes = () => (
    <div class="flex flex-col gap-1.5">
      <Eyebrow label="Tell them by" hint={TARGET_HINTS.channels} />
      <div class="flex gap-4" role="group" aria-label="Tell them by">
        <For each={PEOPLE_CHANNELS}>
          {(c) => (
            <label class="flex items-center gap-2 text-sm">
              <input type="checkbox" class="checkbox checkbox-sm" checked={channels().includes(c)} onChange={() => setChannels((cur) => toggled(cur, c, PEOPLE_CHANNELS))} />
              <span>{c}</span>
            </label>
          )}
        </For>
      </div>
      {shown("channels")}
    </div>
  );

  return (
    <form class="flex flex-col gap-4" onSubmit={(e) => { e.preventDefault(); void submit(); }}>
      <Show when={formErr()}>
        <div role="alert" class="alert alert-error alert-soft text-sm" data-testid="target-form-error"><span>{formErr()}</span></div>
      </Show>
      <Show
        when={!editing}
        fallback={<KVStacked label="Kind" hint={TARGET_HINTS.kind} value={KIND_WORDS[p.target!.kind]} />}
      >
        <FieldRow label="Kind" info={TARGET_HINTS.kind}>
          <select class="select select-bordered w-full" value={kind()} onChange={(e) => setKind(e.currentTarget.value as TargetKind)} data-testid="target-kind-select">
            <For each={TARGET_KINDS}>{(k) => <option value={k}>{KIND_WORDS[k]}</option>}</For>
          </select>
        </FieldRow>
      </Show>
      <div class="grid gap-3 sm:grid-cols-2">
        <FieldRow bind="label" info="A friendly name for this target; optional.">
          <input class="input input-bordered w-full" value={ident.display()} onInput={(e) => ident.setDisplay(e.currentTarget.value)} placeholder="Ops chat" />
        </FieldRow>
        <Show
          when={!editing}
          fallback={<KVStacked bind="name" mono value={p.target!.name} hint="How a rule, the API and the CLI address this target; it cannot be changed." />}
        >
          <div class="flex flex-col gap-1">
            <FieldRow bind="name" info="How a rule, the API and the CLI address this target: lowercase letters, digits and hyphens.">
              <input class="input input-bordered w-full font-data" value={ident.name()} onInput={(e) => ident.setName(e.currentTarget.value)} placeholder="ops-chat" aria-required="true" />
            </FieldRow>
            {shown("name")}
          </div>
        </Show>
      </div>

      <Switch>
        <Match when={kind() === "webhook"}>
          <div class="flex flex-col gap-1">
            <FieldRow label="URL" info={editing ? TARGET_HINTS.urlEdit : TARGET_HINTS.url}>
              <input
                class="input input-bordered w-full font-data"
                value={url()}
                placeholder={editing ? p.target!.url ?? "" : "https://chat.example.com/hooks/ops"}
                onInput={(e) => setUrl(e.currentTarget.value)}
                aria-required={editing ? undefined : "true"}
                autocomplete="off"
              />
            </FieldRow>
            {shown("url")}
          </div>
          <FieldRow label="Signed with" info={TARGET_HINTS.secret}>
            <select ref={bindSelectValue(secret, signing)} class="select select-bordered w-full" onChange={(e) => setSecret(e.currentTarget.value)}>
              <option value="">None: send unsigned</option>
              <For each={signing()}>{(s) => <option value={s.name}>{`${entityLabel(s)} (${s.secret_type})`}</option>}</For>
            </select>
          </FieldRow>
        </Match>
        <Match when={kind() === "email"}>
          <div class="flex flex-col gap-1">
            <FieldRow label="Addresses" info={TARGET_HINTS.addresses}>
              <textarea class="textarea textarea-bordered w-full font-data" rows={3} value={addresses()} placeholder="ops@example.com" onInput={(e) => setAddresses(e.currentTarget.value)} aria-required="true" />
            </FieldRow>
            {shown("addresses")}
          </div>
        </Match>
        <Match when={kind() === "principal_group"}>
          <Show
            when={!editing}
            fallback={<KVStacked label="Group" hint={TARGET_HINTS.group} mono value={p.target!.principal_group || "withheld"} />}
          >
            <div class="flex flex-col gap-1">
              <FieldRow label="Group" info={TARGET_HINTS.group}>
                <select ref={bindSelectValue(group, () => groups.data)} class="select select-bordered w-full" onChange={(e) => setGroup(e.currentTarget.value)} aria-required="true" data-testid="target-group-select">
                  <option value="">Choose a group</option>
                  <For each={groups.data ?? []}>{(g) => <option value={g.name}>{entityLabel(g) === g.name ? g.name : `${entityLabel(g)} (${g.name})`}</option>}</For>
                </select>
              </FieldRow>
              {shown("principalGroup")}
            </div>
          </Show>
          <ChannelBoxes />
        </Match>
        <Match when={kind() === "principal"}>
          <Show
            when={!editing}
            fallback={<KVStacked label="Person" hint={TARGET_HINTS.person} mono value={p.target!.principal || "withheld"} />}
          >
            <div class="flex flex-col gap-1">
              <FieldRow label="Person" info={TARGET_HINTS.person}>
                <select ref={bindSelectValue(person, humans)} class="select select-bordered w-full" onChange={(e) => setPerson(e.currentTarget.value)} aria-required="true">
                  <option value="">Choose a person</option>
                  <For each={humans()}>{(h) => <option value={h.human!.username}>{principalName(h) === h.human!.username ? h.human!.username : `${principalName(h)} (${h.human!.username})`}</option>}</For>
                </select>
              </FieldRow>
              {shown("principal")}
            </div>
          </Show>
          <ChannelBoxes />
        </Match>
      </Switch>

      <FieldRow label="Enabled" info={TARGET_HINTS.enabled}>
        <input type="checkbox" class="toggle toggle-sm" checked={enabled()} onChange={(e) => setEnabled(e.currentTarget.checked)} />
      </FieldRow>
    </form>
  );
}
