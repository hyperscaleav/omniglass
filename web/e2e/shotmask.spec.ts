import { test, expect } from "@playwright/test";

// The mask-geometry contract behind the docs screenshot gate (#773).
//
// docs-shots.mjs turns each `mask` frontmatter entry into page.locator(sel), and
// Playwright paints the mask from the RESOLVED element's bounding box. Playwright's
// text engine pushes an element only when no descendant matches, so a bare
// `text=/…/` resolves to the innermost <span> holding the value and the painted box
// is the size of that value. The console does not zero-pad every field it renders
// (Audit.tsx's `d.toLocaleString()` leaves the hour bare, format.ts's fmtTime leaves
// the day bare), so the same unchanged UI paints a narrower box at 9 PM than at
// 11 PM, and on the 9th than on the 15th. A moved box is changed pixels, which
// fails the freshness gate on a PR that changed no UI.
//
// Masking the containing CELL, whose width its column descriptor pins, makes the
// box independent of the text it hides. These tests run against synthetic markup so
// they can flip a digit on demand: the shots' own proof is a clock-shifted
// recapture (DOCS_SHOTS_TZ), which cannot reach a single-digit day on most days of
// the month. No server needed, so this spec runs wherever Playwright does.
//
// The comparison here is BYTE equality rather than the gate's own compareShot:
// compareShot runs pixelmatch at threshold 0.1, which cannot see a mask edge move
// over a dark surface at all (#774). Byte equality is the property a mask actually
// owes the gate, and it is the one the clock-shifted capture demonstrates.

// A cut-down copy of the row markup the two shots mask: the column width pinned the
// way the real pages pin theirs (Audit.tsx's `when` column declares "190px",
// Files.tsx's `created_at` declares 170) and the value in its own <span>, which is
// the nesting the selector has to see through. The layout is fixed and the type is
// small so the value fits its cell with room to spare on any font stack, as it does
// in the console; a value that outgrew its cell would reflow the row and is a
// different bug from this one.
const rowMarkup = (value: string, cellWidth: number) => `
<style>
  body { margin: 0; background: #0c1320; color: #cbd5e1; font: 11px/1.5 monospace; }
  table { width: 720px; table-layout: fixed; border-collapse: collapse; }
  th, td { padding: 8px 12px; text-align: left; overflow: hidden; }
</style>
<table>
  <colgroup><col style="width: ${cellWidth}px" /><col /><col style="width: 150px" /></colgroup>
  <thead><tr><th>When</th><th>Who</th><th>Action</th></tr></thead>
  <tbody>
    <tr>
      <td><span style="white-space: nowrap; font-variant-numeric: tabular-nums">${value}</span></td>
      <td>dev</td>
      <td>create</td>
    </tr>
  </tbody>
</table>`;

// The two masked shots, each with the regex its page declares, and a pair of values
// that differ by exactly one character: the jitter a clock rollover produces.
const cases = [
  {
    name: "audit's When column (an hour that is not zero-padded)",
    selector: "text=/\\d+\\/\\d+\\/\\d{4}/",
    cellWidth: 190,
    narrow: "8/15/2026, 9:47:03 PM",
    wide: "8/15/2026, 11:47:03 PM",
  },
  {
    name: "files' Added column (a day that is not zero-padded)",
    selector: "text=/[A-Z][a-z]{2} \\d{1,2}, \\d{2}:\\d{2} (AM|PM)/",
    cellWidth: 170,
    narrow: "Aug 9, 09:47 PM",
    wide: "Aug 15, 09:47 PM",
  },
];

const cellOf = (selector: string) => `${selector} >> xpath=ancestor::td[1]`;

test.describe("docs screenshot masks", () => {
  for (const c of cases) {
    test(`resolves to the cell, not the text: ${c.name}`, async ({ page }) => {
      await page.setContent(rowMarkup(c.wide, c.cellWidth));

      const text = page.locator(c.selector);
      const cell = page.locator(cellOf(c.selector));
      expect(await text.evaluate((el) => el.tagName)).toBe("SPAN");
      expect(await cell.evaluate((el) => el.tagName)).toBe("TD");

      // The cell is the column's declared width whatever the value reads, and the
      // value fits inside it, which is why masking the cell neither reflows the row
      // nor tracks the text.
      const textBox = await text.boundingBox();
      const cellBox = await cell.boundingBox();
      expect(Math.round(cellBox!.width)).toBe(c.cellWidth);
      expect(textBox!.width).toBeLessThan(cellBox!.width - 24);
    });

    const shootWith = (mask: string) => async (page: import("@playwright/test").Page, value: string) => {
      await page.setContent(rowMarkup(value, c.cellWidth));
      return page.screenshot({
        animations: "disabled",
        caret: "hide",
        mask: [page.locator(mask)],
        maskColor: "#1a2336",
      });
    };

    test(`a cell mask survives a one-character change: ${c.name}`, async ({ page }) => {
      const shoot = shootWith(cellOf(c.selector));
      const narrow = await shoot(page, c.narrow);
      const wide = await shoot(page, c.wide);
      expect(narrow.equals(wide)).toBe(true);
    });

    test(`a text mask does not, which is the defect: ${c.name}`, async ({ page }) => {
      const shoot = shootWith(c.selector);
      const narrow = await shoot(page, c.narrow);
      const wide = await shoot(page, c.wide);
      expect(narrow.equals(wide)).toBe(false);
    });
  }
});
