import { For, Index, Show, type Accessor } from "solid-js";
import { useViewRows, type ViewCell, type ViewResult } from "../../lib/views";
import ViewEmpty from "./ViewEmpty";
import ViewError, { describeViewError } from "./ViewError";

// ViewTable: the generic renderer, every column of a ViewResult as it comes.
// It needs no field-mapping, which is the point: any view is inspectable
// without a bespoke component, and a view earns a specific renderer only when
// the shape means something a table cannot say.
//
// Rows apply through the shared store with reconcile, and the column loops use
// Index rather than For. That is deliberate: every refetch parses fresh JSON,
// so the columns are a new array of new objects each time, and For keys
// strictly by reference. Keying cells that way would dispose and rebuild every
// td on every update, which is the churn the store exists to avoid; cells are
// positional by contract, so position is the right key.

export default function ViewTable(props: { result: Accessor<ViewResult | undefined>; error?: Accessor<unknown> }) {
  const { rows, columns } = useViewRows(props.result);
  return (
    <Show when={!props.error?.()} fallback={<ViewError message={describeViewError(props.error?.())} />}>
      <Show when={props.result()} fallback={<ViewEmpty message="Loading..." />}>
        <div class="overflow-x-auto">
          <table class="table table-sm">
            <thead>
              <tr>
                <Index each={columns()}>{(c) => <th>{c().name}</th>}</Index>
              </tr>
            </thead>
            <tbody>
              <For each={rows()}>
                {(row) => (
                  <tr>
                    <Index each={columns()}>{(_, i) => <td>{renderCell(row.cells[i])}</td>}</Index>
                  </tr>
                )}
              </For>
            </tbody>
          </table>
          <Show when={rows().length === 0}>
            <ViewEmpty />
          </Show>
        </div>
      </Show>
    </Show>
  );
}

// renderCell prints a cell without inventing a value: a null stays visibly
// absent rather than becoming an empty string that reads as "we know it is
// blank". The em dash matches every other placeholder in the console.
function renderCell(cell: ViewCell) {
  return cell === null || cell === undefined ? "—" : String(cell);
}
