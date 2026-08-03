import { For, Show, createMemo, type Accessor } from "solid-js";
import { requireRole, useViewRows, type FieldMapping, type ViewResult } from "../../lib/views";
import ViewEmpty from "./ViewEmpty";
import ViewError, { describeViewError } from "./ViewError";

// StatTiles: one tile per row, the label column naming it and the value column
// filling it. The row order is the layout, so the view decides the reading
// order rather than the component sorting behind its back.

export default function StatTiles(props: { result: Accessor<ViewResult | undefined>; mapping: FieldMapping; error?: Accessor<unknown> }) {
  const { rows } = useViewRows(props.result);
  // The mapping resolves against the live result. A broken contract must fail
  // THIS widget visibly, not throw out of render and blank the console, so the
  // failure is captured and rendered rather than propagated.
  const at = createMemo<{ label: number; value: number } | Error | null>(() => {
    const r = props.result();
    if (!r) return null;
    try {
      return { label: requireRole(r, props.mapping, "label"), value: requireRole(r, props.mapping, "value") };
    } catch (err) {
      return err instanceof Error ? err : new Error(String(err));
    }
  });
  const failure = () => props.error?.() ?? (at() instanceof Error ? at() : undefined);
  const idx = () => at() as { label: number; value: number };
  return (
    <Show when={!failure()} fallback={<ViewError message={describeViewError(failure())} />}>
      <Show when={at()} fallback={<ViewEmpty message="Loading..." />}>
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
      </Show>
    </Show>
  );
}
