import { For, Show, createMemo, createSignal, type JSX } from "solid-js";
import { useQuery, useQueryClient } from "@tanstack/solid-query";
import FlatList, { type FlatColumn } from "../components/FlatList";
import FieldRow from "../components/FieldRow";
import BladeField from "../components/BladeField";
import { identityColumn } from "../components/IdentityCell";
import BladeTitle from "../components/BladeTitle";
import Button from "../components/Button";
import { useFormActions } from "../lib/formactions";
import { Plus, X } from "../components/icons";
import {
  type Tag,
  type EntityKind,
  ENTITY_KINDS,
  TAGS_KEY,
  listTags,
  createTag,
  updateTag,
  deleteTag,
  appliesToLabel,
} from "../lib/tags";
import { byLabel, createIdentity, entityLabel } from "../lib/entities";
import { useMe, can } from "../lib/auth";
import { registryLock } from "../lib/catalog";
import { describeError } from "../lib/format";
import { type BladeDef, useBlades, useBladeEdit } from "../lib/blades";

// Tags: the governed vocabulary on the FlatList surface. A tag is a
// tenant-wide, normalized label name; this page is the admin directory (mint,
// inspect, edit its governance fields, delete). Minting and editing a key is an
// admin action (tag:create / tag:update); binding a value onto an entity is that
// entity's own write, on its detail page. applies_to narrows a key to entity
// kinds (empty = any); propagates toggles cascade inheritance versus a flat
// per-entity binding.

function propagatesBadge(t: Tag): JSX.Element {
  return t.propagates
    ? <span class="badge badge-ghost badge-sm">cascades</span>
    : <span class="badge badge-ghost badge-sm">flat</span>;
}

const columns: FlatColumn<Tag>[] = [
  // The shared identity cell, under the one header word every list uses: the
  // label over the name it belongs to, and the name alone on a key that carries
  // no label. A tag gained one in #613, which is what ADR-0076 left undone when
  // it froze the name on the entity name rule: `cost-center` is the key every
  // binding carries, and "Cost Center" is what this column now reads.
  identityColumn<Tag>(),
  { key: "applies_to", label: "Applies to", width: "220px", sortVal: (t) => appliesToLabel(t.applies_to), cell: (t) => <span class="text-base-content/70">{appliesToLabel(t.applies_to)}</span> },
  { key: "propagates", label: "Binding", width: "120px", sortVal: (t) => String(t.propagates), cell: (t) => propagatesBadge(t) },
];

export default function Tags() {
  const me = useMe();
  const tags = useQuery(() => ({ queryKey: TAGS_KEY, queryFn: listTags }));

  // Ordered by the label an operator reads, unlabelled keys last, exactly as
  // ListTags orders them in SQL (`order by label nulls last, name`, #613).
  const rows = createMemo(() => [...(tags.data ?? [])].sort(byLabel));

  return (
    <FlatList<Tag>
      config={{
        entity: { name: "tag", plural: "tags" },
        rows,
        loading: () => tags.isPending,
        error: () => tags.error,
        filterKeys: [
          { key: "name", type: "string", hint: "substring", get: (t) => `${entityLabel(t)} ${t.name}`, values: () => [] },
          { key: "applies_to", type: "string", hint: "exact", get: (t) => appliesToLabel(t.applies_to), values: (rs) => [...new Set(rs.map((r) => appliesToLabel(r.applies_to)))].sort() },
          { key: "binding", type: "string", hint: "exact", get: (t) => (t.propagates ? "cascades" : "flat"), values: () => ["cascades", "flat"] },
        ],
        filterPlaceholder: "filter tags by name, applies-to…",
        columns,
        empty: "No tags yet.",
        // Address a row by its key name: the blade and the update/delete paths key
        // on the name (the tag is written by name, not id), and the name is unique.
        rowId: (t) => t.name,
        blades: { registry: { tag: tagBlade }, rootKind: "tag" },
        create: can(me.data, "tag", "create")
          ? { label: "New tag", can: () => can(me.data, "tag", "create"), body: (ctx) => <CreateTagForm onCreated={ctx.select} /> }
          : undefined,
      }}
    />
  );
}

// tagBlade renders a key on the shared blade stack. The footer carries Edit
// (applies_to + propagates) for tag:update and Delete for tag:delete.
export const tagBlade: BladeDef = {
  Title: (p) => <TagBladeTitle name={p.id} />,
  Body: (p) => <TagBladeBody name={p.id} />,
};

// The blade addresses a tag by its key name (the row id is the name, and the
// write paths key on the name); look it up by name from the cached list.
function useTagByName(name: string): () => Tag | undefined {
  const tags = useQuery(() => ({ queryKey: TAGS_KEY, queryFn: listTags }));
  return () => (tags.data ?? []).find((t) => t.name === name);
}

// The heading is the label, falling back to the key in the data face, through
// the one blade-heading primitive. It was a bare span until the entity gained a
// label (#613), which is the moment #581 said to switch.
function TagBladeTitle(p: { name: string }): JSX.Element {
  return <BladeTitle row={useTagByName(p.name)} fallback={p.name} />;
}

