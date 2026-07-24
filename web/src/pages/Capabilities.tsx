import { Show, createEffect, createMemo, createSignal, on, type JSX } from "solid-js";
import { useQuery, useQueryClient } from "@tanstack/solid-query";
import FlatList, { type FlatColumn } from "../components/FlatList";
import KVStacked from "../components/KVStacked";
import { useFormActions } from "../lib/formactions";
import { Plus } from "../components/icons";
import {
  type Capability,
  CAPABILITIES_KEY,
  listCapabilities,
  createCapability,
  updateCapability,
  deleteCapability,
} from "../lib/capabilities";
import { useMe, can } from "../lib/auth";
import { describeError } from "../lib/format";
import { type BladeDef, useBlades, useBladeEdit } from "../lib/blades";

// Capabilities: the capability registry (the capability picker on the product
// form), on the flat FlatList surface. A capability is addressed by its id (a
// kebab id, create-only); official (seed-owned) rows are read-only, same as the
// Types catalog's official rows: no Edit pencil, no Delete.

function officialBadge(official: boolean): JSX.Element {
  return official
    ? <span class="badge badge-ghost badge-sm">official</span>
    : <span class="badge badge-outline badge-sm">custom</span>;
}

const columns: FlatColumn<Capability>[] = [
  { key: "id", label: "Id", sortVal: (c) => c.id, cell: (c) => <span class="font-data font-semibold">{c.id}</span> },
  { key: "display_name", label: "Display name", sortVal: (c) => c.display_name, cell: (c) => <span>{c.display_name}</span> },
  { key: "official", label: "Origin", width: "100px", sortVal: (c) => String(c.official), cell: (c) => officialBadge(c.official) },
];

export default function Capabilities() {
  const me = useMe();
  const caps = useQuery(() => ({ queryKey: CAPABILITIES_KEY, queryFn: listCapabilities }));

  const rows = createMemo(() =>
    [...(caps.data ?? [])].sort((a, b) => a.display_name.localeCompare(b.display_name) || a.id.localeCompare(b.id)),
  );

  return (
    <FlatList<Capability>
      config={{
        entity: { name: "capability", plural: "capabilities" },
        rows,
        loading: () => caps.isPending,
        error: () => caps.error,
        filterKeys: [
          { key: "name", type: "string", hint: "substring", get: (c) => `${c.id} ${c.display_name}`, values: () => [] },
          { key: "official", type: "string", hint: "exact", get: (c) => (c.official ? "official" : "custom"), values: () => ["official", "custom"] },
        ],
        filterPlaceholder: "filter capabilities by id, name…",
        columns,
        empty: "No capabilities yet.",
        // Address a row by id: the write paths key on id, and it is globally unique.
        rowId: (c) => c.id,
        blades: { registry: { capability: capabilityBlade }, rootKind: "capability" },
        create: can(me.data, "capability", "create")
          ? { label: "New capability", can: () => can(me.data, "capability", "create"), body: (ctx) => <CreateCapabilityForm onCreated={ctx.close} /> }
          : undefined,
      }}
    />
  );
}

// capabilityBlade renders an id on the shared blade stack. An official row is
// read-only (no pencil, no destructive action); a custom row carries Edit +
// Delete.
export const capabilityBlade: BladeDef = {
  Title: (p) => <CapabilityBladeTitle id={p.id} />,
  Body: (p) => <CapabilityBladeBody id={p.id} />,
};

function useCapabilityRow(id: string): () => Capability | undefined {
  const caps = useQuery(() => ({ queryKey: CAPABILITIES_KEY, queryFn: listCapabilities }));
  return () => (caps.data ?? []).find((c) => c.id === id);
}

function CapabilityBladeTitle(p: { id: string }): JSX.Element {
  const row = useCapabilityRow(p.id);
  return <span class="font-data">{row()?.id ?? p.id}</span>;
}

