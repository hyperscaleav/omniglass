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

// Read the host platform, preferring the modern userAgentData over the deprecated
// (but universally present) navigator.platform.
export function hostPlatform(): string {
  const uaData = (navigator as Navigator & { userAgentData?: { platform?: string } }).userAgentData;
  return uaData?.platform ?? navigator.platform ?? "";
}
