import { For, Show, createMemo, createSignal, type JSX } from "solid-js";
import { useQuery, useQueryClient } from "@tanstack/solid-query";
import { deleteFieldValue, effectiveFields, effectiveFieldsKey, setFieldValue, type EffectiveField } from "../lib/fields";
import { displayValue, parseInput, type ValueType } from "../lib/variables";
import { ValueDisplay, ValueInput } from "../pages/Variables";
import { useMe, can } from "../lib/auth";
import { describeError } from "../lib/format";
import Button from "./Button";
import { RotateCcw, Save } from "./icons";

// EffectiveFields lists the fields declared on a component's type, each resolved to
// the value that applies to this component: the literal set on the component, or the
// type-level default when unset (is_set marks the override). Unlike secrets and
// variables, fields are a flat per-type schema, not a scope cascade, so this is a
// plain table with no nested cascade blade. When the caller holds field:create,
// each row carries an inline set control that writes a literal and refreshes it.

export default function EffectiveFields(props: { component: string }): JSX.Element {
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
        <span class="eyebrow">Effective fields</span>
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
              <FieldRow component={props.component} field={f} first={i() === 0} canSet={canSet()} canClear={canClear()} />
            )}
          </For>
        </div>
      </Show>
    </div>
  );
}

// FieldRow is one effective-field row: the field's name and type, an override /
// default badge, a revert control when the component overrides the default and the
// caller may clear it (field:delete), and either the inline setter (field:create)
// or a read-only value display. The revert control and the setter carry different
// permissions, so a delete-only caller still sees the revert while reading the value.
function FieldRow(props: {
  component: string;
  field: EffectiveField;
  first: boolean;
  canSet: boolean;
  canClear: boolean;
}): JSX.Element {
  const qc = useQueryClient();
  const [clearing, setClearing] = createSignal(false);
  const [clearErr, setClearErr] = createSignal<string | null>(null);
  // A revert is offered only for a set row that carries the value_id to delete.
  const canRevert = () => props.canClear && props.field.is_set && !!props.field.value_id;

  async function clear() {
    setClearing(true);
    setClearErr(null);
    try {
      await deleteFieldValue(props.field.value_id as string);
      await qc.invalidateQueries({ queryKey: effectiveFieldsKey(props.component) });
    } catch (e) {
      setClearErr(describeError(e));
    } finally {
      setClearing(false);
    }
  }

  return (
    <div class="flex flex-col gap-2 px-3 py-2.5" classList={{ "border-t border-base-300": !props.first }}>
      <div class="flex items-center gap-2">
        <span class="min-w-0 truncate text-sm">{props.field.display_name || props.field.name}</span>
        <span class="badge badge-ghost badge-sm shrink-0">{props.field.data_type}</span>
        <span class="flex-1" />
        <Show when={canRevert()}>
          <Button
            type="button"
            intent="quiet"
            size="xs"
            square
            icon={RotateCcw}
            label="Revert to default"
            title="Revert to default"
            loading={clearing()}
            onClick={() => { void clear(); }}
          />
        </Show>
        <span class="badge badge-sm shrink-0" classList={{ "badge-primary": props.field.is_set, "badge-ghost": !props.field.is_set }}>
          {props.field.is_set ? "override" : "default"}
        </span>
      </div>
      <Show when={props.canSet} fallback={<ValueDisplay valueType={props.field.data_type} value={props.field.value} />}>
        <FieldSetControl component={props.component} field={props.field} />
      </Show>
      <Show when={clearErr()}>
        <span class="text-[11px] text-error">{clearErr()}</span>
      </Show>
    </div>
  );
}

// FieldSetControl is one row's inline setter: a type-aware input seeded from the
// effective value and a Set that parses the text to the field's data_type, writes
// the literal, and refreshes the panel. It holds its own draft so typing in one row
// never disturbs another; a refetch remounts the row and reseeds from the new value.
function FieldSetControl(props: { component: string; field: EffectiveField }): JSX.Element {
  const qc = useQueryClient();
  const [draft, setDraft] = createSignal(displayValue(props.field.value));
  const [busy, setBusy] = createSignal(false);
  const [err, setErr] = createSignal<string | null>(null);

  async function save() {
    setBusy(true);
    setErr(null);
    let parsed: unknown;
    try {
      parsed = parseInput(props.field.data_type as ValueType, draft());
    } catch (e) {
      setErr(describeError(e));
      setBusy(false);
      return;
    }
    try {
      await setFieldValue(props.component, props.field.name, parsed);
      await qc.invalidateQueries({ queryKey: effectiveFieldsKey(props.component) });
    } catch (e) {
      setErr(describeError(e));
    } finally {
      setBusy(false);
    }
  }

  return (
    <div class="flex flex-col gap-1">
      <div class="flex items-start gap-2">
        <div class="min-w-0 flex-1">
          <ValueInput valueType={props.field.data_type as ValueType} value={draft()} onInput={setDraft} />
        </div>
        <Button type="button" intent="action" icon={Save} disabled={busy()} onClick={() => { void save(); }}>Set</Button>
      </div>
      <Show when={err()}>
        <span class="text-[11px] text-error">{err()}</span>
      </Show>
    </div>
  );
}
