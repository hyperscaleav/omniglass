import { Show, createMemo } from "solid-js";
import Select from "./ui/Select";
import { useFieldLabelId } from "../lib/fieldlabel";
import { flattenTree, type FlatOption, type TreeNode } from "../lib/treeselect";

// TreeSelect: a Select (ui/, over Kobalte) whose list reads as a tree. Options come out in
// pre-order with each item padded by its depth and a guide glyph below the roots
// (both drawn by CSS, app.css .tree-glyph), so an operator can tell what tier a
// candidate sits at, which a flat alphabetical list cannot show. The closed
// trigger shows the bare label with its ancestor path muted beside it (.tree-path),
// which a native <select> could not do: its closed face is the selected option's
// text, so any indent typed into the option leaked into the value. The reusable
// parent picker for every tree entity (locations, systems, components, and the
// type catalogs). value is the chosen node's value; the optional root option
// carries the empty value. A node marked disabled is drawn in its place, muted,
// and cannot be chosen, so a caller that narrows the choices keeps the nesting.
//
// It does not use bindSelectValue (lib/selectvalue.ts, #772): that fixes a native
// select that forgets a value it has no <option> for. This control derives the
// selected option from value and the options together, so the value arriving
// before the items needs no repair; the selection appears when they land.
export interface TreeSelectProps {
  items: TreeNode[];
  value: string;
  onChange: (value: string) => void;
  // When set, an option for the empty value is shown first (e.g. "Root (no parent)").
  rootLabel?: string;
  // Drop this node and its subtree from the choices (reparent self-guard).
  excludeSubtreeOf?: string;
  class?: string;
  disabled?: boolean;
  // A DOM id on the trigger, so an external <label for=...> can target it. The
  // trigger is the labelable element; FieldRow finds it by data-field-target.
  id?: string;
}

// Rem of padding for the first level; each level below adds one more.
const PAD_BASE = 0.75;
const padFor = (depth: number): string => `${PAD_BASE + depth}rem`;

export default function TreeSelect(props: TreeSelectProps) {
  // The trigger names itself by its value; add the FieldRow label so the name is
  // "Location Boardroom", not "Boardroom" (lib/fieldlabel.ts).
  const labelId = useFieldLabelId();
  const options = createMemo<FlatOption[]>(() => {
    const flat = flattenTree(props.items, props.excludeSubtreeOf);
    return props.rootLabel === undefined
      ? flat
      : [{ value: "", label: props.rootLabel, depth: 0, ancestors: [] }, ...flat];
  });
  // Derived from value and options together, so a value set before the options
  // load selects once they arrive.
  const selected = createMemo(() => options().find((o) => o.value === props.value));

  return (
    <Select<FlatOption>
      options={options()}
      optionValue="value"
      optionLabel="label"
      value={selected()}
      onChange={(o) => props.onChange(o.value)}
      disabled={props.disabled}
      // Kobalte reads the empty key as no selection and shows the placeholder, so
      // the root option's label is what stands in for it.
      placeholder={props.rootLabel ?? "Select\u2026"}
      optionStyle={(o) => ({ "padding-left": padFor(o.depth) })}
      optionClass={(o) => (o.depth > 0 ? "tree-glyph" : undefined)}
      id={props.id}
      aria-labelledby={labelId}
      class={props.class}
      renderValue={(o) => (
        <Show when={o.ancestors.length > 0} fallback={o.label}>
          <span class="tree-path" data-path={o.ancestors.join(" / ")}>{o.label}</span>
        </Show>
      )}
    />
  );
}
