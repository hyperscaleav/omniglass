import { readdirSync, readFileSync, statSync } from "node:fs";
import { join, relative, sep } from "node:path";
import { describe, expect, it } from "vitest";

// One place renders a label (#683).
//
// `entityLabel` has been the single renderer since #581, and the console went on
// hand-rolling `label || name` beside it anyway, in three dialects:
//
//   c.label || c.name              a label render
//   find(...)?.label ?? handle     a lookup that falls back to the handle
//   `${r.name} ${r.label}`         a filter facet's search haystack
//   n.display === (n.addr ?? n.id)        the same rule one step downstream
//
// The third one is why this guard exists rather than a rendering test. A facet
// interpolating an ABSENT label searched the literal text "undefined", so
// typing "undefined" matched every unlabelled row and typing a label matched
// nothing on the pages that spelled it `${r.name} ${r.label}`. No page
// test catches that, because the page nobody wrote a test for is the page that
// drifts, and a rule written out seventy times drifts in seventy places.
//
// It matters more now than it did: #682 made a label something a RULE renders,
// so a second renderer no longer merely looks different, it disagrees about who
// owns the words. That is what the pen (`label_generated`) records and
// what only lib/entities reads.
const SRC = join(__dirname);

// The label fallback, written out by hand: a label read whose fallback is
// something other than the empty string.
//
// `?? ""` is deliberately allowed. Binding the raw column into a form input is
// not a label render, it is the edit surface for the column itself, and it must
// stay raw: an input seeded with entityLabel would silently rewrite a blank
// label into the name on the next Save.
//
// `props.label` is excluded, and only that exact read. #613 renamed the column
// to `label`, which is also the name of the prop a Button, an InfoTip, an
// InlineActions row and a ProvenanceDot already took: a caption the caller
// always passes, not a row's optional friendly string, so `props.label ?? x` is
// a default and not the entity fallback. The exclusion is spelled `props.` and
// nothing wider: a component handed an entity reads `props.entity.label`, which
// still matches, and no entity row in this console is ever bound to the name
// `props`.
const FALLBACK = /(?<!\bprops\.)\blabel(?:\?\.trim\(\))?\s*(?:\|\||\?\?)\s*(.*)$/;
// The raw column interpolated into a string, which is how the filter facets
// built their search haystacks (and how they came to search "undefined").
//
// The read is anchored to an entity by requiring an IDENTIFIER read on the same
// line, because #613 renamed the column to `label` and the console already had a
// `label` of its own: the prop a Button, an InfoTip, a nav item and a facet chip
// all take, which is always a present string and never falls back to anything.
// A bare /\$\{.*\blabel\b.*\}/ matched eleven of those the day the column moved.
// The haystack bug this pattern exists for is `${r.name} ${r.label}`, an
// identifier and a maybe-absent label interpolated together, and that shape is
// what it now names. The narrower pattern is the price of the collision: a
// haystack that interpolated a label with no identifier beside it would slip
// past, where the FALLBACK pattern above still catches every `label || x`.
const IDENTIFIER = String.raw`\.(?:name|addr|username)\b`;
const INTERPOLATED = new RegExp(String.raw`(?=.*\$\{[^}]*\.label\b[^}]*\})(?=.*${IDENTIFIER})`);
// The same rule spelled downstream of the column, on a row whose label is already
// resolved: `n.display === (n.addr ?? n.id)`. TreeList carried this for the whole
// estate and the two regexes above could not see it, because it never mentions
// `label` at all. It is the copy that mattered most, so it gets its own
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
    match: "e.label?.trim() || e.name",
    why: "the primitive itself: the one place the rule is written",
  },
  {
    file: "lib/groups.ts",
    match: "m.name || m.label || m.principal_id",
    why:
      "memberName is a PRINCIPAL's name, not an entity's: its precedence is identifier-first " +
      "(a service account has no label at all), the other way round from the entity " +
      "rule, which entityLabel does not express and must not learn",
  },
  {
    file: "lib/principals.ts",
    match: "p.human?.label || p.human?.username",
    why: "principalName, the same principal rule over the nested human/service shape",
  },
  {
    file: "components/ContractEditor.tsx",
    match: "p.label ? `${p.name} (${p.label})` : p.name",
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
  it("has no hand-rolled label fallback outside lib/entities", () => {
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
