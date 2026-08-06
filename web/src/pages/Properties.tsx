import { For, Show, createEffect, createSignal, on, type JSX } from "solid-js";
import { useQuery, useQueryClient } from "@tanstack/solid-query";
import FlatList, { type FlatColumn } from "../components/FlatList";
import BladeTitle from "../components/BladeTitle";
import FieldRow from "../components/FieldRow";
import BladeField from "../components/BladeField";
import { identityColumn } from "../components/IdentityCell";
import KVStacked from "../components/KVStacked";
import { useFormActions } from "../lib/formactions";
import { Plus } from "../components/icons";
import {
  type PropertyRow,
  type PropertyDataType,
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

// Properties: the categorical lane of the signal catalog (Catalog > Properties),
// the twin of the Metrics page. A property is a typed, registered non-numeric
// signal named by a key that a sample observes and a field declares; a numeric
// signal is a metric type. Official (seed-owned) properties are read-only; custom
// properties are operator-created. The catalog is estate-wide reference data, not
// a scoped resource, and every affordance gates on property_type, the resource
// the API stamps.

function typeBadge(dataType: string): JSX.Element {
  return <span class="badge badge-ghost badge-sm font-data">{dataType}</span>;
}

function originBadge(official: boolean): JSX.Element {
  return official
    ? <span class="badge badge-ghost badge-sm">official</span>
    : <span class="badge badge-outline badge-sm">custom</span>;
}

// This catalog is a keyspace, so `name` holds a dotted value (icmp-rtt-avg) rather
// than the kebab the rest of the estate addresses rows by. That is a validation
// difference, not a different concept, so the header is the one word every list
// uses. The cell is the shared two-line treatment either way, which is what retires
// the separate "Label" column.
const columns: FlatColumn<PropertyRow>[] = [
  identityColumn<PropertyRow>(),
  { key: "data_type", label: "Type", width: "90px", sortVal: (r) => r.data_type, cell: (r) => typeBadge(r.data_type) },
  { key: "official", label: "Origin", width: "100px", sortVal: (r) => String(r.official), cell: (r) => originBadge(r.official) },
];

export default function Properties(): JSX.Element {
  const me = useMe();
  const properties = useQuery(() => ({ queryKey: PROPERTIES_KEY, queryFn: listProperties }));
  const rows = () => (properties.data ?? []).slice().sort((a, b) => a.name.localeCompare(b.name));
  const canCreate = () => can(me.data, "property_type", "create");

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
          filterPlaceholder: "filter properties by name, display name…",
          columns,
          empty: "No properties.",
          rowId: (r) => r.name,
          blades: { registry: { property: propertyBlade }, rootKind: "property" },
          create: canCreate()
            ? { label: "New property", can: canCreate, body: (ctx) => <CreatePropertyForm onCreated={ctx.select} /> }
            : undefined,
        }}
      />
    </div>
  );
}

// propertyBlade renders one property on the shared blade stack. The title is what the
// list shows, the display name, falling back to the name; official properties are
// read-only (no pencil, no delete).
export const propertyBlade: BladeDef = {
  Title: (p) => <PropertyBladeTitle name={p.id} />,
  Body: (p) => <PropertyBladeBody name={p.id} />,
};

// The blade heading is the display name, falling back to the name, so opening a row
// lands on the same words the row showed. It rendered the bare name before, so
// clicking "ICMP RTT (avg)" opened a panel headed icmp.rtt-avg.
function PropertyBladeTitle(p: { name: string }): JSX.Element {
  return <BladeTitle row={usePropertyRow(p.name)} fallback={p.name} />;
}

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

  createEffect(on(edit.editing, (editing) => {
    if (!editing) return;
    const r = row();
    setDisplayName(r?.display_name ?? "");
    setDescription(r?.description ?? "");
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
      await updateProperty(r.name, { display_name: displayName(), description: description() });
      await qc.invalidateQueries({ queryKey: PROPERTIES_KEY });
    } catch (e) {
      setErr(describeError(e));
      throw e; // keep the blade in edit mode on failure
    }
  }

  edit.bind({
    editable: () => !!row() && !row()!.official && can(me.data, "property_type", "update"),
    save,
    destructive: () =>
      row() && !row()!.official && can(me.data, "property_type", "delete")
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
            <KVStacked bind="name" value={<span class="font-data">{r().name}</span>} />
            <KVStacked label="Type" value={typeBadge(r().data_type)} />
            <KVStacked label="Origin" value={originBadge(r().official)} />
          </div>
          <BladeField
            bind="display_name"
            value={() => r().display_name ?? ""}
            draft={displayName}
            onInput={setDisplayName}
          />
          <BladeField
            label="Description"
            multiline
            value={() => r().description ?? ""}
            draft={description}
            onInput={setDescription}
          />
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

// CreatePropertyForm: register a custom property. Name and data type are required;
// the data types are the non-numeric lane (a numeric signal is a metric type).
export function CreatePropertyForm(p: { onCreated: (r: PropertyRow) => void }): JSX.Element {
  const qc = useQueryClient();
  const [name, setName] = createSignal("");
  const [dataType, setDataType] = createSignal<PropertyDataType>("string");
  const [displayName, setDisplayName] = createSignal("");
  const [description, setDescription] = createSignal("");
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
      const created = await createProperty({
        name: name().trim(),
        data_type: dataType(),
        display_name: displayName().trim() || undefined,
        description: description().trim() || undefined,
      });
      await qc.invalidateQueries({ queryKey: PROPERTIES_KEY });
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
      <FieldRow bind="name" hint="A lowercase kebab name, e.g. serial-number or interface-reachable.">
        <input class="input input-bordered w-full font-data" value={name()} placeholder="serial-number" onInput={(e) => setName(e.currentTarget.value)} />
      </FieldRow>
      <FieldRow label="Data type">
        <select class="select select-bordered w-full" value={dataType()} onChange={(e) => setDataType(e.currentTarget.value as PropertyDataType)}>
          <For each={PROPERTY_DATA_TYPES}>{(t) => <option value={t}>{t}</option>}</For>
        </select>
      </FieldRow>
      <FieldRow bind="display_name">
        <input class="input input-bordered w-full" value={displayName()} placeholder="Serial number" onInput={(e) => setDisplayName(e.currentTarget.value)} />
      </FieldRow>
      <FieldRow label="Description">
        <input class="input input-bordered w-full" value={description()} onInput={(e) => setDescription(e.currentTarget.value)} />
      </FieldRow>
    </form>
  );
}
