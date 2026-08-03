import { For, Show, createMemo, type Accessor } from "solid-js";
import { requireRole, useViewRows, type FieldMapping, type ViewResult } from "../../lib/views";
import ViewEmpty from "./ViewEmpty";
import ViewError, { describeViewError } from "./ViewError";

// StatusGrid: the fleet at a glance, one cell per row coloured by its value
// column. The value rides on data-state as well as the class, so the state is
// readable by a test and by anything styling on it, not encoded only in a
// colour a screenshot would have to be squinted at.

// A Map, not an object literal: a cell value of "constructor" or "toString"
// would resolve against Object.prototype and interpolate a function body into
// the class attribute.
const TONE = new Map<string, string>([
  ["up", "bg-success text-success-content"],
  ["down", "bg-error text-error-content"],
  ["unknown", "bg-base-300 text-base-content"],
]);
const UNKNOWN_TONE = TONE.get("unknown") as string;

export default function StatusGrid(props: { result: Accessor<ViewResult | undefined>; mapping: FieldMapping; error?: Accessor<unknown> }) {
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
          <div class="flex flex-wrap gap-2">
            <For each={rows()}>
              {(row) => {
                const state = () => String(row.cells[idx().value] ?? "unknown");
                return (
                  <div
                    data-state={state()}
                    class={`rounded px-2 py-1 text-xs ${TONE.get(state()) ?? UNKNOWN_TONE}`}
                    title={`${String(row.cells[idx().label])}: ${state()}`}
                  >
                    {String(row.cells[idx().label])}
                  </div>
                );
              }}
            </For>
          </div>
        </Show>
      </Show>
    </Show>
  );
}
