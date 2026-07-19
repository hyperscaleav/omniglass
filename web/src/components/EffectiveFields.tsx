import { For, Show, createEffect, createMemo, on, onCleanup, type JSX } from "solid-js";
import { createStore, reconcile } from "solid-js/store";
import { useQuery, useQueryClient } from "@tanstack/solid-query";
import { deleteFieldValue, effectiveFields, effectiveFieldsKey, setFieldValue, type EffectiveField } from "../lib/fields";
import { displayValue, parseInput, type ValueType } from "../lib/variables";
import { useMe, can } from "../lib/auth";
import { type BladeDef, type BladeEdit } from "../lib/blades";
import { describeError } from "../lib/format";
import FieldControl from "./FieldControl";
import { Check } from "./icons";

// EffectiveFields lists the fields declared on a component's type, each resolved
// to the value that applies to this component: the literal set on the component,
// or the type-level default when unset (is_set marks the override). Fields are a
// flat per-type schema, not a scope cascade, so a row drills in to a small
// resolution blade (kind "field-resolution", via ctx.openBlade). Every row renders
// through the shared FieldControl primitive: read mode is a slim value scan (an
// override reads with an accent dot and colour), edit mode is a stacked cell with
// an explicit Override switch. Editing is BATCHED: the panel registers one saver
// with the blade edit slot, so the blade's Save flushes every staged field
// alongside the component core. The switch on upserts, the switch off reverts (a
// delete); required fields are validated on that Save, not before.
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
  const editable = () => editing() && canSet();

  // Per-field staged edit state: the override switch, the draft value, a write
  // error, and a required-validation flag (set only on a submit attempt).
  const [overriding, setOverriding] = createStore<Record<string, boolean>>({});
  const [drafts, setDrafts] = createStore<Record<string, string>>({});
  const [errs, setErrs] = createStore<Record<string, string | undefined>>({});
  const [invalid, setInvalid] = createStore<Record<string, boolean>>({});

  const resolvedStr = (f: EffectiveField) => (f.value !== null && f.value !== undefined ? displayValue(f.value) : "");
  const hasDefault = (f: EffectiveField) => f.default_value !== null && f.default_value !== undefined;
  // A required field is always overridden; otherwise the toggled state, else the
  // persisted is_set.
  const overridingOf = (f: EffectiveField) => (f.required ? true : (f.name in overriding ? overriding[f.name] : f.is_set));
  // The override input seeds from the resolved value (the set literal or default).
  const draftOf = (f: EffectiveField) => (f.name in drafts ? drafts[f.name] : resolvedStr(f));

  // Leaving edit mode (Cancel, or the refetch after a committed Save) discards
  // all staged state so the rows re-seed from the effective values.
  createEffect(on(editing, (isEditing) => {
    if (!isEditing) {
      setOverriding(reconcile({}));
      setDrafts(reconcile({}));
      setErrs(reconcile({}));
      setInvalid(reconcile({}));
    }
  }));

  // The Fields panel contributes one saver to the blade's Save. It validates
  // required fields first, setting the per-row invalid flag and aborting before
  // any write, so the red box appears only on a submit attempt. Then it applies:
  // an override switched on upserts its value (idempotent, so a retry is safe),
  // an override switched off (or left blank) reverts by deleting. A write error
  // aborts and keeps the blade in edit.
  const flush = async () => {
    setInvalid(reconcile({}));
    let anyInvalid = false;
    for (const f of rows()) {
      if (!f.required) continue;
      const empty = overridingOf(f) ? draftOf(f).trim() === "" : !hasDefault(f);
      if (empty) { setInvalid(f.name, true); anyInvalid = true; }
    }
    if (anyInvalid) throw new Error("A required field is missing a value.");

    let firstErr: string | undefined;
    setErrs(reconcile({}));
    for (const f of rows()) {
      const on = overridingOf(f);
      const draft = draftOf(f);
      try {
        if (!on || draft.trim() === "") {
          // Inherit: delete a persisted override. An unset field is a no-op.
          if (f.is_set && f.value_id) await deleteFieldValue(f.value_id);
        } else {
          // Override: upsert when new or the value changed (the set is idempotent).
          const current = f.is_set ? displayValue(f.value) : null;
          if (current === null || draft !== current) {
            await setFieldValue(props.component, f.name, parseInput(f.data_type as ValueType, draft));
          }
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
        <span class="shrink-0 text-[10.5px] text-base-content/40">the type schema, resolved</span>
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
              <>
                <FieldControl
                  label={f.display_name || f.name}
                  dataType={f.data_type as ValueType}
                  resolved={resolvedStr(f)}
                  isSet={f.is_set}
                  required={f.required}
                  editing={editable()}
                  overriding={overridingOf(f)}
                  draft={draftOf(f)}
                  invalid={invalid[f.name]}
                  canToggle={canSet()}
                  canRevert={canClear()}
                  onToggle={(on) => setOverriding(f.name, on)}
                  onInput={(v) => setDrafts(f.name, v)}
                  onDrillIn={() => props.onOpen(f.name)}
                  first={i() === 0}
                />
                <Show when={errs[f.name]}>
                  <div class="px-3 pb-2 text-[11px] text-error">{errs[f.name]}</div>
                </Show>
              </>
            )}
          </For>
        </div>
      </Show>
    </div>
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
