import { For, Show, type Accessor } from "solid-js";
import { useViewRows, type ViewCell, type ViewResult } from "../../lib/views";
import ViewEmpty from "./ViewEmpty";

// ViewTable: the generic renderer, every column of a ViewResult as it comes.
// It needs no field-mapping, which is the point: any view is inspectable
// without a bespoke component, and a view earns a specific renderer only when
// the shape means something a table cannot say.
//
// Rows apply through the shared store with reconcile, so a live update patches
// the changed cells and leaves every other row's DOM alone.

export default function ViewTable(props: { result: Accessor<ViewResult | undefined> }) {
  const { rows, columns } = useViewRows(props.result);
  return (
    <Show when={props.result()} fallback={<ViewEmpty message="Loading..." />}>
      <div class="overflow-x-auto">
        <table class="table table-sm">
          <thead>
            <tr>
              <For each={columns()}>{(c) => <th>{c.name}</th>}</For>
            </tr>
          </thead>
          <tbody>
            <For each={rows()}>
              {(row) => (
                <tr>
                  <For each={columns()}>{(_, i) => <td>{renderCell(row.cells[i()])}</td>}</For>
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
  );
}

// renderCell prints a cell without inventing a value: a null stays visibly
// absent rather than becoming an empty string that reads as "we know it is
// blank".
function renderCell(cell: ViewCell) {
  return cell === null || cell === undefined ? "–" : String(cell);
}
