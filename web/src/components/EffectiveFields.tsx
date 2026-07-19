import { For, Show, createEffect, createMemo, on, onCleanup, type JSX } from "solid-js";
import { createStore, reconcile } from "solid-js/store";
import { useQuery, useQueryClient } from "@tanstack/solid-query";
import { deleteFieldValue, effectiveFields, effectiveFieldsKey, setFieldValue, type EffectiveField } from "../lib/fields";
import { displayValue, parseInput, type ValueType } from "../lib/variables";
import { ValueInput } from "../pages/Variables";
import { useMe, can } from "../lib/auth";
import { type BladeDef, type BladeEdit } from "../lib/blades";
import { describeError } from "../lib/format";
import KVRow from "./KVRow";
import Button from "./Button";
import { Check, X } from "./icons";

// EffectiveFields lists the fields declared on a component's type, each resolved
// to the value that applies to this component: the literal set on the component,
// or the type-level default when unset (is_set marks the override). Fields are a
// flat per-type schema, not a scope cascade, so a row drills in to a small
// resolution blade (kind "field-resolution", via ctx.openBlade) rather than the
// deep global/location/system/component cascade the secrets and variables rows
// open. Each field renders through KVRow, so the platform edit-mode rule holds:
// inputs appear ONLY in edit mode. Editing is BATCHED, not per-field: the row has
// no inline save, and the panel registers one saver with the blade edit slot, so
// the blade's Save flushes every staged field alongside the component core. In
// edit mode an inherited field is an empty input (a greyed "unset" placeholder);
// a set field shows its value with a clear (x) that stages a revert to the
// default. Read mode is a slim inline value scan: an override reads with weight
// and an "override" badge, a default is quiet.
export default function EffectiveFields(props: { component: string; edit?: BladeEdit; onOpen: (fieldName: string) => void }): JSX.Element {
  const me = useMe();
  const qc = useQueryClient();
  const q = useQuery(() => ({
    queryKey: effectiveFieldsKey(props.component),
    queryFn: () => effectiveFields(props.component),
    // Rows are edited inline; a background window-focus refetch would rebuild them
    // and discard an in-progress edit, so this panel does not refetch on focus.
    refetchOnWindowFocus: false,
  }));
  const rows = createMemo<EffectiveField[]>(() => q.data ?? []);
  const editing = () => props.edit?.editing() ?? false;
  const canSet = () => can(me.data, "field", "create");
  const canClear = () => can(me.data, "field", "delete");
  // Rows accept input only in edit mode and only with the set permission.
  const rowsEditable = () => editing() && canSet();

  // Staged edits keyed by field name (the draft text) and per-row errors. A
  // draft defaults to the effective set value; typing stages an override, and
  // emptying a set field's draft (the clear) stages a revert to the default.
  const [drafts, setDrafts] = createStore<Record<string, string>>({});
  const [errs, setErrs] = createStore<Record<string, string | undefined>>({});
  const originalDraft = (f: EffectiveField) => (f.is_set ? displayValue(f.value) : "");
  const draftOf = (f: EffectiveField) => (f.name in drafts ? drafts[f.name] : originalDraft(f));

  // Leaving edit mode (Cancel, or the refetch after a committed Save) discards
  // any staged drafts so the rows re-seed from the effective values.
  createEffect(on(editing, (isEditing) => {
    if (!isEditing) { setDrafts(reconcile({})); setErrs(reconcile({})); }
  }));

  // The Fields panel contributes one saver to the blade's Save: it flushes every
  // dirty row (an upsert for a set-or-changed value, a delete for a cleared
  // override), records per-row errors, and refetches. A row error aborts the
  // blade save so the operator stays in edit and can fix it. The set is an
  // idempotent upsert, so a retry after a partial failure is safe.
  const flush = async () => {
    let firstErr: string | undefined;
    setErrs(reconcile({}));
    for (const f of rows()) {
      const draft = draftOf(f);
      if (draft === originalDraft(f)) continue; // not dirty
      try {
        if (draft.trim() === "") {
          // Cleared: delete the persisted override. An inherited field that was
          // never set is not dirty, so this runs only for a set field.
          if (f.is_set && f.value_id) await deleteFieldValue(f.value_id);
        } else {
          await setFieldValue(props.component, f.name, parseInput(f.data_type as ValueType, draft));
        }
      } catch (e) {
        const msg = describeError(e);
        setErrs(f.name, msg);
        if (!firstErr) firstErr = msg;
      }
    }
    await qc.invalidateQueries({ queryKey: effectiveFieldsKey(props.component) });
    if (firstErr) throw new Error(firstErr);
  };
  const off = props.edit?.onSave(flush);
  onCleanup(() => off?.());

  return (
    <div class="flex flex-col gap-2">
      <div class="flex items-baseline justify-between gap-2">
        <span class="eyebrow">Fields</span>
        <span class="shrink-0 text-[10.5px] text-base-content/40">the type schema, set or defaulted</span>
      </div>
      <Show when={q.error}>
        <div role="alert" class="alert alert-error alert-soft text-sm"><span>{describeError(q.error)}</span></div>
      </Show>
      <Show when={!q.isLoading && !q.error && !rows().length}>
        <p class="text-sm text-base-content/50">This component's type declares no fields.</p>
      </Show>
      <Show when={rows().length}>
        <div class="overflow-hidden rounded-box border border-base-300">
          <For each={rows()}>
            {(f, i) => (
              <FieldRow
                field={f}
                first={i() === 0}
                editing={rowsEditable()}
                canClear={canClear()}
                draft={draftOf(f)}
                err={errs[f.name]}
                onInput={(v) => setDrafts(f.name, v)}
                onClear={() => setDrafts(f.name, "")}
                onOpen={props.onOpen}
              />
            )}
          </For>
        </div>
      </Show>
    </div>
  );
}

