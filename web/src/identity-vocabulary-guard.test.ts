import { readdirSync, readFileSync, statSync } from "node:fs";
import { join } from "node:path";
import { describe, expect, it } from "vitest";

// The identity triad, three facts and no synonyms.
//
//   id            a uuid, immutable, and never operator-facing.
//   name          the renameable identifier an operator types, what a URL, a CLI
//                 argument, and a topic carry.
//   display_name  an optional friendly string a human reads.
//
// The console used to say four things. A list column header said "Name" for the
// identifier on some pages and for the friendly string on others. A blade said
// "Name" on three pages and "Name" on seven. A later pass swapped the
// two words rather than settling them, so the identifier answered to "Key". The
// same fact answering to three words, and the same word meaning two facts, is the
// confusion the identity work exists to end. The words are now "Name" and
// "Display name", matching the columns.
//
// "Name" and "Segment" stay retired as field labels. A segment is one
// dot-separated component of a name (internal/key fixes that meaning), so it names
// a position in a path and never the value at one. That makes it a fine word in
// prose about topics and a wrong one on a form. Both are allowed in a comment
// (IdentityCell's header names the words it replaced, because that comment IS the
// history) but not in anything an operator reads.
//
// This is a source guard rather than a rendering test on purpose. The failure mode
// is a NEW page reaching for a retired word, and no per-page test catches that,
// because the page nobody wrote a test for is the page that drifts.
const SRC = join(__dirname);
const RETIRED = ["Technical name", "Segment"];

function walk(dir: string, opts: { tests: boolean }, out: string[] = []): string[] {
  for (const entry of readdirSync(dir)) {
    const full = join(dir, entry);
    if (statSync(full).isDirectory()) walk(full, opts, out);
    else if (/\.tsx?$/.test(entry) && (opts.tests || !/\.test\.tsx?$/.test(entry))) out.push(full);
  }
  return out;
}

// A line is prose if it is a comment. The rename is about operator-visible text,
// not about whether history may be described.
function isComment(line: string): boolean {
  const t = line.trim();
  return t.startsWith("//") || t.startsWith("*") || t.startsWith("/*") || t.startsWith("{/*");
}

// The argument text of every identityColumn(...) span in a file, definition
// included. An optional generic is stepped over, then the parentheses are balanced
// by hand rather than matched with a regex, so a nested object or arrow inside the
// options does not truncate the span. A bare mention (an import, a comment) is
// followed by neither `<` nor `(` and is skipped.
function identityColumnArgs(src: string): string[] {
  const out: string[] = [];
  const mention = /\bidentityColumn\b/g;
  let m: RegExpExecArray | null;
  while ((m = mention.exec(src)) !== null) {
    let i = m.index + m[0].length;
    if (src[i] === "<") {
      let angle = 0;
      for (; i < src.length; i++) {
        if (src[i] === "<") angle++;
        else if (src[i] === ">" && --angle === 0) {
          i++;
          break;
        }
      }
    }
    if (src[i] !== "(") continue;
    const start = i + 1;
    let depth = 0;
    for (; i < src.length; i++) {
      if (src[i] === "(") depth++;
      else if (src[i] === ")" && --depth === 0) {
        out.push(src.slice(start, i));
        break;
      }
    }
  }
  return out;
}

describe("identity vocabulary", () => {
  it("uses no retired word in operator-visible text", () => {
    const offenders: string[] = [];
    for (const file of walk(SRC, { tests: false })) {
      readFileSync(file, "utf8")
        .split("\n")
        .forEach((line, i) => {
          if (isComment(line)) return;
          for (const word of RETIRED) {
            // Only label TEXT counts: a quoted string an operator reads, or JSX
            // text between tags. A type or an accessor may legitimately be named
            // Segment (a donut chart has segments, an availability strip has
            // segments), and renaming those would be a rename for its own sake.
            const asString = new RegExp(`["'\`]${word}["'\`]`);
            const asJsxText = new RegExp(`>\\s*${word}\\s*<`);
            if (asString.test(line) || asJsxText.test(line)) {
              offenders.push(`${file.replace(SRC + "/", "")}:${i + 1}  ${word}`);
            }
          }
        });
    }
    expect(
      offenders,
      `\nThese carry a retired word in operator-visible text:\n  ${offenders.join("\n  ")}\n\n` +
        `The identifier is "Name". The friendly string is "Display name".\n` +
        `"Technical name" and "Segment" are retired as labels.\n`,
    ).toEqual([]);
  });

  // The second guard is structural, not lexical, because "key" cannot go on the
  // list above: a tag binding genuinely has a key and a value, and a filter chip
  // genuinely has a key. The failure worth catching is narrower and exact, a page
  // inventing a second word for the identifier by overriding the shared column's
  // header. identityColumn therefore takes no `label` option at all, and this
  // catches both halves of reintroducing one: a caller passing it, and the
  // signature growing it back.
  it("lets no page override the identity column header", () => {
    const offenders: string[] = [];
    for (const file of walk(SRC, { tests: true })) {
      for (const args of identityColumnArgs(readFileSync(file, "utf8"))) {
        if (/\blabel\s*\??\s*:/.test(args)) offenders.push(`${file.replace(SRC + "/", "")}  identityColumn(${args})`);
      }
    }
    expect(
      offenders,
      `\nThese give identityColumn a label option:\n  ${offenders.join("\n  ")}\n\n` +
        `Every list heads the identity column with one word, "Name". A page whose\n` +
        `name is validated differently (a dotted keyspace key, a username) still\n` +
        `holds a name, and a second header word for it is the drift the shared\n` +
        `column exists to prevent.\n`,
    ).toEqual([]);
  });
});

