// The steps a docs shot runs between loading its page and settling it (the
// `steps` frontmatter list), shared by the capture (docs-shots.mjs) and the spec
// that tests them (shotsteps.spec.ts), so the test judges the steps the capture
// really runs.
//
//   wait         hold until a selector shows (a figure the lab writes a while after
//                it comes up); `timeout` in ms, `message` names what never showed
//   click, hover, press, fill   drive the page
//   hide         take every element a CSS selector matches out of the page, for the
//                rest of the capture, matching or not: a region the lab's history
//                renders a variable number of times (a stretch that lasted 0.4s on
//                a fast host and 1.2s on a slow one is a row on one and none on the
//                other, #2707) is removed from both renders instead of masked,
//                since a mask paints over a box and cannot stop the rows below
//                it from moving
//   expect-count the selector's visible matches must number exactly `count`, or the
//                capture fails by name: the guard under `hide`, so a page whose
//                row count is still variable is refused, never written

const ACTIONS = ['wait', 'click', 'hover', 'press', 'fill', 'hide', 'expect-count'];

/** @param {unknown} step */
export function normalizeStep(step) {
  const s = /** @type {Record<string, unknown>} */ (step ?? {});
  if (typeof s.action !== 'string' || !ACTIONS.includes(s.action)) {
    throw new Error(`invalid step ${JSON.stringify(step)}: action must be one of ${ACTIONS.join(', ')}`);
  }
  if (s.action !== 'press' && !(typeof s.selector === 'string' && s.selector.length > 0)) {
    throw new Error(`invalid step ${JSON.stringify(step)}: ${s.action} needs a selector`);
  }
  if (s.action === 'expect-count' && !(Number.isInteger(s.count) && /** @type {number} */ (s.count) >= 0)) {
    throw new Error(`invalid step ${JSON.stringify(step)}: expect-count needs a whole-number count`);
  }
  return s;
}

/**
 * @param {import('@playwright/test').Page} page
 * @param {string} id the shot, for the failure's name
 * @param {Record<string, any>} step
 */
export async function runStep(page, id, step) {
  const s = normalizeStep(step);
  switch (s.action) {
    case 'wait':
      try {
        await page.locator(s.selector).first().waitFor({ state: 'visible', timeout: s.timeout ?? 120_000 });
      } catch (e) {
        // A wait may name what never showed, so the failure reads as its cause.
        if (s.message) throw new Error(`${id}: ${s.message} (waited for ${s.selector})`);
        throw e;
      }
      break;
    case 'click':
      await page.click(s.selector, { modifiers: s.modifiers });
      break;
    case 'hover':
      await page.hover(s.selector);
      break;
    case 'press':
      await page.keyboard.press(s.value);
      break;
    case 'fill':
      await page.fill(s.selector, s.value ?? '');
      break;
    case 'hide':
      await page.addStyleTag({ content: `${s.selector} { display: none !important; }` });
      break;
    case 'expect-count': {
      const n = await page.locator(s.selector).filter({ visible: true }).count();
      if (n !== s.count) {
        throw new Error(`${id}: expected ${s.count} visible ${s.selector}, found ${n} (a row count that varies is not a shot to write)`);
      }
      break;
    }
  }
}
