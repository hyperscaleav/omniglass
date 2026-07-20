import { For, Show, createMemo, createSignal } from "solid-js";
import { Dialog } from "@kobalte/core/dialog";
import { useKeymap } from "./KeymapProvider";
import Button from "./Button";
import { formatCombo } from "../lib/platform";
import { keybindingsCatalog, keybindingLabel } from "../lib/keymap";

// The keyboard help overlay (opened with `?`): a cheat sheet with two views. "Active"
// renders the LIVE registry, so it shows exactly the shortcuts in effect in the
// current context (doctrine 4: the surface teaches what it operates). "All" renders
// the full catalog (every declared shortcut) from the single source, so every
// shortcut is discoverable from one place regardless of context. Each combo is
// labelled per host (⌘K on mac, Ctrl+K elsewhere).

// Friendly section titles for the scope names the registry uses.
const SCOPE_TITLES: Record<string, string> = {
  global: "Global",
  blade: "Blade",
  list: "List",
};

const scopeTitle = (name: string) => SCOPE_TITLES[name] ?? name.charAt(0).toUpperCase() + name.slice(1);

export default function KeyboardHelp(props: { open: boolean; onClose: () => void }) {
  const km = useKeymap();
  const [showAll, setShowAll] = createSignal(false);
  // One computation per render: activeScopes filters, maps, and sorts.
  const scopes = createMemo(() => km.activeScopes());
  // The full catalog as rows, each with its effective combo (an operator override
  // from the keymap, else the catalogued default).
  const catalogRows = createMemo(() =>
    Object.keys(keybindingsCatalog)
      .map((action) => ({ action, label: keybindingLabel(action), combo: km.keys()[action] ?? keybindingsCatalog[action as keyof typeof keybindingsCatalog].default }))
      .sort((a, b) => a.label.localeCompare(b.label)),
  );

  return (
    <Dialog open={props.open} onOpenChange={(o) => !o && props.onClose()}>
      <Dialog.Portal>
        <Dialog.Overlay class="fixed inset-0 z-60 bg-black/50" />
        <div class="fixed inset-0 z-60 flex items-start justify-center px-4 pt-[15vh]">
          <Dialog.Content class="w-full max-w-md overflow-hidden rounded-box border border-base-300 bg-base-100 shadow-2xl">
            <div class="flex items-center justify-between gap-3 border-b border-base-300 px-4 py-3">
              <Dialog.Title class="text-sm font-semibold">Keyboard shortcuts</Dialog.Title>
              <div class="flex items-center gap-2">
                <div class="join">
                  <Button size="xs" intent={showAll() ? "quiet" : "action"} class="join-item" onClick={() => setShowAll(false)}>Active</Button>
                  <Button size="xs" intent={showAll() ? "action" : "quiet"} class="join-item" onClick={() => setShowAll(true)}>All</Button>
                </div>
                <kbd class="kbd kbd-sm">esc</kbd>
              </div>
            </div>
            <div class="max-h-[60vh] overflow-y-auto p-2">
              <Show
                when={!showAll()}
                fallback={
                  <ul>
                    <For each={catalogRows()}>
                      {(b) => (
                        <li class="flex items-center justify-between gap-3 rounded-field px-3 py-1.5 text-sm">
                          <span>{b.label}</span>
                          <kbd class="kbd kbd-sm">{formatCombo(km.platform(), b.combo)}</kbd>
                        </li>
                      )}
                    </For>
                  </ul>
                }
              >
                <Show
                  when={scopes().some((s) => s.bindings.length > 0)}
                  fallback={<p class="px-3 py-6 text-center text-sm text-base-content/40">No shortcuts active here. Switch to <span class="font-medium">All</span> to see every shortcut.</p>}
                >
                  <For each={scopes()}>
                    {(scope) => (
                      <Show when={scope.bindings.length > 0}>
                        <section class="mb-1">
                          <h3 class="px-3 pb-1 pt-2 text-xs font-medium uppercase tracking-wide text-base-content/40">{scopeTitle(scope.name)}</h3>
                          <ul>
                            <For each={scope.bindings}>
                              {(b) => (
                                <li class="flex items-center justify-between gap-3 rounded-field px-3 py-1.5 text-sm">
                                  <span>{b.label}</span>
                                  <kbd class="kbd kbd-sm">{formatCombo(km.platform(), b.combo)}</kbd>
                                </li>
                              )}
                            </For>
                          </ul>
                        </section>
                      </Show>
                    )}
                  </For>
                </Show>
              </Show>
            </div>
          </Dialog.Content>
        </div>
      </Dialog.Portal>
    </Dialog>
  );
}
