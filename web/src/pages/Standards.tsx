import { For, Show, createEffect, createMemo, createSignal, on, type JSX } from "solid-js";
import { useQuery, useQueryClient } from "@tanstack/solid-query";
import FlatList, { type FlatColumn } from "../components/FlatList";
import KVStacked from "../components/KVStacked";
import { useFormActions } from "../lib/formactions";
import ContractEditor from "../components/ContractEditor";
import RoleEditor from "../components/RoleEditor";
import { Plus } from "../components/icons";
import {
  type Standard,
  STANDARDS_KEY,
  listStandards,
  createStandard,
  updateStandard,
  deleteStandard,
} from "../lib/standards";
import { useMe, can } from "../lib/auth";
import { describeError } from "../lib/format";
import { type BladeDef, useBlades, useBladeEdit } from "../lib/blades";

// Standards: the catalog of blueprints a system conforms to ("Meeting room",
// "Huddle space"), on the flat FlatList surface beside Products. A standard is the
// system-side counterpart of a product: it is addressed by its id (a kebab id,
// create-only), it may be a VARIANT of another standard, and it declares the
// property CONTRACT every conforming system exposes (the ContractEditor on the
// detail). Official (seed-owned) rows are read-only, same as the product catalog:
// no Edit pencil, no Delete, and a read-only contract.
//
// A standard lives here rather than as a Types tab because it is no longer a bare
// classifier registry row: it carries a contract and its own authorization
// resource (standard:*), exactly like a product.

function officialBadge(official: boolean): JSX.Element {
  return official
    ? <span class="badge badge-ghost badge-sm">official</span>
    : <span class="badge badge-outline badge-sm">custom</span>;
}

function refCell(id?: string): JSX.Element {
  return <span class="font-data text-xs text-base-content/60">{id || "—"}</span>;
}

const columns: FlatColumn<Standard>[] = [
  { key: "id", label: "Id", sortVal: (s) => s.id, cell: (s) => <span class="font-data font-semibold">{s.id}</span> },
  { key: "display_name", label: "Display name", sortVal: (s) => s.display_name, cell: (s) => <span>{s.display_name}</span> },
  { key: "parent", label: "Variant of", width: "180px", sortVal: (s) => s.parent_standard ?? "", cell: (s) => refCell(s.parent_standard) },
  { key: "official", label: "Origin", width: "100px", sortVal: (s) => String(s.official), cell: (s) => officialBadge(s.official) },
];

export default function Standards() {
  const me = useMe();
  const standards = useQuery(() => ({ queryKey: STANDARDS_KEY, queryFn: listStandards }));

  const rows = createMemo(() =>
    [...(standards.data ?? [])].sort((a, b) => a.display_name.localeCompare(b.display_name) || a.id.localeCompare(b.id)),
  );

  return (
    <FlatList<Standard>
      config={{
        entity: { name: "standard", plural: "standards" },
        rows,
        loading: () => standards.isPending,
        error: () => standards.error,
        filterKeys: [
          { key: "name", type: "string", hint: "substring", get: (s) => `${s.id} ${s.display_name}`, values: () => [] },
          { key: "parent", type: "string", hint: "exact", get: (s) => s.parent_standard ?? "", values: () => [] },
          { key: "official", type: "string", hint: "exact", get: (s) => (s.official ? "official" : "custom"), values: () => ["official", "custom"] },
        ],
        filterPlaceholder: "filter standards by id, name…",
        columns,
        empty: "No standards yet.",
        // Address a row by id: the write paths key on id, and it is globally unique.
        rowId: (s) => s.id,
        blades: { registry: { standard: standardBlade }, rootKind: "standard" },
        create: can(me.data, "standard", "create")
          ? { label: "New standard", can: () => can(me.data, "standard", "create"), body: (ctx) => <CreateStandardForm onCreated={ctx.close} /> }
          : undefined,
      }}
    />
  );
}

// standardBlade renders an id on the shared blade stack. An official row is
// read-only (no pencil, no destructive action); a custom row carries Edit + Delete
// plus a writable contract.
export const standardBlade: BladeDef = {
  Title: (p) => <StandardBladeTitle id={p.id} />,
  Body: (p) => <StandardBladeBody id={p.id} />,
};

function useStandardRow(id: string): () => Standard | undefined {
  const standards = useQuery(() => ({ queryKey: STANDARDS_KEY, queryFn: listStandards }));
  return () => (standards.data ?? []).find((s) => s.id === id);
}

function StandardBladeTitle(p: { id: string }): JSX.Element {
  const row = useStandardRow(p.id);
  return <span class="font-data">{row()?.id ?? p.id}</span>;
}

