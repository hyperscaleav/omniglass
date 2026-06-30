import { For, Show, createMemo, createSignal, createEffect, createUniqueId } from "solid-js";
import { Dialog } from "@kobalte/core/dialog";
import { useNavigate } from "@solidjs/router";
import { navItems } from "../lib/nav";
import { Search, ArrowRight } from "./icons";

// The ⌘K command palette: a global jump across the whole app (distinct from a
// page's own filter). Built on Kobalte Dialog (focus-trap, Esc, scroll-lock for
// free); the command source is the nav IA flattened to destinations. Arrow keys
// move the active row, Enter navigates.
type Command = { label: string; path: string; group?: string };

const commands: Command[] = navItems.flatMap((item) =>
  item.path
    ? [{ label: item.label, path: item.path }]
    : (item.children ?? []).map((c) => ({ label: c.label, path: c.path, group: item.label })),
);

export default function CommandPalette(props: { open: boolean; onClose: () => void }) {
  const navigate = useNavigate();
  const [query, setQuery] = createSignal("");
  const [active, setActive] = createSignal(0);
  const listId = createUniqueId();
  const optId = (i: number) => `${listId}-opt-${i}`;

  const results = createMemo(() => {
    const q = query().trim().toLowerCase();
    if (!q) return commands;
    return commands.filter((c) => c.label.toLowerCase().includes(q) || (c.group ?? "").toLowerCase().includes(q));
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

  const go = (c: Command | undefined) => {
    if (!c) return;
    props.onClose();
    navigate(c.path);
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
                        classList={{ "bg-primary/15 text-primary": i() === active() }}
                        onMouseEnter={() => setActive(i())}
                        onClick={() => go(c)}
                      >
                        <Show when={c.group}>
                          <span class="text-xs text-base-content/40">{c.group}</span>
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
