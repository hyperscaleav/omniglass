// Docs screenshot generator. Reads the `screenshots` frontmatter from every docs
// page, drives the real console (served by the binary), and writes one PNG per
// declared shot into docs/public/screenshots/. The frontmatter is the single
// source: a page declares what it needs, the `::screenshot{#id}` directive embeds
// the same entry, and this generator captures it. Nothing is hardcoded here, so a
// new shot is a frontmatter edit, never a code change.
//
// The PNGs are a generated resource, gated like `make gen`: `make docs-shots-check`
// regenerates and fails on any diff. For that gate to be stable, capture runs in a
// pinned browser container so raster output matches byte-for-byte across machines.
//
// Run via `make docs-shots` (brings up the stack and exports OG_TOKEN + OG_E2E_BASE).
import { chromium } from '@playwright/test';
import matter from 'gray-matter';
import { mkdir, readdir, readFile } from 'node:fs/promises';
import { join } from 'node:path';
import { assertFontsRendered, watchFontRequests } from './fontguard.mjs';

const BASE = process.env.OG_E2E_BASE ?? 'http://localhost:8080';
const TOKEN = process.env.OG_TOKEN ?? '';
const CONTENT = 'docs/src/content/docs';
// Output dir is overridable so the freshness gate can capture into a temp dir and
// diff it against the committed images (see docs-shots-diff.mjs).
const OUT = process.env.DOCS_SHOTS_OUT ?? 'docs/public/screenshots';
// There is no font proxy here any more. DOCS_SHOTS_PROXY existed because the
// console fetched its typefaces from a font CDN, so a capture host that could not
// reach fonts.gstatic.com rendered every shot in fallback metrics; the escape
// hatch was an egress proxy for the browser. The console now serves both families
// from its own origin (web/src/fonts.css, embedded in the binary under test), so
// the capture makes no request off the OG_E2E_BASE host at all and the hatch has
// nothing left to open (#775).
//
// Optional IANA zone for the capture browser (e.g. DOCS_SHOTS_TZ=Asia/Kolkata),
// applied as the context's timezoneId. Unset (the default, including CI) renders
// in the container's own zone, exactly the old behavior.
//
// It exists to VALIDATE the masks (#773). A mask must cover a box whose size does
// not depend on the text it hides, and the only honest way to prove that is to
// recapture with the clock moved far enough that a rendered timestamp changes
// character count: `d.toLocaleString()` does not zero-pad the hour, so 9:47 PM is
// one character narrower than 11:47 PM. Two captures minutes apart share an hour
// and a day, so they are structurally blind to exactly that class of jitter.
const TZ = process.env.DOCS_SHOTS_TZ ?? '';

async function walk(dir) {
  const files = [];
  for (const entry of await readdir(dir, { withFileTypes: true })) {
    const p = join(dir, entry.name);
    if (entry.isDirectory()) files.push(...(await walk(p)));
    else if (entry.name.endsWith('.md') || entry.name.endsWith('.mdx')) files.push(p);
  }
  return files;
}

async function collectSpecs() {
  const specs = [];
  for (const file of await walk(CONTENT)) {
    const { data } = matter(await readFile(file, 'utf8'));
    for (const s of data.screenshots ?? []) specs.push({ ...s, file });
  }
  return specs;
}

const specs = await collectSpecs();
if (specs.length === 0) {
  console.error('docs-shots: no `screenshots` frontmatter found under', CONTENT);
  process.exit(1);
}

const seen = new Set();
for (const s of specs) {
  if (seen.has(s.id)) {
    console.error(`docs-shots: duplicate screenshot id "${s.id}" (ids must be unique across pages)`);
    process.exit(1);
  }
  seen.add(s.id);
}

await mkdir(OUT, { recursive: true });
const browser = await chromium.launch();

for (const spec of specs) {
  const ctx = await browser.newContext({
    viewport: { width: 1320, height: 860 },
    deviceScaleFactor: 2,
    reducedMotion: 'reduce',
    ...(TZ ? { timezoneId: TZ } : {}),
  });
  const page = await ctx.newPage();
  // Attached before navigation so a font file that 404s or never arrives is
  // recorded rather than inferred from the raster later (#775).
  const failedFontRequests = watchFontRequests(page);
  if ((spec.auth ?? true) && TOKEN) {
    await page.addInitScript((t) => localStorage.setItem('og-token', t), TOKEN);
  }
  await page.goto(BASE + spec.path, { waitUntil: 'networkidle' });
  for (const step of spec.steps ?? []) {
    if (step.action === 'click') await page.click(step.selector);
    else if (step.action === 'hover') await page.hover(step.selector);
    else if (step.action === 'press') await page.keyboard.press(step.value);
    else if (step.action === 'fill') await page.fill(step.selector, step.value ?? '');
    await page.waitForTimeout(400);
  }
  await page.waitForTimeout(800);
  // A shot rendered in the wrong typeface is not a shot of the console, and the
  // gate that compares it at zero tolerance cannot tell that apart from UI drift.
  // Refuse to write it (#775): this is what silently shipped two fallback-font
  // PNGs and turned main red on a PR that changed no UI.
  await assertFontsRendered(page, spec.id, failedFontRequests);
  // animations/caret disabled and non-deterministic regions masked so the raster
  // is byte-stable for the diff gate.
  //
  // Playwright paints each mask from the RESOLVED element's bounding box, so what
  // a selector resolves to is the whole game (#773). A `text=/…/` selector resolves
  // to the innermost element carrying the text, which is the value's own <span>:
  // the painted box is then the size of the text it hides, and a value one
  // character narrower (an hour that is not zero-padded, a single-digit day) moves
  // the box, which is changed pixels against a budget of zero. A mask over text of
  // varying width therefore names the containing CELL, whose width its column
  // descriptor pins. The resolution is logged for every shot so a mask that lands
  // on the wrong element is visible in the capture output, not just in the raster.
  const mask = [];
  for (const sel of spec.mask ?? []) {
    const locator = page.locator(sel);
    const shapes = await locator.evaluateAll((els) =>
      els.map((el) => {
        const r = el.getBoundingClientRect();
        return `${el.tagName.toLowerCase()} ${Math.round(r.width)}x${Math.round(r.height)}`;
      }),
    );
    // A mask that matches nothing hides nothing: the region it was written for
    // renders live into the raster and the gate fails somewhere else, blaming the
    // UI. Fail here instead, naming the selector.
    if (shapes.length === 0) {
      console.error(`docs-shots: mask matched no element on ${spec.id}: ${sel}`);
      process.exit(1);
    }
    const distinct = [...new Set(shapes)];
    console.log(
      `  mask ${String(shapes.length).padStart(2)}x ${distinct.slice(0, 3).join(' | ')}` +
        `${distinct.length > 3 ? ` | +${distinct.length - 3} more` : ''}  <- ${sel}`,
    );
    mask.push(locator);
  }
  await page.screenshot({
    path: join(OUT, `${spec.id}.png`),
    animations: 'disabled',
    caret: 'hide',
    mask,
    // A DELIBERATE redaction panel, not an attempt to hide that a mask is there.
    // This is the console's own neutral surface (--color-neutral in web/src/app.css),
    // one step lighter than the card behind it, so a masked cell reads as a block
    // placed over the value rather than as a column that failed to render. Painting
    // the background instead would publish an empty When column with nothing to say
    // why, and would need a hardcoded copy of a theme token to stay invisible.
    maskColor: '#1a2336',
  });
  await ctx.close();
  console.log('wrote', join(OUT, `${spec.id}.png`), `(${spec.path})`);
}

await browser.close();
