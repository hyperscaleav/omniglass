import { For, Show, createMemo, type ParentComponent } from "solid-js";
import { A } from "@solidjs/router";
import { useQuery } from "@tanstack/solid-query";
import { Soon } from "./Sidebar";
import { useMe, can } from "../lib/auth";
import { OVERVIEW_PATH, visibleGroups, type CatalogEntry } from "../lib/catalog";

// The Catalog shell: a layout route (index.tsx wraps the catalog routes in
// <Route component={CatalogShell}>) drawing the two-column chrome around every
// registry page. The subrail on the left is NAVIGATION, not a filter: each
// entry is a link to the registry's own canonical route (/products, /metrics,
// ...), whose real page, with its own FilterBar, create button, and editing
// blades, renders in the right pane; Overview leads to the /catalog landing.
// Membership, order, labels, gates, and count sources come from the
// CATALOG_GROUPS table (lib/catalog.ts), the same table the Overview cards
// derive from, judged through the same can() the app rail uses: a gated entry
// drops (its count query never created), an emptied group drops its header,
// and secret types hold no entry at all though /secret-types still renders in
// the pane.
//
// Scroll: the shell adds no scroll region of its own. The pane's page grows
// naturally and the app shell's <main> keeps the document scroll, exactly as
// when the page stood alone; the subrail sticks to the scrollport top with its
// own overflow, like the app rail.
const CatalogShell: ParentComponent = (props) => {
  const me = useMe();
  const groups = createMemo(() => visibleGroups((tokens) => can(me.data, ...tokens)));

  return (
    <div class="flex items-start gap-4">
      <nav
        class="sticky top-0 max-h-[calc(100vh-3.5rem)] w-52.5 flex-none self-start overflow-y-auto"
        aria-label="Catalog sections"
      >
        <ul class="menu w-full flex-nowrap gap-0.5 p-0 [&_li>*]:rounded-field">
          <li>
            <A href={OVERVIEW_PATH} end activeClass="menu-active">
              <span class="flex-1 truncate">Overview</span>
            </A>
          </li>
          <For each={groups()}>
            {(g) => (
              <>
                <li class="menu-title px-2 pb-0.5 pt-2 text-[10.5px] uppercase tracking-wider">{g.header}</li>
                <For each={g.entries}>
                  {(e) => (
                    <li>
                      <Entry entry={e} />
                    </li>
                  )}
                </For>
              </>
            )}
          </For>
        </ul>
      </nav>

      <div class="min-w-0 flex-1">{props.children}</div>
    </div>
  );
};

export default CatalogShell;

// One subrail entry. A live registry links to its page with its live count; a
// soon slot wears the rail's SOON treatment, linking when its stub page is
// routed (Rules, Notifications) and rendering disabled when no page exists yet
// (the Templates slots).
function Entry(props: { entry: CatalogEntry }) {
  return (
    <Show
      when={!props.entry.soon}
      fallback={
        <Show
          when={props.entry.path}
          fallback={
            <button type="button" disabled class="opacity-45">
              <span class="flex-1 truncate">{props.entry.label}</span>
              <Soon />
            </button>
          }
        >
          <A href={props.entry.path!} activeClass="menu-active" class="opacity-45">
            <span class="flex-1 truncate">{props.entry.label}</span>
            <Soon />
          </A>
        </Show>
      }
    >
      <A href={props.entry.path!} activeClass="menu-active">
        <span class="flex-1 truncate">{props.entry.label}</span>
        <EntryCount entry={props.entry} />
      </A>
    </Show>
  );
}

// A live entry's count: right-aligned, mono, muted; its own query on the
// registry's shared list key (one cache with the page the link opens), absent
// until it resolves and quiet on failure (never an invented zero; the page
// itself owns the error surface).
function EntryCount(props: { entry: CatalogEntry }) {
  const q = useQuery(() => ({ queryKey: [...props.entry.key!], queryFn: props.entry.list! }));
  return (
    <Show when={q.data}>
      {(rows) => <span class="ml-auto font-data tnum text-xs text-base-content/45">{rows().length}</span>}
    </Show>
  );
}
