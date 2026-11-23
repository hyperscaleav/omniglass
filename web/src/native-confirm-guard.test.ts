import { describe, it, expect } from "vitest";
import { readdirSync, readFileSync, statSync } from "node:fs";
import { join, relative } from "node:path";

// Every destructive confirm in the console goes through the one ConfirmDialog
// (lib/confirm, #2463). The browser's own confirm() names no blocker, cannot
// carry the verb on its button, and blocks the page; this guard keeps it out
// of web/src. A bare confirm( call is the same native prompt, so it is barred
// too: a local function or signal that would read as one takes another name.

const SRC = join(__dirname);
const NATIVE = /window\.confirm\b|(?<![\w.$])confirm\s*\(/;

function sources(dir: string): string[] {
  return readdirSync(dir).flatMap((f) => {
    const p = join(dir, f);
    if (statSync(p).isDirectory()) return sources(p);
    return /\.(ts|tsx)$/.test(f) && !/\.test\.tsx?$/.test(f) ? [p] : [];
  });
}

describe("no native confirm", () => {
  it("no source under web/src calls window.confirm or a bare confirm(", () => {
    const hits: string[] = [];
    for (const file of sources(SRC)) {
      readFileSync(file, "utf8")
        .split("\n")
        .forEach((line, i) => {
          if (/^\s*\/\//.test(line)) return;
          if (NATIVE.test(line)) hits.push(`${relative(SRC, file)}:${i + 1}: ${line.trim()}`);
        });
    }
    expect(hits).toEqual([]);
  });

  it("the pattern catches both forms and spares a method of the same name", () => {
    expect(NATIVE.test('if (!window.confirm("x")) return;')).toBe(true);
    expect(NATIVE.test('if (!confirm(`Delete "${n}"?`)) return;')).toBe(true);
    expect(NATIVE.test("copy().confirm(name)")).toBe(false);
    expect(NATIVE.test("await confirmDelete({ kind })")).toBe(false);
  });
});
