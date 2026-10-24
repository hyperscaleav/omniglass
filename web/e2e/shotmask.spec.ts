import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { test, expect, type Page } from "@playwright/test";
import matter from "gray-matter";
import { normalizeMask, paintedBoxes, planMasks, shootBaseline, type MaskEntry } from "./shotmasklib.mjs";

// The mask-geometry contract behind the docs screenshot gate (#773, #1081).
//
// docs-shots.mjs paints each `mask` frontmatter entry into the baseline, and a
// selector mask is painted at the bounding box of the element it RESOLVES to. So
// a mask that names the element holding a value paints a box the size of that
// value. The console does not render every capture-relative value at one width
// (Audit.tsx's `d.toLocaleString()` leaves the hour bare, format.ts's fmtTime
// leaves the day bare and prints a month name and AM or PM, durationText reads 9s,
// 45s or 12m), so the same unchanged UI paints a narrower box at one hour than
// another. A moved box is changed pixels, which every recapture rewrites into the
// baseline on a PR that changed no UI.
//
// A mask over a value of varying width therefore paints a box the value cannot
// move, one of two ways: it names a container the layout pins (the table cell its
// column descriptor pins, #773), or it declares the box, `{ selector, width }`,
// painted at the value's top-left as tall as the value and as wide as declared
// (#1081), for a value whose only fixed container also holds what the shot is
// there to show. These tests drive the capture's own mask code (shotmasklib.mjs)
// against synthetic markup, so they can change the value on demand: the shots'
// own proof is a clock-shifted recapture (DOCS_SHOTS_TZ with DOCS_SHOTS_EXACT=1),
// which cannot reach a single-digit day on most days of the month. No server
// needed, so this spec runs wherever Playwright does.
//
// The masks are READ from the docs pages' frontmatter, never copied here, so the
// tests judge the masks the capture actually uses: a page that loosens a mask back
// onto its text fails this spec.
//
// The comparison is BYTE equality rather than the gate's default compareShot,
// whose pixelmatch threshold cannot see a mask edge move over a dark surface at
// all (#774). Byte equality is the property a mask owes the gate.

const DOCS = fileURLToPath(new URL("../../docs/src/content/docs/", import.meta.url));

type Shot = { id: string; mask?: MaskEntry[] };
const selectorOf = (m: MaskEntry) => (typeof m === "string" ? m : m.selector);

