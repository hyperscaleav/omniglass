import { For, Show, createMemo, createSignal } from "solid-js";
import { type ComboboxRootItemComponentProps, Content, Control, Input, Item, ItemDescription, ItemLabel, Listbox, Portal, Root, Trigger } from "@kobalte/core/combobox";
import { ChevronDown, Plus, X } from "../icons";

// Picker: choose one or several from a list too long to scan, by typing
// (#2485), over Kobalte Combobox. The field the reference fields (#2289), the
// rule form and every "which component, which place" question use, instead of
// a native <select> of hundreds of rows or a hand-rolled role="combobox".
//
// Each option has a value (what is committed, usually an id), a label (what is
// shown and typed) and an optional detail: the secondary text that tells two
// "Huddle 4"s apart (the place it sits in, its type). Typing filters on label
// and detail together, case-insensitively, anywhere in the text.
//
// Keyboard (Kobalte's, unchanged): typing or ArrowDown opens the list, the
// arrows move a highlight while the focus stays in the field, Enter picks, Esc
// closes (and the focus never leaves the field), Home/End jump. The button
// beside the field opens the full list for a pointer. In multiple mode each
// choice is a chip with its own remove button, Enter keeps the list open for
// the next pick, and Backspace in an empty field removes the last chip.
//
// onCreate adds the "create the missing item" row: when the typed text names
// no option exactly (case-insensitive), the list ends with Create "text", and
// picking it calls onCreate with the trimmed text and selects nothing; the
// caller creates the row and sets the value when it exists. The list portals
// to the body and is modal while open, so it works inside a Drawer (a modal
// dialog hides whatever is added outside it) and is never clipped by a card.
export interface PickerOption {
  value: string;
  label: string;
  detail?: string;
  disabled?: boolean;
}

interface PickerCommon {
  options: PickerOption[];
  placeholder?: string;
  disabled?: boolean;
  id?: string;
  "aria-label"?: string;
  "aria-labelledby"?: string;
  "aria-required"?: boolean | "true" | "false";
  class?: string;
  onCreate?: (text: string) => void;
  createLabel?: (text: string) => string;
  emptyText?: string;
}

export type PickerProps =
  | (PickerCommon & { multiple?: false; value: string | null; onChange: (value: string | null) => void })
  | (PickerCommon & { multiple: true; value: string[]; onChange: (values: string[]) => void });

// The create row is an option like any other to Kobalte, marked so the filter
// always keeps it and a pick of it becomes onCreate instead of a value.
type Row = PickerOption & { create?: true };
const CREATE_KEY = "\u0000create";

// The field's shell: a daisyUI input that grows to hold chips. A caller adds
// to it (font-data for values that are identifiers), never restyles it whole.
export const PICKER_CLASS = "input input-bordered flex h-auto min-h-10 w-full flex-wrap items-center gap-1 py-1";

export const matches = (o: PickerOption, text: string): boolean => {
  const q = text.trim().toLowerCase();
  return q === "" || o.label.toLowerCase().includes(q) || (o.detail ?? "").toLowerCase().includes(q);
};

export const quoted = (text: string) => `Create “${text}”`;

