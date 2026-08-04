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
        `"Name" and "Segment" are retired as labels.\n`,
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

// A field bound to display_name must be labelled "Display name", and a field bound
// to name must be labelled "Name".
//
// This is the failure that keeps getting through. Twice now a sweep has fixed the
// create forms (`<Field label=...>`) and left the detail blades, which label with a
// bare `<span class="eyebrow">`, so eleven blades rendered two fields both called
// "Name": the identifier and the friendly string, indistinguishable. Tests did not
// catch it because no test reads a label and its binding together, and 812 of them
// were green while the blades were wrong.
//
// So the check is on the pairing, not on the vocabulary list. A label is only wrong
// relative to what it labels.
describe("a label matches the field it labels", () => {
  // A label is an eyebrow span, or a Field's label prop. The prop is often on its
  // own line, because Fields wrap, so match it standalone too or the window below
  // runs past the end of this field and reads the next one's binding.
  const LABEL = /class="eyebrow">([^<]+)<\/span>|\blabel="([^"]+)"/g;

  it("never labels display_name as Name, or name as Display name", () => {
    for (const file of walk(SRC, { tests: false })) {
      const lines = readFileSync(file, "utf8").split("\n");
      lines.forEach((line, i) => {
        LABEL.lastIndex = 0;
        const m = LABEL.exec(line);
        if (!m) return;
        const label = (m[1] ?? m[2]).trim();
        // The binding shows up within a few lines: the input, or the read-only
        // span. Stop at the NEXT label, or the window bleeds into the adjacent
        // field and reads its binding as this one's.
        const rest = lines.slice(i + 1, i + 8);
        const next = rest.findIndex((l) => LABEL.test((LABEL.lastIndex = 0, l)));
        const window = [lines[i], ...rest.slice(0, next === -1 ? rest.length : next)].join("\n");
        const bindsDisplay = /display_name|displayName/.test(window);
        const bindsName = /\bname\(\)|\.name\b|setName\(/.test(window);

        if (label === "Name" && bindsDisplay && !bindsName) {
          throw new Error(
            `${file}:${i + 1} labels display_name as "Name". The identifier is the name; ` +
              `the friendly string is the display name. A blade showing both under one word ` +
              `is the bug this guard exists for.`,
          );
        }
        if (label === "Display name" && bindsName && !bindsDisplay) {
          throw new Error(
            `${file}:${i + 1} labels name as "Display name", which is backwards.`,
          );
        }
      });
    }
  });
});
