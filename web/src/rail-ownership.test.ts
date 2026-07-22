import { describe, it, expect } from "vitest";
import { readdirSync, readFileSync, statSync } from "node:fs";
import { dirname, join, relative } from "node:path";
import { fileURLToPath } from "node:url";

// The action rail belongs to the shell. BladeStack draws the blade's, Drawer draws
// the Drawer's, and both draw it through PanelFooter, so a form body writes no
// footer markup at all and cannot get one wrong.
//
// It was not always so. The Drawer's rail used to be an opt-in helper (DrawerFooter)
// that each body wrapped its own buttons in, which meant a body could simply forget
// it. Two did, and stayed wrong through nine merged PRs while the helper was copied
// into six new pages around them. String bans are a blunt instrument, but they hold
// this line cheaply until every form factor is behind the CRUD form primitive.

const here = dirname(fileURLToPath(import.meta.url));

function sourceFiles(dir: string, out: string[] = []): string[] {
  for (const entry of readdirSync(dir)) {
    const full = join(dir, entry);
    if (statSync(full).isDirectory()) sourceFiles(full, out);
    else if (/\.tsx?$/.test(entry) && !/\.test\.tsx?$/.test(entry)) out.push(full);
  }
  return out;
}

const files = sourceFiles(here);
const rel = (f: string) => relative(here, f);

describe("action rail ownership", () => {
  it("no source reintroduces the DrawerFooter helper", () => {
    const offenders = files.filter((f) => readFileSync(f, "utf8").includes("DrawerFooter")).map(rel);
    expect(offenders).toEqual([]);
  });

  // The pinned rail's own markup. Only PanelFooter may draw it; anything else is a
  // second rail that will drift from the first.
  it("only PanelFooter draws the pinned rail", () => {
    const rail = "border-t border-base-300 bg-base-100 px-5 py-3";
    const offenders = files.filter((f) => readFileSync(f, "utf8").includes(rail)).map(rel);
    expect(offenders).toEqual(["components/PanelFooter.tsx"]);
  });

  // The exact shape both offenders hand-rolled. A form body that lays out its own
  // right-aligned button row is drawing a rail the shell already owns.
  it("no panel form body hand-rolls a right-aligned button row", () => {
    const handRolled = /class="[^"]*\bmt-1 flex justify-end gap-2\b[^"]*"/;
    const offenders = files.filter((f) => handRolled.test(readFileSync(f, "utf8"))).map(rel);
    expect(offenders).toEqual([]);
  });
});
