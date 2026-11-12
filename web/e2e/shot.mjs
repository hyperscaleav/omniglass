// Headless screenshot helper for PR visual confirmation. Bundled chromium, writes
// PNG to the host FS. Usage:
//   node e2e/shot.mjs <url> <out.png> [--token TOK] [--click SEL]...
//     [--select "SEL||VALUE"]... [--type "SEL||TEXT"]... [--press KEY]...
//     [--hover SEL]... [--viewport WxH] [--wait MS] [--full]
import { chromium } from '@playwright/test';
import { HOUSE_VIEWPORT, parseViewport } from './shotviewport.mjs';
const a = process.argv.slice(2);
const url = a[0], out = a[1];
let token = process.env.OG_TOKEN || '', wait = 450, full = false, viewport = HOUSE_VIEWPORT;
const steps = [];
for (let i = 2; i < a.length; i++) {
  if (a[i] === '--token') token = a[++i];
  else if (a[i] === '--click') steps.push(['click', a[++i]]);
  else if (a[i] === '--select') steps.push(['select', a[++i]]);
  else if (a[i] === '--hover') steps.push(['hover', a[++i]]);
  else if (a[i] === '--type') steps.push(['type', a[++i]]);
  else if (a[i] === '--press') steps.push(['press', a[++i]]);
  else if (a[i] === '--wait') wait = +a[++i];
  else if (a[i] === '--viewport') {
    viewport = parseViewport(a[++i]);
    if (!viewport) {
      console.error('usage: node e2e/shot.mjs <url> <out.png> [--viewport WxH] ... (WxH is two positive integers, e.g. 1280x800)');
      process.exit(1);
    }
  }
  else if (a[i] === '--full') full = true;
}
const b = await chromium.launch();
const ctx = await b.newContext({ viewport, deviceScaleFactor: 2 });
const page = await ctx.newPage();
if (token) await page.addInitScript(t => localStorage.setItem('og-token', t), token);
await page.goto(url, { waitUntil: 'networkidle' });
for (const [kind, arg] of steps) {
  if (kind === 'click') await page.click(arg);
  else if (kind === 'hover') await page.hover(arg);
  else if (kind === 'press') await page.keyboard.press(arg);
  else if (kind === 'type') { const [sel, val] = arg.split('||'); await page.fill(sel, val); }
  else { const [sel, val] = arg.split('||'); await page.selectOption(sel, val); }
  await page.waitForTimeout(wait);
}
await page.waitForTimeout(wait);
await page.screenshot({ path: out, fullPage: full });
await b.close();
console.log('wrote', out);
