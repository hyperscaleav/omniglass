import { For, Show, createEffect, createMemo, on, onCleanup, type JSX } from "solid-js";
import { createStore, reconcile } from "solid-js/store";
import { useQuery, useQueryClient } from "@tanstack/solid-query";
import {
  clearComponentProperty,
  effectiveProperties,
  effectivePropertiesKey,
  setComponentProperty,
  type EffectiveProperty,
} from "../lib/component_properties";
import { displayValue, parseInput, type ValueType } from "../lib/variables";
import { useMe, can } from "../lib/auth";
import { type BladeDef, type BladeEdit } from "../lib/blades";
import { describeError } from "../lib/format";
import FieldControl from "./FieldControl";
import { Check } from "./icons";

// PropertiesPanel lists a component's effective properties. A property comes from
// the PRODUCT the component is an instance of: the product's contract declares the
// property, optionally with a default and a required flag, and the component either
// inherits that default or overrides it (is_set marks the override). A property set
// directly on the component that no contract declares is OFF CONTRACT (ad-hoc), and
// a component with no product has only those, which is why the two groups render
// apart: the contract is the shared shape, the off-contract group is what this one
// component says about itself.
//
// Every row renders through the shared FieldControl primitive, so the language
// matches the rest of the console: read mode is a slim value scan (an override
// reads with an accent dot and colour), edit mode is a stacked cell with an
// explicit Override switch, and a row drills in to its resolution blade (kind
// "property-resolution", via ctx.openBlade). Editing is BATCHED: the panel
// registers one saver with the blade edit slot, so the blade's Save flushes every
// staged property alongside the component core. The switch on sets, the switch off
// clears; required properties are validated on that Save, not before.
export default function PropertiesPanel(props: {
  component: string;
  edit?: BladeEdit;
  onOpen?: (property: string) => void;
}): JSX.Element {
  const me = useMe();
  const qc = useQueryClient();
  const q = useQuery(() => ({
    queryKey: effectivePropertiesKey(props.component),
    queryFn: () => effectiveProperties(props.component),
    // Rows are edited inline; a background window-focus refetch would rebuild them
    // and discard an in-progress edit, so this panel does not refetch on focus.
    refetchOnWindowFocus: false,
  }));
  const rows = createMemo<EffectiveProperty[]>(() => q.data ?? []);
  const contract = createMemo(() => rows().filter((p) => p.from_contract));
  const adhoc = createMemo(() => rows().filter((p) => !p.from_contract));
  const editing = () => props.edit?.editing() ?? false;
  // Both writes are the component's own: setting or clearing a property is an
  // update of the component, not of the product's contract.
  const canWrite = () => can(me.data, "component", "update");
  // Rows accept input only in edit mode and only with the update permission.
  const editable = () => editing() && canWrite();

  // Per-property staged edit state: the override switch, the draft value, a write
  // error, and a required-validation flag (set only on a submit attempt).
  const [overriding, setOverriding] = createStore<Record<string, boolean>>({});
  const [drafts, setDrafts] = createStore<Record<string, string>>({});
  const [errs, setErrs] = createStore<Record<string, string | undefined>>({});
  const [invalid, setInvalid] = createStore<Record<string, boolean>>({});

  const resolvedStr = (p: EffectiveProperty) => (p.value !== null && p.value !== undefined ? displayValue(p.value) : "");
  const hasDefault = (p: EffectiveProperty) => p.default_value !== null && p.default_value !== undefined;
  // A required property is always overridden; otherwise the toggled state, else the
  // persisted is_set.
  const overridingOf = (p: EffectiveProperty) =>
    p.required ? true : (p.property_name in overriding ? overriding[p.property_name] : p.is_set);
  // The override input seeds from the resolved value (the set value or the default).
  const draftOf = (p: EffectiveProperty) => (p.property_name in drafts ? drafts[p.property_name] : resolvedStr(p));

  // Leaving edit mode (Cancel, or the refetch after a committed Save) discards all
  // staged state so the rows re-seed from the effective values.
  createEffect(on(editing, (isEditing) => {
    if (!isEditing) {
      setOverriding(reconcile({}));
      setDrafts(reconcile({}));
      setErrs(reconcile({}));
      setInvalid(reconcile({}));
    }
  }));

  // The Properties panel contributes one saver to the blade's Save. It validates
  // required properties first, setting the per-row invalid flag and aborting before
  // any write, so the red box appears only on a submit attempt. Then it applies: an
  // override switched on sets its value (idempotent, so a retry is safe), an
  // override switched off (or left blank) clears back to the contract default. A
  // write error aborts and keeps the blade in edit.
  const flush = async () => {
    setInvalid(reconcile({}));
    let anyInvalid = false;
    for (const p of rows()) {
      if (!p.required) continue;
      const empty = overridingOf(p) ? draftOf(p).trim() === "" : !hasDefault(p);
      if (empty) { setInvalid(p.property_name, true); anyInvalid = true; }
    }
    if (anyInvalid) throw new Error("A required property is missing a value.");

    let firstErr: string | undefined;
    setErrs(reconcile({}));
    for (const p of rows()) {
      const on = overridingOf(p);
      const draft = draftOf(p);
      try {
        if (!on || draft.trim() === "") {
          // Inherit: clear a declared value. An unset property is a no-op.
          if (p.is_set) await clearComponentProperty(props.component, p.property_name);
        } else {
          // Override: set when new or the value changed (the set is idempotent).
          const current = p.is_set ? displayValue(p.set_value) : null;
          if (current === null || draft !== current) {
            await setComponentProperty(props.component, p.property_name, parseInput(p.data_type as ValueType, draft));
          }
        }
      } catch (e) {
        const msg = describeError(e);
        setErrs(p.property_name, msg);
        if (!firstErr) firstErr = msg;
      }
    }
    await qc.invalidateQueries({ queryKey: effectivePropertiesKey(props.component) });
    if (firstErr) throw new Error(firstErr);
  };
  const off = props.edit?.onSave(flush);
  onCleanup(() => off?.());

  // One row: the shared control, plus this property's write error when the last
  // flush could not commit it.
  const propRow = (p: EffectiveProperty, first: () => boolean) => (
    <>
      <FieldControl
        label={p.display_name || p.property_name}
        dataType={p.data_type as ValueType}
        resolved={resolvedStr(p)}
        isSet={p.is_set}
        required={p.required}
        editing={editable()}
        overriding={overridingOf(p)}
        draft={draftOf(p)}
        invalid={invalid[p.property_name]}
        canToggle={canWrite()}
        canRevert={canWrite()}
        onToggle={(on) => setOverriding(p.property_name, on)}
        onInput={(v) => setDrafts(p.property_name, v)}
        onDrillIn={props.onOpen ? () => props.onOpen?.(p.property_name) : undefined}
        first={first()}
      />
      <Show when={errs[p.property_name]}>
        <div class="px-3 pb-2 text-[11px] text-error">{errs[p.property_name]}</div>
      </Show>
    </>
  );

  return (
    <div class="flex flex-col gap-2">
      <div class="flex items-baseline justify-between gap-2">
        <span class="eyebrow">Properties</span>
        <span class="shrink-0 text-[10.5px] text-base-content/40">the product contract, resolved</span>
      </div>
      <Show when={q.error}>
        <div role="alert" class="alert alert-error alert-soft text-sm"><span>{describeError(q.error)}</span></div>
      </Show>
      <Show when={!q.isLoading && !q.error && !rows().length}>
        <p class="text-sm text-base-content/50">
          A component's properties are declared by the product it is an instance of, and this component has no product
          contract and nothing set directly on it yet.
        </p>
      </Show>
      <Show when={contract().length}>
        <div class="overflow-hidden rounded-box border border-base-300">
          <For each={contract()}>{(p, i) => propRow(p, () => i() === 0)}</For>
        </div>
      </Show>
      {/* Off-contract values are the component's own, so they group apart behind a
          dashed border: nothing declares them, and clearing one removes it. */}
      <Show when={adhoc().length}>
        <div class="flex flex-col gap-1" role="group" aria-label="Off contract properties">
          <div class="flex items-baseline gap-2">
            <span class="text-[10.5px] font-semibold uppercase tracking-wide text-base-content/50">Off contract</span>
            <span class="text-[10.5px] text-base-content/40">set on this component, not declared by its product</span>
          </div>
          <div class="overflow-hidden rounded-box border border-dashed border-base-300">
            <For each={adhoc()}>{(p, i) => propRow(p, () => i() === 0)}</For>
          </div>
        </div>
      </Show>
    </div>
  );
}

