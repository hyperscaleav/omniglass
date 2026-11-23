import { describe, it, expect } from "vitest";
import { readFileSync, readdirSync, statSync } from "node:fs";
import { join, relative, sep } from "node:path";

// One date formatter (#2447): every date, time and locale-formatted number the
// console prints comes from lib/format.ts, so the audit trail, the file list, the
// alarm drawer and the change table read the same way and a locale or format
// change is made in one place. A page that calls toLocaleString,
// toLocaleDateString or toLocaleTimeString itself is how three formats appeared
// ("11/23/2026, 1:56:36 PM", "Nov 23, 01:56 PM", a raw ISO string).
//
// A static scan over the source, tests included, naming each file and line.
const SRC = __dirname;
const HOME = ["lib", "format.ts"].join(sep);
const HOME_TEST = ["lib", "format.test.ts"].join(sep);

function sources(dir: string): string[] {
  return readdirSync(dir).flatMap((f) => {
    const p = join(dir, f);
    if (statSync(p).isDirectory()) return f === "node_modules" ? [] : sources(p);
    return /\.(tsx?|mts|mjs)$/.test(f) ? [p] : [];
  });
}

// Number-only exemption: the simulation surfaces group hit counts with a bare
// toLocaleString and were rebuilt in #2510 while this guard landed, so moving them
// to fmtCount waits on that merge (#2536). A date or time call is forbidden there too.
const NUMBER_ONLY = ["components/SimComponentDrawer.tsx", "components/SimulationSection.tsx", "pages/Simulation.tsx", "lib/simulation.ts"];

const CALL = /\.toLocale(?:Date|Time)?String\s*\(/g;

export function localeCalls(file: string, text: string): string[] {
  const numberOnly = NUMBER_ONLY.includes(file.split(sep).join("/"));
  return [...text.matchAll(CALL)].filter((m) => !(numberOnly && m[0].startsWith(".toLocaleString"))).map((m) => `${file}:${text.slice(0, m.index).split("\n").length}: formats a date or number itself; use lib/format.ts`);
}

describe("locale formatting", () => {
  it("finds each of the three calls and not prose", () => {
    expect(localeCalls("x", `d.toLocaleString()`)).toHaveLength(1);
    expect(localeCalls("x", `d.toLocaleDateString("en-US", {})`)).toHaveLength(1);
    expect(localeCalls("x", `d.toLocaleTimeString([], {})`)).toHaveLength(1);
    expect(localeCalls("x", `// never call toLocaleString here`)).toHaveLength(0);
    expect(localeCalls("x", `fmtTime(iso)`)).toHaveLength(0);
    expect(localeCalls("pages/Simulation.tsx", `n.toLocaleString()`)).toHaveLength(0);
    expect(localeCalls("pages/Simulation.tsx", `d.toLocaleTimeString()`)).toHaveLength(1);
  });

  it("is used only by lib/format.ts", () => {
    const hits = sources(SRC)
      .filter((f) => ![HOME, HOME_TEST].includes(relative(SRC, f)) && !f.endsWith("date-guard.test.ts"))
      .flatMap((f) => localeCalls(relative(SRC, f), readFileSync(f, "utf8")));
    expect(hits, hits.join("\n")).toEqual([]);
  });
});
