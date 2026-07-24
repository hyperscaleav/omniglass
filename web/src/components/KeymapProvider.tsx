import { type Accessor, type ParentComponent, createContext, createSignal, onCleanup, useContext } from "solid-js";
import { hostPlatform, isMac } from "../lib/platform";
import { type Binding, type Scope, chordFromEvent, isEditableTarget, keybindingLabel, parseCombo, resolveBinding } from "../lib/keymap";

// KeymapProvider is the shortcut registry: the one window keydown listener for the
// whole console, plus a scope-registration API. It is the runtime edge over the pure
// keymap core (lib/keymap) and the settings-driven keymap (passed in via `keys`, so
// the provider itself does no data fetching and stays unit-testable). A component
// registers a scope of bindings that is active while its predicate holds; on each
// keydown the provider resolves the active scopes highest-priority-first and runs the
// first binding that claims the chord (see epic #303).

// A binding a scope contributes: an action id, a human label, the combo *spec string*
// (resolved to the host modifier at dispatch time), and the handler.
export type BindingSpec = { action: string; label: string; combo: string; run: () => void };

// A registered scope: a name, a priority (blade over list over global), an optional
// `active` predicate (default always-on), and a reactive `bindings` accessor so a
// scope's combos track the live keymap.
export type ScopeRegistration = {
  name: string;
  priority: number;
  active?: () => boolean;
  bindings: () => BindingSpec[];
};

// A resolved active scope for the help overlay: the display combo per binding.
export type ResolvedScope = { name: string; priority: number; bindings: { action: string; label: string; combo: string }[] };

export type KeymapController = {
  keys: Accessor<Record<string, string>>;
  platform: () => string;
  register: (reg: ScopeRegistration) => () => void;
  activeScopes: () => ResolvedScope[];
};

const KeymapContext = createContext<KeymapController>();

// useKeymap returns the registry controller. It throws outside a provider, matching
// useBlades / useBladeEdit; register the returned unregister with onCleanup.
export function useKeymap(): KeymapController {
  const c = useContext(KeymapContext);
  if (!c) throw new Error("useKeymap called outside a KeymapProvider");
  return c;
}

// catalogBinding builds a binding for a catalogued action: the combo comes from the
// effective keymap (`keys`), the label from the shared catalog, and only the handler
// is supplied at the call site. This keeps a shortcut's metadata single-sourced (the
// Keybindings struct); the consumer contributes just the behavior.
export function catalogBinding(action: string, keys: Record<string, string>, run: () => void): BindingSpec {
  return { action, label: keybindingLabel(action), combo: keys[action] ?? "", run };
}

// useKeymapOptional returns the controller or undefined when there is no provider,
// for a component (like BladeStack) that registers a scope when mounted in the app
// shell but is also rendered standalone (a page test) with no registry.
export function useKeymapOptional(): KeymapController | undefined {
  return useContext(KeymapContext);
}

export const KeymapProvider: ParentComponent<{ keys: Accessor<Record<string, string>>; platform?: string }> = (props) => {
  const [regs, setRegs] = createSignal<ScopeRegistration[]>([]);
  const platform = () => props.platform ?? hostPlatform();

  const register = (reg: ScopeRegistration) => {
    setRegs((rs) => [...rs, reg]);
    return () => setRegs((rs) => rs.filter((r) => r !== reg));
  };

  // The active scopes resolved to concrete Scopes for the pure resolver.
  const liveScopes = (): Scope[] =>
    regs()
      .filter((r) => (r.active ? r.active() : true))
      .map((r) => ({
        name: r.name,
        priority: r.priority,
        bindings: r.bindings().map(
          (b): Binding => ({ action: b.action, label: b.label, combo: parseCombo(b.combo, isMac(platform())), run: b.run }),
        ),
      }));

  // The same active scopes for the help overlay, keeping the combo as a display
  // string and ordered highest-priority-first.
  const activeScopes = (): ResolvedScope[] =>
    regs()
      .filter((r) => (r.active ? r.active() : true))
      .map((r) => ({ name: r.name, priority: r.priority, bindings: r.bindings().map((b) => ({ action: b.action, label: b.label, combo: b.combo })) }))
      .sort((a, b) => b.priority - a.priority);

  const onKey = (e: KeyboardEvent) => {
    // Honor a key an element-local (or Kobalte overlay) handler already consumed: it
    // listens on the element or document, ahead of this window listener in the bubble
    // phase, so a dialog's own Escape must not also trip a scope binding behind it.
    if (e.defaultPrevented) return;
    const hit = resolveBinding(liveScopes(), chordFromEvent(e), isEditableTarget(e.target));
    if (!hit) return;
    e.preventDefault();
    hit.run();
  };
  window.addEventListener("keydown", onKey);
  onCleanup(() => window.removeEventListener("keydown", onKey));

  const controller: KeymapController = { keys: props.keys, platform, register, activeScopes };
  return <KeymapContext.Provider value={controller}>{props.children}</KeymapContext.Provider>;
};
