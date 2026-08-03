import { readdirSync, readFileSync } from "node:fs";
import { join } from "node:path";
import { describe, expect, it } from "vitest";

// "Name" means one thing, and it is the thing an operator typed.
//
// A list column renders the label on its primary line, so its header is "Name".
// A blade shows the kebab address on its own, and that is NOT the name: it is the
// technical name. Using the same word for both put "Name" on two surfaces two
// clicks apart meaning different things, which is exactly the confusion the
// identity work exists to end.
//
// This is a source guard rather than a rendering test on purpose. The failure
// mode is a NEW page reaching for the wrong word, and no per-page test catches
// that, because the page nobody wrote a test for is the page that drifts.
//
// Scope, deliberately: detail FACTS only, not form fields. A create form still
// labels the kebab address "Name" on every page, which is the repo's pre-existing
// convention and predates this guard. It is a real remaining inconsistency (a form
// says "Name" for the address while a list column says "Name" for the label) and
// it is settled by the display_name to name rename in #545 part two, which moves
// both words at once. Widening this guard to forms before that rename would fail
// on eleven pages it has no fix for.
const PAGES = join(__dirname, "pages");

describe("identity vocabulary", () => {
  it("never labels the technical name 'Name' on a detail fact", () => {
    const offenders: string[] = [];
    for (const file of readdirSync(PAGES)) {
      if (!file.endsWith(".tsx") || file.endsWith(".test.tsx")) continue;
      const src = readFileSync(join(PAGES, file), "utf8");
      src.split("\n").forEach((line, i) => {
        if (/<KVStacked\s+label="Name"/.test(line)) {
          offenders.push(`${file}:${i + 1}`);
        }
      });
    }
    expect(
      offenders,
      `\nThese label the kebab address "Name" on a detail surface:\n  ${offenders.join("\n  ")}\n\n` +
        `A list column header is "Name" and renders the label. A detail field showing the kebab\n` +
        `address is "Technical name". One word, one meaning.\n`,
    ).toEqual([]);
  });
});
