// Pixel-freshness gate: compare a freshly captured screenshot dir against the
// committed images and fail on ANY unmasked difference. The tolerance is zero
// (#398, #623): every nondeterministic region a capture can render (v7-uuid
// text, now-relative timestamps) is masked at capture time through the page's
// `screenshots` frontmatter, painted as constant-color boxes into the PNG
// itself, so byte-stable rasters from the pinned browser container leave no
// legitimate residual diff. The old percentage ceiling passed exactly the
// changes the gate exists to catch (a renamed label at 0.033%, a collapsed
// rail at 0.13%); see web/src/shotdiff-gate.test.ts for the pinned contract.
//
// Usage: node web/e2e/docs-shots-diff.mjs <fresh-dir> [committed-dir]
// DOCS_SHOTS_MAX_RATIO overrides the tolerance for unpinned local browsers
// only; CI runs the pinned container and the zero default.
import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import { readdir } from 'node:fs/promises';
import { compareShot, DEFAULT_MAX_RATIO } from './shotdiff.mjs';

const fresh = process.argv[2];
const committed = process.argv[3] ?? 'docs/public/screenshots';
const MAX_RATIO = process.env.DOCS_SHOTS_MAX_RATIO ? Number(process.env.DOCS_SHOTS_MAX_RATIO) : DEFAULT_MAX_RATIO;

if (!fresh) {
  console.error('usage: node web/e2e/docs-shots-diff.mjs <fresh-dir> [committed-dir]');
  process.exit(2);
}

const names = (await readdir(committed)).filter((f) => f.endsWith('.png'));
const failures = [];

for (const name of names) {
  let r;
  try {
    r = compareShot(readFileSync(join(committed, name)), readFileSync(join(fresh, name)), { maxRatio: MAX_RATIO });
  } catch (e) {
    failures.push(`${name}: missing in fresh capture (${e.code ?? e.message})`);
    continue;
  }
  if (r.status === 'size') {
    failures.push(`${name}: size changed ${r.a.width}x${r.a.height} -> ${r.b.width}x${r.b.height}`);
    continue;
  }
  const pct = (r.ratio * 100).toFixed(3);
  if (r.status === 'changed') {
    failures.push(`${name}: ${pct}% changed (${r.diffPixels}px > ${(MAX_RATIO * 100).toFixed(2)}%)`);
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
