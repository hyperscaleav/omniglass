import { For, Show, createEffect, createMemo, createSignal, on, type JSX } from "solid-js";
import { useQuery, useQueryClient } from "@tanstack/solid-query";
import FlatList, { type FlatColumn } from "../components/FlatList";
import BladeTitle from "../components/BladeTitle";
import FieldRow from "../components/FieldRow";
import BladeField from "../components/BladeField";
import { identityColumn } from "../components/IdentityCell";
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
import { createIdentity } from "../lib/entities";
import { describeError } from "../lib/format";
import { type BladeDef, useBlades, useBladeEdit } from "../lib/blades";

// Standards: the catalog of blueprints a system conforms to ("Meeting room",
// "Huddle space"), on the flat FlatList surface beside Products. A standard is the
// system-side counterpart of a product: it is addressed by its kebab name
// (ADR-0062: the uuid is identity, the name is what an operator reads and
// types), it may be a VARIANT of another standard, and it declares the
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
  identityColumn<Standard>(),
  { key: "parent", label: "Variant of", width: "180px", sortVal: (s) => s.parent_standard ?? "", cell: (s) => refCell(s.parent_standard) },
  { key: "official", label: "Origin", width: "100px", sortVal: (s) => String(s.official), cell: (s) => officialBadge(s.official) },
];

export default function Standards() {
  const me = useMe();
  const standards = useQuery(() => ({ queryKey: STANDARDS_KEY, queryFn: listStandards }));

  const rows = createMemo(() =>
    [...(standards.data ?? [])].sort((a, b) => a.display_name.localeCompare(b.display_name) || a.name.localeCompare(b.name)),
  );

  return (
    <FlatList<Standard>
      config={{
        entity: { name: "standard", plural: "standards" },
        rows,
        loading: () => standards.isPending,
        error: () => standards.error,
        filterKeys: [
          { key: "name", type: "string", hint: "substring", get: (s) => `${s.name} ${s.display_name}`, values: () => [] },
          { key: "parent", type: "string", hint: "exact", get: (s) => s.parent_standard ?? "", values: () => [] },
          { key: "official", type: "string", hint: "exact", get: (s) => (s.official ? "official" : "custom"), values: () => ["official", "custom"] },
        ],
        filterPlaceholder: "filter standards by name…",
        columns,
        empty: "No standards yet.",
        // Address a row by its kebab name: globally unique, and what the API
        // and CLI accept (the write paths resolve either form).
        rowId: (s) => s.name,
        blades: { registry: { standard: standardBlade }, rootKind: "standard" },
        create: can(me.data, "standard", "create")
          ? { label: "New standard", can: () => can(me.data, "standard", "create"), body: (ctx) => <CreateStandardForm onCreated={ctx.select} /> }
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
  return () => (standards.data ?? []).find((s) => s.name === id);
}

function StandardBladeTitle(p: { id: string }): JSX.Element {
  return <BladeTitle row={useStandardRow(p.id)} fallback={p.id} />;
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
    if (!confirm(`Delete standard "${r.name}"?`)) return;
    setErr(null);
    try {
      await deleteStandard(r.name);
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
      await updateStandard(r.name, { display_name: displayName(), parent_standard_id: parentId() || undefined });
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
            <KVStacked bind="name" value={<span class="font-data">{r().name}</span>} />
            <KVStacked label="Origin" value={officialBadge(r().official)} />
            <KVStacked label="Id" value={<span class="font-data text-xs text-base-content/60">{r().id}</span>} />
          </div>
          <BladeField
            bind="display_name"
            value={() => r().display_name ?? ""}
            draft={displayName}
            onInput={setDisplayName}
          />
          <BladeField
            label="Variant of"
            mono
            value={() => r().parent_standard ?? ""}
            hint="A standard this one specializes. Leave empty for a standalone standard."
          >
            <ParentStandardSelect value={parentId()} exclude={r().name} onChange={setParentId} />
          </BladeField>
          <ContractEditor classifier="standard" id={r().name} official={r().official} />
          <ContractEditor classifier="standard" lane="metric" id={r().name} official={r().official} />
          <RoleEditor id={r().id} official={r().official} />
          <Show when={r().official}>
            <div role="alert" class="alert alert-soft text-sm"><span>Seed-owned, read-only.</span></div>
          </Show>
        </div>
      )}
    </Show>
  );
}

// CreateStandardForm: name the standard and let the name (the
// operator-facing address; the uuid is the database's to mint) derive from it;
// the parent standard is optional (a variant of an existing one).
export function CreateStandardForm(p: { onCreated: (s: Standard) => void }): JSX.Element {
  const qc = useQueryClient();
  // Display name leads and the handle follows it, stopping the moment the
  // operator edits the handle by hand (lib/entities).
  const { display, setDisplay, name, setName, nameDerived } = createIdentity();
  const [parentId, setParentId] = createSignal("");
  const [busy, setBusy] = createSignal(false);
  const [formErr, setFormErr] = createSignal<string | null>(null);

  useFormActions().bind({
    submitLabel: "Create standard",
    submitIcon: Plus,
    submit: () => void submit(),
    busy,
    disabled: () => !name().trim() || !display().trim(),
  });

  async function submit() {
    setBusy(true);
    setFormErr(null);
    try {
      const created = await createStandard({
        name: name().trim(),
        display_name: display().trim(),
        parent_standard_id: parentId() || undefined,
      });
      await qc.invalidateQueries({ queryKey: STANDARDS_KEY });
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
      <FieldRow bind="display_name" hint="What an operator reads.">
        <input class="input input-bordered w-full" value={display()} placeholder="Meeting room" onInput={(e) => setDisplay(e.currentTarget.value)} />
      </FieldRow>
      <FieldRow bind="name" hint={nameDerived() ? "Derived from the display name. Edit to set your own." : "A kebab name, e.g. meeting-room."}>
        <input class="input input-bordered w-full font-data" value={name()} placeholder="meeting-room" onInput={(e) => setName(e.currentTarget.value)} />
      </FieldRow>
      <FieldRow label="Variant of" hint="A standard this one specializes. Optional.">
        <ParentStandardSelect value={parentId()} onChange={setParentId} />
      </FieldRow>
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
