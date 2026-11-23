import { For, Show, createEffect, createMemo, createSignal, on, type ParentComponent } from "solid-js";
import { A, useHref, useLocation, useNavigate } from "@solidjs/router";
import { useQuery } from "@tanstack/solid-query";
import { Soon } from "./Sidebar";
import Button from "./Button";
import { ChevronDown } from "./icons";
import { useMe, can } from "../lib/auth";
import { OVERVIEW_PATH, entryActive, entryCount, entryHref, visibleGroups, type CatalogEntry } from "../lib/catalog";

// The Catalog shell: a layout route (index.tsx wraps the catalog routes in
// <Route component={CatalogShell}>) drawing the two-column chrome around every
// catalog page. The subrail on the left is NAVIGATION, not a filter: each
// entry is a link to the catalog's own canonical route (/products, /metrics,
// ...), whose real page, with its own FilterBar, create button, and editing
// blades, renders in the right pane; Overview leads to the /catalog landing.
// Membership, order, labels, gates, and count sources come from the
// CATALOG_GROUPS table (lib/catalog.ts), the same table the Overview cards
// derive from, judged through the same can() the app rail uses: a gated entry
// drops (its count query never created), an emptied group drops its header,
// and secret types hold no entry at all though /secret-types still renders in
// the pane.
//
// Scroll: the shell fills the height <main> gives it (h-full). The subrail keeps
// its own overflow and the pane scrolls beside it, so the subrail never leaves
// the screen and a page can fill the pane (h-full) without a guessed offset.
const CatalogShell: ParentComponent = (props) => {
  const me = useMe();
  const groups = createMemo(() => visibleGroups((tokens) => can(me.data, ...tokens)));

  // Below 1100 px the app rail plus a 210 px subrail leave the list about 300 px
  // (150 with a drawer open), so the subrail folds behind one button naming the
  // section you are in; it opens in place and closes on navigation (#2431).
  const location = useLocation();
  const [open, setOpen] = createSignal(false);
  createEffect(on(() => location.pathname + location.search, () => setOpen(false), { defer: true }));
  const current = () => {
    for (const g of groups()) for (const e of g.entries) if (entryActive(e, location.pathname, location.search)) return e.label;
    return location.pathname.replace(/^\/web/, "") === OVERVIEW_PATH ? "Overview" : "sections";
  };

  return (
    <div class="flex h-full flex-col gap-3 min-[1100px]:flex-row min-[1100px]:items-start min-[1100px]:gap-4">
      <Button class="justify-between min-[1100px]:hidden" expanded={open()} controls="catalog-subrail" onClick={() => setOpen(!open())}>
        <span class="truncate">Catalog: {current()}</span>
        <ChevronDown size={14} />
      </Button>
      <nav
        id="catalog-subrail"
        class="w-full flex-none overflow-y-auto min-[1100px]:block min-[1100px]:max-h-full min-[1100px]:w-52.5 min-[1100px]:self-start"
        classList={{ hidden: !open() }}
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

      <div class="min-h-0 min-w-0 flex-1 overflow-y-auto min-[1100px]:h-full">{props.children}</div>
    </div>
  );
};

export default CatalogShell;

// One subrail entry. A live catalog links to its page with its live count; a
// soon slot wears the rail's SOON treatment, linking when its stub page is
// routed (Notifications) and rendering disabled when no page exists yet. A
// filtered slot (the three Templates slots, one page) lights by its query too,
// so only the slot whose kinds are showing reads active.
function Entry(props: { entry: CatalogEntry }) {
  const location = useLocation();
  const active = () => entryActive(props.entry, location.pathname, location.search);
  const navigate = useNavigate();
  const href = useHref(() => entryHref(props.entry));
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
      <Show
        when={props.entry.query}
        fallback={
          <A href={props.entry.path!} activeClass="menu-active">
            <span class="flex-1 truncate">{props.entry.label}</span>
            <EntryCount entry={props.entry} />
          </A>
        }
      >
        {/* A router link marks every query of its path current, so a filtered
            slot is its own anchor: active, and aria-current, on its query. */}
        <a
          href={href()}
          classList={{ "menu-active": active() }}
          aria-current={active() ? "page" : undefined}
          onClick={(e) => {
            if (e.metaKey || e.ctrlKey || e.shiftKey || e.button !== 0) return;
            e.preventDefault();
            navigate(entryHref(props.entry));
          }}
        >
          <span class="flex-1 truncate">{props.entry.label}</span>
          <EntryCount entry={props.entry} />
        </a>
      </Show>
    </Show>
  );
}

// A live entry's count: right-aligned, mono, muted; its own query on the
// catalog's shared list key (one cache with the page the link opens), absent
// until it resolves and quiet on failure (never an invented zero; the page
// itself owns the error surface).
function EntryCount(props: { entry: CatalogEntry }) {
  const q = useQuery(() => ({ queryKey: [...props.entry.key!], queryFn: props.entry.list! }));
  return (
    <Show when={q.data}>
      {(rows) => (
        <span data-testid="catalog-entry-count" class="ml-auto font-data tnum text-xs text-base-content/45">
          {entryCount(props.entry, rows())}
        </span>
      )}
    </Show>
  );
}
