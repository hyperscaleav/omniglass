import { afterEach, describe, expect, it } from "vitest";
import { cleanup, render } from "@solidjs/testing-library";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import Button from "./Button";

afterEach(cleanup);

// Tailwind and daisyUI emit only the classes they can SEE in the source. A
// class assembled at runtime (`btn-${size}`) is invisible to that scan, so the
// rule never reaches the stylesheet and the button silently renders at the
// default size. That is how every "xs" button in the console came to be drawn
// at 40px, larger than an "sm" one. So every class the primitive can put on a
// button has to appear somewhere as a literal.
describe("every class Button emits exists as a literal the stylesheet scan can see", () => {
  // A quoted string in the primitive, or a rule the app's own stylesheet
  // defines (the intent classes). A mention in a comment is neither.
  const primitive = readFileSync(join(__dirname, "Button.tsx"), "utf8");
  const stylesheet = readFileSync(join(__dirname, "..", "app.css"), "utf8");
  const visible = (cls: string) => primitive.includes(`"${cls}"`) || stylesheet.includes(`.${cls} `) || stylesheet.includes(`.${cls}:`);

  for (const size of ["md", "sm", "xs"] as const) {
    for (const intent of ["action", "quiet", "danger", "warn", "ok"] as const) {
      for (const square of [false, true]) {
        it(`${intent} ${size}${square ? " square" : ""}`, () => {
          const { container } = render(() => <Button intent={intent} size={size} square={square} label="x">x</Button>);
          const classes = [...container.querySelector("button")!.classList];
          expect(classes).toContain(size === "md" ? "btn" : `btn-${size}`);
          for (const cls of classes) expect(visible(cls), cls).toBe(true);
        });
      }
    }
  }
});
