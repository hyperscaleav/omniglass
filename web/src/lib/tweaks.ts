import { createSignal } from "solid-js";

// The console's display tweaks (from the design's Tweaks panel): theme, type
// system, and density. Persisted to localStorage and applied to the <html>
// data-attributes that theme.css keys on. A module-level signal makes it a
// shell-wide singleton; App owns the effect that mirrors it to the DOM.
export type Tweaks = {
  theme: "dark" | "light";
  type: "mixed" | "mono";
  density: "comfortable" | "compact";
};

const KEY = "og-tweaks";
const DEFAULTS: Tweaks = { theme: "dark", type: "mixed", density: "comfortable" };

function load(): Tweaks {
  try {
    return { ...DEFAULTS, ...(JSON.parse(localStorage.getItem(KEY) ?? "{}") as Partial<Tweaks>) };
  } catch {
    return DEFAULTS;
  }
}

const [tweaks, setSig] = createSignal<Tweaks>(load());

export const useTweaks = () => tweaks;

export function setTweak<K extends keyof Tweaks>(key: K, value: Tweaks[K]): void {
  setSig((prev) => {
    const next = { ...prev, [key]: value };
    localStorage.setItem(KEY, JSON.stringify(next));
    return next;
  });
}

// applyTweaks mirrors the tweak state onto <html>; theme.css selectors do the
// rest. The theme-switching class suppresses transitions during the swap so
// var-driven colors apply instantly.
export function applyTweaks(t: Tweaks): void {
  const r = document.documentElement;
  r.classList.add("theme-switching");
  // The daisyUI themes are named omniglass-dark / omniglass-light; the tweak
  // stores just the mode.
  r.dataset.theme = `omniglass-${t.theme}`;
  r.dataset.type = t.type;
  r.dataset.density = t.density;
  requestAnimationFrame(() => requestAnimationFrame(() => r.classList.remove("theme-switching")));
}
