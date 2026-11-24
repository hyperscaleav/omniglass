import { describe, it, expect } from "vitest";
import { readdirSync, readFileSync, statSync } from "node:fs";
import { join, relative } from "node:path";

// A hover explanation is the Tooltip wrapper, never the native title attribute
// (#2486): the browser's own tip never shows on keyboard focus, appears after a
// delay no one controls and is announced inconsistently. Button, Popover and
// Tooltip take a `title` prop and render the wrapper; a lowercase element (or
// the router's A) carrying `title=` is the bare attribute this guard bars. SVG
// <title> children are not attributes and are fine.

const SRC = join(__dirname);

function sources(dir: string): string[] {
  return readdirSync(dir).flatMap((f) => {
    const p = join(dir, f);
    if (statSync(p).isDirectory()) return sources(p);
    return /\.tsx$/.test(f) && !/\.test\.tsx$/.test(f) ? [p] : [];
  });
}

// tagEnd finds the '>' that closes an opening tag starting at i, skipping braces
// and string literals inside attribute values.
export function tagEnd(s: string, i: number): number {
  let depth = 0;
  let quote: string | null = null;
  for (let j = i + 1; j < s.length; j++) {
    const c = s[j];
    if (quote) {
      if (c === "\\") j++;
      else if (c === quote) quote = null;
    } else if (depth > 0) {
      if (c === '"' || c === "'" || c === "`") quote = c;
      else if (c === "{") depth++;
      else if (c === "}") depth--;
    } else if (c === '"') quote = '"';
    else if (c === "{") depth++;
    else if (c === ">") return j;
  }
  return -1;
}

export function bareTitles(src: string): number[] {
  const hits: number[] = [];
  const open = /<([a-z][\w-]*|A)\b/g;
  let m: RegExpExecArray | null;
  while ((m = open.exec(src))) {
    const end = tagEnd(src, m.index);
    if (end < 0) continue;
    const tag = src.slice(m.index, end + 1);
    const t = /\stitle=/.exec(tag);
    if (!t) continue;
    const before = tag.slice(0, t.index);
    if (before.split("{").length !== before.split("}").length) continue;
    hits.push(src.slice(0, m.index).split("\n").length);
  }
  return hits;
}

describe("no bare title attribute", () => {
  it("flags a title= on a plain element", () => {
    expect(bareTitles('<span class="x" title="Why">a</span>')).toEqual([1]);
    expect(bareTitles('<A href="/x" title={t()}>a</A>')).toEqual([1]);
    expect(bareTitles('<Tooltip as="span" content="Why">a</Tooltip>')).toEqual([]);
    expect(bareTitles('<Button square title="Edit" />')).toEqual([]);
    expect(bareTitles("<span class={`a ${b({ title: 1 })}`}>x</span>")).toEqual([]);
  });

  it("no element under web/src carries a bare title=", () => {
    const hits: string[] = [];
    for (const file of sources(SRC)) {
      for (const line of bareTitles(readFileSync(file, "utf8"))) hits.push(`${relative(SRC, file)}:${line}`);
    }
    expect(hits).toEqual([]);
  });
});
