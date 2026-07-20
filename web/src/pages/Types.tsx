import { For, Show, createEffect, createMemo, createSignal, on, type JSX } from "solid-js";
import { useQuery, useQueryClient } from "@tanstack/solid-query";
import FlatList, { type FlatColumn } from "../components/FlatList";
import KVStacked from "../components/KVStacked";
import Button from "../components/Button";
import KeyPicker from "../components/KeyPicker";
import { DrawerFooter } from "../components/Drawer";
import { ValueInput } from "./Variables";
import { Plus, Pencil, Trash, Check } from "../components/icons";
import {
  type TypeKind,
  type TypeRow,
  TYPE_KINDS,
  TYPES_KEY,
  ROOT_PLACEMENT,
  listTypes,
  createType,
  updateType,
  deleteType,
} from "../lib/types";
import {
  type FieldDefinition,
  FIELD_DEFINITIONS_KEY,
  listFieldDefinitions,
  createFieldDefinition,
  updateFieldDefinition,
  deleteFieldDefinition,
} from "../lib/fields";
import { type KeyRow } from "../lib/keys";
import { displayValue, parseInput, type ValueType } from "../lib/variables";
import { useMe, can } from "../lib/auth";
import { describeError } from "../lib/format";
import { type BladeDef, useBlades, useBladeEdit } from "../lib/blades";

// Types: the classifier registries (location, system, component, secret), one
// per tab. Each tab is that registry's own directory over the FlatList surface;
// a row is addressed by kind + id (the write paths key on id within a kind, not
// globally). secret_type and any official (seed-owned) row are read-only this
// slice; the writable rows are custom location/system/component entries.

const KIND_LABEL: Record<TypeKind, string> = {
  location: "Location",
  system: "System",
  component: "Component",
  secret: "Secret",
};

function kindBadge(kind: TypeKind): JSX.Element {
  return <span class="badge badge-ghost badge-sm font-data">{kind}</span>;
}

function officialBadge(official: boolean): JSX.Element {
  return official
    ? <span class="badge badge-ghost badge-sm">official</span>
    : <span class="badge badge-outline badge-sm">custom</span>;
}

// Columns for the active kind: every kind shows id, display name, and origin;
// location alone adds its icon glyph. There is no Kind column because the tab
// already names the kind.
function columnsFor(kind: TypeKind): FlatColumn<TypeRow>[] {
  const cols: FlatColumn<TypeRow>[] = [
    { key: "id", label: "Id", sortVal: (r) => r.id, cell: (r) => <span class="font-data font-semibold">{r.id}</span> },
    { key: "display_name", label: "Display name", sortVal: (r) => r.display_name, cell: (r) => <span>{r.display_name}</span> },
  ];
  if (kind === "location") {
    cols.push({ key: "icon", label: "Icon", width: "110px", cell: (r) => <span class="font-data text-xs text-base-content/60">{r.icon ?? "—"}</span> });
  }
  cols.push({ key: "official", label: "Origin", width: "100px", sortVal: (r) => String(r.official), cell: (r) => officialBadge(r.official) });
  return cols;
}

export default function Types() {
  const me = useMe();
  const types = useQuery(() => ({ queryKey: TYPES_KEY, queryFn: listTypes }));
  const [kind, setKind] = createSignal<TypeKind>("location");

  // Rows for the active kind, sorted alphabetically by display name then id.
  const rowsFor = (k: TypeKind) =>
    (types.data ?? [])
      .filter((r) => r.kind === k)
      .sort((a, b) => a.display_name.localeCompare(b.display_name) || a.id.localeCompare(b.id));

  return (
    <div class="flex min-h-full flex-col gap-4">
      <div role="tablist" class="tabs tabs-box w-fit">
        <For each={TYPE_KINDS}>
          {(k) => (
            <button
              role="tab"
              class="tab"
              classList={{ "tab-active": kind() === k }}
              onClick={() => setKind(k)}
            >
              {KIND_LABEL[k]}
            </button>
          )}
        </For>
      </div>
      {/* Keyed on the active kind so the FlatList rebuilds with that kind's
          static config (columns, create, placeholder); the row list itself stays
          a live accessor over the shared listTypes query. */}
      <Show when={kind()} keyed>
        {(k) => {
          const label = KIND_LABEL[k].toLowerCase();
          const canCreate = () => k !== "secret" && can(me.data, "type", "create");
          return (
            <FlatList<TypeRow>
              config={{
                entity: { name: "type", plural: "types" },
                rows: () => rowsFor(k),
                loading: () => types.isPending,
                error: () => types.error,
                filterKeys: [
                  { key: "name", type: "string", hint: "substring", get: (r) => `${r.id} ${r.display_name}`, values: () => [] },
                  { key: "official", type: "string", hint: "exact", get: (r) => (r.official ? "official" : "custom"), values: () => ["official", "custom"] },
                ],
                filterPlaceholder: `filter ${label} types by id, name…`,
                columns: columnsFor(k),
                empty: `No ${label} types.`,
                // Address a row by kind + id: the registries are per-kind, and an
                // id is unique only within its own kind.
                rowId: (r) => `${r.kind}:${r.id}`,
                blades: { registry: { type: typeBlade }, rootKind: "type" },
                create: canCreate()
                  ? { label: "New type", can: canCreate, body: (ctx) => <CreateTypeForm kind={k} onCreated={ctx.close} /> }
                  : undefined,
              }}
            />
          );
        }}
      </Show>
    </div>
  );
}

