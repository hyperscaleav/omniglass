import { For, Show, createMemo, createSignal, type JSX } from "solid-js";
import { useQuery, useQueryClient } from "@tanstack/solid-query";
import FlatList, { type FlatColumn } from "../components/FlatList";
import BladeTitle from "../components/BladeTitle";
import BladeField from "../components/BladeField";
import FieldRow from "../components/FieldRow";
import KVStacked from "../components/KVStacked";
import InfoTip from "../components/InfoTip";
import Button from "../components/Button";
import { identityColumn } from "../components/IdentityCell";
import { Plus } from "../components/icons";
import { entityLabel } from "../lib/entities";
import {
  type NotificationPreview,
  type NotificationTypeRow,
  NOTIFICATION_TYPES_KEY,
  createNotificationType,
  deleteNotificationType,
  listNotificationTypes,
  previewNotificationType,
  updateNotificationType,
} from "../lib/notification_types";
import { COMPONENTS_KEY, listComponents, type Component } from "../lib/components";
import { useMe, can } from "../lib/auth";
import { registryLock } from "../lib/catalog";
import { describeError } from "../lib/format";
import { useFormActions } from "../lib/formactions";
import { bindSelectValue } from "../lib/selectvalue";
import { type BladeDef, useBlades, useBladeEdit } from "../lib/blades";

// Notifications (Catalog > Actions > Notifications, #618): the outbound message
// registry, the peer of Commands. A notification type says WHAT a message about
// an alarm says, as a one-line subject and a body text, each a template in the
// same sandbox a label rule runs in, reading the alarm facts. The blade previews
// a type (or the draft being edited) against a real alarm through the server's
// own engine, so an author sees exactly what a receiver would get and which
// facts a template can read. When and where a message is sent is an action
// rule's and a target's, not this page's.

export const NOTIFICATION_HINTS = {
  subject: "One line a receiver reads first: a chat message's title, a page's summary. Whitespace collapses to single spaces.",
  text: "The body. Lines are kept. Reads the same facts as the subject; a fact the alarm does not have renders empty.",
  preview: "Renders the templates against a component's most recent alarm, through the same engine a delivery uses. Writes nothing.",
  transition: "Which edge to render for. A raised and a cleared message usually say different things.",
};

function originBadge(official: boolean): JSX.Element {
  return official
    ? <span class="badge badge-ghost badge-sm">official</span>
    : <span class="badge badge-outline badge-sm">custom</span>;
}

const columns: FlatColumn<NotificationTypeRow>[] = [
  identityColumn<NotificationTypeRow>(),
  { key: "subject", label: "Subject", cell: (r) => <span class="font-data text-[12px] text-base-content/70">{r.subject}</span> },
  { key: "official", label: "Origin", width: "100px", sortVal: (r) => String(r.official), cell: (r) => originBadge(r.official) },
];

export default function Notifications(): JSX.Element {
  const me = useMe();
  const types = useQuery(() => ({ queryKey: NOTIFICATION_TYPES_KEY, queryFn: listNotificationTypes }));
  const rows = () => (types.data ?? []).slice().sort((a, b) => a.name.localeCompare(b.name));
  const canCreate = () => can(me.data, "notification_type", "create");

  return (
    <div class="flex min-h-full flex-col gap-4">
      <FlatList<NotificationTypeRow>
        config={{
          entity: { name: "notification type", plural: "notification types" },
          rows,
          loading: () => types.isPending,
          error: () => types.error,
          filterKeys: [
            { key: "name", type: "string", hint: "substring", get: (r) => `${entityLabel(r)} ${r.name}`, values: () => [] },
            { key: "official", type: "string", hint: "exact", get: (r) => (r.official ? "official" : "custom"), values: () => ["official", "custom"] },
          ],
          filterPlaceholder: "filter notification types by name, label…",
          columns,
          empty: "No notification types. A notification type is what a message about an alarm says; create one to send something other than the official raised and cleared messages.",
          rowId: (r) => r.name,
          blades: { registry: { notification_type: notificationTypeBlade }, rootKind: "notification_type" },
          create: canCreate()
            ? { label: "New notification type", can: canCreate, body: (ctx) => <CreateNotificationTypeForm onCreated={ctx.select} /> }
            : undefined,
        }}
      />
    </div>
  );
}

export const notificationTypeBlade: BladeDef = {
  Title: (p) => <NotificationTypeBladeTitle name={p.id} />,
  Body: (p) => <NotificationTypeBladeBody name={p.id} />,
};

function useNotificationTypeRow(name: string): () => NotificationTypeRow | undefined {
  const types = useQuery(() => ({ queryKey: NOTIFICATION_TYPES_KEY, queryFn: listNotificationTypes }));
  return () => (types.data ?? []).find((r) => r.name === name);
}

function NotificationTypeBladeTitle(p: { name: string }): JSX.Element {
  const row = useNotificationTypeRow(p.name);
  return <BladeTitle row={row} fallback={p.name} />;
}

