import { Show, createMemo, createSignal, type JSX } from "solid-js";
import { Dynamic } from "solid-js/web";
import { useQuery, useQueryClient } from "@tanstack/solid-query";
import FlatList, { type FlatColumn } from "../components/FlatList";
import BladeTitle from "../components/BladeTitle";
import FieldRow from "../components/FieldRow";
import BladeField, { EMPTY_VALUE } from "../components/BladeField";
import InheritedField from "../components/InheritedField";
import InheritedCell from "../components/InheritedCell";
import KVStacked from "../components/KVStacked";
import { useFormActions } from "../lib/formactions";
import SystemTypeSelect from "../components/SystemTypeSelect";
import { Plus, resolveIcon } from "../components/icons";
import {
  type SystemType,
  SYSTEM_TYPES_KEY,
  systemTypeTree,
  flattenSystemTypeTree,
  listSystemTypes,
  createSystemType,
  updateSystemType,
  deleteSystemType,
} from "../lib/system_types";
import { useMe, can } from "../lib/auth";
import { ROOT_STEM_HINT, TYPE_PARENT_HINT, inheritedFact, registryLock } from "../lib/catalog";
import { createIdentity, entityLabel } from "../lib/entities";
import { describeError } from "../lib/format";
import { type BladeDef, useBlades, useBladeEdit } from "../lib/blades";

// System Types (Catalog > Systems: Types): the coarse space taxonomy
// (ADR-0096). It classifies the SYSTEM (`system.system_type_id`) by what kind
// of space it is, a boardroom, a classroom, a video wall. It is deliberately
// not the standard, which sits beside it and says which blueprint the system is
// built to: one fleet can hold ten signage standards and six classroom
// standards under a single coarse type here. It nests by parent_id (av over
// room over board), on the same official/custom pattern as Component Types, and
// every identity fact (stem, abbrev, icon) inherits down the tree unless a node
// overrides it. There is no reparent leg: a custom type's placement is fixed at
// create, so the edit blade revises a node's own facts only.

function officialBadge(official: boolean): JSX.Element {
  return official
    ? <span class="badge badge-ghost badge-sm">official</span>
    : <span class="badge badge-outline badge-sm">custom</span>;
}

// Depth-indented identity cell: the tree reads without a dedicated tree widget,
// the same non-breaking-space indent SystemTypeSelect's options use, applied to
// a table cell instead of an <option>.
function NameCell(p: { row: SystemType & { depth: number } }): JSX.Element {
  return (
    <span class="flex min-w-0 items-center gap-1.5 py-0.5" style={{ "padding-left": `${p.row.depth * 14}px` }}>
      <span class="flex min-w-0 flex-col gap-0.5">
        <span class="truncate" style={{ "font-weight": 500 }}>{p.row.label}</span>
        <span class="truncate font-data text-[11px] text-base-content/40">{p.row.name}</span>
      </span>
    </span>
  );
}

// The Icon cell resolves through the same InheritedCell the other two inherited
// facts use, for the reason the component registry's copy of it records: it has
// shown the server's resolved glyph since #695 without saying where the glyph
// came from, and that is the treatment the other two now match.
function IconCell(p: { row: SystemType; resolvedIcon: string }): JSX.Element {
  return (
    <InheritedCell
      own={p.row.icon}
      resolved={p.resolvedIcon}
      leading={<Dynamic component={resolveIcon(p.resolvedIcon)} size={14} />}
    />
  );
}