// The mask a page declares for one value: the entry whose selector is the value's
// own, alone or followed by ` >> ` and the hop to its container.
function declaredMask(page: string, shotId: string, valueSelector: string): MaskEntry {
  const { data } = matter(readFileSync(`${DOCS}/${page}`, "utf8"));
  const shot = ((data.screenshots ?? []) as Shot[]).find((s) => s.id === shotId);
  if (!shot) throw new Error(`${page} declares no screenshot "${shotId}"`);
  const mask = (shot.mask ?? []).find((m) => {
    const sel = selectorOf(m);
    return sel === valueSelector || sel.startsWith(`${valueSelector} >> `);
  });
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
// see through and what fixes the box, the way the console fixes it. The type is
// small and the boxes generous so a value fits on any font stack; the declared
// widths' own margin over the console's widest capture text is measured in the
// pages' frontmatter comments.

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

// EntityBlade.tsx's SinceLine: the verdict, then the since span, last on its row.
const bladeRow = (value: string) => `
<style>${baseStyle}</style>
<aside style="width: 448px; padding: 20px; box-sizing: border-box">
  <div style="display: flex; flex-direction: column; gap: 16px">
    <div style="display: flex; flex-wrap: wrap; align-items: center; gap: 8px">
      <span class="badge">healthy</span>
      <span data-testid="blade-since" class="num" style="font-size: 12px; line-height: 16px">${value}</span>
    </div>
    <div>2 buildings</div>
  </div>
</aside>`;

// SystemSummary.tsx's card: the name, the verdict and the since span in one
// wrapping row, in a card the grid track keeps narrow enough that the since span
// always takes the row's second line.
const summaryCard = (value: string) => `
<style>${baseStyle}</style>
<button type="button" style="display: flex; flex-direction: column; gap: 8px; width: 316px; padding: 16px;
        box-sizing: border-box; border: 1px solid #1f2937; background: #0f172a; color: inherit; text-align: left; font: inherit">
  <span style="display: flex; flex-wrap: wrap; align-items: center; column-gap: 12px; row-gap: 4px">
    <span class="name" style="font-weight: 600">Media Lab Classroom</span>
    <span class="badge">healthy</span>
    <span data-testid="summary-since" class="num">${value}</span>
  </span>
  <span>Classroom · 3 components</span>
</button>`;

// Each masked value, the page and shot that declare its mask, and a pair of values
// a clock moves between: one character apart for the timestamps; for the
// since-lines, a single-digit day, PM and a short age against a two-digit day, AM
// and a longer age.
const cases: {
  name: string;
  page: string;
  shot: string;
  value: string;
  markup: (value: string) => string;
  narrow: string;
  wide: string;
  pinned?: number; // the box width the layout or the frontmatter fixes
  keeps?: string[]; // what the shot exists to show, which the mask must leave live
}[] = [
  {
    name: "audit's When column (an hour that is not zero-padded)",
    page: "guides/admin/audit.md",
    shot: "audit",
    value: "text=/\\d+\\/\\d+\\/\\d{4}/",
    markup: tableRow(190),
    narrow: "8/15/2026, 9:47:03 PM",
    wide: "8/15/2026, 11:47:03 PM",
    pinned: 190,
  },
  {
    name: "files' Added column (a day that is not zero-padded)",
    page: "guides/admin/files.md",
    shot: "files",
    value: "text=/[A-Z][a-z]{2} \\d{1,2}, \\d{2}:\\d{2} (AM|PM)/",
    markup: tableRow(170),
    narrow: "Aug 9, 09:47 PM",
    wide: "Aug 15, 09:47 PM",
    pinned: 170,
  },
  {
    name: "the entity blade's since-line",
    page: "guides/operator/entities.md",
    shot: "entity-blade",
    value: "[data-testid=blade-since]",
    markup: bladeRow,
    narrow: "since Oct 4, 06:59 PM · 9s",
    wide: "since Oct 24, 01:57 AM · 17s",
    keeps: [".badge"],
  },
  {
    name: "a system summary card's since-line",
    page: "guides/operator/fleet.md",
    shot: "fleet-place-systems",
    value: "[data-testid=summary-since]",
    markup: summaryCard,
    narrow: "since Oct 4, 06:59 PM · 9s",
    wide: "since Oct 24, 01:57 AM · 17s",
    keeps: [".badge", ".name"],
  },
];

const valueBox = async (page: Page, selector: string) => {
  const box = await page.locator(selector).boundingBox();
  if (!box) throw new Error(`${selector} resolved to nothing`);
  return box;
};

test.describe("docs screenshot masks", () => {
  for (const c of cases) {
    const mask = declaredMask(c.page, c.shot, c.value);

    test(`the declared mask paints a box the value cannot move: ${c.name}`, async ({ page }) => {
      await page.setContent(c.markup(c.narrow));
      const narrowValue = await valueBox(page, c.value);
      const [narrowBox] = paintedBoxes(await planMasks(page, [mask]));
      await page.setContent(c.markup(c.wide));
      const wideValue = await valueBox(page, c.value);
      const [wideBox] = paintedBoxes(await planMasks(page, [mask]));

      // The two values really do render at different widths (otherwise the case
      // proves nothing), and the box the mask paints is the same for both.
      expect(wideValue.width).toBeGreaterThan(narrowValue.width);
      expect(wideBox).toEqual(narrowBox);
      const pinned = c.pinned ?? (typeof mask === "string" ? undefined : mask.width);
      if (pinned) expect(Math.round(wideBox.width)).toBe(pinned);
      // The value ends inside the box with room to spare, so the box covers it
      // whatever it reads.
      expect(wideValue.x + wideValue.width).toBeLessThan(wideBox.x + wideBox.width);
    });

    // The verdict beside a since-line (and a card's name) is what the shot is
    // there to show and renders the same every capture, so the gate must keep
    // seeing it: masking the whole row would hide the one place a baseline still
    // gates the verdict badge.
    if (c.keeps) {
      test(`the declared mask leaves what the shot shows live: ${c.name}`, async ({ page }) => {
        await page.setContent(c.markup(c.wide));
        const boxes = paintedBoxes(await planMasks(page, [mask]));
        for (const keep of c.keeps!) {
          const k = await valueBox(page, keep);
          for (const b of boxes) {
            const overlaps = k.x < b.x + b.width && b.x < k.x + k.width && k.y < b.y + b.height && b.y < k.y + k.height;
            expect(overlaps, `${keep} lies under the mask ${JSON.stringify(mask)}`).toBe(false);
          }
        }
      });
    }

    const shoot = (entry: MaskEntry) => async (page: Page, value: string) => {
      await page.setContent(c.markup(value));
      return shootBaseline(page, await planMasks(page, [entry]));
    };

    test(`a baseline under the declared mask survives the clock: ${c.name}`, async ({ page }) => {
      const narrow = await shoot(mask)(page, c.narrow);
      const wide = await shoot(mask)(page, c.wide);
      expect(narrow.equals(wide)).toBe(true);
    });

    test(`a mask on the value itself does not, which is the defect: ${c.name}`, async ({ page }) => {
      const narrow = await shoot(c.value)(page, c.narrow);
      const wide = await shoot(c.value)(page, c.wide);
      expect(narrow.equals(wide)).toBe(false);
    });
  }

  // A declared box is a promise about the widest the value renders. One the value
  // outgrows would paint over part of it and leave the rest live, so the capture
  // refuses it by name instead of writing a baseline that moves.
  test("a value wider than its declared box fails the capture by name", async ({ page }) => {
    await page.setContent(bladeRow("since Oct 24, 01:57 AM · 17s"));
    const value = await valueBox(page, "[data-testid=blade-since]");
    const tight = { selector: "[data-testid=blade-since]", width: Math.floor(value.width) - 1 };
    await expect(planMasks(page, [tight])).rejects.toThrow(
      `[data-testid=blade-since] renders ${Math.ceil(value.width)}px wide, wider than its declared ${tight.width}px box`,
    );
  });

  test("a mask that matches nothing fails the capture by name", async ({ page }) => {
    await page.setContent(bladeRow("since Oct 24, 01:57 AM · 17s"));
    await expect(planMasks(page, ["[data-testid=nowhere]"])).rejects.toThrow(
      "mask matched no element: [data-testid=nowhere]",
    );
  });

  test("a mask entry is a selector or a selector with a whole-pixel width, nothing else", () => {
    expect(normalizeMask("[data-testid=x]")).toEqual({ selector: "[data-testid=x]" });
    expect(normalizeMask({ selector: "[data-testid=x]", width: 180 })).toEqual({ selector: "[data-testid=x]", width: 180 });
    for (const bad of [42, null, {}, { width: 10 }, { selector: "x" }, { selector: "x", width: 0 }, { selector: "x", width: 1.5 }, { selector: "x", width: 10, widht: 12 }]) {
      expect(() => normalizeMask(bad as never)).toThrow("invalid mask entry");
    }
  });
});
