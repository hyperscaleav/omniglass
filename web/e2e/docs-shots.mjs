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
import { describeMask, planMasks, shootBaseline } from './shotmasklib.mjs';

const BASE = process.env.OG_E2E_BASE ?? 'http://localhost:8080';
const TOKEN = process.env.OG_TOKEN ?? '';
const CONTENT = 'docs/src/content/docs';
// Output dir is overridable so the freshness gate can capture into a temp dir and
// diff it against the committed images (see docs-shots-diff.mjs).
const OUT = process.env.DOCS_SHOTS_OUT ?? 'docs/public/screenshots';
// The gate's baselines live beside the capture tooling, never under public/:
// they are measurement artifacts (masked twins of each shot), not content.
const BASELINE = process.env.DOCS_SHOTS_BASELINE ?? 'docs/screenshots/baseline';
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
// one character narrower than 11:47 PM, and fmtTime's AM is wider than its PM.
// Two captures minutes apart share an hour and a day, so they are structurally
// blind to exactly that class of jitter. Run the proof with DOCS_SHOTS_EXACT=1:
// the gate's default comparison cannot see a one-pixel mask move (#774, #1081).
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
await mkdir(BASELINE, { recursive: true });
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
  // A mask is painted at the size of what it resolves to, so what a selector
  // resolves to is the whole game (#773). A `text=/…/` selector resolves to the
  // innermost element carrying the text, the value's own <span>, and so does a
  // `data-testid` hook placed on it (#1081): the painted box is then the size of
  // the text it hides, and a value one character narrower (an hour that is not
  // zero-padded, a single-digit day, PM for AM) moves the box. A mask over text of
  // varying width therefore names a container the layout pins (a table CELL), or
  // DECLARES its box ({ selector, width }) where the only pinned container also
  // holds what the shot exists to show. shotmasklib.mjs plans and paints both, and
  // web/e2e/shotmask.spec.ts tests that same code. Each mask's resolution is
  // logged so a mask that lands on the wrong element (a box as wide as its text is
  // the tell) shows in the capture output, not just in the raster.
  let plan;
  try {
    plan = await planMasks(page, spec.mask ?? []);
  } catch (e) {
    // A mask that matches nothing hides nothing, and a value wider than its
    // declared box leaves its tail live: either way the region renders into the
    // raster and the gate fails somewhere else, blaming the UI. Fail here.
    console.error(`docs-shots: ${e.message} (on ${spec.id})`);
    process.exit(1);
  }
  for (const m of plan) console.log(describeMask(m));
  // Two renders of the same settled page (#789 review): the CLEAN shot is what
  // the docs embed, every region live, because a masked strip teaches nothing;
  // the MASKED twin is the gate's baseline, where the capture-relative regions
  // (ages, dates, strip weights) are painted over so the zero-tolerance diff
  // measures only what is deterministic. The blind spot is explicit: a real UI
  // change entirely inside a masked region is invisible to the gate, exactly as
  // it was when the masked render was the only render.
  await page.screenshot({
    path: join(OUT, `${spec.id}.png`),
    animations: 'disabled',
    caret: 'hide',
  });
  try {
    await shootBaseline(page, plan, { path: join(BASELINE, `${spec.id}.png`) });
  } catch (e) {
    console.error(`docs-shots: ${e.message} (on ${spec.id})`);
    process.exit(1);
  }
  await ctx.close();
  console.log('wrote', join(OUT, `${spec.id}.png`), `+ baseline (${spec.path})`);
}

await browser.close();
