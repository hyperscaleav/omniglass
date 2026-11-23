import { For, Show, children, type JSX } from "solid-js";
import type { LeaveGuard } from "../lib/leave_guard";
import type { TabDef, TabState } from "../lib/tabstate";

// TabRail: the workspace's facet switcher (#790). Which facet shows is the
// HOST's fact (lib/tabstate, #2423): a page keeps it in the URL (`?tab=`, the
// #763 deep-link rule, the default facet carrying no param), a blade in a
// signal, so a blade over a page never rewrites the page's address. One facet
// is no workspace: the rail renders only once there are two.
//
// `guard` is the host's leave guard (lib/leave_guard): switching to ANOTHER
// facet asks it first, so unsaved Configure drafts are not dropped silently.
//
// `end` is a page-wide control the rail carries at its far end, outside the
// tablist (the page's time range, #2304): it governs every facet's content,
// so it sits on the rail rather than inside one facet.
export default function TabRail(props: { tabs: TabDef[]; state: TabState; guard?: LeaveGuard; end?: JSX.Element }) {
  // Resolved once: reading props.end twice would build the control twice.
  const end = children(() => props.end);
  return (
    <Show when={props.tabs.length > 1}>
      <div data-testid="tab-rail" class="flex flex-wrap items-end gap-x-2 gap-y-1 border-b border-base-300 px-4 pt-2">
      <div role="tablist" class="flex flex-wrap gap-1">
        <For each={props.tabs}>
          {(t) => (
            <button
              type="button"
              role="tab"
              aria-selected={props.state.active() === t.key ? "true" : "false"}
              class="cursor-pointer rounded-t-md px-3 py-1.5 text-sm"
              classList={{
                "border border-b-0 border-base-300 bg-base-100 font-medium": props.state.active() === t.key,
                "text-base-content/60 hover:text-base-content": props.state.active() !== t.key,
              }}
              onClick={() => {
                if (t.key !== props.state.active() && props.guard && !props.guard.confirmLeave()) return;
                props.state.select(t.key);
              }}
            >
              {t.label}
            </button>
          )}
        </For>
      </div>
      <Show when={end()}>
        <div class="ml-auto pb-1">{end()}</div>
      </Show>
      </div>
    </Show>
  );
}
