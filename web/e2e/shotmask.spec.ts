import { readFileSync } from "node:fs";
import { test, expect, type Page } from "@playwright/test";
import matter from "gray-matter";

// The mask-geometry contract behind the docs screenshot gate (#773, #1081).
//
// docs-shots.mjs turns each `mask` frontmatter entry into page.locator(sel), and
// Playwright paints the mask from the RESOLVED element's bounding box. So a mask
// that names the element holding a value paints a box the size of that value. The
// console does not render every capture-relative value at one width (Audit.tsx's
// `d.toLocaleString()` leaves the hour bare, format.ts's fmtTime leaves the day
// bare and prints AM or PM, durationText reads 9s, 45s or 12m), so the same
// unchanged UI paints a narrower box at one hour than another. A moved box is
// changed pixels, which fails the freshness gate on a PR that changed no UI.
//
// A mask over text of varying width therefore names a container whose size the
// layout pins and the text cannot move: a table cell its column descriptor pins
// (#773), or the row a since-line sits in, which stretches to its card (#1081).
// These tests run against synthetic markup so they can change the value on demand:
// the shots' own proof is a clock-shifted recapture (DOCS_SHOTS_TZ), which cannot
// reach a single-digit day on most days of the month. No server needed, so this
// spec runs wherever Playwright does.
//
// The selectors are READ from the docs pages' frontmatter, never copied here, so
// the test judges the masks the capture actually uses: a page that loosens a mask
// back onto its text fails this spec.
//
// The comparison here is BYTE equality rather than the gate's own compareShot:
// compareShot runs pixelmatch at threshold 0.1, which cannot see a mask edge move
// over a dark surface at all (#774). Byte equality is the property a mask actually
// owes the gate, and it is the one the clock-shifted capture demonstrates.

const DOCS = "../docs/src/content/docs"; // Playwright runs with cwd=web.

type Shot = { id: string; mask?: string[] };

// The mask a page declares for one value: the entry that starts from the value's
// own selector, alone or followed by ` >> ` and the hop to its container.
function declaredMask(page: string, shotId: string, valueSelector: string): string {
  const { data } = matter(readFileSync(`${DOCS}/${page}`, "utf8"));
  const shot = ((data.screenshots ?? []) as Shot[]).find((s) => s.id === shotId);
  if (!shot) throw new Error(`${page} declares no screenshot "${shotId}"`);
  const mask = (shot.mask ?? []).find((m) => m === valueSelector || m.startsWith(`${valueSelector} >> `));
  if (!mask) throw new Error(`${page}'s "${shotId}" declares no mask over ${valueSelector}`);
  return mask;
}

const baseStyle = `
  body { margin: 0; background: #0c1320; color: #cbd5e1; font: 14px/20px sans-serif; }
  .badge { display: inline-flex; align-items: center; justify-content: center; width: 80px; height: 20px;
           border-radius: 6px; background: #12302a; color: #4ade80; font-size: 12px; }
  .num { white-space: nowrap; font-variant-numeric: tabular-nums; }
`;

// A cut-down copy of each masked surface's markup: the nesting the selector has to
// see through and the box that pins the container, the way the console pins it.
// The type is small and the boxes are generous so a value fits where the console
// puts it on any font stack; a value that outgrew its box would reflow the layout
// and is a different bug from this one.

// The audit trail and the files directory: the value in its own <span> inside a
// cell whose column declares its width (Audit.tsx's `when` column declares
// "190px", Files.tsx's `created_at` declares 170).
const tableRow = (cellWidth: number) => (value: string) => `
<style>${baseStyle}
  table { width: 720px; table-layout: fixed; border-collapse: collapse; font-size: 11px; }
  th, td { padding: 8px 12px; text-align: left; overflow: hidden; }
</style>
<table>
  <colgroup><col style="width: ${cellWidth}px" /><col /><col style="width: 150px" /></colgroup>
  <thead><tr><th>When</th><th>Who</th><th>Action</th></tr></thead>
  <tbody><tr><td><span class="num">${value}</span></td><td>dev</td><td>create</td></tr></tbody>
</table>`;

// EntityBlade.tsx's SinceLine: the verdict and the since span in a wrapping flex
// row, the row a flex-column child of the blade body, so it stretches to the
// blade's fixed width whatever the span reads.
const bladeRow = (value: string) => `
<style>${baseStyle}</style>
<aside style="width: 448px; padding: 24px; box-sizing: border-box">
  <div style="display: flex; flex-direction: column; gap: 16px">
    <div style="display: flex; flex-wrap: wrap; align-items: center; gap: 8px">
      <span class="badge">healthy</span>
      <span data-testid="blade-since" class="num" style="font-size: 12px">${value}</span>
    </div>
    <div>2 buildings</div>
  </div>
</aside>`;