// typeBlade renders a kind:id row on the shared blade stack. Secret rows and
// official rows are read-only (no pencil, no destructive action); a custom
// location/system/component row carries Edit + Delete.
export const typeBlade: BladeDef = {
  Title: (p) => <TypeBladeTitle id={p.id} />,
  Body: (p) => <TypeBladeBody id={p.id} />,
};

// The blade id is "<kind>:<id>"; split on the FIRST colon (ids are kebab, no
// colons of their own) and look the row up from the cached listTypes query.
function splitBladeId(id: string): { kind: TypeKind; id: string } {
  const i = id.indexOf(":");
  return i < 0 ? { kind: id as TypeKind, id: "" } : { kind: id.slice(0, i) as TypeKind, id: id.slice(i + 1) };
}

function useTypeRow(id: string): () => TypeRow | undefined {
  const types = useQuery(() => ({ queryKey: TYPES_KEY, queryFn: listTypes }));
  const { kind, id: rowId } = splitBladeId(id);
  return () => (types.data ?? []).find((r) => r.kind === kind && r.id === rowId);
}

function TypeBladeTitle(p: { id: string }): JSX.Element {
  const row = useTypeRow(p.id);
  return <span class="font-data">{row()?.id ?? splitBladeId(p.id).id}</span>;
}

