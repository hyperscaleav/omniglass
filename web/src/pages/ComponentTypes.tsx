import { Show, createEffect, createMemo, createSignal, on, type JSX } from "solid-js";
import { Dynamic } from "solid-js/web";
import { useQuery, useQueryClient } from "@tanstack/solid-query";
import FlatList, { type FlatColumn } from "../components/FlatList";
import BladeTitle from "../components/BladeTitle";
import FieldRow from "../components/FieldRow";
import BladeField from "../components/BladeField";
import KVStacked from "../components/KVStacked";
import { useFormActions } from "../lib/formactions";
import ComponentTypeSelect from "../components/ComponentTypeSelect";
import { Plus, resolveIcon } from "../components/icons";
import {
  type ComponentType,
  COMPONENT_TYPES_KEY,
  componentTypeTree,
  flattenComponentTypeTree,
  listComponentTypes,
  createComponentType,
  updateComponentType,
  deleteComponentType,
  restoreComponentType,
} from "../lib/component_types";
import { useMe, can } from "../lib/auth";
import { registryLock, registryOrigin } from "../lib/catalog";
import { createIdentity } from "../lib/entities";
import { describeError } from "../lib/format";
import { type BladeDef, useBlades, useBladeEdit } from "../lib/blades";

// Component Types (Catalog > Components: Types): the device-class genus
// registry (ADR-0085, the component_type registry's partial reversal of
// ADR-0047). It classifies the PRODUCT (`product.component_type_id`), so a
// component's shape comes through the product it is; this registry is not a
// second classifier on the component. It nests by parent_id (mic over
// wireless-mic, ceiling-mic, boundary-mic), on the same official/custom
// pattern as Location Types, and every identity fact (stem, icon, abbrev,
// default_tags) inherits down the tree unless a node overrides it. There is
// no reparent leg: a custom type's placement is fixed at create, so the edit
// blade revises a node's own facts only.

// Origin is three-state on this registry, the first to adopt the fork (#655,
// ADR-0095): a row this release ships, a row the operator made, or a shipped
// row the operator has overridden. The third is the one that has to read
// unambiguously, because it is the only state where what an operator sees is
// not what the release ships.
function originBadge(row: { official: boolean; forked?: boolean }): JSX.Element {
  switch (registryOrigin(row)) {
    case "yours":
      return <span class="badge badge-outline badge-sm">custom</span>;
    case "overridden":
      return <span class="badge badge-warning badge-sm">overridden</span>;
    default:
      return <span class="badge badge-ghost badge-sm">official</span>;
  }
}

// Depth-indented identity cell: the tree reads without a dedicated tree
// widget, the same non-breaking-space indent ComponentTypeSelect's options
// use, applied to a table cell instead of an <option>.
function NameCell(p: { row: ComponentType & { depth: number } }): JSX.Element {
  return (
    <span class="flex min-w-0 items-center gap-1.5 py-0.5" style={{ "padding-left": `${p.row.depth * 14}px` }}>
      <span class="flex min-w-0 flex-col gap-0.5">
        <span class="truncate" style={{ "font-weight": 500 }}>{p.row.display_name}</span>
        <span class="truncate font-data text-[11px] text-base-content/40">{p.row.name}</span>
      </span>
    </span>
  );
}

function IconCell(p: { row: ComponentType; resolvedIcon: string }): JSX.Element {
  return (
    <span class="flex items-center gap-1.5">
      <Dynamic component={resolveIcon(p.resolvedIcon)} size={14} />
      <span class="font-data text-xs text-base-content/60">{p.row.icon ?? p.resolvedIcon}</span>
    </span>
  );
}

