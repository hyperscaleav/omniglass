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
  type UpdateLocationType,
  LOCATION_TYPES_KEY,
  LOCATION_TYPE_PATCH_FIELDS,
  ROOT_PLACEMENT,
  listLocationTypes,
  createLocationType,
  updateLocationType,
  deleteLocationType,
  restoreLocationType,
} from "../lib/location_types";
import { useMe, can } from "../lib/auth";
import { nameStemError } from "../lib/validate";
import { registryLock, registryOrigin } from "../lib/catalog";
import { createIdentity, entityLabel } from "../lib/entities";
import { describeError } from "../lib/format";
import { type BladeDef, useBlades, useBladeEdit } from "../lib/blades";

// Location types (Catalog > Locations): the place classifier registry (campus, building, floor, room,
// and your own) on the FlatList surface. The shipped rows are OFFICIAL and the
// platform owns them (#703), which is what lets a release withdraw a value it
// once shipped; editing one is still allowed and FORKS it (#655, ADR-0095), so
// origin reads three ways here exactly as it does on component types, and the
// blade's destructive slot offers Restore instead of Delete on a forked row. A
// row an operator created is theirs outright (create, edit, delete, gated
// location_type:*). A row's detail carries the location type's declared-property
// contract on the shared ContractEditor.
//
// This page names ONE registry on purpose. Its former home, the tabbed Types
// page, fetched the secret_type registry jointly and threw if either fetch
// failed, so a plain viewer (no secret:read) lost this page too (#598). The
// secret registry now lives on its own read-only page (SecretTypes.tsx).
//
// The other two shape-definers are catalog entities of their own, each with its
// own page and contract editor: a system's shape is the STANDARD it conforms to
// (Standards), and a component's is the product it is an instance of (Products).

// Origin is three-state on a fork-adopting registry: a row this release ships, a
// row the operator made, or a shipped row the operator has overridden. The third
// is the one that has to read unambiguously, because it is the only state where
// what an operator sees is not what the release ships.
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

// How the platform names locations of this type, in the operator's words. Absent
// is the opt-out and the state every shipped type is in (ADR-0103).
//
// The NAMES in the sentence are the server's `examples`, the first two the rule
// actually mints. Nothing here knows that a counted name is "<stem>-<n>" or that
// a suppressed first carries no number: this composes a sentence around two
// strings it was given. That is the #695 / #702 rule applied to a rule rather
// than to a row, and it is what stops the console promising a name no create
// produces. The fallback covers a rule from a server too old to send them, which
// says less rather than something possibly wrong.
function namingSummary(r: LocationType): string {
  const rule = r.name_rule;
  if (!rule) return "You name every location of this type.";
  const [first, second] = rule.examples ?? [];
  if (!first || !second) return "The platform names locations of this type.";
  return `Named ${first}, then ${second}, within their parent.`;
}