function TypeBladeBody(p: { id: string }): JSX.Element {
  const qc = useQueryClient();
  const me = useMe();
  const blades = useBlades();
  const edit = useBladeEdit();
  const row = useTypeRow(p.id);
  const [err, setErr] = createSignal<string | null>(null);
  const [displayName, setDisplayName] = createSignal("");
  const [icon, setIcon] = createSignal("");
  const allTypes = useQuery(() => ({ queryKey: TYPES_KEY, queryFn: listTypes }));
  const locationTypeOptions = () => (allTypes.data ?? []).filter((t) => t.kind === "location");
  const [allowedParents, setAllowedParents] = createSignal<string[]>([]);

  createEffect(on(edit.editing, (editing) => {
    if (!editing) return;
    const r = row();
    setDisplayName(r?.display_name ?? "");
    setIcon(r?.icon ?? "");
    setAllowedParents(r?.allowed_parent_types ?? []);
    setErr(null);
  }));

  async function removeType() {
    const r = row();
    if (!r) return;
    if (!confirm(`Delete ${r.kind} type "${r.id}"?`)) return;
    setErr(null);
    try {
      await deleteType(r.kind, r.id);
      blades.close();
      await qc.invalidateQueries({ queryKey: TYPES_KEY });
    } catch (e) {
      setErr(describeError(e));
    }
  }

  async function save() {
    const r = row();
    if (!r) return;
    setErr(null);
    try {
      await updateType(r.kind, r.id, {
        display_name: displayName(),
        ...(r.kind === "location" ? { icon: icon(), allowed_parent_types: allowedParents() } : {}),
      });
      await qc.invalidateQueries({ queryKey: TYPES_KEY });
    } catch (e) {
      setErr(describeError(e));
      throw e; // keep the blade in edit mode on failure
    }
  }

  edit.bind({
    editable: () => !!row() && row()!.kind !== "secret" && !row()!.official && can(me.data, "type", "update"),
    save,
    destructive: () =>
      row() && row()!.kind !== "secret" && !row()!.official && can(me.data, "type", "delete")
        ? { label: "Delete", tone: "danger", onClick: removeType }
        : undefined,
  });

  return (
    <Show when={row()} fallback={<p class="text-sm text-base-content/50">Type not found.</p>}>
      {(r) => (
        <div class="flex flex-col gap-4">
          <Show when={err()}>
            <div role="alert" class="alert alert-error alert-soft text-sm"><span>{err()}</span></div>
          </Show>
          <div class="grid grid-cols-2 gap-3 text-sm">
            <KVStacked label="Kind" value={kindBadge(r().kind)} />
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
          <Show when={r().kind === "location"}>
            <div class="flex flex-col gap-1.5">
              <span class="eyebrow">Icon</span>
              <Show
                when={edit.editing()}
                fallback={<div class="input input-bordered flex items-center text-sm font-data">{r().icon ?? "map-pin"}</div>}
              >
                <input class="input input-bordered w-full font-data" placeholder="map-pin" value={icon()} onInput={(e) => setIcon(e.currentTarget.value)} />
              </Show>
            </div>
          </Show>
          <Show when={r().kind === "location"}>
            <div class="flex flex-col gap-1.5">
              <span class="eyebrow">Allowed parents</span>
              <Show
                when={edit.editing()}
                fallback={
                  <Show
                    when={(r().allowed_parent_types?.length ?? 0) > 0}
                    fallback={<span class="text-sm text-base-content/50">Unconstrained (any parent, or root).</span>}
                  >
                    <div class="flex flex-wrap gap-1.5">
                      <For each={r().allowed_parent_types}>
                        {(pid) => (
                          <span class="badge badge-outline badge-sm">
                            {pid === ROOT_PLACEMENT ? "Root" : locationTypeOptions().find((t) => t.id === pid)?.display_name ?? pid}
                          </span>
                        )}
                      </For>
                    </div>
                  </Show>
                }
              >
                <AllowedParentsPicker options={locationTypeOptions()} value={allowedParents()} onChange={setAllowedParents} />
              </Show>
              <span class="text-[11px] text-base-content/40">Empty allows any parent (or root). A non-empty set is enforced on create and move.</span>
            </div>
          </Show>
          <Show when={r().kind === "secret"}>
            <div class="flex flex-col gap-1.5">
              <span class="eyebrow">Fields</span>
              <div class="flex flex-col gap-2 rounded-box border border-base-300 p-2.5">
                <For each={r().fields} fallback={<span class="text-[11px] text-base-content/40">No fields declared.</span>}>
                  {(f) => (
                    <div class="flex items-center justify-between gap-2 text-sm">
                      <span class="font-data">{f.name}</span>
                      <span class="flex items-center gap-1.5 text-xs text-base-content/60">
                        <span class="badge badge-ghost badge-sm font-data">{f.type}</span>
                        <Show when={f.secret}><span class="badge badge-ghost badge-sm">secret</span></Show>
                        <span class="text-base-content/40">{f.origin}</span>
                      </span>
                    </div>
                  )}
                </For>
              </div>
              <span class="text-[11px] text-base-content/40">Secret types are read-only here; editing the fields schema is a follow-up.</span>
            </div>
          </Show>
          <Show when={r().kind === "component"}>
            <ComponentTypeFields typeId={r().id} canCreate={can(me.data, "field", "create")} />
          </Show>
          <Show when={r().official}>
            <div role="alert" class="alert alert-soft text-sm"><span>Seed-owned, read-only.</span></div>
          </Show>
        </div>
      )}
    </Show>
  );
}