export default function ComponentTypes() {
  const me = useMe();
  const types = useQuery(() => ({ queryKey: COMPONENT_TYPES_KEY, queryFn: listComponentTypes }));

  // Tree order (parent immediately followed by its children, each level
  // alphabetical), not a flat alphabetical list: the registry's nesting is
  // the point of this page, so the row order teaches it.
  const treeRows = createMemo(() => flattenComponentTypeTree(componentTypeTree(types.data ?? [])));
  const byName = createMemo(() => new Map((types.data ?? []).map((t) => [t.name, t] as const)));

  function resolvedIconOf(row: ComponentType): string {
    let cur: ComponentType | undefined = row;
    for (let depth = 0; cur && depth < 32; depth++) {
      if (cur.icon) return cur.icon;
      cur = cur.parent ? byName().get(cur.parent) : undefined;
    }
    return "box";
  }

  const columns: FlatColumn<ComponentType & { depth: number }>[] = [
    // No sortVal: the row order IS the taxonomy (tree order, parent then
    // children), so this column deliberately does not offer to re-sort it
    // away into a flat alphabetical list.
    { key: "name", label: "Name", cell: (r) => <NameCell row={r} /> },
    { key: "parent", label: "Parent", width: "140px", cell: (r) => <span class="font-data text-xs text-base-content/60">{r.parent ?? "—"}</span> },
    { key: "stem", label: "Stem", width: "110px", cell: (r) => <span class="font-data text-xs text-base-content/60">{r.stem ?? "—"}</span> },
    { key: "abbrev", label: "Abbrev", width: "90px", cell: (r) => <span class="font-data text-xs text-base-content/60">{r.abbrev ?? "—"}</span> },
    { key: "icon", label: "Icon", width: "150px", cell: (r) => <IconCell row={r} resolvedIcon={resolvedIconOf(r)} /> },
    { key: "official", label: "Origin", width: "110px", sortVal: (r) => registryOrigin(r), cell: (r) => originBadge(r) },
  ];

  const canCreate = () => can(me.data, "component_type", "create");

  return (
    <FlatList<ComponentType & { depth: number }>
      config={{
        entity: { name: "component type", plural: "component types" },
        rows: treeRows,
        loading: () => types.isPending,
        error: () => types.error,
        filterKeys: [
          { key: "name", type: "string", hint: "substring", get: (r) => `${r.name} ${r.display_name}`, values: () => [] },
          { key: "official", type: "string", hint: "exact", get: (r) => registryOrigin(r), values: () => ["shipped", "yours", "overridden"] },
        ],
        filterPlaceholder: "filter component types by name…",
        columns,
        empty: "No component types.",
        rowId: (r) => r.name,
        blades: { registry: { component_type: componentTypeBlade }, rootKind: "component_type" },
        create: canCreate()
          ? {
              label: "New component type",
              can: canCreate,
              // createComponentType returns a flat ComponentType; the create
              // Drawer's select() wants this page's row shape (ComponentType
              // plus its tree depth), so a freshly created row gets a
              // synthetic depth of 0 rather than reflowing the whole tree.
              body: (ctx) => <CreateComponentTypeForm onCreated={(t) => ctx.select({ ...t, depth: 0 })} />,
            }
          : undefined,
      }}
    />
  );
}

// componentTypeBlade renders one registry row on the shared blade stack. A
// custom row carries Edit + Delete; a shipped row carries Edit too, because an
// edit forks it rather than writing it, and its destructive action is Restore
// (discard the fork), never Delete.
export const componentTypeBlade: BladeDef = {
  Title: (p) => <ComponentTypeBladeTitle id={p.id} />,
  Body: (p) => <ComponentTypeBladeBody id={p.id} />,
};

function useComponentTypeRow(id: string): () => ComponentType | undefined {
  const types = useQuery(() => ({ queryKey: COMPONENT_TYPES_KEY, queryFn: listComponentTypes }));
  return () => (types.data ?? []).find((r) => r.name === id);
}

function ComponentTypeBladeTitle(p: { id: string }): JSX.Element {
  return <BladeTitle row={useComponentTypeRow(p.id)} fallback={p.id} />;
}

