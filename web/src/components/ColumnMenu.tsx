import { For, createSignal } from "solid-js";
import { Popover } from "@kobalte/core/popover";
import { Columns, Check, GripVertical } from "./icons";

// ColumnMenu: the inventory grid's column hide/show/reorder popup, lifted out of
// TreeList so the floating panel is a self-contained primitive (and testable in
// isolation). Visible columns render in order, draggable to reorder; hidden
// columns sit below, click to append. The panel floats over the grid via a
// Kobalte popover, which portals the content to the document body so it is never
// clipped by the grid card's overflow (the bug a daisyUI in-flow dropdown had).
export interface ColumnMenuProps {
  columns: Record<string, { label: string }>;
  columnKeys: string[];
  cols: () => string[];
  onToggle: (key: string) => void;
  onMove: (from: number, to: number) => void;
}

const box = (on: boolean) =>
  "flex h-4 w-4 flex-none items-center justify-center rounded border " +
  (on ? "border-primary bg-primary text-primary-content" : "border-base-300");

export default function ColumnMenu(props: ColumnMenuProps) {
  const [drag, setDrag] = createSignal<number | null>(null);
  return (
    <Popover placement="bottom-end" gutter={6}>
      <Popover.Trigger class="btn btn-quiet btn-sm btn-square" title="Columns" aria-label="Columns">
        <Columns size={15} />
      </Popover.Trigger>
      <Popover.Portal>
        <Popover.Content class="menu z-50 w-56 rounded-box border border-base-300 bg-base-100 p-1.5 shadow-2xl focus:outline-none">
          <p class="menu-title px-2 pb-1.5 text-[10.5px]">Columns · drag to reorder</p>
          <ul class="flex flex-col">
            <For each={props.cols()}>
              {(k, i) => (
                <li
                  draggable={true}
                  onDragStart={() => setDrag(i())}
                  onDragOver={(e) => e.preventDefault()}
                  onDrop={() => { const from = drag(); if (from !== null && from !== i()) props.onMove(from, i()); setDrag(null); }}
                  onDragEnd={() => setDrag(null)}
                  classList={{ "opacity-40": drag() === i() }}
                >
                  <div class="flex items-center gap-2 px-2 py-1.5">
                    <span class="cursor-grab text-base-content/40"><GripVertical size={13} /></span>
                    <button class="flex flex-1 items-center gap-2.5" onClick={() => props.onToggle(k)}>
                      <span class={box(true)}><Check size={11} /></span>
                      {props.columns[k].label}
                    </button>
                  </div>
                </li>
              )}
            </For>
            <For each={props.columnKeys.filter((k) => !props.cols().includes(k))}>
              {(k) => (
                <li>
                  <button class="flex w-full items-center gap-2.5 px-2 py-1.5 text-base-content/60" onClick={() => props.onToggle(k)}>
                    <span class="w-3.25 flex-none" />
                    <span class={box(false)} />
                    {props.columns[k].label}
                  </button>
                </li>
              )}
            </For>
          </ul>
        </Popover.Content>
      </Popover.Portal>
    </Popover>
  );
}
