import { describe, it, expect } from "vitest";

// Guard (#474): a test fixture that puts the kebab handle in a row's `id` slot
// re-creates the pre-ADR-0062 world, where the id WAS the handle, and lets a
// real uuid-vs-name join break ship green. That is exactly how the #432
// console regressions shipped: the icon map, the allowed-parent filter, and
// the secret_type picker all joined correctly against handle-shaped fixture
// ids and never in production. Registry and entity fixtures mint their ids
// with uuidFor (lib/testids) so tests see the same two-identity shape the
// server serves: a uuid `id` beside a kebab `name`.
//
// Scope, stated rather than silently weakened (the issue's instruction): the
// scan is line-scoped and keys on an object literal carrying BOTH an `id:`
// string literal AND a `name:` on the same line, because id-plus-name is the
// uuid-plus-handle contract. Shapes with no `name` slot (audit events,
// principal stubs, blade ids) are out of scope, as are multi-line literals;
// every registry fixture in the tree today is single-line, and a new offender
// pattern is a cue to widen this scan, not to trust it blindly. The precedent
// for a blunt-but-cheap source scan is style-guard.test.ts.
const files = import.meta.glob("./**/*.test.{ts,tsx}", { query: "?raw", import: "default", eager: true }) as Record<string, string>;

const UUID_LITERAL = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

describe("registry fixture ids", () => {
  it("mints uuid-shaped ids (uuidFor) on every fixture row that carries a name", () => {
    const offenders: string[] = [];
    for (const [path, src] of Object.entries(files)) {
      if (path.includes("registry-fixture-guard")) continue; // this file's own examples
      src.split("\n").forEach((line, i) => {
        // An object literal declaring both identities on one line, with a
        // string-literal id. `id: uuidFor(...)` does not match and passes.
        const m = line.match(/[{,]\s*id:\s*"([^"]*)"/);
        if (!m) return;
        if (!/[{,]\s*name:\s*/.test(line)) return;
        if (UUID_LITERAL.test(m[1])) return;
        offenders.push(`${path}:${i + 1}: id: "${m[1]}" beside a name; mint it with uuidFor("...") from lib/testids`);
      });
    }
    expect(offenders, `\n${offenders.join("\n")}\n`).toEqual([]);
  });
});