function StandardBladeBody(p: { id: string }): JSX.Element {
  const qc = useQueryClient();
  const me = useMe();
  const blades = useBlades();
  const edit = useBladeEdit();
  const row = useStandardRow(p.id);
  const [err, setErr] = createSignal<string | null>(null);
  const [displayName, setDisplayName] = createSignal("");
  const [parentId, setParentId] = createSignal("");

  createEffect(on(edit.editing, (editing) => {
    if (!editing) return;
    const r = row();
    setDisplayName(r?.display_name ?? "");
    setParentId(r?.parent_standard ?? "");
    setErr(null);
  }));

  async function removeStandard() {
    const r = row();
    if (!r) return;
    if (!confirm(`Delete standard "${r.id}"?`)) return;
    setErr(null);
    try {
      await deleteStandard(r.id);
      blades.close();
      await qc.invalidateQueries({ queryKey: STANDARDS_KEY });
    } catch (e) {
      setErr(describeError(e));
    }
  }

  async function save() {
    const r = row();
    if (!r) return;
    setErr(null);
    try {
      await updateStandard(r.id, { display_name: displayName(), parent_standard_id: parentId() || undefined });
      await qc.invalidateQueries({ queryKey: STANDARDS_KEY });
    } catch (e) {
      setErr(describeError(e));
      throw e; // keep the blade in edit mode on failure
    }
  }

  edit.bind({
    editable: () => !!row() && !row()!.official && can(me.data, "standard", "update"),
    save,
    destructive: () =>
      row() && !row()!.official && can(me.data, "standard", "delete")
        ? { label: "Delete", tone: "danger", onClick: removeStandard }
        : undefined,
  });

  return (
    <Show when={row()} fallback={<p class="text-sm text-base-content/50">Standard not found.</p>}>
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
          <div class="flex flex-col gap-1.5">
            <span class="eyebrow">Variant of</span>
            <Show
              when={edit.editing()}
              fallback={<div class="input input-bordered flex items-center text-sm font-data">{r().parent_standard || "—"}</div>}
            >
              <ParentStandardSelect value={parentId()} exclude={r().name} onChange={setParentId} />
            </Show>
            <span class="text-[11px] text-base-content/40">A standard this one specializes. Leave empty for a standalone standard.</span>
          </div>
          <ContractEditor classifier="standard" id={r().name} official={r().official} />
          <RoleEditor id={r().id} official={r().official} />
          <Show when={r().official}>
            <div role="alert" class="alert alert-soft text-sm"><span>Seed-owned, read-only.</span></div>
          </Show>
        </div>
      )}
    </Show>
  );
}

// CreateStandardForm: name the id (a kebab id, immutable after creation), set the
// display name; the parent standard is optional (a variant of an existing one).
export function CreateStandardForm(p: { onCreated: (id: string) => void }): JSX.Element {
  const qc = useQueryClient();
  const [id, setId] = createSignal("");
  const [displayName, setDisplayName] = createSignal("");
  const [parentId, setParentId] = createSignal("");
  const [busy, setBusy] = createSignal(false);
  const [formErr, setFormErr] = createSignal<string | null>(null);

  useFormActions().bind({
    submitLabel: "Create standard",
    submitIcon: Plus,
    submit: () => void submit(),
    busy,
    disabled: () => !id().trim() || !displayName().trim(),
  });

  async function submit() {
    setBusy(true);
    setFormErr(null);
    try {
      await createStandard({
        name: id().trim(),
        display_name: displayName().trim(),
        parent_standard_id: parentId() || undefined,
      });
      await qc.invalidateQueries({ queryKey: STANDARDS_KEY });
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
      <Field label="Id" hint="A kebab id, e.g. meeting-room.">
        <input class="input input-bordered w-full font-data" value={id()} placeholder="meeting-room" onInput={(e) => setId(e.currentTarget.value)} />
      </Field>
      <Field label="Display name">
        <input class="input input-bordered w-full" value={displayName()} placeholder="Meeting room" onInput={(e) => setDisplayName(e.currentTarget.value)} />
      </Field>
      <Field label="Variant of" hint="A standard this one specializes. Optional.">
        <ParentStandardSelect value={parentId()} onChange={setParentId} />
      </Field>
    </form>
  );
}

// ParentStandardSelect: the variant-parent picker over the standard registry, with
// a "None" option. `exclude` drops the standard being edited, so a row cannot be
// made a variant of itself.
function ParentStandardSelect(p: { value: string; exclude?: string; onChange: (v: string) => void }): JSX.Element {
  const standards = useQuery(() => ({ queryKey: STANDARDS_KEY, queryFn: listStandards }));
  const options = createMemo(() =>
    [...(standards.data ?? [])]
      .filter((s) => s.name !== p.exclude)
      .sort((a, b) => a.display_name.localeCompare(b.display_name)),
  );
  return (
    <select class="select select-bordered w-full" aria-label="Variant of" value={p.value} onChange={(e) => p.onChange(e.currentTarget.value)}>
      <option value="">None</option>
      <For each={options()}>{(s) => <option value={s.name}>{s.display_name}</option>}</For>
    </select>
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
