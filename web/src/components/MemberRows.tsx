import { For, Show } from "solid-js";
import { Dynamic } from "solid-js/web";
import { HealthSlots, NARROW_HIDDEN, OUTLINE_COLS, OUTLINE_FRAME, ROOMY_ONLY, WIDE_ONLY } from "./Outline";
import { LayoutDashboard, resolveIcon } from "./icons";
import type { MemberGroup, MemberRow } from "../lib/detail";

// MemberRows (#872): a system's components in the outline's row idiom, on the
// outline's own grid, so the room's hardware reads the way Explore drew it:
// name, role, product, the alarm behind a red light, health. A role earns a
// header only where it says something a column cannot (lib/detail's
// memberModel); its gap is drawn as empty slots beneath it.

const TONE: Record<NonNullable<MemberGroup["tone"]>, string> = {
  incomplete: "text-incomplete",
  degraded: "text-warning",
  outage: "text-error",
};

function Row(props: { row: MemberRow; depth: number; onOpen: (id: string) => void }) {
  const r = () => props.row;
  return (
    <button
      type="button"
      data-testid={`member-${r().id}`}
      class={`grid w-full ${OUTLINE_COLS} h-9 cursor-pointer items-center px-4 text-left outline-none hover:bg-base-content/[0.03] focus-visible:shadow-[inset_0_0_0_1px_var(--color-primary)]`}
      onClick={() => props.onOpen(r().id)}
    >
      <span class="flex min-w-0 items-center gap-2 pr-4" style={{ "padding-left": `${props.depth * 22}px` }}>
        <span class="flex-none text-base-content/35"><Dynamic component={resolveIcon(r().icon)} size={16} /></span>
        <span data-label class="truncate font-data text-[13px]">{r().label}</span>
        <Show when={r().also.length > 0}>
          <span class="flex-none rounded border border-base-content/15 px-1.5 text-[11px] leading-4 text-base-content/55">also in {r().also.join(", ")}</span>
        </Show>
      </span>
      <span class={`truncate pr-4 text-base-content/55 ${ROOMY_ONLY}`}>{r().noRole ? "no role" : r().role}</span>
      <span class={`truncate pr-4 text-base-content/75 ${WIDE_ONLY}`}>{r().product}</span>
      <span class={`truncate pr-4 ${NARROW_HIDDEN}`} classList={{ "text-error": !!r().alarm, "text-base-content/55": !r().alarm }} title={r().alarm}>{r().alarm ?? ""}</span>
      <span><HealthSlots single={r().health} /></span>
      <span />
    </button>
  );
}

export default function MemberRows(props: { rows: MemberRow[]; groups: MemberGroup[]; onOpen: (id: string) => void }) {
  return (
    <div data-testid="member-rows" class={`${OUTLINE_FRAME} rounded-box border border-base-300 bg-base-100`}>
      <div class={`grid ${OUTLINE_COLS} h-8 items-center border-b border-base-300 px-4 text-xs font-medium text-base-content/40`} aria-hidden="true">
        <span style={{ "padding-left": "24px" }}>Component</span>
        <span class={ROOMY_ONLY}>Role</span>
        <span class={WIDE_ONLY}>Product</span>
        <span class={NARROW_HIDDEN}>Detail</span>
        <span>Health</span>
        <span />
      </div>
      <For each={props.rows}>{(r) => <Row row={r} depth={0} onOpen={props.onOpen} />}</For>
      <For each={props.groups}>
        {(g) => (
          <div data-testid={`rolegroup-${g.key}`} role="group" aria-label={g.label}>
            <div class={`grid ${OUTLINE_COLS} h-9 items-center px-4`}>
              <span class="flex min-w-0 items-center gap-2 pr-4">
                <span class="flex-none text-base-content/35"><LayoutDashboard size={16} /></span>
                <span class="truncate text-[13px] font-medium text-base-content/70">{g.label}</span>
                <span class="flex-none font-data text-xs tabular-nums" classList={{ [TONE[g.tone!]]: !!g.tone, "text-base-content/50": !g.tone }}>{g.arithmetic}</span>
              </span>
              <span class={ROOMY_ONLY} />
              <span class={WIDE_ONLY} />
              <span class={NARROW_HIDDEN} />
              <span />
              <span />
            </div>
            <For each={g.members}>{(r) => <Row row={r} depth={1} onOpen={props.onOpen} />}</For>
            <For each={Array.from({ length: g.empty })}>
              {() => (
                <div data-testid="empty-slot" class={`grid ${OUTLINE_COLS} h-9 items-center px-4`}>
                  <span class="flex items-center gap-2 pr-4" style={{ "padding-left": "22px" }}>
                    <span class="h-4 w-4 flex-none rounded border border-dashed border-base-content/30" aria-hidden="true" />
                    <span class="text-[13px] italic" classList={{ [TONE[g.tone ?? "incomplete"]]: true }}>empty slot</span>
                  </span>
                  <span class={ROOMY_ONLY} />
                  <span class={WIDE_ONLY} />
                  <span class={NARROW_HIDDEN} />
                  <span />
                  <span />
                </div>
              )}
            </For>
          </div>
        )}
      </For>
    </div>
  );
}