// A field or a fact that shows one of the two identity words takes its label from
// `IDENTITY_LABELS` in lib/entities, by naming the fact it is bound to:
//
//   <BladeField bind="display_name" .../>   labels itself "Display name"
//   <KVStacked  bind="name" .../>           labels itself "Name"
//
// `label` and `bind` are mutually exclusive on all three components, so there is
// no prop through which a caller can pair the wrong word with a binding. That
// makes the pairing a type, and the rule itself is a unit test on the component
// (BladeField.test.tsx, "the identity pairing").
//
// What is left here is the one failure a type cannot catch: a page BYPASSING the
// primitive and hand-typing one of the two words as a label. That is exactly the
// failure this guard was written for, so it shrinks rather than being deleted.
//
// What it replaced was much bigger and much weaker: a four-alternate regex over
// every call form (an eyebrow span, a `label=` prop, and the positional
// `field(...)` / `fact(...)` helpers), an eight-line lookahead window to find the
// binding, a second terminator so the window would not run into the next field,
// and a `display()` / `setDisplay(` heuristic to recognise the display-name
// signal. All of that existed only because a label and its binding were paired by
// hand at 74 sites. Its first version knew only two of the four forms and was
// therefore vacuous on the four biggest pages, which is the shape of the bug it
// was written to catch.
describe("the identity words are written in exactly one place", () => {
  // The components that render a labelled field or fact. A `label` prop carrying
  // an identity word on any of them means the caller went around `bind`.
  const LABELLED = ["BladeField", "FieldRow", "KVStacked"];
  const IDENTITY_WORDS = ["Name", "Display name"];

  // The opening tag of every `<Component ...>` in a file, attributes included.
  // Angle brackets are balanced by hand rather than matched with a regex, so a
  // multi-line tag (the common case once a field carries a hint) is one span and
  // not several.
  function openingTags(src: string, component: string): string[] {
    const out: string[] = [];
    const mention = new RegExp(`<${component}\\b`, "g");
    let m: RegExpExecArray | null;
    while ((m = mention.exec(src)) !== null) {
      let i = m.index + m[0].length;
      let brace = 0;
      for (; i < src.length; i++) {
        if (src[i] === "{") brace++;
        else if (src[i] === "}") brace--;
        else if (src[i] === ">" && brace === 0) break;
      }
      out.push(src.slice(m.index, i));
    }
    return out;
  }

  it("no page hand-types an identity word as a field or fact label", () => {
    const offenders: string[] = [];
    for (const file of walk(SRC, { tests: false })) {
      const src = readFileSync(file, "utf8");
      for (const component of LABELLED) {
        for (const tag of openingTags(src, component)) {
          for (const word of IDENTITY_WORDS) {
            if (new RegExp(`\\blabel=["']${word}["']`).test(tag)) {
              offenders.push(`${file.replace(SRC + "/", "")}  <${component} label="${word}" ...>`);
            }
          }
        }
      }
    }
    expect(
      offenders,
      `\nThese label a field or fact with an identity word by hand:\n  ${offenders.join("\n  ")}\n\n` +
        `Say which fact it is bound to instead: bind="name" or bind="display_name".\n` +
        `The label then comes from IDENTITY_LABELS in lib/entities, which is the only\n` +
        `place either word is written, so the two cannot be swapped again.\n`,
    ).toEqual([]);
  });

  it("the identity map is the only source of the two words", () => {
    // The rule above is only worth anything while the map is where the words
    // live. If someone inlines them back into a component, `bind` keeps working
    // and the guard keeps passing while the single source is gone.
    const entities = readFileSync(join(SRC, "lib", "entities.ts"), "utf8");
    expect(entities).toContain('name: "Name"');
    expect(entities).toContain('display_name: "Display name"');
    for (const component of ["BladeField", "FieldRow", "KVStacked"]) {
      const src = readFileSync(join(SRC, "components", `${component}.tsx`), "utf8");
      expect(src, `${component} should take the words from IDENTITY_LABELS`).toContain("IDENTITY_LABELS");
    }
  });
});

// A blade's heading must resolve the display name, not render the raw blade id.
//
// A blade is opened from a row, and the row shows the display name over the name.
// If the heading renders the id, clicking "ICMP RTT (avg)" opens a panel headed
// `icmp.rtt-avg`, so the operator cannot tell they opened the thing they clicked.
// Three pages did exactly that while 813 tests were green, because no test reads a
// blade heading: the id IS the name for these entities, so the wrong value is a
// plausible-looking string rather than an obvious defect.
//
// The rule is about resolution, not about text: a Title that renders `p.id` straight
// through has not looked the row up, whatever it wraps it in.
describe("a blade heading resolves the display name", () => {
  it("never renders the blade id as the whole heading", () => {
    const bare = /Title:\s*\(p\)\s*=>\s*(?:<[^>]*>)?\{p\.id\}(?:<\/[^>]*>)?,/;
    for (const file of walk(SRC, { tests: false })) {
      const src = readFileSync(file, "utf8");
      src.split("\n").forEach((line, i) => {
        if (bare.test(line)) {
          throw new Error(
            `${file}:${i + 1} renders the blade id as the heading. Look the row up and use ` +
              `entityLabel(row), falling back to the id, so the heading matches the row the ` +
              `operator clicked.`,
          );
        }
      });
    }
  });
});
