import { describe, it, expect } from "vitest";

// Guard: every button uses the semantic intent vocabulary (btn-action / btn-quiet
// / btn-danger / btn-warn / btn-ok, defined in app.css), never the raw daisyUI
// color/emphasis classes, so button styling stays unified and a future theme
// restyles them from one place. See #92. Structural classes (btn, btn-sm, btn-xs,
// btn-square) are fine; only the raw intent classes are banned.
const files = import.meta.glob("./**/*.tsx", { query: "?raw", import: "default", eager: true }) as Record<string, string>;

const BANNED = ["btn-primary", "btn-ghost"];

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