function TagBladeBody(p: { name: string }): JSX.Element {
  const qc = useQueryClient();
  const me = useMe();
  const blades = useBlades();
  const edit = useBladeEdit();
  const tag = useTagByName(p.name);
  const [err, setErr] = createSignal<string | null>(null);
  const [label, setLabel] = createSignal("");
  const [appliesTo, setAppliesTo] = createSignal<EntityKind[]>([]);
  const [propagates, setPropagates] = createSignal(true);
  const [isEnum, setIsEnum] = createSignal(false);
  const [allowedValues, setAllowedValues] = createSignal<string[]>([]);

  // Fill the drafts from the row as it stands; bound below, the slot runs this
  // on entering edit and again on reseed (#748).
  const seedDrafts = () => {
    const t = tag();
    // The RAW column, never entityLabel: seeding the editor with the fallback
    // would turn "no label" into a label the operator never typed on the next
    // save.
    setLabel(t?.label ?? "");
    setAppliesTo((t?.applies_to ?? []) as EntityKind[]);
    setPropagates(t?.propagates ?? true);
    setIsEnum((t?.allowed_values ?? []).length > 0);
    setAllowedValues(t?.allowed_values ?? []);
    setErr(null);
  };

  async function removeTag() {
    const t = tag();
    if (!t) return;
    if (!confirm(`Delete tag "${t.name}"? Its bindings across the fleet are removed too.`)) return;
    setErr(null);
    try {
      await deleteTag(t.name);
      blades.close();
      await qc.invalidateQueries({ queryKey: TAGS_KEY });
    } catch (e) {
      setErr(describeError(e));
    }
  }

  async function save() {
    const t = tag();
    if (!t) return;
    setErr(null);
    try {
      await updateTag(t.name, { label: label(), applies_to: appliesTo(), propagates: propagates(), allowed_values: isEnum() ? allowedValues() : [] });
      await qc.invalidateQueries({ queryKey: TAGS_KEY });
    } catch (e) {
      setErr(describeError(e));
      throw e; // keep the blade in edit mode on failure
    }
  }

  edit.bind({
    editable: () => !!tag() && can(me.data, "tag", "update"),
    seed: seedDrafts,
    save,
    destructive: () => (tag() && can(me.data, "tag", "delete") ? { label: "Delete", tone: "danger", onClick: removeTag } : undefined),
    // A tag key has no official flag (every key is operator-owned), so the
    // lock is judged on the permission arm alone.
    locked: () => registryLock(tag() && { official: false }, me.data, "tag"),
  });

  return (
    <Show when={tag()} fallback={<p class="text-sm text-base-content/50">Tag key not found.</p>}>
      {(t) => (
        <div class="flex flex-col gap-4">
          <Show when={err()}>
            <div role="alert" class="alert alert-error alert-soft text-sm"><span>{err()}</span></div>
          </Show>
          <BladeField bind="label" value={() => t().label ?? ""} draft={label} onInput={setLabel} />
          <BladeField label="Applies to" value={() => appliesToLabel(t().applies_to)}>
            <AppliesToPicker value={appliesTo()} onChange={setAppliesTo} />
          </BladeField>
          <BladeField
            label="Binding"
            value={() => (t().propagates ? "cascades to descendants" : "flat (own entity only)")}
          >
            <PropagatesToggle value={propagates()} onChange={setPropagates} />
          </BladeField>
          <BladeField
            label="Value domain"
            value={() => (t().allowed_values.length ? `one of: ${t().allowed_values.join(", ")}` : "free text")}
          >
            <ValueDomainEditor isEnum={isEnum()} values={allowedValues()} onIsEnum={setIsEnum} onValues={setAllowedValues} />
          </BladeField>
        </div>
      )}
    </Show>
  );
}

// AppliesToPicker: a checkbox per entity kind. No box checked means the key is
// universal (applies to any kind).
function AppliesToPicker(p: { value: EntityKind[]; onChange: (v: EntityKind[]) => void }): JSX.Element {
  function toggle(kind: EntityKind, on: boolean) {
    const next = on ? [...p.value, kind] : p.value.filter((k) => k !== kind);
    p.onChange(next);
  }
  return (
    <div class="flex flex-col gap-1.5">
      <For each={ENTITY_KINDS}>{(kind) => (
        <label class="flex items-center gap-2 text-sm">
          <input type="checkbox" class="checkbox checkbox-sm" checked={p.value.includes(kind)} onChange={(e) => toggle(kind, e.currentTarget.checked)} />
          <span class="font-data">{kind}</span>
        </label>
      )}</For>
      <span class="text-[11px] text-base-content/40">None checked means the key applies to any entity kind.</span>
    </div>
  );
}