// SystemSummary.tsx's card: the name, the verdict and the since span in one
// wrapping flex row (a <span>), itself a flex-column child of the card, whose
// width the grid track pins. At the house viewport the track is narrow enough
// that the since span always takes the row's second line, as it does here.
const summaryCard = (value: string) => `
<style>${baseStyle}</style>
<button type="button" style="display: flex; flex-direction: column; gap: 8px; width: 316px; padding: 16px;
        box-sizing: border-box; border: 1px solid #1f2937; background: #0f172a; color: inherit; text-align: left; font: inherit">
  <span style="display: flex; flex-wrap: wrap; align-items: center; column-gap: 12px; row-gap: 4px">
    <span style="font-weight: 600">Media Lab Classroom</span>
    <span class="badge">healthy</span>
    <span data-testid="summary-since" class="num">${value}</span>
  </span>
  <span>Classroom · 3 components</span>
</button>`;

// Each masked value, the page and shot that declare its mask, and a pair of values
// a clock moves between: one character apart for the timestamps, an AM against a
// PM and a duration a digit longer for the since-lines.
const cases: {
  name: string;
  page: string;
  shot: string;
  value: string;
  markup: (value: string) => string;
  narrow: string;
  wide: string;
  pinned?: number; // the width the layout declares for the container, where it declares one
}[] = [
  {
    name: "audit's When column (an hour that is not zero-padded)",
    page: "guides/admin/audit.md",
    shot: "audit",
    value: "text=/\\d+\\/\\d+\\/\\d{4}/",
    markup: tableRow(190),
    pinned: 190,
    narrow: "8/15/2026, 9:47:03 PM",
    wide: "8/15/2026, 11:47:03 PM",
  },
  {
    name: "files' Added column (a day that is not zero-padded)",
    page: "guides/admin/files.md",
    shot: "files",
    value: "text=/[A-Z][a-z]{2} \\d{1,2}, \\d{2}:\\d{2} (AM|PM)/",
    markup: tableRow(170),
    pinned: 170,
    narrow: "Aug 9, 09:47 PM",
    wide: "Aug 15, 09:47 PM",
  },
  {
    name: "the entity blade's since-line",
    page: "guides/operator/entities.md",
    shot: "entity-blade",
    value: "[data-testid=blade-since]",
    markup: bladeRow,
    narrow: "since Oct 4, 06:59 PM · 9s",
    wide: "since Oct 24, 01:57 AM · 17s",
  },
  {
    name: "a system summary card's since-line",
    page: "guides/operator/fleet.md",
    shot: "fleet-place-systems",
    value: "[data-testid=summary-since]",
    markup: summaryCard,
    narrow: "since Oct 4, 06:59 PM · 9s",
    wide: "since Oct 24, 01:57 AM · 17s",
  },
];

const boxOf = async (page: Page, selector: string) => {
  const box = await page.locator(selector).boundingBox();
  if (!box) throw new Error(`${selector} resolved to nothing`);
  return box;
};

test.describe("docs screenshot masks", () => {
  for (const c of cases) {
    const mask = declaredMask(c.page, c.shot, c.value);

    test(`the declared mask resolves to a box the value cannot move: ${c.name}`, async ({ page }) => {
      await page.setContent(c.markup(c.narrow));
      const narrowValue = await boxOf(page, c.value);
      const narrowMask = await boxOf(page, mask);
      await page.setContent(c.markup(c.wide));
      const wideValue = await boxOf(page, c.value);
      const wideMask = await boxOf(page, mask);

      // The two values really do render at different widths (otherwise the case
      // proves nothing), and the box the mask paints is the same for both.
      expect(wideValue.width).toBeGreaterThan(narrowValue.width);
      expect(wideMask).toEqual(narrowMask);
      if (c.pinned) expect(Math.round(wideMask.width)).toBe(c.pinned);
      // The value ends inside the box with room to spare, which is why masking
      // the container neither reflows the layout nor tracks the text.
      expect(wideValue.x + wideValue.width).toBeLessThan(wideMask.x + wideMask.width);
    });

    const shootWith = (selector: string) => async (page: Page, value: string) => {
      await page.setContent(c.markup(value));
      return page.screenshot({
        animations: "disabled",
        caret: "hide",
        mask: [page.locator(selector)],
        maskColor: "#1a2336",
      });
    };

    test(`a capture under the declared mask survives the clock: ${c.name}`, async ({ page }) => {
      const shoot = shootWith(mask);
      const narrow = await shoot(page, c.narrow);
      const wide = await shoot(page, c.wide);
      expect(narrow.equals(wide)).toBe(true);
    });

    test(`a mask on the value itself does not, which is the defect: ${c.name}`, async ({ page }) => {
      const shoot = shootWith(c.value);
      const narrow = await shoot(page, c.narrow);
      const wide = await shoot(page, c.wide);
      expect(narrow.equals(wide)).toBe(false);
    });
  }
});
