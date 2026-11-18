import {
  For,
  Show,
  createMemo,
  createSignal,
  createEffect,
  createUniqueId,
} from "solid-js";
import { Dialog } from "@kobalte/core/dialog";
import { useNavigate } from "@solidjs/router";
import { navItems, filterNav } from "../lib/nav";
import { entryHref, visibleGroups } from "../lib/catalog";
import { useMe, can } from "../lib/auth";
import { tray, triageCall, type Subject } from "../lib/handover";
import { Search, ArrowRight, Bot } from "./icons";

// The ⌘K command palette: a global jump across the whole app (distinct from a
// page's own filter). Built on Kobalte Dialog (focus-trap, Esc, scroll-lock for
// free). Arrow keys move the active row, Enter navigates.
export type Command = {
  label: string;
  path: string;
  group?: string;
  section?: string;
};

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
      .map((e) => ({
        label: e.label,
        path: entryHref(e),
        group: `Catalog · ${g.header}`,
        section: g.header,
      })),
  );
  return [...rail, ...catalog];
}

// matches is the palette filter: label, group, or section.
export const matches = (c: Command, q: string): boolean =>
  c.label.toLowerCase().includes(q) || (c.group ?? "").toLowerCase().includes(q) || (c.section ?? "").toLowerCase().includes(q);

// An ask row hands the palette's query to omnibot (#1167) instead of jumping;
// a triage row runs the triage-alarm skill on an alarm gathered into omnibot's
// tray (#2122).
export type AskRow = { label: string; ask: true };
export type TriageRow = { label: string; triage: Subject };
export type Row = Command | AskRow | TriageRow;

// withAsk ends the results with a triage row for each gathered alarm the query
// matches, then the ask row, for a person who may run omnibot.
export function withAsk(
  results: Command[],
  q: string,
  allowed: boolean,
  gathered: Subject[] = [],
): Row[] {
  if (!allowed) return results;
  const triage = gathered
    .filter((s) => triageCall(s))
    .map((s) => ({ label: `Triage alarm ${s.ref}`, triage: s }))
    .filter((r) => r.label.toLowerCase().includes(q.toLowerCase()));
  return [
    ...results,
    ...triage,
    { label: q ? `Ask omnibot: ${q}` : "Ask omnibot", ask: true },
  ];
}

export default function CommandPalette(props: {
  open: boolean;
  onClose: () => void;
  onAsk?: (prompt: string) => void;
  onTriage?: (alarm: Subject) => void;
}) {
  const navigate = useNavigate();
  const me = useMe();
  const commands = createMemo(() =>
    buildCommands((tokens) => can(me.data, ...tokens)),
  );
  const [query, setQuery] = createSignal("");
  const [active, setActive] = createSignal(0);
  const listId = createUniqueId();
  const optId = (i: number) => `${listId}-opt-${i}`;

  const results = createMemo(() => {
    const raw = query().trim();
    const q = raw.toLowerCase();
    const found = q ? commands().filter((c) => matches(c, q)) : commands();
    return withAsk(
      found,
      raw,
      !!props.onAsk && can(me.data, "assistant", "use"),
      props.onTriage ? tray() : [],
    );
  });

  // Reset query + selection each time it opens; keep active in range as results change.
  createEffect(() => {
    if (props.open) {
      setQuery("");
      setActive(0);
    }
  });
  createEffect(() => {
    if (active() >= results().length) setActive(Math.max(0, results().length - 1));
  });

  const go = (c: Row | undefined) => {
    if (!c) return;
    props.onClose();
    if ("ask" in c) props.onAsk?.(query().trim());
    else if ("triage" in c) props.onTriage?.(c.triage);
    else navigate(c.path);
  };

  const onKey = (e: KeyboardEvent) => {
    if (e.key === "ArrowDown") {
      e.preventDefault();
      setActive((i) => Math.min(i + 1, results().length - 1));
    } else if (e.key === "ArrowUp") {
      e.preventDefault();
      setActive((i) => Math.max(i - 1, 0));
    } else if (e.key === "Enter") {
      e.preventDefault();
      go(results()[active()]);
    }
  };

  return (
    <Dialog open={props.open} onOpenChange={(o) => !o && props.onClose()}>
      <Dialog.Portal>
        <Dialog.Overlay class="fixed inset-0 z-60 bg-black/50" />
        <div class="fixed inset-0 z-60 flex items-start justify-center px-4 pt-[15vh]">
          <Dialog.Content class="w-full max-w-lg overflow-hidden rounded-box border border-base-300 bg-base-100 shadow-2xl">
            <Dialog.Title class="sr-only">Command palette</Dialog.Title>
            <div class="flex items-center gap-2 border-b border-base-300 px-4">
              <Search size={16} />
              <input
                class="h-12 w-full bg-transparent text-sm outline-none placeholder:text-base-content/40"
                placeholder="Jump to…"
                value={query()}
                role="combobox"
                aria-expanded={results().length > 0}
                aria-controls={listId}
                aria-autocomplete="list"
                aria-activedescendant={results().length ? optId(active()) : undefined}
                onInput={(e) => setQuery(e.currentTarget.value)}
                onKeyDown={onKey}
                autofocus
              />
              <kbd class="kbd kbd-sm">esc</kbd>
            </div>
            <ul id={listId} role="listbox" class="max-h-80 overflow-y-auto p-2">
              <Show when={results().length > 0} fallback={<li class="px-3 py-6 text-center text-sm text-base-content/40">No matches</li>}>
                <For each={results()}>
                  {(c, i) => (
                    <li>
                      <button
                        id={optId(i())}
                        role="option"
                        aria-selected={i() === active()}
                        class="flex w-full items-center gap-3 rounded-field px-3 py-2 text-left text-sm"
                        classList={{
                          "bg-primary/15 text-primary": i() === active(),
                        }}
                        onMouseEnter={() => setActive(i())}
                        onClick={() => go(c)}
                      >
                        <Show when={"ask" in c || "triage" in c}>
                          <Bot size={14} />
                        </Show>
                        <Show when={"group" in c && c.group}>
                          <span class="text-xs text-base-content/40">{"group" in c && c.group}</span>
                          <span class="text-base-content/30">/</span>
                        </Show>
                        <span class="flex-1">{c.label}</span>
                        <Show when={i() === active()}>
                          <ArrowRight size={14} />
                        </Show>
                      </button>
                    </li>
                  )}
                </For>
              </Show>
            </ul>
          </Dialog.Content>
        </div>
      </Dialog.Portal>
    </Dialog>
  );
}
