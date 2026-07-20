// The keymap primitive's pure core: a keyboard-shortcut grammar, a match predicate,
// a typing guard, and scope-ordered resolution. Everything here is DOM-free and
// deterministic (a KeyChord is a plain record), so it unit-tests without a browser;
// the provider (KeymapProvider) is the one thin runtime edge that reads real
// KeyboardEvents and the settings keymap. This is the shortcut registry the settings
// `keybindings` namespace was built to feed (epic #303).

// A Combo is a parsed binding spec. `mod` is already resolved to the host modifier
// (meta on mac, ctrl elsewhere) at parse time, so matching stays platform-agnostic.
export type Combo = { key: string; meta: boolean; ctrl: boolean; alt: boolean; shift: boolean };

// A KeyChord is a normalized keydown: the pressed key plus the modifier flags, the
// shape matchCombo compares a Combo against.
export type KeyChord = { key: string; meta: boolean; ctrl: boolean; alt: boolean; shift: boolean };

// A Binding pairs a resolved combo with the action to run; `action` is a stable id
// and `label` is the human name shown in the help overlay.
export type Binding = { action: string; label: string; combo: Combo; run: () => void };

// A Scope is a named, prioritized set of bindings. Higher priority claims a chord
// first (blade over list over global), so an open blade's Escape wins over a list's.
export type Scope = { name: string; priority: number; bindings: Binding[] };

// The code-layer keymap defaults, mirroring the Keybindings struct
// (internal/settings/schema.go). /settings/me overlays operator overrides on top,
// but these keep the registry working before the settings read resolves.
export const DEFAULT_KEYBINDINGS: Record<string, string> = {
  open_detail: "d",
  open_edit: "e",
  close_blade: "Escape",
  command_palette: "mod+k",
};

// keybindingsFromMe resolves the effective keymap: the code defaults with the
// client-visible `keybindings` namespace from /settings/me layered over them. Only
// non-empty string overrides win, so a malformed value falls back to its default.
export function keybindingsFromMe(me: { values?: Record<string, Record<string, unknown>> } | undefined): Record<string, string> {
  const overrides = me?.values?.keybindings ?? {};
  const out: Record<string, string> = { ...DEFAULT_KEYBINDINGS };
  for (const action of Object.keys(out)) {
    const v = overrides[action];
    if (typeof v === "string" && v.length > 0) out[action] = v;
  }
  return out;
}

const MODIFIER_TOKENS = new Set(["mod", "cmd", "command", "meta", "super", "ctrl", "control", "alt", "option", "opt", "shift"]);

// parseCombo turns a spec string ("mod+k", "shift+3", "Escape", "?") into a Combo.
// The `mod` token resolves to meta on mac and ctrl elsewhere; the last non-modifier
// token is the key, lower-cased so matching is case-insensitive.
export function parseCombo(spec: string, isMacHost: boolean): Combo {
  const combo: Combo = { key: "", meta: false, ctrl: false, alt: false, shift: false };
  const tokens = spec.split("+").map((t) => t.trim()).filter((t) => t.length > 0);
  for (const raw of tokens) {
    const t = raw.toLowerCase();
    if (!MODIFIER_TOKENS.has(t)) {
      combo.key = t;
      continue;
    }
    switch (t) {
      case "mod":
        if (isMacHost) combo.meta = true;
        else combo.ctrl = true;
        break;
      case "cmd":
      case "command":
      case "meta":
      case "super":
        combo.meta = true;
        break;
      case "ctrl":
      case "control":
        combo.ctrl = true;
        break;
      case "alt":
      case "option":
      case "opt":
        combo.alt = true;
        break;
      case "shift":
        combo.shift = true;
        break;
    }
  }
  return combo;
}

// matchCombo is the pure predicate: the key matches case-insensitively, each named
// control modifier must match exactly (present when the combo names it, absent when
// it does not), and shift is only enforced when the combo names it. Shift is left
// permissive otherwise because symbol keys ("?", "#") carry their own shift in the
// produced character, so requiring shift-absent would never match them.
export function matchCombo(chord: KeyChord, combo: Combo): boolean {
  if (chord.key.toLowerCase() !== combo.key) return false;
  if (chord.meta !== combo.meta) return false;
  if (chord.ctrl !== combo.ctrl) return false;
  if (chord.alt !== combo.alt) return false;
  if (combo.shift && !chord.shift) return false;
  // Shift is permissive for symbol keys (a "?" or "#" carries its own shift), but a
  // bare *letter* binding must not fire with Shift held: Shift+D is not `d`. Only when
  // the combo does not itself name shift and the key is a single letter.
  if (!combo.shift && chord.shift && /^[a-z]$/.test(combo.key)) return false;
  return true;
}

// chordFromEvent normalizes a real KeyboardEvent into a KeyChord. It reads only the
// fields the core needs, so a plain object stands in for a jsdom event in tests.
export function chordFromEvent(e: Pick<KeyboardEvent, "key" | "metaKey" | "ctrlKey" | "altKey" | "shiftKey">): KeyChord {
  return { key: e.key.toLowerCase(), meta: e.metaKey, ctrl: e.ctrlKey, alt: e.altKey, shift: e.shiftKey };
}

// isEditableTarget is the typing guard's DOM edge: true when focus is in a field the
// user is typing into, so single-key shortcuts do not fire mid-word.
export function isEditableTarget(target: EventTarget | null): boolean {
  if (!(target instanceof HTMLElement)) return false;
  const tag = target.tagName;
  if (tag === "INPUT" || tag === "TEXTAREA" || tag === "SELECT") return true;
  if (target.isContentEditable) return true;
  // isContentEditable is not implemented in jsdom, so fall back to the attribute.
  const ce = target.getAttribute("contenteditable");
  return ce === "" || ce === "true";
}

// allowsWhileTyping decides whether a binding may fire while focus is in a text
// field. A control-modified combo (mod/ctrl/alt) or Escape always may; a bare or
// shift-only key (a letter, "?", "#") is suppressed so it does not eat typing.
export function allowsWhileTyping(combo: Combo): boolean {
  return combo.meta || combo.ctrl || combo.alt || combo.key === "escape";
}

// resolveBinding walks the active scopes highest-priority first and returns the
// first binding that claims the chord, honoring the typing guard when `editable` is
// true. Returns null when no active scope handles the chord (it falls through to the
// browser and to element-local handlers).
export function resolveBinding(scopes: Scope[], chord: KeyChord, editable: boolean): Binding | null {
  const ordered = [...scopes].sort((a, b) => b.priority - a.priority);
  for (const scope of ordered) {
    for (const binding of scope.bindings) {
      if (editable && !allowsWhileTyping(binding.combo)) continue;
      if (matchCombo(chord, binding.combo)) return binding;
    }
  }
  return null;
}
