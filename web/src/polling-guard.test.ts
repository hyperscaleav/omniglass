import { describe, it, expect } from "vitest";
import { readdirSync, readFileSync, statSync } from "node:fs";
import { join, relative } from "node:path";

// The console follows state over its one live stream (#2731, ADR-2731): a
// read refetches when the server says what it shows changed (liveQuery in
// lib/live.ts), never on a timer. This guard bars polling from web/src: a
// refetchInterval, a setInterval, a second stream (new EventSource), and a
// setTimeout that re-schedules a read. Two files may keep a timer: lib/clock.ts
// (the UI's tick, which moves ages, not reads) and lib/live.ts (the hub's
// reconnect and its fallback refresh).
//
// polling-allowlist.json names the files that still poll while their pages
// move over. It only shrinks: a file not on it that polls fails, and a file on
// it that no longer polls fails until it is taken off.

const SRC = join(__dirname);
const ALLOWED = new Set(["lib/clock.ts", "lib/live.ts"]);
const STILL_POLLING: string[] = JSON.parse(readFileSync(join(SRC, "polling-allowlist.json"), "utf8"));

// The line patterns: each a way to read on a timer.
const LINE_PATTERNS: [string, RegExp][] = [
  ["refetchInterval", /\brefetchInterval\b/],
  ["setInterval", /(?<![\w.$])(?:window\.|globalThis\.)?setInterval\s*\(/],
  ["EventSource", /\bnew\s+EventSource\b/],
  // A timeout whose callback reads: it refetches, invalidates, polls, or
  // reloads.
  ["setTimeout that reads", /\bsetTimeout\s*\(\s*(?:async\s*)?(?:\(\s*\)\s*=>\s*)?\{?\s*(?:void\s+)?(?:await\s+)?[\w.?]*?\b\w*(?:refetch|invalidateQueries|poll|reload)\w*/i],
];

// selfScheduling finds a function that sets a timeout to call itself: a
// timer loop, however it is spelled.
function selfScheduling(src: string): string[] {
  const hits: string[] = [];
  const def = /(?:function\s+([A-Za-z_$][\w$]*)\s*\([^)]*\)[^{]*\{|(?:const|let)\s+([A-Za-z_$][\w$]*)\s*=\s*(?:async\s*)?(?:function\s*)?\([^)]*\)\s*(?::[^=]+)?=?>?\s*\{)/g;
  for (const m of src.matchAll(def)) {
    const name = m[1] ?? m[2];
    const open = (m.index ?? 0) + m[0].length - 1;
    let depth = 0;
    let end = src.length;
    for (let i = open; i < src.length; i++) {
      if (src[i] === "{") depth++;
      else if (src[i] === "}" && --depth === 0) {
        end = i;
        break;
      }
    }
    const body = src.slice(open, end);
    const again = new RegExp(`\\bsetTimeout\\s*\\(\\s*(?:(?:async\\s*)?\\(\\s*\\)\\s*=>\\s*\\{?\\s*(?:void\\s+)?(?:await\\s+)?)?${name.replace(/\$/g, "\\$")}\\b`);
    if (again.test(body)) hits.push(name);
  }
  return hits;
}

// polls lists how one source reads on a timer.
function polls(src: string): string[] {
  const code = src
    .split("\n")
    .map((line) => (/^\s*\/\//.test(line) ? "" : line))
    .join("\n");
  const found = LINE_PATTERNS.filter(([, re]) => code.split("\n").some((line) => re.test(line))).map(([what]) => what);
  return [...found, ...selfScheduling(code).map((n) => `${n} re-schedules itself`)];
}

function sources(dir: string): string[] {
  return readdirSync(dir).flatMap((f) => {
    const p = join(dir, f);
    if (statSync(p).isDirectory()) return sources(p);
    return /\.(ts|tsx)$/.test(f) && !/\.test\.tsx?$/.test(f) && !/\.gen\.ts$/.test(f) && f !== "test-timers.ts" ? [p] : [];
  });
}

const polling = () =>
  sources(SRC)
    .map((file) => ({ file: relative(SRC, file).split("\\").join("/"), how: polls(readFileSync(file, "utf8")) }))
    .filter((p) => p.how.length > 0 && !ALLOWED.has(p.file));

describe("no polling", () => {
  it("no source outside the allowlist reads on a timer", () => {
    const listed = new Set(STILL_POLLING);
    expect(polling().filter((p) => !listed.has(p.file)).map((p) => `${p.file}: ${p.how.join(", ")}`)).toEqual([]);
  });

  it("the allowlist names only files that still poll, so it only shrinks", () => {
    const still = new Set(polling().map((p) => p.file));
    expect(STILL_POLLING.filter((f) => !still.has(f))).toEqual([]);
    expect([...STILL_POLLING].sort()).toEqual(STILL_POLLING);
  });

  it("the patterns catch each way to poll and spare what is not one", () => {
    expect(polls("useQuery(() => ({ queryKey: k, queryFn: f, refetchInterval: 5000 }))")).toEqual(["refetchInterval"]);
    expect(polls("const id = setInterval(() => tick(), 1000);")).toEqual(["setInterval"]);
    expect(polls("const id = window.setInterval(load, 1000);")).toEqual(["setInterval"]);
    expect(polls('const es = new EventSource("/x");')).toEqual(["EventSource"]);
    expect(polls("setTimeout(() => void qc.invalidateQueries({ queryKey }), 5000);")).toEqual(["setTimeout that reads"]);
    expect(polls("setTimeout(() => q.refetch(), 1000);")).toEqual(["setTimeout that reads"]);
    expect(polls("function load() {\n  fetchIt();\n  setTimeout(load, 5000);\n}")).toEqual(["load re-schedules itself"]);
    expect(polls("const tick = async () => {\n  await read();\n  timer = setTimeout(() => void tick(), 5000);\n};")).toEqual(["tick re-schedules itself"]);
    // Not polling: a debounce, a blink, a clearInterval, a comment, a name
    // that only contains the word.
    expect(polls("timer = setTimeout(async () => { const r = await validate(src); }, 300);")).toEqual([]);
    expect(polls("setTimeout(() => setCopied(false), 2000);")).toEqual([]);
    expect(polls("onCleanup(() => clearInterval(id));")).toEqual([]);
    expect(polls("// refetchInterval: 5000 used to be here")).toEqual([]);
    expect(polls("const commit = () => setFilter(v);\nsetTimer(setTimeout(commit, 250));")).toEqual([]);
    expect(polls("const mySetInterval = 3;")).toEqual([]);
  });
});
