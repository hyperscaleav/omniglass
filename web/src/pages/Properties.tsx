import { For, Show, createEffect, createSignal, on, type JSX } from "solid-js";
import { useQuery, useQueryClient } from "@tanstack/solid-query";
import FlatList, { type FlatColumn } from "../components/FlatList";
import KVStacked from "../components/KVStacked";
import { useFormActions } from "../lib/formactions";
import { Plus } from "../components/icons";
import {
  type PropertyRow,
  type PropertyDataType,
  type PropertyKind,
  PROPERTY_DATA_TYPES,
  PROPERTIES_KEY,
  listProperties,
  createProperty,
  updateProperty,
  deleteProperty,
} from "../lib/properties";
import { useMe, can } from "../lib/auth";
import { describeError } from "../lib/format";
import { type BladeDef, useBlades, useBladeEdit } from "../lib/blades";

// Properties: the estate-model signal catalog (Catalog > Properties). A property is
// a typed, registered signal named by a key that a datapoint observes and a field
// declares. Official (seed-owned) properties are read-only; custom properties are
// operator-created. The catalog is estate-wide reference data, not a scoped resource.

function typeBadge(dataType: string): JSX.Element {
  return <span class="badge badge-ghost badge-sm font-data">{dataType}</span>;
}

function kindBadge(kind: string | undefined): JSX.Element {
  return kind
    ? <span class="badge badge-outline badge-sm">{kind}</span>
    : <span class="text-base-content/30">—</span>;
}

function originBadge(official: boolean): JSX.Element {
  return official
    ? <span class="badge badge-ghost badge-sm">official</span>
    : <span class="badge badge-outline badge-sm">custom</span>;
}

const columns: FlatColumn<PropertyRow>[] = [
  { key: "name", label: "Key", sortVal: (r) => r.name, cell: (r) => <span class="font-data font-semibold">{r.name}</span> },
  { key: "data_type", label: "Type", width: "90px", sortVal: (r) => r.data_type, cell: (r) => typeBadge(r.data_type) },
  { key: "display_name", label: "Label", sortVal: (r) => r.display_name ?? "", cell: (r) => <span>{r.display_name}</span> },
  { key: "kind", label: "Kind", width: "90px", cell: (r) => kindBadge(r.kind) },
  { key: "official", label: "Origin", width: "100px", sortVal: (r) => String(r.official), cell: (r) => originBadge(r.official) },
];

export default function Properties(): JSX.Element {
  const me = useMe();
  const properties = useQuery(() => ({ queryKey: PROPERTIES_KEY, queryFn: listProperties }));
  const rows = () => (properties.data ?? []).slice().sort((a, b) => a.name.localeCompare(b.name));
  const canCreate = () => can(me.data, "property", "create");

  return (
    <div class="flex min-h-full flex-col gap-4">
      <FlatList<PropertyRow>
        config={{
          entity: { name: "property", plural: "properties" },
          rows,
          loading: () => properties.isPending,
          error: () => properties.error,
          filterKeys: [
            { key: "name", type: "string", hint: "substring", get: (r) => `${r.name} ${r.display_name ?? ""}`, values: () => [] },
            { key: "type", type: "string", hint: "exact", get: (r) => r.data_type, values: () => PROPERTY_DATA_TYPES },
            { key: "official", type: "string", hint: "exact", get: (r) => (r.official ? "official" : "custom"), values: () => ["official", "custom"] },
          ],
          filterPlaceholder: "filter properties by name, label…",
          columns,
          empty: "No properties.",
          rowId: (r) => r.name,
          blades: { registry: { property: propertyBlade }, rootKind: "property" },
          create: canCreate()
            ? { label: "New property", can: canCreate, body: (ctx) => <CreatePropertyForm onCreated={ctx.close} /> }
            : undefined,
        }}
      />
    </div>
  );
}

// propertyBlade renders one property on the shared blade stack. The title is the mono
// property key; official properties are read-only (no pencil, no delete).
export const propertyBlade: BladeDef = {
  Title: (p) => <span class="font-data">{p.id}</span>,
  Body: (p) => <PropertyBladeBody name={p.id} />,
};

function usePropertyRow(name: string): () => PropertyRow | undefined {
  const properties = useQuery(() => ({ queryKey: PROPERTIES_KEY, queryFn: listProperties }));
  return () => (properties.data ?? []).find((r) => r.name === name);
}