// ComponentTypeFields is the field-definition editor on a component_type blade. It
// lists the fields declared on this type, each with its display name, key, data_type,
// default, and required flag, and (holding field:create) offers a KeyPicker-driven
// add row: pick a registered key from the catalog, then a type-aware default input
// keyed by the key's data_type, and a required toggle. Each declared field carries a
// per-row edit (default + required) and delete, so the type schema is fully
// operable. It reads the whole field-definition catalog and filters to this type's
// id. Rendered outside the type's edit mode: a field is operator data layered onto
// the type, so it is editable even for a read-only (official) component_type.
function ComponentTypeFields(props: { typeId: string; canCreate: boolean }): JSX.Element {
  const qc = useQueryClient();
  const defs = useQuery(() => ({ queryKey: FIELD_DEFINITIONS_KEY, queryFn: listFieldDefinitions }));
  const rows = createMemo(() =>
    (defs.data ?? [])
      .filter((d) => d.component_type === props.typeId)
      .sort((a, b) => (a.display_name || a.name).localeCompare(b.display_name || b.name)),
  );
  // Keys already declared on this type, so the picker never offers a duplicate.
  const usedKeys = createMemo(() => rows().map((d) => d.key ?? d.name));
  const refresh = () => qc.invalidateQueries({ queryKey: FIELD_DEFINITIONS_KEY });

  const [pickedKey, setPickedKey] = createSignal<KeyRow | null>(null);
  const [defaultText, setDefaultText] = createSignal("");
  const [required, setRequired] = createSignal(false);
  const [busy, setBusy] = createSignal(false);
  const [err, setErr] = createSignal<string | null>(null);

  async function add(e: Event) {
    e.preventDefault();
    const key = pickedKey();
    if (!key) return;
    setBusy(true);
    setErr(null);
    // A blank default leaves the field with no type-level default; a non-blank one
    // is coerced to the key's data_type (so an int default is a number, not a string).
    let default_value: unknown;
    const raw = defaultText().trim();
    if (raw !== "") {
      try {
        default_value = parseInput(key.data_type as ValueType, defaultText());
      } catch (er) {
        setErr(describeError(er));
        setBusy(false);
        return;
      }
    }
    try {
      await createFieldDefinition({
        component_type: props.typeId,
        key: key.name,
        ...(default_value === undefined ? {} : { default_value }),
        required: required(),
      });
      await refresh();
      setPickedKey(null);
      setDefaultText("");
      setRequired(false);
    } catch (er) {
      setErr(describeError(er));
    } finally {
      setBusy(false);
    }
  }

  return (
    <div class="flex flex-col gap-1.5">
      <span class="eyebrow">Fields</span>
      <div class="flex flex-col gap-2 rounded-box border border-base-300 p-2.5">
        <For each={rows()} fallback={<span class="text-[11px] text-base-content/40">No fields declared.</span>}>
          {(d) => <FieldDefRow def={d} canEdit={props.canCreate} onChanged={refresh} />}
        </For>
        <Show when={props.canCreate}>
          <form class="flex flex-col gap-2 border-t border-base-300 pt-2" onSubmit={add}>
            <Show when={err()}>
              <div role="alert" class="alert alert-error alert-soft text-xs"><span>{err()}</span></div>
            </Show>
            {/* Not wrapped in a <label>: the KeyPicker's trigger is a button, and a
                button inside a label steals the label and breaks hover. */}
            <div class="flex flex-col gap-1">
              <span class="text-[11px] font-medium text-base-content/70">Add a field</span>
              <KeyPicker
                value={pickedKey()?.name}
                onSelect={(k) => { setPickedKey(k); setDefaultText(""); }}
                exclude={usedKeys()}
                aria-label="Field key"
                placeholder="Pick a key…"
              />
            </div>
            {/* Once a key is picked, its type and label are surfaced, and the default
                input is keyed by the key's data_type. */}
            <Show when={pickedKey()}>
              {(k) => (
                <div class="flex flex-col gap-2">
                  <div class="flex flex-wrap items-center gap-2 text-[11px] text-base-content/50">
                    <span>key <span class="font-data text-base-content/70">{k().name}</span></span>
                    <span>type <span class="badge badge-ghost badge-sm font-data">{k().data_type}</span></span>
                    <Show when={k().display_name}><span>label <span class="text-base-content/70">{k().display_name}</span></span></Show>
                  </div>
                  <div class="flex flex-wrap items-center gap-2">
                    <div class="w-40">
                      <ValueInput
                        valueType={k().data_type as ValueType}
                        value={defaultText()}
                        onInput={setDefaultText}
                        placeholder="default (optional)"
                      />
                    </div>
                    <label class="flex items-center gap-1.5 text-xs text-base-content/70">
                      <input type="checkbox" class="checkbox checkbox-sm" checked={required()} onChange={(e) => setRequired(e.currentTarget.checked)} />
                      required
                    </label>
                    <Button type="submit" intent="action" icon={Plus} disabled={busy()}>Add</Button>
                  </div>
                </div>
              )}
            </Show>
            <span class="text-[11px] text-base-content/40">A field draws its name, type, and label from the key. A default applies to every component of this type until the component sets its own value.</span>
          </form>
        </Show>
      </div>
    </div>
  );
}

