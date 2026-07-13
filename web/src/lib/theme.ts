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