export default function SystemTypes() {
  const me = useMe();
  const types = useQuery(() => ({ queryKey: SYSTEM_TYPES_KEY, queryFn: listSystemTypes }));

  // Tree order (parent immediately followed by its children, each level
  // alphabetical), not a flat alphabetical list: the registry's nesting is the
  // point of this page, so the row order teaches it.
  const treeRows = createMemo(() => flattenSystemTypeTree(systemTypeTree(types.data ?? [])));

  const columns: FlatColumn<SystemType & { depth: number }>[] = [
    // No sortVal: the row order IS the taxonomy (tree order, parent then
    // children), so this column deliberately does not offer to re-sort it away
    // into a flat alphabetical list.
    { key: "name", label: "Name", cell: (r) => <NameCell row={r} /> },
    // Parent is the row's own fact and never inherits: it IS the edge the other
    // three facts inherit along.
    { key: "parent", label: "Parent", width: "140px", cell: (r) => <span class="font-data text-xs text-base-content/60">{r.parent ?? EMPTY_VALUE}</span> },
    // Stem and Abbrev show what the row TAKES when it states nothing, and show
    // it undifferentiated (#743), the same treatment the component registry
    // gives the same three facts. Provenance is the blade's answer.
    { key: "stem", label: "Stem", width: "120px", cell: (r) => <InheritedCell own={r.stem} resolved={inheritedFact(r, "stem").value} /> },
    { key: "abbrev", label: "Abbrev", width: "90px", cell: (r) => <InheritedCell own={r.abbrev} resolved={inheritedFact(r, "abbrev").value} /> },
    { key: "icon", label: "Icon", width: "150px", cell: (r) => <IconCell row={r} resolvedIcon={r.resolved_icon || "map-pin"} /> },
    { key: "official", label: "Origin", width: "100px", sortVal: (r) => String(r.official), cell: (r) => officialBadge(r.official) },
  ];

  const canCreate = () => can(me.data, "system_type", "create");

  return (
    <FlatList<SystemType & { depth: number }>
      config={{
        entity: { name: "system type", plural: "system types" },
        rows: treeRows,
        loading: () => types.isPending,
        error: () => types.error,
        filterKeys: [
          { key: "name", type: "string", hint: "substring", get: (r) => `${entityLabel(r)} ${r.name}`, values: () => [] },
          { key: "official", type: "string", hint: "exact", get: (r) => (r.official ? "official" : "custom"), values: () => ["official", "custom"] },
        ],
        filterPlaceholder: "filter system types by name…",
        columns,
        empty: "No system types.",
        rowId: (r) => r.name,
        blades: { registry: { system_type: systemTypeBlade }, rootKind: "system_type" },
        create: canCreate()
          ? {
              label: "New system type",
              can: canCreate,
              // createSystemType returns a flat SystemType; the create Drawer's
              // select() wants this page's row shape (SystemType plus its tree
              // depth), so a freshly created row gets a synthetic depth of 0
              // rather than reflowing the whole tree.
              body: (ctx) => <CreateSystemTypeForm onCreated={(t) => ctx.select({ ...t, depth: 0 })} />,
            }
          : undefined,
      }}
    />
  );
}

// systemTypeBlade renders one registry row on the shared blade stack. An
// official row is read-only; a custom row carries Edit + Delete.
export const systemTypeBlade: BladeDef = {
  Title: (p) => <SystemTypeBladeTitle id={p.id} />,
  Body: (p) => <SystemTypeBladeBody id={p.id} />,
};

function useSystemTypeRow(id: string): () => SystemType | undefined {
  const types = useQuery(() => ({ queryKey: SYSTEM_TYPES_KEY, queryFn: listSystemTypes }));
  return () => (types.data ?? []).find((r) => r.name === id);
}

function SystemTypeBladeTitle(p: { id: string }): JSX.Element {
  return <BladeTitle row={useSystemTypeRow(p.id)} fallback={p.id} />;
}