// FieldDefRow renders one declared field with an inline edit (its default and
// required flag; the key, data_type, and label are fixed) and a delete. The read
// row shows the display name, the mono key, the data_type badge, and the default so
// the schema reads clearly.
function FieldDefRow(props: { def: FieldDefinition; canEdit: boolean; onChanged: () => Promise<unknown> }): JSX.Element {
  const [editing, setEditing] = createSignal(false);
  const [defaultText, setDefaultText] = createSignal("");
  const [required, setRequired] = createSignal(false);
  const [busy, setBusy] = createSignal(false);
  const [err, setErr] = createSignal<string | null>(null);
  const dt = () => props.def.data_type as ValueType;
  const key = () => props.def.key ?? props.def.name;
  const hasDefault = () => props.def.default_value !== undefined && props.def.default_value !== null;

  function startEdit() {
    setDefaultText(hasDefault() ? displayValue(props.def.default_value) : "");
    setRequired(props.def.required);
    setErr(null);
    setEditing(true);
  }

  async function save() {
    setBusy(true);
    setErr(null);
    // A blank default clears the type-level default (omitted from the patch); a
    // non-blank one is coerced to the field's data_type.
    let default_value: unknown;
    const raw = defaultText().trim();
    if (raw !== "") {
      try {
        default_value = parseInput(dt(), defaultText());
      } catch (er) {
        setErr(describeError(er));
        setBusy(false);
        return;
      }
    }
    try {
      await updateFieldDefinition(props.def.id, {
        required: required(),
        ...(default_value === undefined ? {} : { default_value }),
      });
      await props.onChanged();
      setEditing(false);
    } catch (er) {
      setErr(describeError(er));
    } finally {
      setBusy(false);
    }
  }

  async function remove() {
    if (!confirm(`Delete field "${props.def.display_name || props.def.name}" from this type?`)) return;
    setBusy(true);
    setErr(null);
    try {
      await deleteFieldDefinition(props.def.id);
      await props.onChanged();
    } catch (er) {
      setErr(describeError(er));
      setBusy(false);
    }
  }

  return (
    <div class="flex flex-col gap-1.5">
      <div class="flex items-center gap-2 text-sm">
        <span class="min-w-0 truncate">
          {props.def.display_name || props.def.name}
          <Show when={props.def.required}><span class="ml-1 font-semibold text-error" aria-label="required">*</span></Show>
        </span>
        <span class="shrink-0 font-data text-[11px] text-base-content/40">{key()}</span>
        <span class="badge badge-ghost badge-sm shrink-0 font-data">{props.def.data_type}</span>
        <span class="flex-1" />
        <Show
          when={hasDefault()}
          fallback={<span class="shrink-0 text-[11px] text-base-content/40">no default</span>}
        >
          <span class="shrink-0 font-data text-sm text-base-content/60">{displayValue(props.def.default_value)}</span>
        </Show>
        <Show when={props.canEdit && !editing()}>
          <button type="button" class="shrink-0 text-base-content/40 hover:text-base-content" aria-label={`Edit ${key()}`} onClick={startEdit}>
            <Pencil size={13} />
          </button>
          <button type="button" class="shrink-0 text-base-content/40 hover:text-error" aria-label={`Delete ${key()}`} disabled={busy()} onClick={remove}>
            <Trash size={13} />
          </button>
        </Show>
      </div>
      <Show when={editing()}>
        <div class="flex flex-col gap-2 rounded-box border border-base-300 bg-base-200/40 p-2">
          <Show when={err()}>
            <div role="alert" class="alert alert-error alert-soft text-xs"><span>{err()}</span></div>
          </Show>
          <div class="flex flex-wrap items-center gap-2">
            <div class="w-40">
              <ValueInput valueType={dt()} value={defaultText()} onInput={setDefaultText} placeholder="default (optional)" />
            </div>
            <label class="flex items-center gap-1.5 text-xs text-base-content/70">
              <input type="checkbox" class="checkbox checkbox-sm" checked={required()} onChange={(e) => setRequired(e.currentTarget.checked)} />
              required
            </label>
            <Button type="button" intent="action" icon={Check} disabled={busy()} onClick={save}>Save</Button>
            <Button type="button" intent="quiet" disabled={busy()} onClick={() => setEditing(false)}>Cancel</Button>
          </div>
        </div>
      </Show>
    </div>
  );
}