function NotificationTypeBladeBody(p: { name: string }): JSX.Element {
  const qc = useQueryClient();
  const me = useMe();
  const blades = useBlades();
  const edit = useBladeEdit();
  const row = useNotificationTypeRow(p.name);
  const [err, setErr] = createSignal<string | null>(null);
  const [label, setLabel] = createSignal("");
  const [description, setDescription] = createSignal("");
  const [subject, setSubject] = createSignal("");
  const [text, setText] = createSignal("");

  const seedDrafts = () => {
    const r = row();
    setLabel(r?.label ?? "");
    setDescription(r?.description ?? "");
    setSubject(r?.subject ?? "");
    setText(r?.text ?? "");
    setErr(null);
  };

  async function remove() {
    const r = row();
    if (!r) return;
    if (!confirm(`Delete notification type "${r.name}"?`)) return;
    setErr(null);
    try {
      await deleteNotificationType(r.name);
      blades.close();
      await qc.invalidateQueries({ queryKey: NOTIFICATION_TYPES_KEY });
    } catch (e) {
      setErr(describeError(e));
    }
  }

  async function save() {
    const r = row();
    if (!r) return;
    setErr(null);
    try {
      await updateNotificationType(r.name, { label: label(), description: description(), subject: subject(), text: text() });
      await qc.invalidateQueries({ queryKey: NOTIFICATION_TYPES_KEY });
    } catch (e) {
      setErr(describeError(e));
      throw e;
    }
  }

  edit.bind({
    editable: () => !!row() && !row()!.official && can(me.data, "notification_type", "update"),
    seed: seedDrafts,
    save,
    destructive: () =>
      row() && !row()!.official && can(me.data, "notification_type", "delete")
        ? { label: "Delete", tone: "danger", onClick: remove }
        : undefined,
    locked: () => registryLock(row(), me.data, "notification_type"),
  });

  // The preview renders the drafts while editing and the stored templates
  // otherwise, so what it shows is always what Save would store.
  const drafts = () => (edit.editing() ? { subject: subject(), text: text() } : {});

  return (
    <Show when={row()} fallback={<p class="text-sm text-base-content/50">Notification type not found.</p>}>
      {(r) => (
        <div class="flex flex-col gap-4">
          <Show when={err()}>
            <div role="alert" class="alert alert-error alert-soft text-sm"><span>{err()}</span></div>
          </Show>
          <div class="grid grid-cols-2 gap-3 text-sm">
            <KVStacked bind="name" value={<span class="font-data">{r().name}</span>} />
            <KVStacked label="Origin" value={originBadge(r().official)} />
          </div>
          <BladeField bind="label" value={() => r().label ?? ""} draft={label} onInput={setLabel} />
          <BladeField label="Description" multiline value={() => r().description ?? ""} draft={description} onInput={setDescription} />
          <BladeField label="Subject" info={NOTIFICATION_HINTS.subject} mono value={() => r().subject} draft={subject} onInput={setSubject} />
          <BladeField label="Text" info={NOTIFICATION_HINTS.text} multiline mono rows={6} value={() => r().text} draft={text} onInput={setText} />
          <PreviewPanel type={r().name} drafts={drafts} />
        </div>
      )}
    </Show>
  );
}

// componentOption names a component in the preview's picker by its label and,
// when the label is not its name, its name too: generated labels repeat across
// a fleet ("DSP"), and the picker must tell two apart.
export function componentOption(c: Pick<Component, "name" | "label">): string {
  const label = entityLabel(c);
  return label === c.name ? c.name : `${label} (${c.name})`;
}

