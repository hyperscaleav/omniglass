import { For, Show, createMemo, createSignal, type JSX } from "solid-js";

// DataTable: a semantic <table> (no headless primitive needed; row activation is
// Enter/Space). Tri-state sort (off -> asc -> desc), pointer-drag column resize,
// and optional grouping. The ListView consumes this in list mode; tree mode
// renders its own nested rows.
export type Column<T> = {
  key: string;
  header: string;
  align?: "left" | "right";
  sortable?: boolean;
  width?: number;
  flex?: boolean;
  render?: (row: T) => JSX.Element;
  sortVal?: (row: T) => string | number;
};

type SortState = { key: string | null; dir: "asc" | "desc" | null };

export default function DataTable<T>(props: {
  columns: Column<T>[];
  rows: T[];
  onRowClick?: (row: T) => void;
  rowKey?: (row: T) => string;
  groupBy?: (row: T) => string;
  groupHeader?: (key: string, rows: T[]) => JSX.Element;
  groupSort?: (a: string, b: string) => number;
}) {
  const [sort, setSort] = createSignal<SortState>({ key: null, dir: null });
  const [widths, setWidths] = createSignal<Record<string, number>>({});

  const sorted = createMemo(() => {
    const s = sort();
    if (!s.key) return props.rows;
    const col = props.columns.find((c) => c.key === s.key);
    if (!col) return props.rows;
    const acc = col.sortVal ?? ((r: T) => (r as Record<string, unknown>)[col.key] as string | number);
    const out = [...props.rows].sort((a, b) => {
      const x = acc(a);
      const y = acc(b);
      if (x === y) return 0;
      if (typeof x === "number" && typeof y === "number") return x - y;
      return String(x).localeCompare(String(y));
    });
    return s.dir === "desc" ? out.reverse() : out;
  });

  const toggleSort = (c: Column<T>) => {
    if (c.sortable === false) return;
    setSort((s) => (s.key !== c.key ? { key: c.key, dir: "asc" } : s.dir === "asc" ? { key: c.key, dir: "desc" } : { key: null, dir: null }));
  };
  const sortGlyph = (c: Column<T>) => (sort().key === c.key ? (sort().dir === "asc" ? "↑" : "↓") : "↕");

  const startResize = (e: PointerEvent, key: string) => {
    e.preventDefault();
    e.stopPropagation();
    const handle = e.currentTarget as HTMLElement;
    const th = handle.parentElement as HTMLElement;
    const startX = e.clientX;
    const startW = th.offsetWidth;
    handle.classList.add("active");
    const move = (ev: PointerEvent) => setWidths((w) => ({ ...w, [key]: Math.max(64, startW + (ev.clientX - startX)) }));
    const up = () => {
      document.removeEventListener("pointermove", move);
      document.removeEventListener("pointerup", up);
      handle.classList.remove("active");
    };
    document.addEventListener("pointermove", move);
    document.addEventListener("pointerup", up);
  };

  const groups = createMemo(() => {
    if (!props.groupBy) return null;
    const m = new Map<string, T[]>();
    for (const r of sorted()) {
      const g = props.groupBy(r);
      if (!m.has(g)) m.set(g, []);
      m.get(g)!.push(r);
    }
    const entries = [...m.entries()];
    if (props.groupSort) entries.sort((a, b) => props.groupSort!(a[0], b[0]));
    return entries;
  });

  const Row = (r: T) => (
    <tr
      class={props.onRowClick ? "clickable cursor-pointer hover:bg-base-content/5" : ""}
      role={props.onRowClick ? "button" : undefined}
      tabindex={props.onRowClick ? 0 : undefined}
      onClick={() => props.onRowClick?.(r)}
      onKeyDown={(e) => {
        if (props.onRowClick && (e.key === "Enter" || e.key === " ")) {
          e.preventDefault();
          props.onRowClick(r);
        }
      }}
    >
      <For each={props.columns}>
        {(c) => (
          <td class="overflow-hidden text-ellipsis whitespace-nowrap" style={{ "text-align": c.align ?? "left" }}>
            <span class={c.align === "right" ? "tnum" : ""}>
              {c.render ? c.render(r) : <span class="text-base-content/70">{String((r as Record<string, unknown>)[c.key] ?? "")}</span>}
            </span>
          </td>
        )}
      </For>
    </tr>
  );

  return (
    <div class="card overflow-x-auto border border-base-300 bg-base-200">
      <table class="og-rows table table-fixed table-sm">
        <colgroup>
          <For each={props.columns}>
            {(c) => <col style={{ width: widths()[c.key] ? `${widths()[c.key]}px` : c.flex ? undefined : `${c.width ?? 150}px` }} />}
          </For>
        </colgroup>
        <thead>
          <tr>
            <For each={props.columns}>
              {(c) => (
                <th class="relative" style={{ "text-align": c.align ?? "left" }}>
                  <Show
                    when={c.sortable !== false}
                    fallback={<span>{c.header}</span>}
                  >
                    <button class="inline-flex items-center gap-1.5" onClick={() => toggleSort(c)}>
                      {c.header}
                      <span class="text-[11px]" classList={{ "opacity-80": sort().key === c.key, "opacity-30": sort().key !== c.key }}>{sortGlyph(c)}</span>
                    </button>
                  </Show>
                  <span class="col-resizer" onPointerDown={(e) => startResize(e, c.key)} />
                </th>
              )}
            </For>
          </tr>
        </thead>
        <tbody>
          <Show
            when={groups()}
            fallback={
              <Show when={sorted().length} fallback={<tr><td colspan={props.columns.length} class="py-8 text-center text-base-content/50">No rows match the filter.</td></tr>}>
                <For each={sorted()}>{(r) => Row(r)}</For>
              </Show>
            }
          >
            <For each={groups()!}>
              {([g, rs]) => (
                <>
                  <tr class="bg-base-content/[0.04]">
                    <td colspan={props.columns.length} class="py-2">
                      {props.groupHeader ? props.groupHeader(g, rs) : <span class="eyebrow">{g}</span>}
                    </td>
                  </tr>
                  <For each={rs}>{(r) => Row(r)}</For>
                </>
              )}
            </For>
          </Show>
        </tbody>
      </table>
    </div>
  );
}
