import { type JSX } from "solid-js";
import { Select as K } from "@kobalte/core/select";

// Select: a single choice from a short, fixed list whose options need more
// than a native <select> can draw (#2485), over Kobalte Select: an option
// padded by its depth (TreeSelect), a closed face with muted context beside the
// label. A plain list of strings stays a native <select>; a long list the
// operator should filter by typing is a Picker.
//
// Kobalte owns the listbox: Enter, Space or ArrowDown open it, arrows move,
// typing jumps to a match, Enter picks, Esc closes, and the focus returns to
// the trigger. The list portals to the body and the select is modal while
// open, as a native dropdown is: a drawer (a modal dialog) marks anything added
// outside it aria-hidden unless the list hides the page itself.
//
// options are the caller's objects; optionValue and optionLabel name the
// fields that key and name them, optionDisabled marks one that is drawn but
// cannot be chosen. renderOption draws an option's row (the label by default),
// optionStyle styles it (a depth indent), renderValue the closed face.
// The trigger carries data-field-target, so FieldRow's <label for> finds it,
// and takes id, aria-labelledby and class.
export const SELECT_ITEM_CLASS =
  "flex cursor-pointer items-center rounded-field py-1.5 pr-3 text-sm outline-none data-[highlighted]:bg-base-200 data-[selected]:font-semibold data-[disabled]:cursor-default data-[disabled]:text-base-content/40";

export interface SelectProps<T extends object> {
  options: T[];
  value: T | undefined;
  onChange: (option: T) => void;
  optionValue: keyof T & string;
  optionLabel: keyof T & string;
  optionDisabled?: keyof T & string;
  placeholder?: string;
  disabled?: boolean;
  id?: string;
  "aria-labelledby"?: string;
  class?: string;
  renderOption?: (option: T) => JSX.Element;
  optionClass?: (option: T) => string | undefined;
  optionStyle?: (option: T) => JSX.CSSProperties;
  renderValue?: (option: T) => JSX.Element;
}

export default function Select<T extends object>(props: SelectProps<T>) {
  const label = (o: T) => String(o[props.optionLabel] ?? "");
  return (
    <K<T>
      options={props.options}
      optionValue={props.optionValue}
      optionTextValue={props.optionLabel}
      optionDisabled={props.optionDisabled}
      value={props.value}
      onChange={(o) => {
        if (o) props.onChange(o);
      }}
      disallowEmptySelection
      disabled={props.disabled}
      placeholder={props.placeholder ?? "Select…"}
      modal
      sameWidth
      gutter={4}
      itemComponent={(p) => (
        <K.Item item={p.item} class={SELECT_ITEM_CLASS} style={props.optionStyle?.(p.item.rawValue) ?? { "padding-left": "0.75rem" }}>
          <K.ItemLabel class={props.optionClass?.(p.item.rawValue)}>
            {props.renderOption ? props.renderOption(p.item.rawValue) : label(p.item.rawValue)}
          </K.ItemLabel>
        </K.Item>
      )}
    >
      <K.Trigger id={props.id} aria-labelledby={props["aria-labelledby"]} data-field-target="" class={props.class ?? "select select-bordered w-full text-left"}>
        <K.Value<T> class="min-w-0 truncate">
          {(state) => (props.renderValue ? props.renderValue(state.selectedOption()) : label(state.selectedOption()))}
        </K.Value>
      </K.Trigger>
      <K.Portal>
        <K.Content class="z-100 rounded-box border border-base-300 bg-base-100 shadow-lg">
          <K.Listbox class="max-h-72 overflow-y-auto p-1 focus:outline-none" />
        </K.Content>
      </K.Portal>
    </K>
  );
}
