import { For, Show, createMemo, type Accessor } from "solid-js";
import { requireRole, useViewRows, type FieldMapping, type ViewResult } from "../../lib/views";
import ViewEmpty from "./ViewEmpty";

// StatTiles: one tile per row, the label column naming it and the value column
// filling it. The row order is the layout, so the view decides the reading
// order rather than the component sorting behind its back.

export default function StatTiles(props: { result: Accessor<ViewResult | undefined>; mapping: FieldMapping }) {
  const { rows } = useViewRows(props.result);
  // The mapping resolves against the live result, so a contract break throws
  // here (loudly, once) instead of rendering a wall of blank tiles.
  const at = createMemo(() => {
    const r = props.result();
    if (!r) return null;
    return { label: requireRole(r, props.mapping, "label"), value: requireRole(r, props.mapping, "value") };
  });
  return (
    <Show when={at()} fallback={<ViewEmpty message="Loading..." />}>
      {(idx) => (
        <Show when={rows().length > 0} fallback={<ViewEmpty />}>
          <div class="stats stats-horizontal shadow">
            <For each={rows()}>
              {(row) => (
                <div class="stat">
                  <div class="stat-title">{String(row.cells[idx().label])}</div>
                  <div class="stat-value">{String(row.cells[idx().value])}</div>
                </div>
              )}
            </For>
          </div>
        </Show>
      )}
    </Show>
  );
}