// The blade id encodes the component and property name, so the blade body can
// re-resolve the property from the id alone (blades carry only { kind, id }). The
// property name is the catalog key, which the drill-in surfaces (the row shows the
// display name); neither a component nor a property name contains a space.
export const propertyBladeId = (component: string, property: string): string => `${component} ${property}`;
const splitPropertyBladeId = (id: string): [string, string] => {
  const i = id.indexOf(" ");
  return i < 0 ? [id, ""] : [id.slice(0, i), id.slice(i + 1)];
};

// propertyResolutionBlade renders one property's resolution on the shared blade
// stack. It re-resolves the effective properties for the component encoded in the
// id and picks out the named property, so it renders from the id alone across a
// refetch (the shared-stack contract).
export const propertyResolutionBlade: BladeDef = {
  Title: (p) => <PropertyBladeTitle id={p.id} />,
  Body: (p) => <PropertyResolutionBody id={p.id} />,
};

function usePropertyOf(id: () => string) {
  const parts = createMemo(() => splitPropertyBladeId(id()));
  const q = useQuery(() => ({
    queryKey: effectivePropertiesKey(parts()[0]),
    queryFn: () => effectiveProperties(parts()[0]),
    refetchOnWindowFocus: false,
  }));
  const property = createMemo<EffectiveProperty | undefined>(() =>
    (q.data ?? []).find((p) => p.property_name === parts()[1]),
  );
  return { key: () => parts()[1], property };
}

