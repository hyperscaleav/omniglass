import { For, Show } from "solid-js";
import HealthBadge from "./HealthBadge";
import type { RailModel } from "../lib/zoom_rail";

// The right rail (#630 design): one primitive on every zoom. Headline, a
// stacked health ratio bar over the components in scope, a topology line
// with the location types present, a worst-first attention list, and the
// gaps footer. A page-level aside, not a blade: the blade stack is a
// portaled overlay for focused edits, and this is a persistent read-only
// summary that shares the page.
export default function ZoomRail(props: { model: RailModel; onOpen?: (row: RailModel["list"][number]) => void }) {
  const m = () => props.model;
  const pct = (n: number) => (m().ratio.total ? `${(n / m().ratio.total) * 100}%` : "0%");
  return (
    <aside data-testid="zoom-rail" class="flex w-72 flex-none flex-col gap-5 border-l border-base-content/10 pl-5">
      <div>
        <div class="text-[10px] uppercase tracking-wider text-base-content/50">{m().kicker}</div>
        <h2 class="text-base font-semibold">{m().title}</h2>
        <div class="text-xs text-base-content/60">{m().sub}</div>
        <Show when={m().ratio.total > 0}>
          <div data-testid="rail-ratio" class="mt-2 flex h-1.5 w-full overflow-hidden rounded-full bg-base-content/10" title={`${m().ratio.healthy} healthy, ${m().ratio.incomplete} incomplete, ${m().ratio.degraded} degraded, ${m().ratio.outage} outage`}>
            <div class="bg-success" style={{ width: pct(m().ratio.healthy) }} />
            <div class="bg-incomplete" style={{ width: pct(m().ratio.incomplete) }} />
            <div class="bg-warning" style={{ width: pct(m().ratio.degraded) }} />
            <div class="bg-error" style={{ width: pct(m().ratio.outage) }} />
          </div>
        </Show>
      </div>

      <Show when={m().topology || m().types.length > 0}>
        <div>
          <div class="text-[10px] uppercase tracking-wider text-base-content/50">Topology here</div>
          <Show when={m().topology}>
            <p class="mt-1 text-xs text-base-content/70">{m().topology}</p>
          </Show>
          <Show when={m().types.length > 0}>
            <div class="mt-2 flex flex-wrap gap-1">
              <For each={m().types}>{(t) => <span class="rounded border border-base-content/15 px-1.5 py-0.5 text-[10px] uppercase tracking-wider text-base-content/60">{t}</span>}</For>
            </div>
          </Show>
        </div>
      </Show>

      <Show when={m().listTitle}>
        <div data-testid="rail-list">
          <div class="text-[10px] uppercase tracking-wider text-base-content/50">{m().listTitle}</div>
          <Show when={m().list.length > 0} fallback={<p class="mt-1 text-xs text-base-content/50">Nothing needs attention.</p>}>
            <ul class="mt-1 flex flex-col gap-1">
              <For each={m().list}>
                {(row) => (
                  <li>
                    <button
                      type="button"
                      class="flex w-full cursor-pointer items-center justify-between gap-2 rounded-md px-2 py-1 text-left text-sm hover:bg-base-content/5"
                      onClick={() => props.onOpen?.(row)}
                    >
                      <span class="truncate">{row.label}</span>
                      <HealthBadge verdict={row.verdict ?? undefined} size="xs" />
                    </button>
                  </li>
                )}
              </For>
            </ul>
          </Show>
        </div>
      </Show>

      <Show when={m().gaps}>
        <div class="mt-auto border-t border-base-content/10 pt-3">
          <div class="text-[10px] uppercase tracking-wider text-base-content/50">Gaps at this level</div>
          <p class="mt-1 text-xs text-base-content/70">{m().gaps}</p>
        </div>
      </Show>
    </aside>
  );
}
