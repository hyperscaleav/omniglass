import { batch, createMemo, createSignal, createEffect, onCleanup } from "solid-js";
import ModalDialog, { DialogHeading } from "./ui/ModalDialog";
import Search, { type SearchOption } from "./ui/Search";
import { useNavigate } from "@solidjs/router";
import { navItems, filterNav } from "../lib/nav";
import { entryHref, visibleGroups } from "../lib/catalog";
import { useMe, can } from "../lib/auth";
import { type DrawerKind, drawerHref } from "../lib/drawer_address";
import { Bot } from "./icons";
import { api } from "../api/client";
import { MIN_QUERY, type Hit, type SearchResult, layout, pageHit, readRecent, remember, searchHits, sections } from "../lib/palette";

// The ⌘K command palette: a global jump across the whole app (distinct from a
// page's own filter). Built on Kobalte Dialog (focus-trap, Esc, scroll-lock for
// free). Arrow keys move the active row, Enter navigates.
// A result for one catalog or admin entity sets `entity` instead of a hand-built
// path: it opens that entity's drawer at its address (drawerHref, #2429).
export type Command = { label: string; path: string; group?: string; section?: string; entity?: { kind: DrawerKind; address: string } };

// commandPath is where a result goes: its entity's drawer address, else its path.
export const commandPath = (c: Command): string => (c.entity ? drawerHref(c.entity.kind, c.entity.address) : c.path);

// buildCommands is the palette's source: every destination the caller may
// reach, flattened to one searchable list.
//
// Two halves, each judged through the SAME `allow` its own surface uses, so the
// palette can never offer a jump the route guard would bounce (and never hides
// one the rail shows). The rail half runs through nav.ts's filterNav, the very
// function that hides a sidebar button. The catalog half runs through
// catalog.ts's visibleGroups, the very function the shell's subrail and the
// Overview cards render from: the catalogs left the rail for that subrail
// (#625), and reading them from CATALOG_GROUPS is what keeps them findable by
// name here without a second membership list to drift (#628).
//
// A catalog wears its catalog group as a section, so the group tag reads
// `Catalog · Telemetry` and a lane name finds its catalogs. A pathless soon
// slot (the Templates reservations) is no destination and is left out; a routed
// soon slot (Rules, Notifications) jumps to its stub, exactly as the rail's own
// unbuilt sections do.
export function buildCommands(allow: (tokens: string[]) => boolean): Command[] {
  const rail: Command[] = filterNav(navItems, allow).flatMap((item) =>
    item.path
      ? [{ label: item.label, path: item.path }]
      : (item.children ?? []).map((c) => ({
          label: c.label,
          path: c.path,
          group: c.section ? `${item.label} · ${c.section}` : item.label,
          section: c.section,
        })),
  );
  const catalog: Command[] = visibleGroups(allow).flatMap((g) =>
    g.entries
      .filter((e) => e.path)
      .map((e) => ({ label: e.label, path: entryHref(e), group: `Catalog · ${g.header}`, section: g.header })),
  );
  return [...rail, ...catalog];
}

// matches is the palette filter: label, group, or section.
export const matches = (c: Command, q: string): boolean =>
  c.label.toLowerCase().includes(q) || (c.group ?? "").toLowerCase().includes(q) || (c.section ?? "").toLowerCase().includes(q);

// How long the palette waits after a keystroke before it asks the server.
export const SEARCH_DEBOUNCE_MS = 200;
// The most hits the server returns per kind (the palette shows a handful).
const PER_KIND = 6;

export default function CommandPalette(props: { open: boolean; onClose: () => void; onAsk?: (prompt: string) => void }) {
  const navigate = useNavigate();
  const me = useMe();
  const commands = createMemo(() => buildCommands((tokens) => can(me.data, ...tokens)));
  const [query, setQuery] = createSignal("");
  const [found, setFound] = createSignal<SearchResult | undefined>();
  const [searching, setSearching] = createSignal(false);
  const userId = () => me.data?.user.id ?? "";
  const [recent, setRecent] = createSignal<Hit[]>([]);

  // Each keystroke cancels the last one's pending timer and in-flight request; the
  // survivor searches after a pause. A query too short to search clears the hits.
  createEffect(() => {
    const raw = query().trim();
    if (!props.open || raw.length < MIN_QUERY) {
      setFound(undefined);
      setSearching(false);
      return;
    }
    setSearching(true);
    const ctrl = new AbortController();
    const timer = setTimeout(async () => {
      try {
        const res = await api.GET("/search", { params: { query: { q: raw, limit: PER_KIND } }, signal: ctrl.signal });
        if (ctrl.signal.aborted) return;
        batch(() => {
          setFound(res.data as SearchResult | undefined);
          setSearching(false);
        });
      } catch {
        if (ctrl.signal.aborted) return;
        batch(() => {
          setFound(undefined);
          setSearching(false);
        });
      }
    }, SEARCH_DEBOUNCE_MS);
    onCleanup(() => {
      clearTimeout(timer);
      ctrl.abort();
    });
  });

  // A fresh open starts blank, with this person's recents.
  createEffect(() => {
    if (props.open) {
      setQuery("");
      setRecent(readRecent(userId()));
    }
  });

  const canAsk = () => !!props.onAsk && can(me.data, "assistant", "use");
  const rows = createMemo<Hit[]>(() => {
    const raw = query().trim();
    const q = raw.toLowerCase();
    const pages = (q ? commands().filter((c) => matches(c, q)) : commands()).map(pageHit);
    return layout({ pages, found: searchHits(found()), recent: recent(), query: raw, canAsk: canAsk(), searching: searching() });
  });
  const byValue = createMemo(() => new Map(rows().map((h) => [h.value, h])));
  const shown = createMemo(() =>
    sections(rows()).map((s) => ({
      label: s.label as string,
      options: s.options.map((h): SearchOption => ({ value: h.value, label: h.label, detail: h.detail, icon: h.ask !== undefined ? <Bot size={14} /> : undefined })),
    })),
  );

  const go = (o: SearchOption) => {
    const hit = byValue().get(o.value);
    if (!hit) return;
    if (hit.ask !== undefined) {
      props.onClose();
      return props.onAsk?.(hit.ask);
    }
    if (!hit.command) return;
    const path = commandPath(hit.command);
    remember(userId(), hit, path);
    props.onClose();
    navigate(path);
  };

  return (
    <ModalDialog
      open={props.open}
      onClose={props.onClose}
      overlayClass="fixed inset-0 z-60 bg-black/50"
      frameClass="fixed inset-0 z-60 flex items-start justify-center px-4 pt-[15vh]"
      class="w-full max-w-lg rounded-box border border-base-300 bg-base-100 shadow-2xl"
    >
      <DialogHeading class="sr-only">Command palette</DialogHeading>
      <Search
        aria-label="Search the console"
        placeholder="Jump to a page, location, system, component or catalog entry…"
        sections={shown()}
        loading={searching() && query().trim().length >= MIN_QUERY}
        onQuery={setQuery}
        onPick={go}
        onEscape={props.onClose}
      />
    </ModalDialog>
  );
}
