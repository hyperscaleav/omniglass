import { Show, createEffect, createSignal, on, type JSX } from "solid-js";
import { useQuery, useQueryClient } from "@tanstack/solid-query";
import FlatList, { type FlatColumn } from "../components/FlatList";
import KVStacked from "../components/KVStacked";
import { useFormActions } from "../lib/formactions";
import { Plus } from "../components/icons";
import {
  type EventTypeRow,
  EVENT_TYPES_KEY,
  listEventTypes,
  createEventType,
  updateEventType,
  deleteEventType,
} from "../lib/event_types";
import { useMe, can } from "../lib/auth";
import { describeError } from "../lib/format";
import { type BladeDef, useBlades, useBladeEdit } from "../lib/blades";

// Event Types: the occurrence-key catalog (Catalog > Event Types), the twin of the
// Properties catalog. An event type names a discrete happening (call.started,
// syslog.line) that an occurrence is typed by. Official (seed-owned) event types are
// read-only; custom ones are operator-created. Estate-wide reference data, not scoped.

function originBadge(official: boolean): JSX.Element {
  return official
    ? <span class="badge badge-ghost badge-sm">official</span>
    : <span class="badge badge-outline badge-sm">custom</span>;
}

const columns: FlatColumn<EventTypeRow>[] = [
  { key: "name", label: "Key", sortVal: (r) => r.name, cell: (r) => <span class="font-data font-semibold">{r.name}</span> },
  { key: "display_name", label: "Label", sortVal: (r) => r.display_name ?? "", cell: (r) => <span>{r.display_name}</span> },
  { key: "official", label: "Origin", width: "100px", sortVal: (r) => String(r.official), cell: (r) => originBadge(r.official) },
];

export default function EventTypes(): JSX.Element {
  const me = useMe();
  const eventTypes = useQuery(() => ({ queryKey: EVENT_TYPES_KEY, queryFn: listEventTypes }));
  const rows = () => (eventTypes.data ?? []).slice().sort((a, b) => a.name.localeCompare(b.name));
  const canCreate = () => can(me.data, "event_type", "create");

  return (
    <div class="flex min-h-full flex-col gap-4">
      <FlatList<EventTypeRow>
        config={{
          entity: { name: "event type", plural: "event types" },
          rows,
          loading: () => eventTypes.isPending,
          error: () => eventTypes.error,
          filterKeys: [
            { key: "name", type: "string", hint: "substring", get: (r) => `${r.name} ${r.display_name ?? ""}`, values: () => [] },
            { key: "official", type: "string", hint: "exact", get: (r) => (r.official ? "official" : "custom"), values: () => ["official", "custom"] },
          ],
          filterPlaceholder: "filter event types by name, label…",
          columns,
          empty: "No event types.",
          rowId: (r) => r.name,
          blades: { registry: { event_type: eventTypeBlade }, rootKind: "event_type" },
          create: canCreate()
            ? { label: "New event type", can: canCreate, body: (ctx) => <CreateEventTypeForm onCreated={ctx.close} /> }
            : undefined,
        }}
      />
    </div>
  );
}

// eventTypeBlade renders one event type on the shared blade stack. The title is the
// mono key; official event types are read-only (no pencil, no delete).
export const eventTypeBlade: BladeDef = {
  Title: (p) => <span class="font-data">{p.id}</span>,
  Body: (p) => <EventTypeBladeBody name={p.id} />,
};

function useEventTypeRow(name: string): () => EventTypeRow | undefined {
  const eventTypes = useQuery(() => ({ queryKey: EVENT_TYPES_KEY, queryFn: listEventTypes }));
  return () => (eventTypes.data ?? []).find((r) => r.name === name);
}

