// The docs capture's masks, extracted from docs-shots.mjs so the code that paints
// a baseline is the code web/e2e/shotmask.spec.ts tests (#773, #1081).
//
// A mask entry in a page's `screenshots` frontmatter is one of two things:
//
//   "<selector>"                  painted at the bounding box of every element the
//                                 selector resolves to.
//   { selector: "<s>", width: N } a DECLARED box: painted at each resolved
//                                 element's top-left, as tall as the element and N
//                                 CSS pixels wide, whatever the element's own width.
//
// What a selector mask resolves to is the whole contract, because a mask is
// painted at the size of what it resolves to. A selector on a value's own span
// paints a box the size of the value, and a capture-relative value (a timestamp,
// an age) changes width with the clock, so the box moves and every recapture
// rewrites the baseline. A selector mask over such a value names a container the
// layout pins (a table cell, `>> xpath=ancestor::td[1]`). Where the only pinned
// container also holds what the shot is there to show (a since-line beside the
// verdict it qualifies), the box is declared instead: the value's left edge is
// deterministic, its width is not, so the frontmatter fixes the width. That holds
// only for a value whose LEFT edge stays put as its width changes (it starts its
// own run of the line and ends it): a right-aligned or centered value moves its
// left edge with its text, and a declared box over it moves too. A value wider
// than its declared box would leave its tail live, so the capture refuses it by
// name rather than writing a baseline that moves.

// A DELIBERATE redaction panel over the baseline, the console's own neutral
// surface (--color-neutral in web/src/app.css), so a diff artifact viewed by a
// human still reads as a block placed over a value.
export const MASK_COLOR = '#1a2336';

const DECLARED_BOX = 'data-og-mask-box';

/** @param {unknown} entry */
export function normalizeMask(entry) {
  if (typeof entry === 'string' && entry.length > 0) return { selector: entry };
  if (entry && typeof entry === 'object' && !Array.isArray(entry)) {
    const keys = Object.keys(entry).sort().join(',');
    const { selector, width } = /** @type {{ selector?: unknown, width?: unknown }} */ (entry);
    if (keys === 'selector,width' && typeof selector === 'string' && selector.length > 0 && Number.isInteger(width) && width > 0) {
      return { selector, width };
    }
  }
  throw new Error(
    `invalid mask entry ${JSON.stringify(entry)}: a selector string, or { selector, width } with a whole-pixel width`,
  );
}

// The viewport rect of every element a mask's selector matches, measured now.
// Throws, naming the selector, when it matches nothing (the mask would hide
// nothing, and the region it was written for would render live and fail the gate
// somewhere else) or when a value renders wider than its declared box.
async function measure(mask, locator) {
  const rects = await locator.evaluateAll((els) =>
    els.map((el) => {
      const r = el.getBoundingClientRect();
      return { tag: el.tagName.toLowerCase(), x: r.left, y: r.top, width: r.width, height: r.height };
    }),
  );
  if (rects.length === 0) throw new Error(`mask matched no element: ${mask.selector}`);
  if (mask.width !== undefined) {
    const widest = Math.max(...rects.map((r) => r.width));
    if (widest > mask.width) {
      throw new Error(`${mask.selector} renders ${Math.ceil(widest)}px wide, wider than its declared ${mask.width}px box`);
    }
  }
  return rects;
}

// Resolve every entry against the settled page: the locator a selector mask
// paints through, and the rects of the elements it matches (for the log, and for
// a test to inspect; the baseline measures again when it is shot).
export async function planMasks(page, entries) {
  const plan = [];
  for (const entry of entries) {
    const mask = normalizeMask(entry);
    const locator = page.locator(mask.selector);
    plan.push({ ...mask, locator, rects: await measure(mask, locator) });
  }
  return plan;
}

// The boxes a baseline paints, in viewport CSS pixels: a selector mask's element
// rects, or a declared box anchored at each element it matches.
export function paintedBoxes(plan) {
  return plan.flatMap((m) =>
    m.rects.map((r) =>
      m.width === undefined
        ? { x: r.x, y: r.y, width: r.width, height: r.height }
        : { x: r.x, y: r.y, width: m.width, height: r.height },
    ),
  );
}

// One log line per entry, so a mask that lands on the wrong element is visible in
// the capture output rather than only in the raster. A selector mask prints what
// it resolved to; a box that is as wide as its text is the tell of a mask that
// will move with it.
export function describeMask(m) {
  const shapes = m.rects.map((r) =>
    m.width === undefined
      ? `${r.tag} ${Math.round(r.width)}x${Math.round(r.height)}`
      : `box ${m.width}x${Math.round(r.height)} over ${r.tag} ${Math.round(r.width)}`,
  );
  const distinct = [...new Set(shapes)];
  return (
    `  mask ${String(m.rects.length).padStart(2)}x ${distinct.slice(0, 3).join(' | ')}` +
    `${distinct.length > 3 ? ` | +${distinct.length - 3} more` : ''}  <- ${m.selector}`
  );
}

// The masked BASELINE render of the settled page: each declared box is measured
// again and laid over its value as a fixed element (no layout moves), every mask
// is painted through Playwright's own mask in MASK_COLOR, and the declared boxes
// are removed again so a later render of the same page (or a test reusing it)
// sees the page untouched. Measuring again matters because the clean shot is
// taken between planning and this render, and a page can reflow in between: a
// selector mask re-resolves through its locator at the screenshot, and a declared
// box planted where the value used to be would leave the value's tail live.
export async function shootBaseline(page, plan, { path } = {}) {
  const boxes = [];
  for (const m of plan.filter((m) => m.width !== undefined)) {
    boxes.push({ ...m, rects: await measure(m, m.locator) });
  }
  await page.evaluate(
    ({ attr, color, boxes }) => {
      for (const b of boxes) {
        const el = document.createElement('div');
        el.setAttribute(attr, '');
        Object.assign(el.style, {
          position: 'fixed',
          left: `${b.x}px`,
          top: `${b.y}px`,
          width: `${b.width}px`,
          height: `${b.height}px`,
          background: color,
          pointerEvents: 'none',
          zIndex: '2147483647',
        });
        document.body.appendChild(el);
      }
    },
    { attr: DECLARED_BOX, color: MASK_COLOR, boxes: paintedBoxes(boxes) },
  );
  try {
    const mask = plan.filter((m) => m.width === undefined).map((m) => m.locator);
    if (boxes.length > 0) mask.push(page.locator(`[${DECLARED_BOX}]`));
    return await page.screenshot({ path, animations: 'disabled', caret: 'hide', mask, maskColor: MASK_COLOR });
  } finally {
    await page.evaluate((attr) => document.querySelectorAll(`[${attr}]`).forEach((el) => el.remove()), DECLARED_BOX);
  }
}