function SystemTypeBladeBody(p: { id: string }): JSX.Element {
  const qc = useQueryClient();
  const me = useMe();
  const blades = useBlades();
  const edit = useBladeEdit();
  const row = useSystemTypeRow(p.id);
  const [err, setErr] = createSignal<string | null>(null);
  const [label, setLabel] = createSignal("");
  const [stem, setStem] = createSignal("");
  const [abbrev, setAbbrev] = createSignal("");
  const [icon, setIcon] = createSignal("");

  // Fill the drafts from the row as it stands; bound below, the slot runs this
  // on entering edit and again on reseed (#748).
  const seedDrafts = () => {
    const r = row();
    setLabel(r?.label ?? "");
    setStem(r?.stem ?? "");
    setAbbrev(r?.abbrev ?? "");
    setIcon(r?.icon ?? "");
    setErr(null);
  };

  async function removeType() {
    const r = row();
    if (!r) return;
    if (!confirm(`Delete system type "${r.name}"?`)) return;
    setErr(null);
    try {
      await deleteSystemType(r.id);
      blades.close();
      await qc.invalidateQueries({ queryKey: SYSTEM_TYPES_KEY });
    } catch (e) {
      setErr(describeError(e));
    }
  }

  async function save() {
    const r = row();
    if (!r) return;
    setErr(null);
    try {
      // An empty box means "this node declares no fact of its own, inherit the
      // nearest ancestor's", and the wire spells that with the empty string
      // (#716): the patch routes all three through a CASE where "" clears the
      // column to NULL, which is the state resolveSystemTypeFacts's walk reads
      // as inherit. Sending `undefined` instead is what this used to do, and it
      // made the clear inexpressible: the coalescing patch kept the old value
      // and the console silently retained a fact the operator had just deleted.
      //
      // The sentinel, not the update_mask: ADR-0106 scopes mask-with-no-value to
      // nullable OBJECT fields, and ADR-0091 keeps the sentinel for strings.
      // Emptying a ROOT type's stem is refused (422), since a root has no
      // ancestor to inherit one from.
      await updateSystemType(r.id, {
        label: label(),
        stem: stem().trim(),
        abbrev: abbrev().trim(),
        icon: icon().trim(),
      });
      await qc.invalidateQueries({ queryKey: SYSTEM_TYPES_KEY });
    } catch (e) {
      setErr(describeError(e));
      throw e; // keep the blade in edit mode on failure
    }
  }

  edit.bind({
    editable: () => !!row() && !row()!.official && can(me.data, "system_type", "update"),
    seed: seedDrafts,
    save,
    destructive: () =>
      row() && !row()!.official && can(me.data, "system_type", "delete")
        ? { label: "Delete", tone: "danger", onClick: removeType }
        : undefined,
    locked: () => registryLock(row(), me.data, "system_type"),
  });

  return (
    <Show when={row()} fallback={<p class="text-sm text-base-content/50">System type not found.</p>}>
      {(r) => (
        <div class="flex flex-col gap-4">
          <Show when={err()}>
            <div role="alert" class="alert alert-error alert-soft text-sm"><span>{err()}</span></div>
          </Show>
          <div class="grid grid-cols-2 gap-3 text-sm">
            <KVStacked label="Origin" value={officialBadge(r().official)} />
            <KVStacked label="Id" value={<span class="font-data text-xs text-base-content/60">{r().id}</span>} />
            <KVStacked
              label="Parent"
              value={r().parent ? <span class="font-data">{r().parent}</span> : <span class="text-base-content/50">Root</span>}
            />
          </div>
          <BladeField
            bind="label"
            value={() => r().label ?? ""}
            draft={label}
            onInput={setLabel}
          />
          {/*
            The same three inherited facts the component registry's blade shows,
            through the same field (#716): the placeholder carries the value this
            type would take with the box empty, the mark beside the label names
            the ancestor it comes from, and both are the server's answers off the
            listing.
          */}
          <InheritedField
            label="Stem"
            value={() => r().stem ?? ""}
            draft={stem}
            onInput={setStem}
            inherited={() => inheritedFact(r(), "stem")}
            hint="The prefix a generated system name is built from."
          />
          <InheritedField
            label="Abbrev"
            value={() => r().abbrev ?? ""}
            draft={abbrev}
            onInput={setAbbrev}
            inherited={() => inheritedFact(r(), "abbrev")}
            hint="The compact label form (br, cls, vw)."
          />
          <InheritedField
            label="Icon"
            value={() => r().icon ?? ""}
            draft={icon}
            onInput={setIcon}
            inherited={() => inheritedFact(r(), "icon")}
            hint="A glyph key."
          />
        </div>
      )}
    </Show>
  );
}

