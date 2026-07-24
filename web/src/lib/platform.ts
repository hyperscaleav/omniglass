// Platform-native labelling for keyboard shortcuts. The command-palette binding
// listens for both Meta and Ctrl (App.tsx), so the only thing that varies by host
// is how we *label* it: the ⌘ glyph on macOS, "Ctrl" everywhere else (the glyph is
// mac-only and renders as tofu in non-mac fonts). The string-in core is pure so it
// unit-tests without a browser; hostPlatform() is the thin runtime edge.

export function isMac(platform: string): boolean {
  return /mac/i.test(platform);
}

// The modifier key as the host writes it: ⌘ on mac, Ctrl elsewhere.
export function shortcutModifier(platform: string): "⌘" | "Ctrl" {
  return isMac(platform) ? "⌘" : "Ctrl";
}

// A flat combo string for tooltips/aria, native per platform: "⌘K" vs "Ctrl+K"
// (mac convention omits the plus, others keep it).
export function shortcutHint(platform: string, key: string): string {
  return isMac(platform) ? `⌘${key}` : `Ctrl+${key}`;
}

// Named-key display labels; anything else single-char is upper-cased and any other
// multi-char token is shown verbatim.
const KEY_LABELS: Record<string, string> = {
  escape: "Esc",
  arrowup: "↑",
  arrowdown: "↓",
  arrowleft: "←",
  arrowright: "→",
  " ": "Space",
  space: "Space",
  enter: "Enter",
  tab: "Tab",
  backspace: "⌫",
};

function keyLabel(key: string): string {
  const lower = key.toLowerCase();
  if (KEY_LABELS[lower]) return KEY_LABELS[lower];
  return key.length === 1 ? key.toUpperCase() : key;
}

// formatCombo renders a binding spec ("mod+k", "shift+3", "Escape", "?") as a
// platform-native label for a <kbd> hint: glyphs joined tight on mac (⇧⌘K), words
// joined with "+" elsewhere (Ctrl+Shift+K). It is the display sibling of the keymap
// core's parseCombo, kept here beside the other platform labelling.
export function formatCombo(platform: string, spec: string): string {
  const mac = isMac(platform);
  const mods = { ctrl: false, alt: false, shift: false, mod: false, meta: false };
  let key = "";
  for (const raw of spec.split("+").map((t) => t.trim()).filter((t) => t.length > 0)) {
    switch (raw.toLowerCase()) {
      case "mod": mods.mod = true; break;
      case "ctrl": case "control": mods.ctrl = true; break;
      case "alt": case "option": case "opt": mods.alt = true; break;
      case "shift": mods.shift = true; break;
      case "cmd": case "command": case "meta": case "super": mods.meta = true; break;
      default: key = raw;
    }
  }
  const parts: string[] = [];
  if (mods.ctrl) parts.push(mac ? "⌃" : "Ctrl");
  if (mods.alt) parts.push(mac ? "⌥" : "Alt");
  if (mods.shift) parts.push(mac ? "⇧" : "Shift");
  if (mods.mod) parts.push(mac ? "⌘" : "Ctrl");
  if (mods.meta) parts.push(mac ? "⌘" : "Meta");
  parts.push(keyLabel(key));
  return parts.join(mac ? "" : "+");
}

// Read the host platform, preferring the modern userAgentData over the deprecated
// (but universally present) navigator.platform.
export function hostPlatform(): string {
  const uaData = (navigator as Navigator & { userAgentData?: { platform?: string } }).userAgentData;
  return uaData?.platform ?? navigator.platform ?? "";
}