// CreateTypeForm: name the id and set the display name for a new custom type of
// the active kind (the tab decides the kind; secret_type has no write routes
// this slice, so it never opens this form). A location type also gets an icon
// glyph key.
export function CreateTypeForm(p: { kind: TypeKind; onCreated: (id: string) => void }): JSX.Element {
  const qc = useQueryClient();
  const types = useQuery(() => ({ queryKey: TYPES_KEY, queryFn: listTypes }));
  const locationTypeOptions = () => (types.data ?? []).filter((r) => r.kind === "location");
  const [id, setId] = createSignal("");
  const [displayName, setDisplayName] = createSignal("");
  const [icon, setIcon] = createSignal("");
  const [allowedParents, setAllowedParents] = createSignal<string[]>([]);
  const [busy, setBusy] = createSignal(false);
  const [formErr, setFormErr] = createSignal<string | null>(null);

  async function submit(e: Event) {
    e.preventDefault();
    setBusy(true);
    setFormErr(null);
    try {
      await createType(p.kind, {
        id: id().trim(),
        display_name: displayName().trim(),
        ...(p.kind === "location" ? { icon: icon().trim() || "map-pin", allowed_parent_types: allowedParents() } : {}),
      });
      await qc.invalidateQueries({ queryKey: TYPES_KEY });
      p.onCreated(id().trim());
    } catch (er) {
      setFormErr(describeError(er));
    } finally {
      setBusy(false);
    }
  }

  return (
    <form class="flex min-h-full flex-col gap-4" onSubmit={submit}>
      <Show when={formErr()}>
        <div role="alert" class="alert alert-error alert-soft text-sm"><span>{formErr()}</span></div>
      </Show>
      <div class="flex items-center gap-2 text-sm text-base-content/70">
        <span class="eyebrow">Kind</span>
        {kindBadge(p.kind)}
      </div>
      <Field label="Id" hint="A kebab id, e.g. wing.">
        <input class="input input-bordered w-full font-data" value={id()} placeholder="wing" onInput={(e) => setId(e.currentTarget.value)} />
      </Field>
      <Field label="Display name">
        <input class="input input-bordered w-full" value={displayName()} placeholder="Wing" onInput={(e) => setDisplayName(e.currentTarget.value)} />
      </Field>
      <Show when={p.kind === "location"}>
        <Field label="Icon" hint="A glyph key, e.g. map-pin (the default).">
          <input class="input input-bordered w-full font-data" value={icon()} placeholder="map-pin" onInput={(e) => setIcon(e.currentTarget.value)} />
        </Field>
      </Show>
      <Show when={p.kind === "location"}>
        {/* Not wrapped in Field: Field's root is a <label>, and a picker of one
            <label> per checkbox nested inside it is invalid HTML that makes a
            for-less outer label forward a click on the heading or hint straight
            to the first checkbox. The heading and hint render as plain text
            instead. */}
        <div class="flex flex-col gap-1.5">
          <span class="eyebrow">Allowed parents</span>
          <AllowedParentsPicker options={locationTypeOptions()} value={allowedParents()} onChange={setAllowedParents} />
          <span class="text-[11px] text-base-content/40">Where a location of this type may be placed. Leave every box unchecked to allow any parent (unconstrained).</span>
        </div>
      </Show>
      <DrawerFooter>
        <Button type="submit" intent="action" icon={Plus} disabled={busy() || !id().trim() || !displayName().trim()}>Create type</Button>
      </DrawerFooter>
    </form>
  );
}

// AllowedParentsPicker: a checkbox per location type plus a Root option, the set
// of types a location of this kind may be placed under. No box checked means
// unconstrained (any parent, or root). Mirrors Tags.tsx's AppliesToPicker;
// shared by the create form and the edit blade so the markup and toggle logic
// exist once. Each option is its own <label> (not nested inside another one),
// so a click on it only ever toggles that option's own checkbox.
function AllowedParentsPicker(p: { options: TypeRow[]; value: string[]; onChange: (v: string[]) => void }): JSX.Element {
  function toggle(id: string) {
    p.onChange(p.value.includes(id) ? p.value.filter((x) => x !== id) : [...p.value, id]);
  }
  return (
    <div class="flex flex-col gap-1.5 rounded-box border border-base-300 p-2.5">
      <label class="flex items-center gap-2 text-sm">
        <input type="checkbox" class="checkbox checkbox-sm" checked={p.value.includes(ROOT_PLACEMENT)} onChange={() => toggle(ROOT_PLACEMENT)} />
        <span>Root (no parent)</span>
      </label>
      <For each={p.options}>
        {(t) => (
          <label class="flex items-center gap-2 text-sm">
            <input type="checkbox" class="checkbox checkbox-sm" checked={p.value.includes(t.id)} onChange={() => toggle(t.id)} />
            <span>{t.display_name}</span>
            <span class="font-data text-xs text-base-content/40">{t.id}</span>
          </label>
        )}
      </For>
    </div>
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
