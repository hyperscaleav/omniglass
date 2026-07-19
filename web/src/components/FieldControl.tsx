import { Show, type JSX } from "solid-js";
import { ValueInput } from "../pages/Variables";
import { type ValueType } from "../lib/variables";
import { ChevronRight } from "./icons";

// FieldControl is the console's one field renderer: it shows a field's RESOLVED
// value (the type default, an inherited value, or later a source) and its
// OVERRIDE state, in two modes. Read is a slim one-line row where an override
// reads with an accent dot on the key AND the value in the accent colour; an
// inherited value is muted with no dot, so the value column stays scannable.
// Edit is a stacked two-row cell: the key with a right-aligned Override switch,
// and below it the resolved value (switch off) or a type-aware input (switch on).
// Revert IS the switch off, there is no separate control. A bool inherits as the
// resolved word and overrides as a real toggle (the case the model exists to
// fix). A required field marks with a red asterisk, is forced overridden, and
// shows a validation error only after a submit attempt leaves it empty. It is the
// field-facing sibling of KVRow / KVStacked; field-like value surfaces converge
// on it (the source picker and sourced-value display land with the sources model).
export default function FieldControl(props: {
  label: string;
  dataType: ValueType;
  // The resolved value, already stringified ("" when unset with no default).
  resolved: string;
  // True when this component sets its own value (an override), false when it
  // inherits the resolved value (the type default this slice).
  isSet: boolean;
  required?: boolean;
  // Edit wiring; without `editing` the control is a read-only scan row.
  editing?: boolean;
  overriding?: boolean;
  draft?: string;
  // A required override was left empty on a submit attempt.
  invalid?: boolean;
  // The caller may toggle the override (holds field:create); read-only otherwise.
  canToggle?: boolean;
  // The caller may clear a persisted override (holds field:delete). When false, a
  // field that is already set stays overridden (turning it off would delete, which
  // the operator cannot do), so the switch is locked on.
  canRevert?: boolean;
  onToggle?: (on: boolean) => void;
  onInput?: (v: string) => void;
  onDrillIn?: () => void;
  first?: boolean;
}): JSX.Element {
  const overriding = () => !!props.overriding;
  // The switch cannot be turned off (the field stays overridden) when it is
  // required (it must carry a value) or when it is a persisted override the
  // operator lacks field:delete to clear.
  const lockOn = () => !!props.required || (!!props.isSet && props.canRevert === false);
  const hasResolved = () => props.resolved !== "";

  return (
    <Show
      when={props.editing}
      fallback={
        <div
          class="flex items-center gap-2 px-3 py-2"
          classList={{
            "border-t border-base-300": !props.first,
            "cursor-pointer hover:bg-base-content/5": !!props.onDrillIn,
          }}
          onClick={props.onDrillIn ? () => props.onDrillIn?.() : undefined}
        >
          <span class="min-w-0 truncate text-sm">
            {props.label}
            <Show when={props.isSet}>
              <span class="ml-1.5 inline-block h-1.5 w-1.5 rounded-full bg-primary align-middle" aria-label="override" />
            </Show>
          </span>
          <span class="flex-1" />
          <span
            class="min-w-0 max-w-[60%] truncate text-right font-data text-sm"
            classList={{ "font-medium text-primary": props.isSet, "text-base-content/70": !props.isSet }}
          >
            {hasResolved() ? props.resolved : "—"}
          </span>
          <Show when={props.onDrillIn}>
            <button
              type="button"
              class="shrink-0 text-base-content/40 hover:text-base-content"
              aria-label="Show resolution"
              onClick={(e) => { e.stopPropagation(); props.onDrillIn?.(); }}
            >
              <ChevronRight size={14} />
            </button>
          </Show>
        </div>
      }
    >
      <div class="px-3 py-2.5" classList={{ "border-t border-base-300": !props.first }}>
        <div class="flex items-center justify-between gap-3">
          <span class="text-[13px] font-medium">
            {props.label}
            <Show when={props.required}><span class="ml-1 font-semibold text-error" aria-label="required">*</span></Show>
            <span class="ml-2 font-data text-[10px] font-normal text-base-content/40">{props.dataType}</span>
          </span>
          <label
            class="flex shrink-0 items-center gap-1.5 text-[11px]"
            classList={{ "text-primary": overriding(), "text-base-content/50": !overriding() }}
          >
            <span>Override</span>
            <input
              type="checkbox"
              class="toggle toggle-sm toggle-primary"
              checked={overriding()}
              disabled={!props.canToggle || lockOn()}
              onChange={(e) => props.onToggle?.(e.currentTarget.checked)}
            />
          </label>
        </div>
        <div class="mt-2">
          <Show
            when={overriding()}
            fallback={
              <div class="flex items-center gap-2">
                <span class="font-data text-sm text-base-content/60">{hasResolved() ? props.resolved : "unset"}</span>
                <span class="rounded border border-base-300 px-1.5 py-px text-[10px] text-base-content/40">{hasResolved() ? "default" : "no default"}</span>
              </div>
            }
          >
            <ValueInput
              valueType={props.dataType}
              value={props.draft ?? ""}
              onInput={(v) => props.onInput?.(v)}
              placeholder="unset"
              class={props.invalid ? "input-error" : undefined}
            />
            <Show when={props.invalid}>
              <p class="mt-1 text-[11px] text-error">This value is required</p>
            </Show>
          </Show>
        </div>
      </div>
    </Show>
  );
}