// Identity (the display name above the name, ADR-0062: the name is the
// operator-facing address; the uuid stays in the blade), the icon glyph, origin.
const columns: FlatColumn<LocationType>[] = [
  identityColumn<LocationType>(),
  { key: "icon", label: "Icon", width: "110px", cell: (r) => <span class="font-data text-xs text-base-content/60">{r.icon ?? "\u2014"}</span> },
  { key: "official", label: "Origin", width: "110px", sortVal: (r) => registryOrigin(r), cell: (r) => originBadge(r) },
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
          { key: "name", type: "string", hint: "substring", get: (r) => `${entityLabel(r)} ${r.name}`, values: () => [] },
          { key: "official", type: "string", hint: "exact", get: (r) => registryOrigin(r), values: () => ["shipped", "yours", "overridden"] },
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

// locationTypeBlade renders one registry row on the shared blade stack. A
// shipped row is editable (the edit forks it) and carries Restore once forked; a
// custom row carries Edit + Delete.
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
  // An allowed parent is stored as a type NAME (or the root sentinel), and the
  // badge reads its label. A name with no row behind it (a type deleted out from
  // under the constraint) shows the name itself rather than a blank badge.
  const parentTypeLabel = (name: string) => {
    if (name === ROOT_PLACEMENT) return "Root";
    const row = typeOptions().find((t) => t.name === name);
    return row ? entityLabel(row) : name;
  };
  const [allowedParents, setAllowedParents] = createSignal<string[]>([]);
  // The name rule's three drafts. `naming` is the opt-in itself: a rule's
  // PRESENCE is what says this type generates (ADR-0102 keeps no boolean beside
  // it), so the checkbox is the rule's existence rather than a fourth field.
  const [naming, setNaming] = createSignal(false);
  const [ruleStem, setRuleStem] = createSignal("");
  const [bareFirst, setBareFirst] = createSignal(false);
  const stemErr = () => (naming() ? nameStemError(ruleStem().trim()) : null);

  // Every draft this blade edits, seeded from the row. Entering edit is one
  // caller; RESTORE is the other, because it replaces the row underneath an open
  // editor and the drafts would otherwise still hold the values the operator
  // just discarded. That was invisible while the blade's fields were a display
  // name and an icon, which restore rarely changes; it is not invisible with the
  // naming rule in the same form, where a stale draft means the next Save
  // re-forks the row with the rule Restore had just taken away.
  function seedDrafts() {
    const r = row();
    setDisplayName(r?.display_name ?? "");
    setIcon(r?.icon ?? "");
    setAllowedParents(r?.allowed_parent_types ?? []);
    setNaming(!!r?.name_rule);
    setRuleStem(r?.name_rule?.stem ?? "");
    setBareFirst(!!r?.name_rule?.bare_first);
  }

  createEffect(on(edit.editing, (editing) => {
    if (!editing) return;
    seedDrafts();
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

  // Restore keeps the blade open: the row survives, it just goes back to the
  // shipped values, so closing would hide the very change the operator asked for.
  async function restoreType() {
    const r = row();
    if (!r) return;
    if (!confirm(`Discard your changes to "${r.name}" and go back to the shipped values?`)) return;
    setErr(null);
    try {
      await restoreLocationType(r.name);
      await qc.invalidateQueries({ queryKey: LOCATION_TYPES_KEY });
      seedDrafts();
    } catch (e) {
      setErr(describeError(e));
    }
  }

  async function save() {
    const r = row();
    if (!r) return;
    setErr(null);
    const body: UpdateLocationType = {
      display_name: displayName(),
      icon: icon(),
      allowed_parent_types: allowedParents(),
    };
    if (naming()) {
      // Setting is the ordinary implied-mask write: a populated field is
      // written. An empty stem is a real value here (the positional type), not
      // an omission, which is why the rule is an object rather than two loose
      // fields the body could half-carry.
      body.name_rule = { stem: ruleStem().trim(), bare_first: bareFirst() };
    } else if (r.name_rule) {
      // Clearing is the mask (#692, ADR-0106): an omitted key and an explicit
      // null are the same absent value once a body is decoded, so the mask is
      // what carries the intent for a nullable OBJECT. It names every field this
      // write changes rather than name_rule alone, because a mask governs the
      // WHOLE write and a narrower one would drop the display name and icon
      // edited in the same pass.
      body.update_mask = [...LOCATION_TYPE_PATCH_FIELDS];
    }
    try {
      await updateLocationType(r.name, body);
      await qc.invalidateQueries({ queryKey: LOCATION_TYPES_KEY });
    } catch (e) {
      setErr(describeError(e));
      throw e; // keep the blade in edit mode on failure
    }
  }

  edit.bind({
    // A shipped row is editable now: the edit forks it rather than writing it.
    editable: () => !!row() && can(me.data, "location_type", "update"),
    save,
    // ADR-0113: the rule is a TypeScript pure function and the footer Save is
    // what refuses, since a blade has no <form> for a native constraint to fire
    // on.
    valid: () => !stemErr(),
    // One destructive slot, two meanings, chosen by what the row IS: a custom
    // row is deleted, a forked shipped row has the fork discarded, and a
    // pristine shipped row offers nothing (there is neither a row to delete nor
    // changes to discard).
    destructive: () => {
      const r = row();
      if (!r) return undefined;
      if (r.official) {
        return r.forked && can(me.data, "location_type", "update")
          ? { label: "Restore shipped", tone: "danger" as const, onClick: restoreType }
          : undefined;
      }
      return can(me.data, "location_type", "delete")
        ? { label: "Delete", tone: "danger" as const, onClick: removeType }
        : undefined;
    },
    locked: () => registryLock(row(), me.data, "location_type", { forkable: true }),
  });

  return (
    <Show when={row()} fallback={<p class="text-sm text-base-content/50">Location type not found.</p>}>
      {(r) => (
        <div class="flex flex-col gap-4">
          <Show when={err()}>
            <div role="alert" class="alert alert-error alert-soft text-sm"><span>{err()}</span></div>
          </Show>
          <div class="grid grid-cols-2 gap-3 text-sm">
            <KVStacked label="Origin" value={originBadge(r())} />
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
            label="Naming"
            hint="Turning naming on or off leaves every existing name alone: the rule decides how the NEXT one is named."
            read={<span class="text-base-content/70">{namingSummary(r())}</span>}
          >
            <NameRuleEditor
              naming={naming()}
              onNaming={setNaming}
              stem={ruleStem()}
              onStem={setRuleStem}
              stemError={stemErr()}
              bareFirst={bareFirst()}
              onBareFirst={setBareFirst}
            />
          </BladeField>
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
                        {parentTypeLabel(pid)}
                      </span>
                    )}
                  </For>
                </div>
              </Show>
            }
          >
            <AllowedParentsPicker options={typeOptions()} value={allowedParents()} onChange={setAllowedParents} />
          </BladeField>
          {/* The location type's declared contracts, one panel per catalog lane:
              what every location of this type exposes (properties) and carries
              (metrics). The panels read the blade's edit slot from context, so
              their declare/edit/withdraw controls render only in edit mode;
              writes stay immediate (a PUT per line) once revealed. */}
          {/* Not r().official, deliberately. The panel's flag means "this
              classifier's contract is release-owned", which a location type's is
              not even now that the ROW is: nothing seeds a line here, every one
              is the operator's, and the gateway writes them on a shipped type on
              purpose (#703). A standard's and a product's contracts do ship, so
              those pages still pass their own official flag. */}
          <ContractEditor classifier="location-type" id={r().name} official={false} />
          <ContractEditor classifier="location-type" lane="metric" id={r().name} official={false} />
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

// NameRuleEditor: the whole of a location type's name rule, which after ADR-0102
// is a DECLARATION rather than a template, so it is a checkbox, a stem, and one
// more checkbox. Its presence is the opt-in (there is no boolean beside it in
// the data, so there is none here either): unchecking it is the clear #692 gave
// the wire, which now lives inside the editor instead of as a button beside it.
//
// It deliberately shows NOTHING about what the rule will produce. A rule being
// edited has not been saved, and the only honest source for the names a rule
// mints is the mint itself, which the server answers with once the rule exists
// (`name_rule.examples`, read by namingSummary above). Predicting them here
// would be the TypeScript re-implementation #695 tracked and #702 closed, and
// ADR-0104 already refused the same trade for a create form's name. So the loop
// is: set the rule, save, and read what it produces from the server.
//
// Each option is its own <label> wrapping its control, which gives the control
// its accessible name without a `for` (the pattern AllowedParentsPicker uses);
// the stem input carries an aria-label, since its own text sits under it as a
// hint rather than above it as a label.
function NameRuleEditor(p: {
  naming: boolean;
  onNaming: (v: boolean) => void;
  stem: string;
  onStem: (v: string) => void;
  stemError: string | null;
  bareFirst: boolean;
  onBareFirst: (v: boolean) => void;
}): JSX.Element {
  return (
    <div class="flex flex-col gap-2 rounded-box border border-base-300 p-2.5">
      <label class="flex items-center gap-2 text-sm">
        <input
          type="checkbox"
          class="checkbox checkbox-sm"
          checked={p.naming}
          onChange={(e) => p.onNaming(e.currentTarget.checked)}
        />
        <span>The platform names locations of this type</span>
      </label>
      <Show when={p.naming}>
        <div class="flex flex-col gap-2 pl-6">
          <input
            class="input input-bordered input-sm w-full font-data"
            classList={{ "input-error": !!p.stemError }}
            aria-label="Name stem"
            placeholder="wing"
            value={p.stem}
            onInput={(e) => p.onStem(e.currentTarget.value)}
          />
          <Show
            when={p.stemError}
            fallback={
              <p class="text-[11px] text-base-content/40">
                The generated name's prefix. Leave it empty for a positional type, whose number is its
                whole name (a parking deck called 1, then 2).
              </p>
            }
          >
            {(msg) => <p class="text-[11px] text-error">{msg()}</p>}
          </Show>
          <label class="flex items-center gap-2 text-sm">
            <input
              type="checkbox"
              class="checkbox checkbox-sm"
              checked={p.bareFirst}
              onChange={(e) => p.onBareFirst(e.currentTarget.checked)}
            />
            <span>No number on the first one under a parent</span>
          </label>
        </div>
      </Show>
    </div>
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