export default function Picker(props: PickerProps) {
  const [text, setText] = createSignal("");
  const typed = () => text().trim();
  const creatable = () =>
    !!props.onCreate && typed() !== "" && !props.options.some((o) => o.label.toLowerCase() === typed().toLowerCase());
  const rows = createMemo<Row[]>(() =>
    creatable() ? [...props.options, { value: CREATE_KEY, label: typed(), create: true }] : props.options,
  );
  const byValue = createMemo(() => new Map(props.options.map((o) => [o.value, o])));
  const pick = (r: Row) => {
    if (r.create) props.onCreate?.(r.label);
  };

  const shared = {
    get options() {
      return rows();
    },
    optionValue: "value" as const,
    optionTextValue: "label" as const,
    optionLabel: "label" as const,
    optionDisabled: "disabled" as const,
    defaultFilter: (o: Row, input: string) => !!o.create || matches(o, input),
    onInputChange: setText,
    get placeholder() {
      return props.placeholder;
    },
    get disabled() {
      return props.disabled;
    },
    // The list's own aria-hidden handling, as TreeSelect's: inside a Drawer
    // the dialog would otherwise hide the portalled list from the reader.
    modal: true,
    sameWidth: true,
    gutter: 4,
    itemComponent: (p: ComboboxRootItemComponentProps<Row>) => (
      <Item
        item={p.item}
        class="flex cursor-pointer flex-col rounded-field px-3 py-1.5 text-sm outline-none data-[highlighted]:bg-base-200 data-[selected]:font-semibold data-[disabled]:cursor-default data-[disabled]:text-base-content/40"
      >
        <Show
          when={!p.item.rawValue.create}
          fallback={
            <ItemLabel class="flex items-center gap-1.5 text-primary">
              <Plus size={13} />
              {(props.createLabel ?? quoted)(p.item.rawValue.label)}
            </ItemLabel>
          }
        >
          <ItemLabel>{p.item.rawValue.label}</ItemLabel>
          <Show when={p.item.rawValue.detail}>
            <ItemDescription class="text-xs font-normal text-base-content/60">{p.item.rawValue.detail}</ItemDescription>
          </Show>
        </Show>
      </Item>
    ),
  };

  // Functions, not elements: each part must be created inside the Root.
  const input = () => (
    <Input
      id={props.id}
      aria-label={props["aria-label"]}
      aria-labelledby={props["aria-labelledby"]}
      aria-required={props["aria-required"]}
      data-field-target=""
      class="min-w-24 flex-1 bg-transparent py-1 text-sm outline-none placeholder:text-base-content/40"
    />
  );
  const button = () => (
    <Trigger aria-label="Show suggestions" class="flex-none text-base-content/50 hover:text-base-content">
      <ChevronDown size={15} />
    </Trigger>
  );
  const list = () => (
    <Portal>
      <Content class="z-100 rounded-box border border-base-300 bg-base-100 shadow-lg">
        <Show when={rows().some((r) => matches(r, text()) || r.create)} fallback={<p class="px-3 py-2 text-sm text-base-content/50">{props.emptyText ?? "No matches"}</p>}>
          <Listbox class="max-h-72 overflow-y-auto p-1 focus:outline-none" />
        </Show>
      </Content>
    </Portal>
  );
  const shell = props.class ?? PICKER_CLASS;

  if (props.multiple) {
    const p = props;
    const value = () => p.value.map((v) => byValue().get(v)).filter((o): o is PickerOption => !!o);
    return (
      <Root<Row>
        {...shared}
        multiple
        removeOnBackspace
        value={value()}
        onChange={(next) => {
          const made = next.find((r) => r.create);
          if (made) return pick(made);
          p.onChange(next.map((r) => r.value));
        }}
        allowsEmptyCollection
      >
        <Control<Row> class={shell}>
          {(state) => (
            <>
              <For each={state.selectedOptions()}>
                {(o) => (
                  <span class="badge badge-ghost gap-1">
                    {o.label}
                    <button
                      type="button"
                      aria-label={`Remove ${o.label}`}
                      class="text-base-content/50 hover:text-base-content"
                      onPointerDown={(e) => e.stopPropagation()}
                      onClick={() => state.remove(o)}
                    >
                      <X size={11} />
                    </button>
                  </span>
                )}
              </For>
              {input()}
              {button()}
            </>
          )}
        </Control>
        {list()}
      </Root>
    );
  }
  const p = props;
  return (
    <Root<Row>
      {...shared}
      value={p.value === null ? null : (byValue().get(p.value) ?? null)}
      onChange={(next) => {
        if (next?.create) return pick(next);
        p.onChange(next ? next.value : null);
      }}
      allowsEmptyCollection
    >
      <Control class={shell}>
        {input()}
        {button()}
      </Control>
      {list()}
    </Root>
  );
}

