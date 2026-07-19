import { For, Show, createMemo, createSignal, type JSX } from "solid-js";
import { useQuery, useQueryClient } from "@tanstack/solid-query";
import { deleteFieldValue, effectiveFields, effectiveFieldsKey, setFieldValue, type EffectiveField } from "../lib/fields";
import { displayValue, parseInput, type ValueType } from "../lib/variables";
import { ValueInput } from "../pages/Variables";
import { useMe, can } from "../lib/auth";
import { type BladeDef } from "../lib/blades";
import { describeError } from "../lib/format";
import KVRow from "./KVRow";
import Button from "./Button";
import { Check, RotateCcw, Save } from "./icons";

// EffectiveFields lists the fields declared on a component's type, each resolved
// to the value that applies to this component: the literal set on the component,
// or the type-level default when unset (is_set marks the override). Fields are a
// flat per-type schema, not a scope cascade, so a row drills in to a small
// resolution blade (kind "field-resolution", via ctx.openBlade) rather than the
// deep global/location/system/component cascade the secrets and variables rows
// open. Each field renders through KVRow, so the platform edit-mode rule holds:
// the setter input and the revert control appear ONLY in edit mode (driven by the
// component-detail edit context, exactly as TagAdder is), and read mode is a slim
// inline value scan (no box) even for a field:create caller. An override reads
// with weight and an "override" badge; a default is quiet.
export default function EffectiveFields(props: { component: string; editing: boolean; onOpen: (fieldName: string) => void }): JSX.Element {
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
                onOpen={props.onOpen}
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
  onOpen: (fieldName: string) => void;
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
  // The save is quiet by default (a loud accent on every row reads as "unsaved
  // everywhere"); it goes accent only when the draft diverges from the effective
  // value, i.e. there is actually something to save.
  const dirty = () => draft() !== displayValue(props.field.value);

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
        onDrillIn={() => props.onOpen(props.field.name)}
        value={hasValue() ? displayValue(props.field.value) : "—"}
        input={<ValueInput valueType={props.field.data_type as ValueType} value={draft()} onInput={setDraft} class="join-item grow" />}
        actions={
          // Edit mode only: read mode is a pure scan with zero controls (rule 2).
          <Show when={props.editing}>
            <Show when={props.canSet}>
              <Button type="button" intent={dirty() ? "action" : "quiet"} square icon={Save} class="join-item" loading={saving()} label="Set field value" title="Set" onClick={() => { void save(); }} />
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
