import { For, Show } from "solid-js";
import { attentionOf, countsLine, totalOf } from "../lib/explore_view";
import type { MatrixCell, MatrixModel } from "../lib/matrix";

// The matrix (#840): place down the side, standard across the top.
//
// The only view that answers "how is one standard doing everywhere", which no
// place-grouped face can. Rows follow the same per-root cut the cards use, so
// the two faces agree about what a unit of the estate is.
//
// Past a size the cells stop being a grid of dots and become counts. That is
// the model's call, not this component's, so the threshold is testable and the
// renderer only has to draw what it is told.

function cellTone(cell: MatrixCell): string {
  const bad = attentionOf(cell.counts);
  if (cell.counts.outage > 0) return "text-error";
  if (bad > 0) return "text-warning";
  return "text-base-content/70";
}

export default function MatrixFace(props: {
  model: MatrixModel;
  onDrill: (id: string) => void;
  onHover: (h: { label: string; verdict: string } | null) => void;
}) {
  return (
    <div data-testid="explore-matrix" class="overflow-x-auto">
      <Show
        when={props.model.rows.length > 0}
        fallback={<p class="px-3 py-6 text-sm text-base-content/60">Nothing to pivot.</p>}
      >
        <table class="table table-sm w-full">
          <thead>
            <tr>
              <th class="sticky left-0 z-10 bg-base-200 text-left">Place</th>
              <For each={props.model.columns}>
                {(col) => <th class="whitespace-nowrap text-center text-[10px] uppercase tracking-wider">{col}</th>}
              </For>
            </tr>
          </thead>
          <tbody>
            <For each={props.model.rows}>
              {(row) => (
                <tr data-row={row.id} class="hover:bg-base-content/5">
                  <th class="sticky left-0 z-10 bg-base-200 text-left font-normal">
                    <button
                      type="button"
                      class="cursor-pointer text-left hover:underline"
                      classList={{ "pl-4 text-base-content/80": row.indent, "font-semibold": !row.indent }}
                      onClick={() => props.onDrill(row.id)}
                      aria-label={`Open ${row.label}`}
                    >
                      {row.label}
                    </button>
                    <span class="ml-2 font-mono text-[10px] text-base-content/40">{row.type}</span>
                  </th>
                  <For each={props.model.columns}>
                    {(col) => {
                      const cell = () => row.cells[col];
                      return (
                        <td class="text-center">
                          <Show when={cell()} fallback={<span class="text-base-content/20">·</span>}>
                            {(c) => (
                              <span
                                class={`font-mono text-xs tabular-nums ${cellTone(c())}`}
                                title={`${row.label} · ${col} · ${countsLine(c().counts)}`}
                                onMouseEnter={() =>
                                  props.onHover({ label: `${row.label} · ${col}`, verdict: countsLine(c().counts) })
                                }
                              >
                                {totalOf(c().counts)}
                                <Show when={attentionOf(c().counts) > 0}>
                                  <span class="ml-1 font-semibold">!{attentionOf(c().counts)}</span>
                                </Show>
                              </span>
                            )}
                          </Show>
                        </td>
                      );
                    }}
                  </For>
                </tr>
              )}
            </For>
          </tbody>
        </table>
      </Show>
      <Show when={props.model.dense}>
        <p class="px-1 pt-2 font-mono text-[11px] text-base-content/50">
          Past a hundred systems the cells are counts, not dots, and the sub-rows are dropped: at this size the
          pivot has stopped being a browse surface and become a report.
        </p>
      </Show>
    </div>
  );
}
