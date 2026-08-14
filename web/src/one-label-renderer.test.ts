import { readdirSync, readFileSync, statSync } from "node:fs";
import { join, relative, sep } from "node:path";
import { describe, expect, it } from "vitest";

// One place renders a label (#683).
//
// `entityLabel` has been the single renderer since #581, and the console went on
// hand-rolling `display_name || name` beside it anyway, in three dialects:
//
//   c.display_name || c.name              a label render
//   find(...)?.display_name ?? handle     a lookup that falls back to the handle
//   `${r.name} ${r.display_name}`         a filter facet's search haystack
//   n.display === (n.addr ?? n.id)        the same rule one step downstream
//
// The third one is why this guard exists rather than a rendering test. A facet
// interpolating an ABSENT display_name searched the literal text "undefined", so
// typing "undefined" matched every unlabelled row and typing a label matched
// nothing on the pages that spelled it `${r.name} ${r.display_name}`. No page
// test catches that, because the page nobody wrote a test for is the page that
// drifts, and a rule written out seventy times drifts in seventy places.
//
// It matters more now than it did: #682 made a label something a RULE renders,
// so a second renderer no longer merely looks different, it disagrees about who
// owns the words. That is what the pen (`display_name_generated`) records and
// what only lib/entities reads.
const SRC = join(__dirname);

// The label fallback, written out by hand: a display_name read whose fallback is
// something other than the empty string.
//
// `?? ""` is deliberately allowed. Binding the raw column into a form input is
// not a label render, it is the edit surface for the column itself, and it must
// stay raw: an input seeded with entityLabel would silently rewrite a blank
// label into the name on the next Save.
const FALLBACK = /display_name(?:\?\.trim\(\))?\s*(?:\|\||\?\?)\s*(.*)$/;
// The raw column interpolated into a string, which is how the filter facets
// built their search haystacks (and how they came to search "undefined").
const INTERPOLATED = /\$\{[^}]*\bdisplay_name\b[^}]*\}/;
// The same rule spelled downstream of the column, on a row whose label is already
// resolved: `n.display === (n.addr ?? n.id)`. TreeList carried this for the whole
// estate and the two regexes above could not see it, because it never mentions
// `display_name` at all. It is the copy that mattered most, so it gets its own
// pattern rather than trusting the next reader to notice.
const RESOLVED = /\bdisplay\s*(?:===|!==)\s*\(?\w+\.(?:addr|id)\b/;

function handRolled(line: string): boolean {
  for (let rest = line; ; ) {
    const m = FALLBACK.exec(rest);
    if (!m) return false;
    if (!m[1].startsWith('""')) return true;
    rest = m[1];
  }
}

// The exceptions, each one a rule that is genuinely not the entity label's.
//
// Keyed on the LINE and not just the file: a file-level exemption would let the
// next hand-rolled fallback hide behind a neighbour that had a good reason, which
// is how a sweep ends up half done. lib/principals.ts is exactly that case, since
// its role facet goes through entityLabel and the function beneath it does not.
const ALLOWED: { file: string; match: string; why: string }[] = [
  {
    file: "lib/entities.ts",
    match: "e.display_name?.trim() || e.name",
    why: "the primitive itself: the one place the rule is written",
  },
  {
    file: "lib/groups.ts",
    match: "m.name || m.display_name || m.principal_id",
    why:
      "memberName is a PRINCIPAL's name, not an entity's: its precedence is identifier-first " +
      "(a service account has no display name at all), the other way round from the entity " +
      "rule, which entityLabel does not express and must not learn",
  },
  {
    file: "lib/principals.ts",
    match: "p.human?.display_name || p.human?.username",
    why: "principalName, the same principal rule over the nested human/service shape",
  },
  {
    file: "components/ContractEditor.tsx",
    match: "p.display_name ? `${p.name} (${p.display_name})` : p.name",
    why:
      "renders BOTH facts as `name (Label)` to disambiguate a property picker, so it is a " +
      "compound, not a fallback: there is no single label for entityLabel to choose",
  },
];

function walk(dir: string, out: string[] = []): string[] {
  for (const entry of readdirSync(dir)) {
    const full = join(dir, entry);
    if (statSync(full).isDirectory()) walk(full, out);
    else if (/\.tsx?$/.test(entry) && !/\.test\.tsx?$/.test(entry) && !/\.gen\.ts$/.test(entry)) out.push(full);
  }
  return out;
}

// A line is prose if it is a comment. This guard is about what the console RUNS,
// and a comment naming the shape it replaced is the history, not a second
// renderer (lib/entities.ts's own header does exactly that).
function isComment(line: string): boolean {
  const t = line.trim();
  return t.startsWith("//") || t.startsWith("*") || t.startsWith("/*") || t.startsWith("{/*");
}

type Hit = { file: string; line: number; text: string };

function hits(): Hit[] {
  const out: Hit[] = [];
  for (const full of walk(SRC)) {
    const file = relative(SRC, full).split(sep).join("/");
    readFileSync(full, "utf8")
      .split("\n")
      .forEach((line, i) => {
        if (isComment(line)) return;
        if (handRolled(line) || INTERPOLATED.test(line) || RESOLVED.test(line)) out.push({ file, line: i + 1, text: line.trim() });
      });
  }
  return out;
}

const isAllowed = (h: Hit) => ALLOWED.some((a) => a.file === h.file && h.text.includes(a.match));

describe("one label renderer", () => {
  it("has no hand-rolled display-name fallback outside lib/entities", () => {
    const stray = hits().filter((h) => !isAllowed(h));
    expect(stray.map((h) => `${h.file}:${h.line}  ${h.text}`)).toEqual([]);
  });

  // The other direction. An allowlist entry that no longer describes anything is
  // permission nobody is using, and the next hand-rolled fallback on that line
  // would inherit it.
  it.each(ALLOWED)("keeps $file on the list for a reason that is still true", ({ file, match }) => {
    expect(hits().some((h) => h.file === file && h.text.includes(match))).toBe(true);
  });
});
