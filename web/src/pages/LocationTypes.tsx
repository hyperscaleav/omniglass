import { For, Show, createEffect, createSignal, on, type JSX } from "solid-js";
import { useQuery, useQueryClient } from "@tanstack/solid-query";
import FlatList, { type FlatColumn } from "../components/FlatList";
import BladeTitle from "../components/BladeTitle";
import FieldRow from "../components/FieldRow";
import BladeField from "../components/BladeField";
import { identityColumn } from "../components/IdentityCell";
import KVStacked from "../components/KVStacked";
import { useFormActions } from "../lib/formactions";
import ContractEditor from "../components/ContractEditor";
import { Plus } from "../components/icons";
import {
  type LocationType,
  LOCATION_TYPES_KEY,
  ROOT_PLACEMENT,
  listLocationTypes,
  createLocationType,
  updateLocationType,
  deleteLocationType,
} from "../lib/location_types";
import { useMe, can } from "../lib/auth";
import { createIdentity } from "../lib/entities";
import { describeError } from "../lib/format";
import { type BladeDef, useBlades, useBladeEdit } from "../lib/blades";

// Location Types: the place classifier registry (campus, building, floor, room,
// and your own) on the FlatList surface. The registry is operator-owned example
// content: the shipped rows seed only-if-absent, so every row here is normally
// custom and writable (create, edit, delete, gated location_type:*); an official
// row, where one exists, is seed-owned and read-only. A row's detail carries the
// location type's declared-property contract on the shared ContractEditor.
//
// This page names ONE registry on purpose. Its former home, the tabbed Types
// page, fetched the secret_type registry jointly and threw if either fetch
// failed, so a plain viewer (no secret:read) lost this page too (#598). The
// secret registry now lives on its own read-only page (SecretTypes.tsx).
//
// The other two shape-definers are catalog entities of their own, each with its
// own page and contract editor: a system's shape is the STANDARD it conforms to
// (Standards), and a component's is the product it is an instance of (Products).

function officialBadge(official: boolean): JSX.Element {
  return official
    ? <span class="badge badge-ghost badge-sm">official</span>
    : <span class="badge badge-outline badge-sm">custom</span>;
}

// Identity (the display name above the name, ADR-0062: the name is the
// operator-facing address; the uuid stays in the blade), the icon glyph, origin.
const columns: FlatColumn<LocationType>[] = [
  identityColumn<LocationType>(),
  { key: "icon", label: "Icon", width: "110px", cell: (r) => <span class="font-data text-xs text-base-content/60">{r.icon ?? "\u2014"}</span> },
  { key: "official", label: "Origin", width: "100px", sortVal: (r) => String(r.official), cell: (r) => officialBadge(r.official) },
];

export default function LocationTypes() {
  const me = useMe();
  const types = useQuery(() => ({ queryKey: LOCATION_TYPES_KEY, queryFn: listLocationTypes }));

  // Sorted alphabetically by display name then name.
  const rows = () =>
    [...(types.data ?? [])].sort((a, b) => a.display_name.localeCompare(b.display_name) || a.name.localeCompare(b.name));

  const canCreate = () => can(me.data, "location_type", "create");

  return (
    <FlatList<LocationType>
      config={{
        entity: { name: "location type", plural: "location types" },
        rows,
        loading: () => types.isPending,
        error: () => types.error,
        filterKeys: [
          { key: "name", type: "string", hint: "substring", get: (r) => `${r.name} ${r.display_name}`, values: () => [] },
          { key: "official", type: "string", hint: "exact", get: (r) => (r.official ? "official" : "custom"), values: () => ["official", "custom"] },
        ],
        filterPlaceholder: "filter location types by name…",
        columns,
        empty: "No location types.",
        rowId: (r) => r.name,
        blades: { registry: { location_type: locationTypeBlade }, rootKind: "location_type" },
        create: canCreate()
          ? { label: "New location type", can: canCreate, body: (ctx) => <CreateLocationTypeForm onCreated={ctx.select} /> }
          : undefined,
      }}
    />
  );
}