function PropertyBladeBody(p: { name: string }): JSX.Element {
  const qc = useQueryClient();
  const me = useMe();
  const blades = useBlades();
  const edit = useBladeEdit();
  const row = usePropertyRow(p.name);
  const [err, setErr] = createSignal<string | null>(null);
  const [displayName, setDisplayName] = createSignal("");
  const [description, setDescription] = createSignal("");
  const [unit, setUnit] = createSignal("");

  createEffect(on(edit.editing, (editing) => {
    if (!editing) return;
    const r = row();
    setDisplayName(r?.display_name ?? "");
    setDescription(r?.description ?? "");
    setUnit(r?.unit ?? "");
    setErr(null);
  }));

  async function removeProperty() {
    const r = row();
    if (!r) return;
    if (!confirm(`Delete property "${r.name}"?`)) return;
    setErr(null);
    try {
      await deleteProperty(r.name);
      blades.close();
      await qc.invalidateQueries({ queryKey: PROPERTIES_KEY });
    } catch (e) {
      setErr(describeError(e));
    }
  }

  async function save() {
    const r = row();
    if (!r) return;
    setErr(null);
    try {
      await updateProperty(r.name, { display_name: displayName(), description: description(), unit: unit() || undefined });
      await qc.invalidateQueries({ queryKey: PROPERTIES_KEY });
    } catch (e) {
      setErr(describeError(e));
      throw e; // keep the blade in edit mode on failure
    }
  }

  edit.bind({
    editable: () => !!row() && !row()!.official && can(me.data, "property", "update"),
    save,
    destructive: () =>
      row() && !row()!.official && can(me.data, "property", "delete")
        ? { label: "Delete", tone: "danger", onClick: removeProperty }
        : undefined,
  });

  return (
    <Show when={row()} fallback={<p class="text-sm text-base-content/50">Property not found.</p>}>
      {(r) => (
        <div class="flex flex-col gap-4">
          <Show when={err()}>
            <div role="alert" class="alert alert-error alert-soft text-sm"><span>{err()}</span></div>
          </Show>
          <div class="grid grid-cols-2 gap-3 text-sm">
            <KVStacked label="Type" value={typeBadge(r().data_type)} />
            <KVStacked label="Kind" value={kindBadge(r().kind)} />
            <KVStacked label="Origin" value={originBadge(r().official)} />
            <KVStacked label="Unit" value={<span class="font-data">{r().unit ?? "—"}</span>} />
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
          <Show when={edit.editing()}>
            <div class="flex flex-col gap-1.5">
              <span class="eyebrow">Unit</span>
              <input class="input input-bordered w-full font-data" placeholder="ms" value={unit()} onInput={(e) => setUnit(e.currentTarget.value)} />
            </div>
          </Show>
          <Show when={r().validation != null}>
            <div class="flex flex-col gap-1.5">
              <span class="eyebrow">Validation (JSON Schema)</span>
              <pre class="overflow-x-auto rounded-box border border-base-300 bg-base-200 p-2.5 font-data text-xs">{JSON.stringify(r().validation, null, 2)}</pre>
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

// CreatePropertyForm: register a custom property. Name and data type are required; kind
// (observed metric/state/log) is optional, omitted for a declared attribute property.
export function CreatePropertyForm(p: { onCreated: (name: string) => void }): JSX.Element {
  const qc = useQueryClient();
  const [name, setName] = createSignal("");
  const [dataType, setDataType] = createSignal<PropertyDataType>("string");
  const [displayName, setDisplayName] = createSignal("");
  const [description, setDescription] = createSignal("");
  const [unit, setUnit] = createSignal("");
  const [kind, setKind] = createSignal<"" | PropertyKind>("");
  const [busy, setBusy] = createSignal(false);
  const [formErr, setFormErr] = createSignal<string | null>(null);

  useFormActions().bind({
    submitLabel: "Create property",
    submitIcon: Plus,
    submit: () => void submit(),
    busy,
    disabled: () => !name().trim(),
  });

  async function submit() {
    setBusy(true);
    setFormErr(null);
    try {
      await createProperty({
        name: name().trim(),
        data_type: dataType(),
        display_name: displayName().trim() || undefined,
        description: description().trim() || undefined,
        unit: unit().trim() || undefined,
        kind: kind() || undefined,
      });
      await qc.invalidateQueries({ queryKey: PROPERTIES_KEY });
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
      <Field label="Key" hint="A lowercase, dot-hierarchied name, e.g. serial_number or interface.reachable.">
        <input class="input input-bordered w-full font-data" value={name()} placeholder="serial_number" onInput={(e) => setName(e.currentTarget.value)} />
      </Field>
      <Field label="Data type">
        <select class="select select-bordered w-full" value={dataType()} onChange={(e) => setDataType(e.currentTarget.value as PropertyDataType)}>
          <For each={PROPERTY_DATA_TYPES}>{(t) => <option value={t}>{t}</option>}</For>
        </select>
      </Field>
      <Field label="Display name">
        <input class="input input-bordered w-full" value={displayName()} placeholder="Serial number" onInput={(e) => setDisplayName(e.currentTarget.value)} />
      </Field>
      <Field label="Description">
        <input class="input input-bordered w-full" value={description()} onInput={(e) => setDescription(e.currentTarget.value)} />
      </Field>
      <Field label="Unit" hint="Optional, for an observed measurement (e.g. ms).">
        <input class="input input-bordered w-full font-data" value={unit()} placeholder="ms" onInput={(e) => setUnit(e.currentTarget.value)} />
      </Field>
      <Field label="Kind" hint="Observed kind: metric, state, or log. Leave declared for an operator-set attribute.">
        <select class="select select-bordered w-full" value={kind()} onChange={(e) => setKind(e.currentTarget.value as "" | PropertyKind)}>
          <option value="">declared (no observed kind)</option>
          <option value="metric">metric</option>
          <option value="state">state</option>
          <option value="log">log</option>
        </select>
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
