import { For, Show, createEffect, createMemo, createSignal, on, type JSX } from "solid-js";
import { useQuery, useQueryClient } from "@tanstack/solid-query";
import FlatList, { type FlatColumn } from "../components/FlatList";
import KVStacked from "../components/KVStacked";
import Button from "../components/Button";
import { DrawerFooter } from "../components/Drawer";
import { Plus } from "../components/icons";
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
  type FieldDataType,
  FIELD_DATA_TYPES,
  FIELD_DEFINITIONS_KEY,
  listFieldDefinitions,
  createFieldDefinition,
} from "../lib/fields";
import { displayValue, parseInput } from "../lib/variables";
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

// ComponentTypeFields is the field-definition editor on a component_type blade: it
// lists the fields declared on this type (name, data_type, and default) and, when
// the caller holds field:create, an inline add row (name + a data_type select +
// optional default). It reads the whole field-definition catalog and filters to this
// type's id. Rendered outside the type's edit mode: a field is operator data layered
// onto the type, so it is editable even for a read-only (official) component_type.
function ComponentTypeFields(props: { typeId: string; canCreate: boolean }): JSX.Element {
  const qc = useQueryClient();
  const defs = useQuery(() => ({ queryKey: FIELD_DEFINITIONS_KEY, queryFn: listFieldDefinitions }));
  const rows = createMemo(() =>
    (defs.data ?? [])
      .filter((d) => d.component_type === props.typeId)
      .sort((a, b) => a.name.localeCompare(b.name)),
  );

  const [name, setName] = createSignal("");
  const [displayName, setDisplayName] = createSignal("");
  const [dataType, setDataType] = createSignal<FieldDataType>("string");
  const [defaultText, setDefaultText] = createSignal("");
  const [required, setRequired] = createSignal(false);
  const [busy, setBusy] = createSignal(false);
  const [err, setErr] = createSignal<string | null>(null);

  async function add(e: Event) {
    e.preventDefault();
    setBusy(true);
    setErr(null);
    // A blank default leaves the field with no type-level default; a non-blank one
    // is coerced to the data_type (so an int default is a number, not a string).
    let default_value: unknown;
    const raw = defaultText().trim();
    if (raw !== "") {
      try {
        default_value = parseInput(dataType(), defaultText());
      } catch (er) {
        setErr(describeError(er));
        setBusy(false);
        return;
      }
    }
    try {
      await createFieldDefinition({
        component_type: props.typeId,
        name: name().trim(),
        ...(displayName().trim() === "" ? {} : { display_name: displayName().trim() }),
        data_type: dataType(),
        ...(default_value === undefined ? {} : { default_value }),
        required: required(),
      });
      await qc.invalidateQueries({ queryKey: FIELD_DEFINITIONS_KEY });
      setName("");
      setDisplayName("");
      setDefaultText("");
      setDataType("string");
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
          {(d) => (
            <div class="flex items-center gap-2 text-sm">
              <span class="min-w-0 truncate">
                {d.display_name || d.name}
                <Show when={d.required}><span class="ml-1 font-semibold text-error" aria-label="required">*</span></Show>
                <span class="ml-2 font-data text-[10px] font-normal text-base-content/40">{d.data_type}</span>
              </span>
              <Show when={d.display_name}><span class="shrink-0 font-data text-[11px] text-base-content/40">{d.name}</span></Show>
              <span class="flex-1" />
              <Show
                when={d.default_value !== undefined && d.default_value !== null}
                fallback={<span class="shrink-0 text-[11px] text-base-content/40">no default</span>}
              >
                <span class="shrink-0 font-data text-sm text-base-content/60">{displayValue(d.default_value)}</span>
              </Show>
            </div>
          )}
        </For>
        <Show when={props.canCreate}>
          <form class="flex flex-col gap-2 border-t border-base-300 pt-2" onSubmit={add}>
            <Show when={err()}>
              <div role="alert" class="alert alert-error alert-soft text-xs"><span>{err()}</span></div>
            </Show>
            <div class="flex flex-wrap items-end gap-2">
              <input
                class="input input-bordered input-sm w-40"
                placeholder="Display name (optional)"
                value={displayName()}
                onInput={(e) => setDisplayName(e.currentTarget.value)}
              />
              <input
                class="input input-bordered input-sm w-36 font-data"
                placeholder="asset_tag"
                value={name()}
                onInput={(e) => setName(e.currentTarget.value)}
              />
              <select
                class="select select-bordered select-sm w-28"
                value={dataType()}
                onChange={(e) => { setDataType(e.currentTarget.value as FieldDataType); setDefaultText(""); }}
              >
                <For each={FIELD_DATA_TYPES}>{(t) => <option value={t}>{t}</option>}</For>
              </select>
              <input
                class="input input-bordered input-sm w-32 font-data"
                placeholder="default (optional)"
                type={dataType() === "int" || dataType() === "float" ? "number" : "text"}
                value={defaultText()}
                onInput={(e) => setDefaultText(e.currentTarget.value)}
              />
              <label class="flex items-center gap-1.5 text-xs text-base-content/70">
                <input type="checkbox" class="checkbox checkbox-sm" checked={required()} onChange={(e) => setRequired(e.currentTarget.checked)} />
                required
              </label>
              <Button type="submit" intent="action" icon={Plus} disabled={busy() || !name().trim()}>Add</Button>
            </div>
            <span class="text-[11px] text-base-content/40">A default applies to every component of this type until the component sets its own value.</span>
          </form>
        </Show>
      </div>
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
