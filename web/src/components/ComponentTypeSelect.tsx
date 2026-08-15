import { For, Show, type JSX } from "solid-js";
import { bindSelectValue } from "../lib/selectvalue";
import { type ComponentType, componentTypeTree, flattenComponentTypeTree } from "../lib/component_types";

// ComponentTypeSelect: a single <select> over the component_type tree,
// indented by depth so the nesting (mic > wireless-mic) reads without a
// dedicated tree widget. Shared by the product classification picker
// (Products.tsx) and the registry admin page's parent picker
// (ComponentTypes.tsx), the two places a caller chooses a node in this tree.
export default function ComponentTypeSelect(p: {
  types: ComponentType[];
  value: string;
  onChange: (v: string) => void;
  // An empty leading option (e.g. "Root (no parent)"); omitted means every
  // option is a real type, for a picker (like product classification) that
  // has no "none" state.
  emptyLabel?: string;
  id?: string;
}): JSX.Element {
  const rows = () => flattenComponentTypeTree(componentTypeTree(p.types));
  // A <select>'s rendered option text collapses an ordinary ASCII space run,
  // so the depth indent uses an explicit non-breaking space (\u00A0), three
  // per level, which is what actually survives to the rendered glyph.
  const indent = (depth: number) => "\u00A0\u00A0\u00A0".repeat(depth);
  // Its rows are a query answer at every caller, so the control is routinely
  // rendered before its options exist and must take the value again once they
  // land (lib/selectvalue.ts, #772).
  return (
    <select
      id={p.id}
      ref={bindSelectValue(() => p.value, rows)}
      class="select select-bordered w-full font-data"
      onChange={(e) => p.onChange(e.currentTarget.value)}
    >
      <Show when={p.emptyLabel}>
        <option value="">{p.emptyLabel}</option>
      </Show>
      <For each={rows()}>
        {(t) => <option value={t.name}>{indent(t.depth)}{t.label}</option>}
      </For>
    </select>
  );
}
