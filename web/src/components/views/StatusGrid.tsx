import { For, Show, createMemo, type Accessor } from "solid-js";
import { requireRole, useViewRows, type FieldMapping, type ViewResult } from "../../lib/views";
import ViewEmpty from "./ViewEmpty";

// StatusGrid: the fleet at a glance, one cell per row coloured by its value
// column. The value rides on data-state as well as the class, so the state is
// readable by a test and by anything styling on it, not encoded only in a
// colour a screenshot would have to be squinted at.

const TONE: Record<string, string> = {
  up: "bg-success text-success-content",
  down: "bg-error text-error-content",
  unknown: "bg-base-300 text-base-content",
};

export default function StatusGrid(props: { result: Accessor<ViewResult | undefined>; mapping: FieldMapping }) {
  const { rows } = useViewRows(props.result);
  const at = createMemo(() => {
    const r = props.result();
    if (!r) return null;
    return { label: requireRole(r, props.mapping, "label"), value: requireRole(r, props.mapping, "value") };
  });
  return (
    <Show when={at()} fallback={<ViewEmpty message="Loading..." />}>
      {(idx) => (
        <Show when={rows().length > 0} fallback={<ViewEmpty />}>
          <div class="flex flex-wrap gap-2">
            <For each={rows()}>
              {(row) => {
                const state = () => String(row.cells[idx().value] ?? "unknown");
                return (
                  <div
                    data-state={state()}
                    class={`rounded px-2 py-1 text-xs ${TONE[state()] ?? TONE.unknown}`}
                    title={`${String(row.cells[idx().label])}: ${state()}`}
                  >
                    {String(row.cells[idx().label])}
                  </div>
                );
              }}
            </For>
          </div>
        </Show>
      )}
    </Show>
  );
}
