import type { JSX } from "solid-js";
import TreeSelect from "./TreeSelect";
import type { ComponentType } from "../lib/component_types";

// ComponentTypeSelect: TreeSelect over the component_type tree, so the nesting
// (mic > wireless-mic) reads in the open list and the closed value is the bare
// label. Shared by the product classification picker (Products.tsx) and the
// registry admin page's parent picker (ComponentTypes.tsx), the two places a
// caller chooses a node in this tree. The option value is the type's name.
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
  const items = () => p.types.map((t) => ({ id: t.id, value: t.name, label: t.label, parentId: t.parent_id }));
  return (
    <TreeSelect
      id={p.id}
      items={items()}
      value={p.value}
      onChange={p.onChange}
      rootLabel={p.emptyLabel}
      class="select select-bordered w-full text-left font-data"
    />
  );
}
