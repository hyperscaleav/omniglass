import { readdirSync, readFileSync, statSync } from "node:fs";
import { join } from "node:path";
import { describe, expect, it } from "vitest";

// Two words, two meanings, and no synonyms.
//
//   Name  the friendly label an operator types and reads.
//   Key   the machine identifier, what a URL, a CLI argument, and a topic carry.
//
// The console used to say four things. A list column header said "Name" for the
// address on some pages and for the label on others. A blade said "Technical name"
// on three pages and "Name" on seven. Every label field said "Display name". The
// same fact answered to three words and the same word meant two facts, which is
// the confusion the identity work exists to end.
//
// "Display name", "Technical name", and "Segment" are all retired as field labels.
// A segment is one dot-separated component of a key (internal/key fixes that
// meaning), so it names a position in a path and never the value at one. That makes
// it a fine word in prose about topics and a wrong one on a form. They are allowed in a comment
// (IdentityCell's header names both, because that comment IS the history) but not
// in anything an operator reads.
//
// This is a source guard rather than a rendering test on purpose. The failure mode
// is a NEW page reaching for a retired word, and no per-page test catches that,
// because the page nobody wrote a test for is the page that drifts.
const SRC = join(__dirname);
const RETIRED = ["Display name", "Technical name", "Segment"];

function walk(dir: string, out: string[] = []): string[] {
  for (const entry of readdirSync(dir)) {
    const full = join(dir, entry);
    if (statSync(full).isDirectory()) walk(full, out);
    else if (/\.tsx?$/.test(entry) && !/\.test\.tsx?$/.test(entry)) out.push(full);
  }
  return out;
}

// A line is prose if it is a comment. The rename is about operator-visible text,
// not about whether history may be described.
function isComment(line: string): boolean {
  const t = line.trim();
  return t.startsWith("//") || t.startsWith("*") || t.startsWith("/*") || t.startsWith("{/*");
}

describe("identity vocabulary", () => {
  it("uses no retired word in operator-visible text", () => {
    const offenders: string[] = [];
    for (const file of walk(SRC)) {
      readFileSync(file, "utf8")
        .split("\n")
        .forEach((line, i) => {
          if (isComment(line)) return;
          for (const word of RETIRED) {
            // Only label TEXT counts: a quoted string an operator reads, or JSX
            // text between tags. A type or an accessor may legitimately be named
            // Segment (a donut chart has segments; IdentityCell has showSegment),
            // and renaming those would be a rename for its own sake.
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
        `The label an operator types is "Name". The machine identifier is "Key".\n` +
        `"Display name", "Technical name", and "Segment" are retired as labels.\n`,
    ).toEqual([]);
  });
});
