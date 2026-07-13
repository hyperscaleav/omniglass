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

const BASE = process.env.OG_E2E_BASE ?? 'http://localhost:8080';
const TOKEN = process.env.OG_TOKEN ?? '';
const CONTENT = 'docs/src/content/docs';
// Output dir is overridable so the freshness gate can capture into a temp dir and
// diff it against the committed images (see docs-shots-diff.mjs).
const OUT = process.env.DOCS_SHOTS_OUT ?? 'docs/public/screenshots';

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
  });
  const page = await ctx.newPage();
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
  // animations/caret disabled and non-deterministic regions masked so the raster
  // is byte-stable for the diff gate.
  const mask = (spec.mask ?? []).map((sel) => page.locator(sel));
  await page.screenshot({
    path: join(OUT, `${spec.id}.png`),
    animations: 'disabled',
    caret: 'hide',
    mask,
    maskColor: '#0d1117',
  });
  await ctx.close();
  console.log('wrote', join(OUT, `${spec.id}.png`), `(${spec.path})`);
}

await browser.close();
