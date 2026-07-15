import { For, Show } from "solid-js";
import { flattenTree, type TreeNode } from "../lib/treeselect";

// TreeSelect: a native <select> whose options read as a tree. Options come out in
// pre-order with each one indented by its depth (and a guide glyph below the
// roots), so an operator can tell what tier a candidate sits at, which a flat
// alphabetical list cannot show. The reusable parent picker for every tree entity
// (locations, systems, and components when it lands). value is the chosen node's
// value; the optional root option carries the empty value.
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
  // A DOM id passthrough, so a caller can associate an external <label for=...>
  // when the picker sits inside a helper that only auto-associates a label for a
  // raw intrinsic control (see TreeList.tsx's ctx.field and its use in
  // Locations.tsx's reparent field).
  id?: string;
}

// Three non-breaking spaces per level (not collapsed by the browser) plus a guide
// glyph for non-roots, so the indentation survives inside a native <option>.
const indent = (depth: number): string =>
  depth === 0 ? "" : "   ".repeat(depth) + "└ ";

export default function TreeSelect(props: TreeSelectProps) {
  const options = () => flattenTree(props.items, props.excludeSubtreeOf);
  return (
    <select
      id={props.id}
      class={props.class ?? "select select-bordered w-full"}
      value={props.value}
      disabled={props.disabled}
      onChange={(e) => props.onChange(e.currentTarget.value)}
    >
      <Show when={props.rootLabel !== undefined}>
        <option value="">{props.rootLabel}</option>
      </Show>
      <For each={options()}>
        {(o) => <option value={o.value}>{indent(o.depth)}{o.label}</option>}
      </For>
    </select>
  );
}
