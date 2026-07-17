import { For, Show, createMemo, createSignal, type JSX } from "solid-js";
import { useQuery, useQueryClient } from "@tanstack/solid-query";
import { deleteFieldValue, effectiveFields, effectiveFieldsKey, setFieldValue, type EffectiveField } from "../lib/fields";
import { displayValue, parseInput, type ValueType } from "../lib/variables";
import { ValueInput } from "../pages/Variables";
import { useMe, can } from "../lib/auth";
import { describeError } from "../lib/format";
import KVRow from "./KVRow";
import Button from "./Button";
import { RotateCcw, Save } from "./icons";

// EffectiveFields lists the fields declared on a component's type, each resolved
// to the value that applies to this component: the literal set on the component,
// or the type-level default when unset (is_set marks the override). Unlike secrets
// and variables, fields are a flat per-type schema, not a scope cascade, so this is
// a plain list with no nested cascade blade. Each field renders through KVRow, so
// the platform edit-mode rule holds: the setter input and the revert control appear
// ONLY in edit mode (driven by the component-detail edit context, exactly as
// TagAdder is), and read mode is a slim inline value scan (no box) even for a
// field:create caller. An override reads with weight and an "override" badge; a
// default is quiet.
export default function EffectiveFields(props: { component: string; editing: boolean }): JSX.Element {
  const me = useMe();
  const q = useQuery(() => ({
    queryKey: effectiveFieldsKey(props.component),
    queryFn: () => effectiveFields(props.component),
    // Rows are edited inline; a background window-focus refetch would rebuild them
    // and discard an in-progress edit, so this panel does not refetch on focus.
    refetchOnWindowFocus: false,
  }));
  const rows = createMemo<EffectiveField[]>(() => q.data ?? []);
  const canSet = () => can(me.data, "field", "create");
  const canClear = () => can(me.data, "field", "delete");

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
                component={props.component}
                field={f}
                first={i() === 0}
                editing={props.editing}
                canSet={canSet()}
                canClear={canClear()}
              />
            )}
          </For>
        </div>
      </Show>
    </div>
  );
}

// FieldRow is one effective-field KVRow. Read mode shows the effective value (a
// dash when unset with no default); edit mode swaps in the type-aware setter and,
// on an override the caller may clear, a revert control, both riding the row's
// daisyUI join as the inline-action family. The setter holds its own draft so
// typing in one row never disturbs another; a refetch remounts the row and reseeds
// from the new value. The revert and the setter carry different permissions, so a
// delete-only caller still sees the revert while reading the value.
function FieldRow(props: {
  component: string;
  field: EffectiveField;
  first: boolean;
  editing: boolean;
  canSet: boolean;
  canClear: boolean;
}): JSX.Element {
  const qc = useQueryClient();
  const [draft, setDraft] = createSignal(displayValue(props.field.value));
  const [saving, setSaving] = createSignal(false);
  const [clearing, setClearing] = createSignal(false);
  const [err, setErr] = createSignal<string | null>(null);

  // A revert is offered only for a set row that carries the value_id to delete.
  const canRevert = () => props.canClear && props.field.is_set && !!props.field.value_id;
  const hasValue = () => props.field.value !== null && props.field.value !== undefined;
  const label = () => props.field.display_name || props.field.name;

  async function save() {
    setSaving(true);
    setErr(null);
    let parsed: unknown;
    try {
      parsed = parseInput(props.field.data_type as ValueType, draft());
    } catch (e) {
      setErr(describeError(e));
      setSaving(false);
      return;
    }
    try {
      await setFieldValue(props.component, props.field.name, parsed);
      await qc.invalidateQueries({ queryKey: effectiveFieldsKey(props.component) });
    } catch (e) {
      setErr(describeError(e));
    } finally {
      setSaving(false);
    }
  }

  async function clear() {
    setClearing(true);
    setErr(null);
    try {
      await deleteFieldValue(props.field.value_id as string);
      await qc.invalidateQueries({ queryKey: effectiveFieldsKey(props.component) });
    } catch (e) {
      setErr(describeError(e));
    } finally {
      setClearing(false);
    }
  }

  return (
    <>
      <KVRow
        first={props.first}
        label={label()}
        emphasize={props.field.is_set}
        origin={props.field.is_set ? "override" : ""}
        editing={props.editing && props.canSet}
        value={hasValue() ? displayValue(props.field.value) : "—"}
        input={<ValueInput valueType={props.field.data_type as ValueType} value={draft()} onInput={setDraft} class="join-item grow" />}
        actions={
          // Edit mode only: read mode is a pure scan with zero controls (rule 2).
          <Show when={props.editing}>
            <Show when={props.canSet}>
              <Button type="button" intent="action" square icon={Save} class="join-item" loading={saving()} label="Set field value" title="Set" onClick={() => { void save(); }} />
            </Show>
            <Show when={canRevert()}>
              <Button type="button" intent="quiet" square icon={RotateCcw} class="join-item" loading={clearing()} label="Revert to default" title="Revert to default" onClick={() => { void clear(); }} />
            </Show>
          </Show>
        }
      />
      <Show when={err()}>
        <div class="px-3 pb-2 text-[11px] text-error">{err()}</div>
      </Show>
    </>
  );
}