// locationTypeBlade renders one registry row on the shared blade stack. An
// official (seed-owned) row is read-only; a custom row carries Edit + Delete.
export const locationTypeBlade: BladeDef = {
  Title: (p) => <LocationTypeBladeTitle id={p.id} />,
  Body: (p) => <LocationTypeBladeBody id={p.id} />,
};

// The blade id is the name (unique within the registry); the row is looked up
// from the cached registry query.
function useLocationTypeRow(id: string): () => LocationType | undefined {
  const types = useQuery(() => ({ queryKey: LOCATION_TYPES_KEY, queryFn: listLocationTypes }));
  return () => (types.data ?? []).find((r) => r.name === id);
}

function LocationTypeBladeTitle(p: { id: string }): JSX.Element {
  return <BladeTitle row={useLocationTypeRow(p.id)} fallback={p.id} />;
}

function LocationTypeBladeBody(p: { id: string }): JSX.Element {
  const qc = useQueryClient();
  const me = useMe();
  const blades = useBlades();
  const edit = useBladeEdit();
  const row = useLocationTypeRow(p.id);
  const [err, setErr] = createSignal<string | null>(null);
  const [displayName, setDisplayName] = createSignal("");
  const [icon, setIcon] = createSignal("");
  const allTypes = useQuery(() => ({ queryKey: LOCATION_TYPES_KEY, queryFn: listLocationTypes }));
  const typeOptions = () => allTypes.data ?? [];
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
    if (!confirm(`Delete location type "${r.name}"?`)) return;
    setErr(null);
    try {
      await deleteLocationType(r.name);
      blades.close();
      await qc.invalidateQueries({ queryKey: LOCATION_TYPES_KEY });
    } catch (e) {
      setErr(describeError(e));
    }
  }

  async function save() {
    const r = row();
    if (!r) return;
    setErr(null);
    try {
      await updateLocationType(r.name, {
        display_name: displayName(),
        icon: icon(),
        allowed_parent_types: allowedParents(),
      });
      await qc.invalidateQueries({ queryKey: LOCATION_TYPES_KEY });
    } catch (e) {
      setErr(describeError(e));
      throw e; // keep the blade in edit mode on failure
    }
  }

  edit.bind({
    editable: () => !!row() && !row()!.official && can(me.data, "location_type", "update"),
    save,
    destructive: () =>
      row() && !row()!.official && can(me.data, "location_type", "delete")
        ? { label: "Delete", tone: "danger", onClick: removeType }
        : undefined,
  });

  return (
    <Show when={row()} fallback={<p class="text-sm text-base-content/50">Location type not found.</p>}>
      {(r) => (
        <div class="flex flex-col gap-4">
          <Show when={err()}>
            <div role="alert" class="alert alert-error alert-soft text-sm"><span>{err()}</span></div>
          </Show>
          <div class="grid grid-cols-2 gap-3 text-sm">
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
            label="Icon"
            mono
            placeholder="map-pin"
            value={() => r().icon ?? "map-pin"}
            draft={icon}
            onInput={setIcon}
          />
          <BladeField
            label="Allowed parents"
            hint="Empty allows any parent (or root). A non-empty set is enforced on create and move."
            read={
              <Show
                when={(r().allowed_parent_types?.length ?? 0) > 0}
                fallback={<span class="text-base-content/50">Unconstrained (any parent, or root).</span>}
              >
                <div class="flex flex-wrap gap-1.5">
                  <For each={r().allowed_parent_types}>
                    {(pid) => (
                      <span class="badge badge-outline badge-sm">
                        {pid === ROOT_PLACEMENT ? "Root" : typeOptions().find((t) => t.name === pid)?.display_name ?? pid}
                      </span>
                    )}
                  </For>
                </div>
              </Show>
            }
          >
            <AllowedParentsPicker options={typeOptions()} value={allowedParents()} onChange={setAllowedParents} />
          </BladeField>
          {/* The location type's declared-property contract: what every location
              of this type exposes. Writes are immediate (a PUT per line), so the
              panel sits outside the blade's edit slot, which the core facts own. */}
          <ContractEditor classifier="location-type" id={r().name} official={r().official} />
          <Show when={r().official}>
            <div role="alert" class="alert alert-soft text-sm"><span>Seed-owned, read-only.</span></div>
          </Show>
        </div>
      )}
    </Show>
  );
}

