// Live capture for #683: the three label states, against the real binary and the
// real dev seed (never mocked).
import { chromium } from '@playwright/test';
import { mkdir } from 'node:fs/promises';
import { join } from 'node:path';

const BASE = process.env.OG_E2E_BASE ?? 'http://localhost:8080';
const TOKEN = process.env.OG_TOKEN ?? '';
const OUT = process.env.SHOT_OUT ?? '.superpowers/shots';

const shots = [
  { id: '683-components-list', path: '/web/components' },
  { id: '683-locations-list', path: '/web/locations' },
  { id: '683-systems-list', path: '/web/systems' },
  { id: '683-catalog-products', path: '/web/catalog/products' },
];

await mkdir(OUT, { recursive: true });
const browser = await chromium.launch();
for (const s of shots) {
  const ctx = await browser.newContext({ viewport: { width: 1320, height: 860 }, deviceScaleFactor: 2, reducedMotion: 'reduce' });
  const page = await ctx.newPage();
  if (TOKEN) await page.addInitScript((t) => localStorage.setItem('og-token', t), TOKEN);
  await page.goto(BASE + s.path, { waitUntil: 'networkidle' });
  await page.waitForTimeout(1200);
  await page.screenshot({ path: join(OUT, `${s.id}.png`), animations: 'disabled', caret: 'hide' });
  await ctx.close();
  console.log('wrote', s.id, s.path);
}
await browser.close();