// FieldRow is one effective-field KVRow, a presentational row over the panel's
// staged draft (the panel owns the drafts and flushes them on the blade's Save,
// so the row has no save button of its own). Read mode shows the effective value
// (a dash when unset with no default). Edit mode swaps in the type-aware input
// seeded from the draft: an inherited field is empty with a greyed "unset"
// placeholder, a set field shows its value with a clear (x) that empties the
// draft, staging a revert to the type default. Clearing a persisted override
// needs field:delete; clearing a value only typed this session is a local reset.
function FieldRow(props: {
  field: EffectiveField;
  first: boolean;
  editing: boolean;
  canClear: boolean;
  draft: string;
  err: string | undefined;
  onInput: (v: string) => void;
  onClear: () => void;
  onOpen: (fieldName: string) => void;
}): JSX.Element {
  const f = () => props.field;
  const hasValue = () => f().value !== null && f().value !== undefined;
  const label = () => f().display_name || f().name;
  // The clear (x) shows when there is a value in the box to remove: a set field
  // (a delete on Save, so it needs field:delete) or a value typed into an
  // inherited field (a local reset, always allowed).
  const showClear = () => props.editing && props.draft.trim() !== "" && (!f().is_set || props.canClear);
  return (
    <>
      <KVRow
        first={props.first}
        label={label()}
        emphasize={f().is_set}
        origin={f().is_set ? "override" : ""}
        editing={props.editing}
        onDrillIn={() => props.onOpen(f().name)}
        value={hasValue() ? displayValue(f().value) : "—"}
        input={<ValueInput valueType={f().data_type as ValueType} value={props.draft} onInput={props.onInput} placeholder="unset" class="join-item grow" />}
        actions={
          // Edit mode only (read mode is a pure scan): the clear that stages a
          // revert to the type default.
          <Show when={showClear()}>
            <Button type="button" size="md" square intent="quiet" icon={X} class="join-item" label="Clear override" title="Clear override" onClick={props.onClear} />
          </Show>
        }
      />
      <Show when={props.err}>
        <div class="px-3 pb-2 text-[11px] text-error">{props.err}</div>
      </Show>
    </>
  );
}

// The blade id encodes the component and field name, so the blade body can
// re-resolve the field from the id alone (blades carry only { kind, id }). The
// field name is the raw key, which the drill-in surfaces (the row shows the
// display name); neither a component nor a field name contains a space.
export const fieldBladeId = (component: string, field: string): string => `${component} ${field}`;
const splitFieldBladeId = (id: string): [string, string] => {
  const i = id.indexOf(" ");
  return i < 0 ? [id, ""] : [id.slice(0, i), id.slice(i + 1)];
};

