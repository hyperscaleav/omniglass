import { describe, it, expect } from "vitest";
import { readdirSync, readFileSync, statSync } from "node:fs";
import { dirname, join, relative } from "node:path";
import { fileURLToPath } from "node:url";

// A validation rule in this console lives in TypeScript, never in a native
// constraint attribute (#724).
//
// The attributes were not a second line of defence, they were decoration. The
// browser enforces `required`, `min`, `max`, `pattern` and friends on a real form
// SUBMISSION, and this console performs none on the paths an operator uses: a
// Drawer's action rail is portaled outside the <form> it belongs to (ADR-0054,
// the shell owns the rail and the body registers), a blade has no <form> at all,
// and the inline editors save from a plain onClick. The audit found 21 such
// attributes on 24 rendered controls and not one could ever fire, the four on
// real form paths included, because those forms disable their submit button in
// exactly the states native validation would have refused.
//
// So there is one vocabulary rather than two: a pure function judges the typed
// value (lib/validate, lib/command_types#readSettleWindow), the surface renders
// the message inline beside the field, and the binding's `disabled` / `valid`
// refuses the submit. That reads the same on every surface, form or not, and it
// says what is wrong rather than showing an unstyled browser bubble that vanishes
// on blur.
//
// `aria-required` is deliberately allowed. It announces the field to a screen
// reader without claiming the browser will refuse the value, which is exactly
// what is true here.

const here = dirname(fileURLToPath(import.meta.url));

function sourceFiles(dir: string, out: string[] = []): string[] {
  for (const entry of readdirSync(dir)) {
    const full = join(dir, entry);
    if (statSync(full).isDirectory()) sourceFiles(full, out);
    else if (/\.tsx$/.test(entry) && !/\.test\.tsx$/.test(entry)) out.push(full);
  }
  return out;
}

const files = sourceFiles(here);
const rel = (f: string) => relative(here, f);

// The native controls. A capitalised tag is a component, and a prop named
// `required` on one is a prop: what it does with it is that component's business,
// and PasswordField's forwards to `aria-required`.
const CONTROLS = ["input", "textarea", "select"];

// controlTag returns the source of each native control element, from `<input` to
// the `>` that closes the opening tag. Brace depth is tracked because a JSX
// attribute value can contain `>` (`onInput={(e) => ...}` does, which is what
// makes a single regex over the tag miss every attribute written after a handler).
function controlTags(src: string): string[] {
  const out: string[] = [];
  for (const name of CONTROLS) {
    let i = 0;
    for (;;) {
      const start = src.indexOf("<" + name, i);
      if (start < 0) break;
      i = start + 1;
      // `<inputs` or `<selection` would be another word entirely.
      if (/[A-Za-z0-9]/.test(src[start + name.length + 1] ?? "")) continue;
      let depth = 0;
      for (let j = start; j < src.length; j++) {
        const c = src[j];
        if (c === "{") depth++;
        else if (c === "}") depth--;
        else if (c === ">" && depth === 0) {
          out.push(src.slice(start, j + 1));
          i = j;
          break;
        }
      }
    }
  }
  return out;
}

// One entry per banned attribute, matched as an attribute of the tag rather than
// as a word, so `required: () => ...` in a binding and the word in prose are not
// findings.
const banned = ["required", "min", "max", "minlength", "maxlength", "pattern", "step"];

function attrOf(tag: string, attr: string): boolean {
  // Quoted VALUES are blanked first, so a placeholder or a title that happens to
  // contain the word ("Required, so you can tell them apart") is not a finding.
  const bare = tag.replace(/"[^"]*"/g, '""').replace(/'[^']*'/g, "''");
  // `attr=` in either JSX form, or the bare boolean form (`required` alone).
  return new RegExp(`\\s${attr}\\s*(?:=\\s*["'{]|/?>|\\s)`, "i").test(bare);
}

function offenders(attr: string): string[] {
  return files.filter((f) => controlTags(readFileSync(f, "utf8")).some((tag) => attrOf(tag, attr))).map(rel);
}

describe("validation lives in TypeScript, not in attributes", () => {
  for (const attr of banned) {
    it(`no control carries a native ${attr} constraint`, () => {
      expect(offenders(attr)).toEqual([]);
    });
  }

  // The scanner has to be able to FIND one, or an empty result would also be what
  // a broken regex returns. Both shapes that defeated the naive version are here:
  // an attribute written after an arrow-function handler, and one on its own line.
  it("finds the attributes when they are written", () => {
    const sample = `
      <input
        class="input"
        type="number"
        min="0"
        onInput={(e) => p.onInput(e.currentTarget.value)}
        required={p.targeted}
      />
      <textarea maxlength="10" />
      <Button required />`;
    const tags = controlTags(sample);
    expect(tags.length).toBe(2);
    expect(banned.filter((a) => tags.some((t) => attrOf(t, a)))).toEqual(["required", "min", "maxlength"]);
  });

  // aria-required is the honest spelling and must survive the ban.
  it("allows aria-required", () => {
    const tags = controlTags(`<input aria-required="true" value={v()} />`);
    expect(banned.filter((a) => tags.some((t) => attrOf(t, a)))).toEqual([]);
  });
});