// CreateSystemTypeForm: names a new custom system_type. The label leads
// and the kebab name derives from it until the operator edits it by hand
// (lib/entities). Parent placement is chosen once, at create: the gateway has
// no reparent leg, so this is the only moment a custom type's position in the
// tree is set. Stem, abbrev, and icon are optional overrides; left blank, the
// type inherits its parent's, except on a ROOT, where a stem is required
// because there is no ancestor to inherit one from (the server refuses it).
export function CreateSystemTypeForm(p: { onCreated: (t: SystemType) => void }): JSX.Element {
  const qc = useQueryClient();
  const types = useQuery(() => ({ queryKey: SYSTEM_TYPES_KEY, queryFn: listSystemTypes }));
  const { display, setDisplay, name, setName, nameDerived } = createIdentity();
  const [parentId, setParentId] = createSignal("");
  const [stem, setStem] = createSignal("");
  const [abbrev, setAbbrev] = createSignal("");
  const [icon, setIcon] = createSignal("");
  const [busy, setBusy] = createSignal(false);
  const [formErr, setFormErr] = createSignal<string | null>(null);

  useFormActions().bind({
    submitLabel: "Create system type",
    submitIcon: Plus,
    submit: () => void submit(),
    busy,
    disabled: () => !name().trim() || !display().trim(),
  });

  async function submit() {
    setBusy(true);
    setFormErr(null);
    try {
      const created = await createSystemType({
        name: name().trim(),
        label: display().trim(),
        parent_id: parentId() || undefined,
        stem: stem().trim() || undefined,
        abbrev: abbrev().trim() || undefined,
        icon: icon().trim() || undefined,
      });
      await qc.invalidateQueries({ queryKey: SYSTEM_TYPES_KEY });
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
      <FieldRow bind="label" hint="What an operator reads.">
        <input class="input input-bordered w-full" value={display()} placeholder="Huddle Room" onInput={(e) => setDisplay(e.currentTarget.value)} />
      </FieldRow>
      <FieldRow bind="name" hint={nameDerived() ? "Derived from the label. Edit to set your own." : "Globally unique address, used by the API and CLI."}>
        <input class="input input-bordered w-full font-data" value={name()} placeholder="huddle" onInput={(e) => setName(e.currentTarget.value)} />
      </FieldRow>
      <FieldRow label="Parent" hint={TYPE_PARENT_HINT}>
        <SystemTypeSelect types={types.data ?? []} value={parentId()} onChange={setParentId} emptyLabel="Root (no parent)" />
      </FieldRow>
      <FieldRow label="Stem" hint={`The prefix a generated system name is built from. ${ROOT_STEM_HINT}`}>
        <input class="input input-bordered w-full font-data" value={stem()} placeholder="inherit" onInput={(e) => setStem(e.currentTarget.value)} />
      </FieldRow>
      <FieldRow label="Abbrev" hint="The compact label form (br, cls, vw). Leave blank to inherit.">
        <input class="input input-bordered w-full font-data" value={abbrev()} placeholder="inherit" onInput={(e) => setAbbrev(e.currentTarget.value)} />
      </FieldRow>
      <FieldRow label="Icon" hint="A glyph key. Leave blank to inherit.">
        <input class="input input-bordered w-full font-data" value={icon()} placeholder="inherit" onInput={(e) => setIcon(e.currentTarget.value)} />
      </FieldRow>
    </form>
  );
}
