import { createSignal } from "solid-js";

// The console ships DARK-ONLY for now. The light theme (omniglass-light) is still
// defined in app.css but is not reachable: the top-bar toggle was removed while the
// design tokens settle. Maintaining a second theme meant every teal element needed
// per-context tuning (a brand teal reads well as a fill but not as text on white),
// which was churn without enough payoff at this stage. Re-enable later by restoring a
// persisted setTheme + the toggle; kept as a signal so App's effect wires up unchanged.
export type ThemeMode = "dark" | "light";

const [theme] = createSignal<ThemeMode>("dark");

export const useTheme = () => theme;

// applyTheme mirrors the mode onto <html data-theme> (the daisyUI theme name is
// omniglass-<mode>). Dark-only today, so this runs once with "dark".
export function applyTheme(mode: ThemeMode): void {
  document.documentElement.dataset.theme = `omniglass-${mode}`;
}

// themeFromMe extracts the daisyUI mode from a /settings/me payload. The stored
// value is the full theme name (omniglass-dark|omniglass-light); the mode is the
// suffix. Falls back to dark when unset. The top-bar toggle is not restored, but
// the effective value now comes from the settings engine, so an admin setting the
// org default to omniglass-light re-themes the SPA on the next load.
export function themeFromMe(me: { values?: Record<string, Record<string, unknown>> }): ThemeMode {
  const name = me?.values?.ui?.theme;
  if (name === "omniglass-light") return "light";
  return "dark";
}