function ComponentTypeBladeBody(p: { id: string }): JSX.Element {
  const qc = useQueryClient();
  const me = useMe();
  const blades = useBlades();
  const edit = useBladeEdit();
  const row = useComponentTypeRow(p.id);
  const [err, setErr] = createSignal<string | null>(null);
  const [displayName, setDisplayName] = createSignal("");
  const [stem, setStem] = createSignal("");
  const [abbrev, setAbbrev] = createSignal("");
  const [icon, setIcon] = createSignal("");

  createEffect(on(edit.editing, (editing) => {
    if (!editing) return;
    const r = row();
    setDisplayName(r?.display_name ?? "");
    setStem(r?.stem ?? "");
    setAbbrev(r?.abbrev ?? "");
    setIcon(r?.icon ?? "");
    setErr(null);
  }));

  async function removeType() {
    const r = row();
    if (!r) return;
    if (!confirm(`Delete component type "${r.name}"?`)) return;
    setErr(null);
    try {
      await deleteComponentType(r.id);
      blades.close();
      await qc.invalidateQueries({ queryKey: COMPONENT_TYPES_KEY });
    } catch (e) {
      setErr(describeError(e));
    }
  }

  // Restore keeps the blade open: the row survives, it just goes back to the
  // shipped values, so closing would hide the very change the operator asked
  // for.
  async function restoreType() {
    const r = row();
    if (!r) return;
    if (!confirm(`Discard your changes to "${r.name}" and go back to the shipped values?`)) return;
    setErr(null);
    try {
      await restoreComponentType(r.id);
      await qc.invalidateQueries({ queryKey: COMPONENT_TYPES_KEY });
    } catch (e) {
      setErr(describeError(e));
    }
  }

  async function save() {
    const r = row();
    if (!r) return;
    setErr(null);
    try {
      await updateComponentType(r.id, {
        display_name: displayName(),
        stem: stem(),
        abbrev: abbrev(),
        icon: icon(),
      });
      await qc.invalidateQueries({ queryKey: COMPONENT_TYPES_KEY });
    } catch (e) {
      setErr(describeError(e));
      throw e; // keep the blade in edit mode on failure
    }
  }

  edit.bind({
    editable: () => !!row() && can(me.data, "component_type", "update"),
    save,
    // One destructive slot, two meanings, chosen by what the row IS: a custom
    // row is deleted, a forked shipped row has the fork discarded, and a
    // pristine shipped row offers nothing (there is neither a row to delete
    // nor changes to discard).
    destructive: () => {
      const r = row();
      if (!r) return undefined;
      if (r.official) {
        return r.forked && can(me.data, "component_type", "update")
          ? { label: "Restore shipped", tone: "danger" as const, onClick: restoreType }
          : undefined;
      }
      return can(me.data, "component_type", "delete")
        ? { label: "Delete", tone: "danger" as const, onClick: removeType }
        : undefined;
    },
    locked: () => registryLock(row(), me.data, "component_type", { forkable: true }),
  });

  return (
    <Show when={row()} fallback={<p class="text-sm text-base-content/50">Component type not found.</p>}>
      {(r) => (
        <div class="flex flex-col gap-4">
          <Show when={err()}>
            <div role="alert" class="alert alert-error alert-soft text-sm"><span>{err()}</span></div>
          </Show>
          <div class="grid grid-cols-2 gap-3 text-sm">
            <KVStacked label="Origin" value={originBadge(r())} />
            <KVStacked label="Id" value={<span class="font-data text-xs text-base-content/60">{r().id}</span>} />
            <KVStacked
              label="Parent"
              value={r().parent ? <span class="font-data">{r().parent}</span> : <span class="text-base-content/50">Root</span>}
            />
          </div>
          <BladeField
            bind="display_name"
            value={() => r().display_name ?? ""}
            draft={displayName}
            onInput={setDisplayName}
          />
          <BladeField
            label="Stem"
            mono
            placeholder="inherits from its parent"
            value={() => r().stem ?? ""}
            draft={stem}
            onInput={setStem}
            hint="The auto-generated component name's prefix. Empty inherits the nearest ancestor's."
          />
          <BladeField
            label="Abbrev"
            mono
            placeholder="inherits from its parent"
            value={() => r().abbrev ?? ""}
            draft={abbrev}
            onInput={setAbbrev}
            hint="The compact hostname-render form (fp, cam, dsp). Empty inherits."
          />
          <BladeField
            label="Icon"
            mono
            placeholder="inherits from its parent"
            value={() => r().icon ?? ""}
            draft={icon}
            onInput={setIcon}
            hint="A glyph key. Empty inherits the nearest ancestor's."
          />
        </div>
      )}
    </Show>
  );
}