function CapabilityBladeBody(p: { id: string }): JSX.Element {
  const qc = useQueryClient();
  const me = useMe();
  const blades = useBlades();
  const edit = useBladeEdit();
  const row = useCapabilityRow(p.id);
  const [err, setErr] = createSignal<string | null>(null);
  const [displayName, setDisplayName] = createSignal("");

  createEffect(on(edit.editing, (editing) => {
    if (!editing) return;
    const r = row();
    setDisplayName(r?.display_name ?? "");
    setErr(null);
  }));

  async function removeCapability() {
    const r = row();
    if (!r) return;
    if (!confirm(`Delete capability "${r.id}"?`)) return;
    setErr(null);
    try {
      await deleteCapability(r.id);
      blades.close();
      await qc.invalidateQueries({ queryKey: CAPABILITIES_KEY });
    } catch (e) {
      setErr(describeError(e));
    }
  }

  async function save() {
    const r = row();
    if (!r) return;
    setErr(null);
    try {
      await updateCapability(r.id, {
        display_name: displayName(),
      });
      await qc.invalidateQueries({ queryKey: CAPABILITIES_KEY });
    } catch (e) {
      setErr(describeError(e));
      throw e; // keep the blade in edit mode on failure
    }
  }

  edit.bind({
    editable: () => !!row() && !row()!.official && can(me.data, "capability", "update"),
    save,
    destructive: () =>
      row() && !row()!.official && can(me.data, "capability", "delete")
        ? { label: "Delete", tone: "danger", onClick: removeCapability }
        : undefined,
  });

  return (
    <Show when={row()} fallback={<p class="text-sm text-base-content/50">Capability not found.</p>}>
      {(r) => (
        <div class="flex flex-col gap-4">
          <Show when={err()}>
            <div role="alert" class="alert alert-error alert-soft text-sm"><span>{err()}</span></div>
          </Show>
          <div class="grid grid-cols-2 gap-3 text-sm">
            <KVStacked label="Id" value={<span class="font-data">{r().id}</span>} />
            <KVStacked label="Origin" value={officialBadge(r().official)} />
          </div>
          <div class="flex flex-col gap-1.5">
            <span class="eyebrow">Display name</span>
            <Show
              when={edit.editing()}
              fallback={<div class="input input-bordered flex items-center text-sm">{r().display_name}</div>}
            >
              <input class="input input-bordered w-full" value={displayName()} onInput={(e) => setDisplayName(e.currentTarget.value)} />
            </Show>
          </div>
          <Show when={r().official}>
            <div role="alert" class="alert alert-soft text-sm"><span>Seed-owned, read-only.</span></div>
          </Show>
        </div>
      )}
    </Show>
  );
}

// CreateCapabilityForm: name the id (a kebab id, immutable after creation) and
// set the display name.
export function CreateCapabilityForm(p: { onCreated: (id: string) => void }): JSX.Element {
  const qc = useQueryClient();
  const [id, setId] = createSignal("");
  const [displayName, setDisplayName] = createSignal("");
  const [busy, setBusy] = createSignal(false);
  const [formErr, setFormErr] = createSignal<string | null>(null);

  useFormActions().bind({
    submitLabel: "Create capability",
    submitIcon: Plus,
    submit: () => void submit(),
    busy,
    disabled: () => !id().trim() || !displayName().trim(),
  });

  async function submit() {
    setBusy(true);
    setFormErr(null);
    try {
      await createCapability({
        name: id().trim(),
        display_name: displayName().trim(),
      });
      await qc.invalidateQueries({ queryKey: CAPABILITIES_KEY });
      p.onCreated(id().trim());
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
      <Field label="Id" hint="A kebab id, e.g. microphone.">
        <input class="input input-bordered w-full font-data" value={id()} placeholder="microphone" onInput={(e) => setId(e.currentTarget.value)} />
      </Field>
      <Field label="Display name">
        <input class="input input-bordered w-full" value={displayName()} placeholder="Microphone" onInput={(e) => setDisplayName(e.currentTarget.value)} />
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
