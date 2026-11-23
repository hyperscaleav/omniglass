import { describe, it, expect } from "vitest";
import { readdirSync, readFileSync, statSync } from "node:fs";
import { join } from "node:path";

// Nothing in the console says "soon" (#2445): the product shows no unfinished
// marker. A page that is not built has no door; an unknown address lands on the
// not-found face. This scans every non-test source under web/src, with comments
// stripped (prose in a comment is not rendered), and fails on a "soon" word
// (word boundary, case-insensitive) or on the older "not built yet" /
// "not wired yet" placeholders, outside the allowlist below.
//
// Allowlist (each entry is file + the exact line, so a new marker in the same
// file still fails):
//  - lib/platformhealth.ts NOT_WIRED "not wired yet": Platform health's Queue
//    depth and Busy workers figures. Owned by #2446, which replaces them.
//  - lib/tape.ts / lib/binding.ts style prose "as soon as" is a comment and is
//    stripped; no code-line prose is allowlisted.
const ALLOW: { file: string; line: string; why: string }[] = [
  { file: "lib/platformhealth.ts", line: 'export const NOT_WIRED = "not wired yet";', why: "#2446 owns Platform health's unwired figures" },
];

const PATTERN = /\bsoon\b|not (?:built|wired) yet/i;
const SRC = join(__dirname);

function walk(dir: string, out: string[] = []): string[] {
  for (const name of readdirSync(dir)) {
    const p = join(dir, name);
    if (statSync(p).isDirectory()) walk(p, out);
    else if (/\.(ts|tsx)$/.test(name) && !/\.test\.(ts|tsx)$/.test(name) && !name.endsWith(".gen.ts")) out.push(p);
  }
  return out;
}

function stripComments(src: string): string {
  return src
    .replace(/\/\*[\s\S]*?\*\//g, (m) => m.replace(/[^\n]/g, " "))
    .split("\n")
    .map((l) => l.replace(/(^|\s)\/\/.*$/, "$1"))
    .join("\n");
}

describe("no rendered soon marker", () => {
  it("finds none under web/src outside the allowlist", () => {
    const hits: string[] = [];
    for (const f of walk(SRC)) {
      const rel = f.slice(SRC.length + 1);
      stripComments(readFileSync(f, "utf8")).split("\n").forEach((line, i) => {
        if (!PATTERN.test(line)) return;
        if (ALLOW.some((a) => a.file === rel && a.line === line.trim())) return;
        hits.push(`${rel}:${i + 1}: ${line.trim()}`);
      });
    }
    expect(hits).toEqual([]);
  });
});
