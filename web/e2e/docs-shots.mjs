// Docs screenshot capture: drive the real console (served by the binary) and
// write the key-flow PNGs the guide pages embed. Run via `make docs-shots`, which
// brings up the dev stack, seeds an example estate, and exports OG_TOKEN +
// OG_E2E_BASE. Retina (deviceScaleFactor 2) so the images stay crisp.
//
// Usage: OG_TOKEN=<bearer> OG_E2E_BASE=http://localhost:8080 node web/e2e/docs-shots.mjs
import { chromium } from '@playwright/test';
import { mkdir } from 'node:fs/promises';

const BASE = process.env.OG_E2E_BASE ?? 'http://localhost:8080';
const TOKEN = process.env.OG_TOKEN ?? '';
const OUT = 'docs/src/assets/screenshots';

await mkdir(OUT, { recursive: true });
const browser = await chromium.launch();

// One shot per key flow. token:false captures the signed-out surface (the login
// screen); otherwise the bearer token is injected the way the console reads it.
async function shot(name, path, { token = true, wait = 800 } = {}) {
  const ctx = await browser.newContext({
    viewport: { width: 1320, height: 860 },
    deviceScaleFactor: 2,
  });
  const page = await ctx.newPage();
  if (token && TOKEN) {
    await page.addInitScript((t) => localStorage.setItem('og-token', t), TOKEN);
  }
  await page.goto(BASE + path, { waitUntil: 'networkidle' });
  await page.waitForTimeout(wait);
  await page.screenshot({ path: `${OUT}/${name}.png` });
  await ctx.close();
  console.log('wrote', `${OUT}/${name}.png`);
}

// Key flows only (see the docs plan): the sign-in screen, the inventory, the
// admin user directory, and the secrets directory.
await shot('sign-in', '/web/login', { token: false });
await shot('inventory', '/web/locations');
await shot('users', '/web/users');
await shot('secrets', '/web/secrets');

await browser.close();
