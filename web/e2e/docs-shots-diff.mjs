// Pixel-freshness gate: compare a freshly captured screenshot dir against the
// committed images and fail if any differs by more than a small tolerance. Byte
// equality is not achievable because the dev seed generates random UUIDs (which
// drive the avatar gradient hues and the audit ids), so a fresh capture differs
// from the committed one by a fraction of a percent of cosmetic pixels. A real UI
// change moves far more than that, so a 0.5% ratio (a ~5x margin over the observed
// seed noise) catches genuine drift without flaking.
//
// Usage: node web/e2e/docs-shots-diff.mjs <fresh-dir> [committed-dir]
import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import { PNG } from 'pngjs';
import pixelmatch from 'pixelmatch';
import { readdir } from 'node:fs/promises';

const fresh = process.argv[2];
const committed = process.argv[3] ?? 'docs/public/screenshots';
const MAX_RATIO = Number(process.env.DOCS_SHOTS_MAX_RATIO ?? '0.005');

if (!fresh) {
  console.error('usage: node web/e2e/docs-shots-diff.mjs <fresh-dir> [committed-dir]');
  process.exit(2);
}

const names = (await readdir(committed)).filter((f) => f.endsWith('.png'));
const failures = [];

for (const name of names) {
  let a, b;
  try {
    a = PNG.sync.read(readFileSync(join(committed, name)));
    b = PNG.sync.read(readFileSync(join(fresh, name)));
  } catch (e) {
    failures.push(`${name}: missing in fresh capture (${e.code ?? e.message})`);
    continue;
  }
  if (a.width !== b.width || a.height !== b.height) {
    failures.push(`${name}: size changed ${a.width}x${a.height} -> ${b.width}x${b.height}`);
    continue;
  }
  const diff = pixelmatch(a.data, b.data, null, a.width, a.height, { threshold: 0.1 });
  const ratio = diff / (a.width * a.height);
  const pct = (ratio * 100).toFixed(3);
  if (ratio > MAX_RATIO) {
    failures.push(`${name}: ${pct}% changed (> ${(MAX_RATIO * 100).toFixed(2)}%)`);
  } else {
    console.log(`ok    ${name} (${pct}%)`);
  }
}

if (failures.length > 0) {
  console.error('\ndocs-shots freshness FAIL: the console changed but the screenshots were not refreshed.');
  for (const f of failures) console.error('  - ' + f);
  console.error("\nrun 'make docs-shots' and commit the updated PNGs.");
  process.exit(1);
}
console.log(`\ndocs-shots freshness OK (${names.length} shots within ${(MAX_RATIO * 100).toFixed(2)}%)`);