// CreateLocationTypeForm: name a new custom location type. The display name
// leads and the kebab name (the operator-facing address; the uuid is the
// database's to mint) follows it, stopping the moment the operator edits the
// name by hand (lib/entities). An icon glyph key and the allowed-parents
// constraint round it out.
export function CreateLocationTypeForm(p: { onCreated: (t: LocationType) => void }): JSX.Element {
  const qc = useQueryClient();
  const types = useQuery(() => ({ queryKey: LOCATION_TYPES_KEY, queryFn: listLocationTypes }));
  const typeOptions = () => types.data ?? [];
  const { display, setDisplay, name, setName, nameDerived } = createIdentity();
  const [icon, setIcon] = createSignal("");
  const [allowedParents, setAllowedParents] = createSignal<string[]>([]);
  const [busy, setBusy] = createSignal(false);
  const [formErr, setFormErr] = createSignal<string | null>(null);

  useFormActions().bind({
    submitLabel: "Create location type",
    submitIcon: Plus,
    submit: () => void submit(),
    busy,
    disabled: () => !name().trim() || !display().trim(),
  });

  async function submit() {
    setBusy(true);
    setFormErr(null);
    try {
      const created = await createLocationType({
        name: name().trim(),
        display_name: display().trim(),
        icon: icon().trim() || "map-pin",
        allowed_parent_types: allowedParents(),
      });
      await qc.invalidateQueries({ queryKey: LOCATION_TYPES_KEY });
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
        <input class="input input-bordered w-full" value={display()} placeholder="Wing" onInput={(e) => setDisplay(e.currentTarget.value)} />
      </FieldRow>
      <FieldRow bind="name" hint={nameDerived() ? "Derived from the display name. Edit to set your own." : "A kebab name, e.g. wing. Addressed by the API and CLI."}>
        <input class="input input-bordered w-full font-data" value={name()} placeholder="wing" onInput={(e) => setName(e.currentTarget.value)} />
      </FieldRow>
      <FieldRow label="Icon" hint="A glyph key, e.g. map-pin (the default).">
        <input class="input input-bordered w-full font-data" value={icon()} placeholder="map-pin" onInput={(e) => setIcon(e.currentTarget.value)} />
      </FieldRow>
      {/* Not wrapped in Field: Field's root is a <label>, and a picker of one
          <label> per checkbox nested inside it is invalid HTML that makes a
          for-less outer label forward a click on the heading or hint straight
          to the first checkbox. The heading and hint render as plain text
          instead. */}
      <FieldRow
        label="Allowed parents"
        hint="Where a location of this type may be placed. Leave every box unchecked to allow any parent (unconstrained)."
      >
        <AllowedParentsPicker options={typeOptions()} value={allowedParents()} onChange={setAllowedParents} />
      </FieldRow>
    </form>
  );
}

// AllowedParentsPicker: a checkbox per location type plus a Root option, the set
// of types a location of this kind may be placed under. No box checked means
// unconstrained (any parent, or root). Mirrors Tags.tsx's AppliesToPicker;
// shared by the create form and the edit blade so the markup and toggle logic
// exist once. Each option is its own <label> (not nested inside another one),
// so a click on it only ever toggles that option's own checkbox.
function AllowedParentsPicker(p: { options: LocationType[]; value: string[]; onChange: (v: string[]) => void }): JSX.Element {
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
            <input type="checkbox" class="checkbox checkbox-sm" checked={p.value.includes(t.name)} onChange={() => toggle(t.name)} />
            <span>{t.display_name}</span>
            <span class="font-data text-xs text-base-content/40">{t.name}</span>
          </label>
        )}
      </For>
    </div>
  );
}
