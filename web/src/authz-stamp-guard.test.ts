import { readdirSync, readFileSync, statSync } from "node:fs";
import { join } from "node:path";
import { describe, expect, it } from "vitest";

// The console's gate strings match the API's stamps (#592).
//
// Authorization is one vocabulary spoken in two places. The API stamps every
// gated route with its resource through gated(...) ("metric_type", "read"), and
// the console hides affordances with the same words (can(me, "metric_type",
// "create")). Nothing ties the two strings together at build time: a page that
// gates on a word the API never stamps still typechecks, still renders, and
// still passes every rendering test that mounts it as an admin, because a
// wildcard principal (">" or "*") matches ANY resource word, wrong ones
// included.
//
// That is exactly how the Properties page shipped gating on "property" while
// the API stamps "property_type": invisible under a wildcard, wrong for every
// role-scoped principal, whose real permission (property_type:create) matched
// the server and never the console. The console hid buttons the server would
// have honored.
//
// So this is a source guard in the identity-vocabulary-guard mold: it reads the
// console's gate strings out of the page sources, reads the API's stamps out of
// the Go sources (the registration files are fixtures here, read the way the
// vocabulary guard reads .tsx), and asserts every word the console gates on is
// a word the API stamps. It cannot catch a gate on the WRONG stamped resource
// (a page gating files on "secret" passes), only a gate on a word that exists
// nowhere server-side, which is the drift renames actually produce.
const SRC = join(__dirname);
const API = join(__dirname, "..", "..", "internal", "api");

function walk(dir: string, out: string[] = []): string[] {
  for (const entry of readdirSync(dir)) {
    const full = join(dir, entry);
    if (statSync(full).isDirectory()) walk(full, out);
    else if (/\.tsx?$/.test(entry) && !/\.test\.tsx?$/.test(entry)) out.push(full);
  }
  return out;
}

// --- the console side: every literal resource the pages gate on -------------

type Gate = { at: string; resource: string };

// A can(me, "resource", ...) or canAtPlatform(me, "resource", ...) call whose
// resource is a string literal. A dynamic resource (can(me, owner().kind, ...))
// is a join, not a vocabulary site, and is out of scope on purpose.
function pageGates(): Gate[] {
  const out: Gate[] = [];
  const call = /\bcan(?:AtPlatform)?\(\s*[^,"'()]+,\s*"([a-z_]+)"/g;
  for (const file of walk(join(SRC, "pages"))) {
    readFileSync(file, "utf8")
      .split("\n")
      .forEach((line, i) => {
        for (const m of line.matchAll(call)) {
          out.push({ at: `pages/${file.replace(join(SRC, "pages") + "/", "")}:${i + 1}`, resource: m[1] });
        }
      });
  }
  return out;
}

// The nav config gates the same way with different spelling: a `resource:` field
// becomes <resource>:read, and a `perm:` field carries the topic whole. The
// sidebar and the route guard both read it, so a wrong word here hides a tab
// from exactly the principals the server would have let in.
function navGates(): Gate[] {
  const out: Gate[] = [];
  readFileSync(join(SRC, "lib", "nav.ts"), "utf8")
    .split("\n")
    .forEach((line, i) => {
      const res = /\bresource:\s*"([a-z_]+)"/.exec(line);
      if (res) out.push({ at: `lib/nav.ts:${i + 1}`, resource: res[1] });
      const perm = /\bperm:\s*"([a-z_]+):/.exec(line);
      if (perm) out.push({ at: `lib/nav.ts:${i + 1}`, resource: perm[1] });
    });
  return out;
}

// --- the API side: every resource gated(...) stamps -------------------------

// Each `.gated(huma.Operation{...}, "resource", "action"[, "tier"])` span in a
// Go registration file, parens balanced by hand exactly like the vocabulary
// guard balances identityColumn's. The operation literal is stepped over by
// balancing its braces, so a "metric_type:read" inside a Description never
// masquerades as the token list; a span with no `{` is a mention in a comment,
// not a registration, and is skipped.
function gatedResources(src: string): string[] {
  const out: string[] = [];
  const mention = /\.gated\(/g;
  let m: RegExpExecArray | null;
  while ((m = mention.exec(src)) !== null) {
    let i = m.index + m[0].length;
    const start = i;
    let depth = 1;
    for (; i < src.length && depth > 0; i++) {
      if (src[i] === "(") depth++;
      else if (src[i] === ")") depth--;
    }
    const span = src.slice(start, i - 1);
    const brace = span.indexOf("{");
    if (brace === -1) continue;
    let j = brace;
    let braces = 0;
    for (; j < span.length; j++) {
      if (span[j] === "{") braces++;
      else if (span[j] === "}" && --braces === 0) break;
    }
    const tokens = [...span.slice(j + 1).matchAll(/"([a-z_]+)"/g)].map((t) => t[1]);
    if (tokens.length > 0) out.push(tokens[0]);
  }
  return out;
}

function apiStamps(): Set<string> {
  const stamps = new Set<string>();
  for (const entry of readdirSync(API)) {
    if (!entry.endsWith(".go") || entry.endsWith("_test.go")) continue;
    const src = readFileSync(join(API, entry), "utf8");
    for (const r of gatedResources(src)) stamps.add(r);
    // The platform tier is stamped by its own helper (platformGated publishes
    // x-omniglass-platform-permission = platform:<action>), so "platform" is a
    // word the API speaks even though no gated(...) call carries it.
    if (src.includes("x-omniglass-platform-permission")) stamps.add("platform");
  }
  return stamps;
}

describe("console gate strings match the API's stamps", () => {
  const stamps = apiStamps();
  const gates = [...pageGates(), ...navGates()];

  it("reads both vocabularies (the guard is not vacuous)", () => {
    // If either extraction silently breaks, the assertion below passes on
    // nothing. Anchor the two resources this slice split the catalog into, and
    // require the sweep to have found a realistic population on both sides.
    expect(stamps.has("property_type")).toBe(true);
    expect(stamps.has("metric_type")).toBe(true);
    expect(stamps.size).toBeGreaterThanOrEqual(10);
    expect(gates.length).toBeGreaterThanOrEqual(10);
  });

  it("gates only on resource words the API stamps", () => {
    const offenders = gates.filter((g) => !stamps.has(g.resource)).map((g) => `${g.at}  "${g.resource}"`);
    expect(
      offenders,
      `\nThese gate on a resource word no API route stamps:\n  ${offenders.join("\n  ")}\n\n` +
        `The server's word is the one gated(...) registers in internal/api (the\n` +
        `x-omniglass-permission stamp). A console gate on any other word is invisible\n` +
        `under a wildcard principal and wrong for every role-scoped one: the console\n` +
        `hides affordances the server would honor. Use the stamped resource.\n`,
    ).toEqual([]);
  });
});