function PropagatesToggle(p: { value: boolean; onChange: (v: boolean) => void }): JSX.Element {
  return (
    <label class="flex items-center gap-2">
      <input type="checkbox" class="toggle toggle-sm" checked={p.value} onChange={(e) => p.onChange(e.currentTarget.checked)} />
      <span class="font-data text-sm">{p.value ? "cascades to descendants" : "flat (own entity only)"}</span>
    </label>
  );
}

// CreateTagForm: name the key (a normalized lowercase identifier), pick the
// entity kinds it applies to and whether it cascades, then mint it.
export function CreateTagForm(p: { onCreated: (t: Tag) => void; initialName?: string }): JSX.Element {
  const qc = useQueryClient();
  const { display, setDisplay, name, setName, nameDerived } = createIdentity({ name: p.initialName });
  const [appliesTo, setAppliesTo] = createSignal<EntityKind[]>([]);
  const [propagates, setPropagates] = createSignal(true);
  const [isEnum, setIsEnum] = createSignal(false);
  const [allowedValues, setAllowedValues] = createSignal<string[]>([]);
  const [busy, setBusy] = createSignal(false);
  const [formErr, setFormErr] = createSignal<string | null>(null);

  useFormActions().bind({
    submitLabel: "Create tag",
    submitIcon: Plus,
    submit: () => void submit(),
    busy,
    disabled: () => !name().trim() || (isEnum() && !allowedValues().length),
  });

  async function submit() {
    setBusy(true);
    setFormErr(null);
    try {
      const created = await createTag({
        name: name().trim(),
        label: display().trim(),
        applies_to: appliesTo(),
        propagates: propagates(),
        allowed_values: isEnum() ? allowedValues() : [],
      });
      await qc.invalidateQueries({ queryKey: TAGS_KEY });
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
      <FieldRow bind="label" hint="What an operator reads in lists and pickers. Optional: a key with none reads as its name.">
        <input class="input input-bordered w-full" value={display()} placeholder="Cost Center" onInput={(e) => setDisplay(e.currentTarget.value)} />
      </FieldRow>
      <FieldRow bind="name" hint={nameDerived() ? "Derived from the label. Edit to set your own." : "A lowercase identifier, unique tenant-wide (e.g. environment, cost-center)."}>
        <input class="input input-bordered w-full font-data" value={name()} placeholder="cost-center" onInput={(e) => setName(e.currentTarget.value)} />
      </FieldRow>
      <FieldRow label="Applies to">
        <AppliesToPicker value={appliesTo()} onChange={setAppliesTo} />
      </FieldRow>
      <FieldRow label="Binding">
        <PropagatesToggle value={propagates()} onChange={setPropagates} />
      </FieldRow>
      <FieldRow label="Value domain" hint="Leave free for any text, or constrain the values to a fixed set (an enum), like environment being one of prod, staging, dev.">
        <ValueDomainEditor isEnum={isEnum()} values={allowedValues()} onIsEnum={setIsEnum} onValues={setAllowedValues} />
      </FieldRow>
    </form>
  );
}

// ValueDomainEditor edits a key's value domain: a checkbox turns the free-text
// key into an enum, and when on, a chip list plus an add field build the allowed
// value set. Empty enum is not submittable (the form guards it).
function ValueDomainEditor(p: { isEnum: boolean; values: string[]; onIsEnum: (b: boolean) => void; onValues: (v: string[]) => void }): JSX.Element {
  const [draft, setDraft] = createSignal("");
  function addValue() {
    const v = draft().trim();
    if (v && !p.values.includes(v)) p.onValues([...p.values, v]);
    setDraft("");
  }
  return (
    <div class="flex flex-col gap-2">
      <label class="flex items-center gap-2 text-sm font-normal">
        <input type="checkbox" class="checkbox checkbox-sm" checked={p.isEnum} onChange={(e) => p.onIsEnum(e.currentTarget.checked)} />
        Constrain to a fixed set of values
      </label>
      <Show when={p.isEnum}>
        <div class="flex flex-col gap-2 rounded-box border border-base-300 p-2.5">
          <div class="flex flex-wrap items-center gap-1.5">
            <For each={p.values} fallback={<span class="text-[11px] text-base-content/40">No values yet. Add the allowed values below.</span>}>
              {(v) => (
                <span class="badge badge-ghost gap-1 font-data">
                  {v}
                  <button type="button" class="inline-flex opacity-60 hover:opacity-100" aria-label={`Remove ${v}`} onClick={() => p.onValues(p.values.filter((x) => x !== v))}>
                    <X size={11} />
                  </button>
                </span>
              )}
            </For>
          </div>
          <div class="flex gap-1.5">
            <input
              class="input input-bordered input-sm flex-1 font-data"
              placeholder="add a value (e.g. prod)"
              value={draft()}
              onInput={(e) => setDraft(e.currentTarget.value)}
              onKeyDown={(e) => { if (e.key === "Enter") { e.preventDefault(); addValue(); } }}
            />
            <Button square intent="action" icon={Plus} label="Add value" title="Add value" disabled={!draft().trim()} onClick={addValue} />
          </div>
        </div>
      </Show>
    </div>
  );
}
