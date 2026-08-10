import { For, Show, type JSX } from "solid-js";
import { type SystemType, systemTypeTree, flattenSystemTypeTree } from "../lib/system_types";

// SystemTypeSelect: a single <select> over the system_type tree, indented by
// depth so the nesting (av > room > board) reads without a dedicated tree
// widget. Shared by the system classification picker (Systems.tsx, on both the
// create form and the edit blade) and the registry admin page's parent picker
// (SystemTypes.tsx), the places a caller chooses a node in this tree.
export default function SystemTypeSelect(p: {
  types: SystemType[];
  value: string;
  onChange: (v: string) => void;
  // An empty leading option (e.g. "Root (no parent)"); omitted means every
  // option is a real type, for a picker that has no "none" state.
  emptyLabel?: string;
  id?: string;
}): JSX.Element {
  const rows = () => flattenSystemTypeTree(systemTypeTree(p.types));
  // A <select>'s rendered option text collapses an ordinary ASCII space run,
  // so the depth indent uses an explicit non-breaking space (\u00A0), three per
  // level, which is what actually survives to the rendered glyph.
  const indent = (depth: number) => "\u00A0\u00A0\u00A0".repeat(depth);
  return (
    <select
      id={p.id}
      class="select select-bordered w-full font-data"
      value={p.value}
      onChange={(e) => p.onChange(e.currentTarget.value)}
    >
      <Show when={p.emptyLabel}>
        <option value="">{p.emptyLabel}</option>
      </Show>
      <For each={rows()}>
        {(t) => <option value={t.name}>{indent(t.depth)}{t.display_name}</option>}
      </For>
    </select>
  );
}
