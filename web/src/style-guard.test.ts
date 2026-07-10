import { describe, it, expect } from "vitest";

// Guard: every button uses the semantic intent vocabulary (btn-action / btn-quiet
// / btn-danger / btn-warn / btn-ok, defined in app.css), never the raw daisyUI
// color/emphasis classes, so button styling stays unified and a future theme
// restyles them from one place. See #92. Structural classes (btn, btn-sm, btn-xs,
// btn-square) are fine; only the raw intent classes are banned.
const files = import.meta.glob("./**/*.tsx", { query: "?raw", import: "default", eager: true }) as Record<string, string>;

// Every raw daisyUI color/emphasis button class is banned: a button carries an
// intent class (btn-action / btn-quiet / btn-danger / btn-warn / btn-ok) so the
// vocabulary is unified and both themes restyle from one place. A raw color class
// (btn-error, btn-soft, ...) is exactly what renders inconsistently across themes.
const BANNED = [
  "btn-primary", "btn-secondary", "btn-accent", "btn-info", "btn-success",
  "btn-warning", "btn-error", "btn-neutral", "btn-ghost", "btn-outline", "btn-soft",
];

describe("button vocabulary", () => {
  it("uses semantic intent classes, not raw daisyUI button colors", () => {
    const offenders: string[] = [];
    for (const [path, src] of Object.entries(files)) {
      if (path.includes(".test.")) continue; // tests may render arbitrary markup
      for (const m of src.matchAll(/class(?:List)?=(?:"([^"]*)"|\{\{([^}]*)\}\})/g)) {
        const s = m[1] ?? m[2] ?? "";
        if (!s.includes("btn")) continue;
        for (const b of BANNED) {
          if (new RegExp(`\\b${b}\\b`).test(s)) offenders.push(`${path}: "${b}" in \`${s.trim().slice(0, 70)}\``);
        }
      }
    }
    expect(offenders, `\n${offenders.join("\n")}\n`).toEqual([]);
  });
});
