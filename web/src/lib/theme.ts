import { createSignal } from "solid-js";

// Theme is the only user-facing display preference (dark / light). The
// prototype's Tweaks panel (type system, density) was a Claude Design
// design-axis switcher, not a product feature: those are fixed defaults
// (mixed / comfortable) set on <html> in index.html. A module-level signal
// makes the theme a shell-wide singleton; App owns the effect that mirrors it.
export type ThemeMode = "dark" | "light";

const KEY = "og-theme";

function load(): ThemeMode {
  return localStorage.getItem(KEY) === "light" ? "light" : "dark";
}

const [theme, setSig] = createSignal<ThemeMode>(load());

export const useTheme = () => theme;

export function setTheme(mode: ThemeMode): void {
  localStorage.setItem(KEY, mode);
  setSig(mode);
}

export function toggleTheme(): void {
  setTheme(theme() === "dark" ? "light" : "dark");
}

// applyTheme mirrors the mode onto <html data-theme> (the daisyUI themes are
// omniglass-dark / omniglass-light). theme-switching suppresses transitions
// during the swap so var-driven colors apply instantly.
export function applyTheme(mode: ThemeMode): void {
  const r = document.documentElement;
  r.classList.add("theme-switching");
  r.dataset.theme = `omniglass-${mode}`;
  requestAnimationFrame(() => requestAnimationFrame(() => r.classList.remove("theme-switching")));
}
