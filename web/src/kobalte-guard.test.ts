import { describe, it, expect } from "vitest";
import { readFileSync, readdirSync, statSync } from "node:fs";
import { join, relative, sep } from "node:path";

// The console's interactive primitives are in-house wrappers over Kobalte
// (#2485): only components/ui/ imports @kobalte/core, so the library under
// them stays swappable in one directory and every surface gets the same
// keyboard, focus and portal behavior from the same few files. A page or
// component that needs a dialog, popover, tooltip, select, picker, tabs,
// toast, collapsible, switch or segmented control imports the wrapper; one
// that needs a primitive with no wrapper yet adds the wrapper first
// (design-system.md, "Interactive primitives are wrappers").
//
// A static scan over the source, tests included (a test that renders a raw
// Kobalte part tests the library, not the console), naming each file and line.
const SRC = __dirname;
const WRAPPERS = `components${sep}ui${sep}`;

function sources(dir: string): string[] {
  return readdirSync(dir).flatMap((f) => {
    const p = join(dir, f);
    if (statSync(p).isDirectory()) return f === "node_modules" ? [] : sources(p);
    return /\.(tsx?|mts|mjs)$/.test(f) ? [p] : [];
  });
}

// An import or a dynamic import of the package or any of its subpaths. Prose
// naming the package (this comment) is not an import.
const IMPORT = /(?:\bfrom\s*|\bimport\s*\(?\s*)["']@kobalte\/core(?:\/[^"']*)?["']/g;

export function kobalteImports(file: string, text: string): string[] {
  return [...text.matchAll(IMPORT)].map((m) => `${file}:${text.slice(0, m.index).split("\n").length}: imports @kobalte/core outside components/ui/`);
}

describe("@kobalte/core imports", () => {
  it("finds static, subpath, side-effect and dynamic imports, not prose", () => {
    expect(kobalteImports("x", `import { Dialog } from "@kobalte/core/dialog";`)).toHaveLength(1);
    expect(kobalteImports("x", `import { Tooltip } from '@kobalte/core';`)).toHaveLength(1);
    expect(kobalteImports("x", `import "@kobalte/core/toast";`)).toHaveLength(1);
    expect(kobalteImports("x", `const m = await import("@kobalte/core/tabs");`)).toHaveLength(1);
    expect(kobalteImports("x", `// wraps @kobalte/core, see the ui directory`)).toEqual([]);
  });

  it("happen only inside components/ui/", () => {
    const hits = sources(SRC)
      .map((p) => [relative(SRC, p), p] as const)
      // This file's own fixtures are strings, not imports.
      .filter(([rel]) => !rel.startsWith(WRAPPERS) && rel !== "kobalte-guard.test.ts")
      .flatMap(([rel, p]) => kobalteImports(rel, readFileSync(p, "utf8")));
    expect(hits).toEqual([]);
  });
});