function PropertyBladeTitle(p: { id: string }): JSX.Element {
  const { key, property } = usePropertyOf(() => p.id);
  // Fall back to the raw key until the property resolves (or if it is gone).
  return <span>{property()?.display_name || property()?.property_name || key()}</span>;
}

function PropertyResolutionBody(p: { id: string }): JSX.Element {
  const { property } = usePropertyOf(() => p.id);
  return (
    <Show
      when={property()}
      fallback={<p class="text-sm text-base-content/50">This property is no longer on the component.</p>}
    >
      {(prop) => <PropertyResolutionDetail property={prop()} />}
    </Show>
  );
}

// PropertyResolutionDetail is the blade content: the key/type meta line, then the
// deepest-wins resolution chain (the product contract's default, then this
// component). It reuses the field drill-in's row language (a tier badge, the value,
// a winner check) so the two read as siblings. An off-contract property has no
// contract step to shadow: its only step is the component itself.
function PropertyResolutionDetail(props: { property: EffectiveProperty }): JSX.Element {
  const p = () => props.property;
  const isSet = () => p().is_set;
  const onContract = () => p().from_contract;
  const hasDefault = () => p().default_value !== null && p().default_value !== undefined;
  return (
    <div class="flex flex-col gap-5">
      <div class="flex flex-wrap items-center gap-x-3 gap-y-1 text-[11px] text-base-content/50">
        <span>key <span class="font-data text-base-content/70">{p().property_name}</span></span>
        <span>type <span class="font-data text-base-content/70">{p().data_type}</span></span>
        <span class="badge badge-sm badge-ghost">{onContract() ? "on contract" : "off contract"}</span>
      </div>

      <div class="flex flex-col gap-1.5">
        <span class="eyebrow">Resolution</span>
        <p class="text-[11px] text-base-content/40">contract default &rsaquo; this component; the deepest set wins</p>
        <div class="overflow-hidden rounded-box border border-base-300">
          {/* The contract default: shadowed (struck, dim) once the component
              overrides it, and absent entirely for an off-contract property. */}
          <Show
            when={onContract()}
            fallback={
              <div class="flex items-center gap-2 px-3 py-2">
                <span class="badge badge-ghost badge-sm shrink-0">no contract</span>
                <span class="min-w-0 flex-1 text-sm text-base-content/40">the product does not declare this property</span>
              </div>
            }
          >
            <div class="flex items-center gap-2 px-3 py-2">
              <span class="badge badge-sm shrink-0" classList={{ "badge-primary": !isSet(), "badge-ghost": isSet() }}>contract default</span>
              <span class="min-w-0 flex-1 truncate font-data text-sm" classList={{ "text-base-content/40 line-through": isSet() }}>
                {hasDefault() ? displayValue(p().default_value) : "—"}
              </span>
              <Show when={!isSet()}><span class="shrink-0 text-primary"><Check size={14} /></span></Show>
            </div>
          </Show>
          {/* This component: the winner whenever the component declares a value. */}
          <div class="flex items-center gap-2 border-t border-base-300 px-3 py-2">
            <span class="badge badge-sm shrink-0" classList={{ "badge-primary": isSet(), "badge-ghost": !isSet() }}>this component</span>
            <span class="min-w-0 flex-1 truncate font-data text-sm" classList={{ "text-base-content/40": !isSet() }}>
              {isSet() ? displayValue(p().set_value) : "not set"}
            </span>
            <Show when={isSet()}><span class="shrink-0 text-primary"><Check size={14} /></span></Show>
          </div>
        </div>
        <p class="text-[11px] text-base-content/40">
          <Show
            when={onContract()}
            fallback="Nothing declares this property, so clearing it removes the value from the component."
          >
            The product's contract declares this property, so clearing the override falls back to its default.
          </Show>
        </p>
      </div>
    </div>
  );
}