function EventTypeBladeBody(p: { name: string }): JSX.Element {
  const qc = useQueryClient();
  const me = useMe();
  const blades = useBlades();
  const edit = useBladeEdit();
  const row = useEventTypeRow(p.name);
  const [err, setErr] = createSignal<string | null>(null);
  const [displayName, setDisplayName] = createSignal("");
  const [description, setDescription] = createSignal("");

  createEffect(on(edit.editing, (editing) => {
    if (!editing) return;
    const r = row();
    setDisplayName(r?.display_name ?? "");
    setDescription(r?.description ?? "");
    setErr(null);
  }));

  async function removeEventType() {
    const r = row();
    if (!r) return;
    if (!confirm(`Delete event type "${r.name}"?`)) return;
    setErr(null);
    try {
      await deleteEventType(r.name);
      blades.close();
      await qc.invalidateQueries({ queryKey: EVENT_TYPES_KEY });
    } catch (e) {
      setErr(describeError(e));
    }
  }

  async function save() {
    const r = row();
    if (!r) return;
    setErr(null);
    try {
      await updateEventType(r.name, { display_name: displayName(), description: description() });
      await qc.invalidateQueries({ queryKey: EVENT_TYPES_KEY });
    } catch (e) {
      setErr(describeError(e));
      throw e; // keep the blade in edit mode on failure
    }
  }

  edit.bind({
    editable: () => !!row() && !row()!.official && can(me.data, "event_type", "update"),
    save,
    destructive: () =>
      row() && !row()!.official && can(me.data, "event_type", "delete")
        ? { label: "Delete", tone: "danger", onClick: removeEventType }
        : undefined,
  });

  return (
    <Show when={row()} fallback={<p class="text-sm text-base-content/50">Event type not found.</p>}>
      {(r) => (
        <div class="flex flex-col gap-4">
          <Show when={err()}>
            <div role="alert" class="alert alert-error alert-soft text-sm"><span>{err()}</span></div>
          </Show>
          <div class="grid grid-cols-2 gap-3 text-sm">
            <KVStacked label="Origin" value={originBadge(r().official)} />
          </div>
          <div class="flex flex-col gap-1.5">
            <span class="eyebrow">Display name</span>
            <Show when={edit.editing()} fallback={<div class="input input-bordered flex items-center text-sm">{r().display_name}</div>}>
              <input class="input input-bordered w-full" value={displayName()} onInput={(e) => setDisplayName(e.currentTarget.value)} />
            </Show>
          </div>
          <div class="flex flex-col gap-1.5">
            <span class="eyebrow">Description</span>
            <Show when={edit.editing()} fallback={<div class="input input-bordered flex items-center text-sm">{r().description}</div>}>
              <input class="input input-bordered w-full" value={description()} onInput={(e) => setDescription(e.currentTarget.value)} />
            </Show>
          </div>
          <Show when={r().payload_schema != null}>
            <div class="flex flex-col gap-1.5">
              <span class="eyebrow">Payload schema (JSON Schema)</span>
              <pre class="overflow-x-auto rounded-box border border-base-300 bg-base-200 p-2.5 font-data text-xs">{JSON.stringify(r().payload_schema, null, 2)}</pre>
              <span class="text-[11px] text-base-content/40">Editing the schema is a follow-up; set it via the API for now.</span>
            </div>
          </Show>
          <Show when={r().official}>
            <div role="alert" class="alert alert-soft text-sm"><span>Seed-owned, read-only.</span></div>
          </Show>
        </div>
      )}
    </Show>
  );
}

// CreateEventTypeForm: register a custom event type. Only the name is required.
export function CreateEventTypeForm(p: { onCreated: (name: string) => void }): JSX.Element {
  const qc = useQueryClient();
  const [name, setName] = createSignal("");
  const [displayName, setDisplayName] = createSignal("");
  const [description, setDescription] = createSignal("");
  const [busy, setBusy] = createSignal(false);
  const [formErr, setFormErr] = createSignal<string | null>(null);

  useFormActions().bind({
    submitLabel: "Create event type",
    submitIcon: Plus,
    submit: () => void submit(),
    busy,
    disabled: () => !name().trim(),
  });

  async function submit() {
    setBusy(true);
    setFormErr(null);
    try {
      await createEventType({
        name: name().trim(),
        display_name: displayName().trim() || undefined,
        description: description().trim() || undefined,
      });
      await qc.invalidateQueries({ queryKey: EVENT_TYPES_KEY });
      p.onCreated(name().trim());
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
      <Field label="Key" hint="A lowercase, dot-hierarchied name, e.g. call.started or cable.unplugged.">
        <input class="input input-bordered w-full font-data" value={name()} placeholder="call.started" onInput={(e) => setName(e.currentTarget.value)} />
      </Field>
      <Field label="Display name">
        <input class="input input-bordered w-full" value={displayName()} placeholder="Call started" onInput={(e) => setDisplayName(e.currentTarget.value)} />
      </Field>
      <Field label="Description">
        <input class="input input-bordered w-full" value={description()} onInput={(e) => setDescription(e.currentTarget.value)} />
      </Field>
    </form>
  );
}

function Field(p: { label: string; hint?: string; children: JSX.Element }): JSX.Element {
  return (
    <label class="flex flex-col gap-1">
      <span class="text-[12px] font-medium text-base-content/70">{p.label}</span>
      {p.children}
      <Show when={p.hint}><span class="text-[11px] text-base-content/40">{p.hint}</span></Show>
    </label>
  );
}