// fieldResolutionBlade renders one field's resolution on the shared blade stack.
// It re-resolves the effective fields for the component encoded in the id and
// picks out the named field, so it renders from the id alone across a refetch
// (the shared-stack contract). The title is the field's display name (the key is
// in the meta line); secrets and variables title by their mono key because they
// have no display name, so this deliberately differs.
export const fieldResolutionBlade: BladeDef = {
  Title: (p) => <FieldBladeTitle id={p.id} />,
  Body: (p) => <FieldResolutionBody id={p.id} />,
};

function useFieldOf(id: () => string) {
  const parts = createMemo(() => splitFieldBladeId(id()));
  const q = useQuery(() => ({
    queryKey: effectiveFieldsKey(parts()[0]),
    queryFn: () => effectiveFields(parts()[0]),
    refetchOnWindowFocus: false,
  }));
  const field = createMemo<EffectiveField | undefined>(() => (q.data ?? []).find((f) => f.name === parts()[1]));
  return { key: () => parts()[1], field };
}

function FieldBladeTitle(p: { id: string }): JSX.Element {
  const { key, field } = useFieldOf(() => p.id);
  // Fall back to the raw key until the field resolves (or if it no longer exists).
  return <span>{field()?.display_name || field()?.name || key()}</span>;
}

function FieldResolutionBody(p: { id: string }): JSX.Element {
  const { field } = useFieldOf(() => p.id);
  return (
    <Show when={field()} fallback={<p class="text-sm text-base-content/50">This field is no longer declared on the component's type.</p>}>
      {(f) => <FieldResolutionDetail field={f()} />}
    </Show>
  );
}

// FieldResolutionDetail is the blade content: the key/type meta line, then the
// deepest-wins resolution chain (type default, then this component). It reuses the
// secret cascade's row language (a tier badge, the value, a winner check) so the
// two drill-ins read as siblings. The chain is intentionally short today; a note
// names the steps that a later slice adds.
function FieldResolutionDetail(props: { field: EffectiveField }): JSX.Element {
  const f = () => props.field;
  const isSet = () => f().is_set;
  const hasDefault = () => f().default_value !== null && f().default_value !== undefined;
  return (
    <div class="flex flex-col gap-5">
      <div class="flex flex-wrap items-center gap-x-3 gap-y-1 text-[11px] text-base-content/50">
        <span>key <span class="font-data text-base-content/70">{f().name}</span></span>
        <span>type <span class="font-data text-base-content/70">{f().data_type}</span></span>
      </div>

      <div class="flex flex-col gap-1.5">
        <span class="eyebrow">Resolution</span>
        <p class="text-[11px] text-base-content/40">type default &rsaquo; this component; the deepest set wins</p>
        <div class="overflow-hidden rounded-box border border-base-300">
          {/* Type default: shadowed (struck, dim) once the component overrides it. */}
          <div class="flex items-center gap-2 px-3 py-2">
            <span class="badge badge-sm shrink-0" classList={{ "badge-primary": !isSet(), "badge-ghost": isSet() }}>type default</span>
            <span class="min-w-0 flex-1 truncate font-data text-sm" classList={{ "text-base-content/40 line-through": isSet() }}>
              {hasDefault() ? displayValue(f().default_value) : "—"}
            </span>
            <Show when={!isSet()}><span class="shrink-0 text-primary"><Check size={14} /></span></Show>
          </div>
          {/* This component: the winner when the component sets an override. */}
          <div class="flex items-center gap-2 border-t border-base-300 px-3 py-2">
            <span class="badge badge-sm shrink-0" classList={{ "badge-primary": isSet(), "badge-ghost": !isSet() }}>this component</span>
            <span class="min-w-0 flex-1 truncate font-data text-sm" classList={{ "text-base-content/40": !isSet() }}>
              {isSet() ? displayValue(f().set_value) : "not set"}
            </span>
            <Show when={isSet()}><span class="shrink-0 text-primary"><Check size={14} /></span></Show>
          </div>
        </div>
        <p class="text-[11px] text-base-content/40">
          The cross-type cascade (product, location, system) and $var: / $sec: / $datapoint: sources resolve in a later slice.
        </p>
      </div>
    </div>
  );
}