// PreviewPanel renders a type against a real alarm through the server's engine:
// pick a component (its most recent alarm is the one rendered) and the edge.
// It is the surface that teaches what a template sees: the rendered message
// beside every fact the templates could have read.
export function PreviewPanel(p: { type: string; drafts: () => { subject?: string; text?: string } }): JSX.Element {
  const components = useQuery(() => ({ queryKey: COMPONENTS_KEY, queryFn: listComponents }));
  const options = createMemo(() => [...(components.data ?? [])].sort((a, b) => entityLabel(a).localeCompare(entityLabel(b))) as Component[]);
  const [component, setComponent] = createSignal("");
  const [transition, setTransition] = createSignal<"" | "raised" | "cleared">("");
  const [result, setResult] = createSignal<NotificationPreview | null>(null);
  const [err, setErr] = createSignal<string | null>(null);
  const [busy, setBusy] = createSignal(false);
  let resultEl: HTMLDivElement | undefined;

  async function run() {
    if (!component()) return;
    setBusy(true);
    setErr(null);
    try {
      setResult(await previewNotificationType(p.type, { component: component(), transition: transition() || undefined, ...p.drafts() }));
      // The result renders below the controls, often below the blade's fold
      // while editing: bring it into view, since it is what was asked for.
      requestAnimationFrame(() => resultEl?.scrollIntoView?.({ block: "nearest", behavior: "smooth" }));
    } catch (e) {
      setResult(null);
      setErr(describeError(e));
    } finally {
      setBusy(false);
    }
  }

  return (
    <section class="flex flex-col gap-3 rounded-box border border-base-300 p-3" aria-label="Preview">
      <span class="flex items-center gap-1.5">
        <span class="eyebrow">Preview</span>
        <InfoTip text={NOTIFICATION_HINTS.preview} label="Preview" />
      </span>
      <div class="grid grid-cols-2 gap-3">
        <FieldRow label="Component">
          <select ref={bindSelectValue(component, options)} class="select select-bordered select-sm w-full" onChange={(e) => setComponent(e.currentTarget.value)}>
            <option value="">Choose a component</option>
            <For each={options()}>{(c) => <option value={c.id}>{componentOption(c)}</option>}</For>
          </select>
        </FieldRow>
        <FieldRow label="Transition" info={NOTIFICATION_HINTS.transition}>
          <select class="select select-bordered select-sm w-full" value={transition()} onChange={(e) => setTransition(e.currentTarget.value as "" | "raised" | "cleared")}>
            <option value="">As the alarm stands</option>
            <option value="raised">Raised</option>
            <option value="cleared">Cleared</option>
          </select>
        </FieldRow>
      </div>
      <div>
        <Button intent="quiet" size="sm" loading={busy()} disabled={!component()} onClick={() => void run()}>
          Preview
        </Button>
      </div>
      <Show when={err()}>
        <div role="alert" class="alert alert-warning alert-soft text-sm"><span>{err()}</span></div>
      </Show>
      <Show when={result()}>
        {(res) => (
          <div ref={resultEl} class="flex flex-col gap-3">
            <KVStacked label="Rendered subject" value={<span class="font-data text-[12px]">{res().subject}</span>} />
            <KVStacked label="Rendered text" value={<pre class="whitespace-pre-wrap font-data text-[12px]">{res().text}</pre>} />
            <div class="flex flex-col gap-1">
              <span class="eyebrow">Facts a template reads ({res().transition})</span>
              <table class="table table-xs">
                <tbody>
                  <For each={res().facts ?? []}>
                    {(f) => (
                      <tr>
                        <td class="font-data text-[11px] text-base-content/60">{`.${f.name}`}</td>
                        <td class="font-data text-[11px]">{f.value || <span class="text-base-content/30">empty</span>}</td>
                      </tr>
                    )}
                  </For>
                </tbody>
              </table>
            </div>
          </div>
        )}
      </Show>
    </section>
  );
}

// CreateNotificationTypeForm registers an operator type. Both templates start
// from the official raised message's shape, so an author edits a working
// template rather than facing two empty boxes.
export function CreateNotificationTypeForm(p: { onCreated: (r: NotificationTypeRow) => void }): JSX.Element {
  const qc = useQueryClient();
  const [name, setName] = createSignal("");
  const [label, setLabel] = createSignal("");
  const [description, setDescription] = createSignal("");
  const [subject, setSubject] = createSignal("[{{.SeverityLabel}}] {{.ComponentLabel}}: {{.Message}}");
  const [text, setText] = createSignal("{{.ComponentLabel}}{{if .Place}} at {{.Place}}{{end}}: {{.Message}}");
  const [busy, setBusy] = createSignal(false);
  const [formErr, setFormErr] = createSignal<string | null>(null);

  useFormActions().bind({
    submitLabel: "Create notification type",
    submitIcon: Plus,
    submit: () => void submit(),
    busy,
    disabled: () => !name().trim(),
  });

  async function submit() {
    setBusy(true);
    setFormErr(null);
    try {
      const created = await createNotificationType({
        name: name().trim(),
        label: label().trim() || undefined,
        description: description().trim() || undefined,
        subject: subject(),
        text: text(),
      });
      await qc.invalidateQueries({ queryKey: NOTIFICATION_TYPES_KEY });
      p.onCreated(created);
    } catch (er) {
      setFormErr(describeError(er));
    } finally {
      setBusy(false);
    }
  }

  return (
    <form class="flex flex-col gap-4" onSubmit={(e) => { e.preventDefault(); void submit(); }}>
      <Show when={formErr()}>
        <div role="alert" class="alert alert-error alert-soft text-sm"><span>{formErr()}</span></div>
      </Show>
      <FieldRow bind="name" hint="A lowercase kebab name, e.g. page-oncall.">
        <input class="input input-bordered w-full font-data" value={name()} placeholder="page-oncall" onInput={(e) => setName(e.currentTarget.value)} />
      </FieldRow>
      <FieldRow bind="label">
        <input class="input input-bordered w-full" value={label()} placeholder="Page on-call" onInput={(e) => setLabel(e.currentTarget.value)} />
      </FieldRow>
      <FieldRow label="Description">
        <input class="input input-bordered w-full" value={description()} onInput={(e) => setDescription(e.currentTarget.value)} />
      </FieldRow>
      <FieldRow label="Subject" info={NOTIFICATION_HINTS.subject}>
        <input class="input input-bordered w-full font-data" value={subject()} onInput={(e) => setSubject(e.currentTarget.value)} />
      </FieldRow>
      <FieldRow label="Text" info={NOTIFICATION_HINTS.text}>
        <textarea class="textarea textarea-bordered w-full font-data" rows={5} value={text()} onInput={(e) => setText(e.currentTarget.value)} />
      </FieldRow>
    </form>
  );
}