// CreateComponentTypeForm: names a new custom component_type. The display
// name leads and the kebab name derives from it until the operator edits it
// by hand (lib/entities). Parent placement is chosen once, at create: the
// gateway has no reparent leg, so this is the only moment a custom type's
// position in the tree is set. Stem, abbrev, and icon are optional
// overrides; left blank, the type inherits its parent's.
export function CreateComponentTypeForm(p: { onCreated: (t: ComponentType) => void }): JSX.Element {
  const qc = useQueryClient();
  const types = useQuery(() => ({ queryKey: COMPONENT_TYPES_KEY, queryFn: listComponentTypes }));
  const { display, setDisplay, name, setName, nameDerived } = createIdentity();
  const [parentId, setParentId] = createSignal("");
  const [stem, setStem] = createSignal("");
  const [abbrev, setAbbrev] = createSignal("");
  const [icon, setIcon] = createSignal("");
  const [busy, setBusy] = createSignal(false);
  const [formErr, setFormErr] = createSignal<string | null>(null);

  useFormActions().bind({
    submitLabel: "Create component type",
    submitIcon: Plus,
    submit: () => void submit(),
    busy,
    disabled: () => !name().trim() || !display().trim(),
  });

  async function submit() {
    setBusy(true);
    setFormErr(null);
    try {
      const created = await createComponentType({
        name: name().trim(),
        display_name: display().trim(),
        parent_id: parentId() || undefined,
        stem: stem().trim() || undefined,
        abbrev: abbrev().trim() || undefined,
        icon: icon().trim() || undefined,
      });
      await qc.invalidateQueries({ queryKey: COMPONENT_TYPES_KEY });
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
        <input class="input input-bordered w-full" value={display()} placeholder="Wireless Microphone" onInput={(e) => setDisplay(e.currentTarget.value)} />
      </FieldRow>
      <FieldRow bind="name" hint={nameDerived() ? "Derived from the display name. Edit to set your own." : "Globally unique address, used by the API and CLI."}>
        <input class="input input-bordered w-full font-data" value={name()} placeholder="wireless-mic" onInput={(e) => setName(e.currentTarget.value)} />
      </FieldRow>
      <FieldRow label="Parent" hint="Where this type grafts in the tree. Root creates a new top-level genus; the gateway has no reparent leg, so choose carefully.">
        <ComponentTypeSelect types={types.data ?? []} value={parentId()} onChange={setParentId} emptyLabel="Root (no parent)" />
      </FieldRow>
      <FieldRow label="Stem" hint="The auto-generated component name's prefix. Leave blank to inherit the parent's.">
        <input class="input input-bordered w-full font-data" value={stem()} placeholder="inherit" onInput={(e) => setStem(e.currentTarget.value)} />
      </FieldRow>
      <FieldRow label="Abbrev" hint="The compact hostname-render form (fp, cam, dsp). Leave blank to inherit.">
        <input class="input input-bordered w-full font-data" value={abbrev()} placeholder="inherit" onInput={(e) => setAbbrev(e.currentTarget.value)} />
      </FieldRow>
      <FieldRow label="Icon" hint="A glyph key. Leave blank to inherit.">
        <input class="input input-bordered w-full font-data" value={icon()} placeholder="inherit" onInput={(e) => setIcon(e.currentTarget.value)} />
      </FieldRow>
    </form>
  );
}
