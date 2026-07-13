import { describe, it, expect } from "vitest";
import { readdirSync, readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

// Ban: `badge-neutral` is forbidden anywhere in the SPA source. daisyUI's neutral
// token is a near-black navy in BOTH themes, so `badge-soft badge-neutral` paints
// dark text on a dark tint and is illegible (the codec-type-badge bug). The app's
// one legible neutral/type chip is `badge-ghost` (overridden in app.css); every
// type/neutral chip uses it. This test fails if any file reintroduces the class,
// so the regression can never come back silently. The `<TypeBadge>` primitive
// (follow-up) will make this structural; until then, the string ban holds the line.
const SRC = dirname(fileURLToPath(import.meta.url));

function walk(dir: string): string[] {
  const out: string[] = [];
  for (const entry of readdirSync(dir, { withFileTypes: true })) {
    const full = join(dir, entry.name);
    if (entry.isDirectory()) {
      out.push(...walk(full));
    } else if (/\.tsx?$/.test(entry.name) && !/\.test\.tsx?$/.test(entry.name)) {
      out.push(full);
    }
  }
  return out;
}

describe("badge theming ban", () => {
  it("never uses the illegible badge-neutral class in SPA source", () => {
    const offenders = walk(SRC).filter((f) => readFileSync(f, "utf8").includes("badge-neutral"));
    expect(offenders.map((f) => f.slice(SRC.length + 1))).toEqual([]);
  });
});
