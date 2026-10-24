// Pixel-freshness gate: compare freshly captured BASELINE screenshots (the
// masked twins) against the committed baselines and fail on ANY difference.
// The tolerance is zero (#398, #623): every nondeterministic region a capture
// can render (v7-uuid text, now-relative timestamps, the history strip's
// weights) is masked in the baseline through the page's `screenshots`
// frontmatter, painted as constant-color boxes, so byte-stable rasters from
// the pinned browser container leave no legitimate residual diff. The CLEAN
// shots under docs/public/screenshots are what the docs embed and are not
// diffed: their masked-region pixels legitimately differ every capture (#789
// review). The old percentage ceiling passed exactly the changes the gate
// exists to catch (a renamed label at 0.033%, a collapsed rail at 0.13%); see
// web/src/shotdiff-gate.test.ts for the pinned contract.
//
// Usage: node web/e2e/docs-shots-diff.mjs <fresh-baseline-dir> [committed-baseline-dir]
// DOCS_SHOTS_MAX_RATIO overrides the tolerance for unpinned local browsers
// only; CI runs the pinned container and the zero default.
// DOCS_SHOTS_EXACT=1 compares every pixel exactly instead of through
// pixelmatch's perceptual threshold, which scores a mask edge moving over a
// dark surface as zero (#774). It is the proof that a mask holds: recapture with
// the clock moved and require no pixel to differ (#1081).
import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import { readdir } from 'node:fs/promises';
import { compareShot, DEFAULT_MAX_RATIO } from './shotdiff.mjs';

const fresh = process.argv[2];
const committed = process.argv[3] ?? 'docs/screenshots/baseline';
const MAX_RATIO = process.env.DOCS_SHOTS_MAX_RATIO ? Number(process.env.DOCS_SHOTS_MAX_RATIO) : DEFAULT_MAX_RATIO;
const EXACT = process.env.DOCS_SHOTS_EXACT === '1';

if (!fresh) {
  console.error('usage: node web/e2e/docs-shots-diff.mjs <fresh-dir> [committed-dir]');
  process.exit(2);
}

const names = (await readdir(committed)).filter((f) => f.endsWith('.png'));
const failures = [];

for (const name of names) {
  let r;
  try {
    r = compareShot(readFileSync(join(committed, name)), readFileSync(join(fresh, name)), { maxRatio: MAX_RATIO, exact: EXACT });
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
    failures.push(
      EXACT
        ? `${name}: ${r.diffPixels}px differ (exact)`
        : `${name}: ${pct}% changed (${r.diffPixels}px > ${(MAX_RATIO * 100).toFixed(2)}%)`,
    );
  } else {
    console.log(`ok    ${name} (${EXACT ? 'exact' : `${pct}%`})`);
  }
}

if (failures.length > 0) {
  console.error('\ndocs-shots freshness FAIL: the console changed but the screenshots were not refreshed.');
  for (const f of failures) console.error('  - ' + f);
  console.error("\nrun 'make docs-shots' and commit the updated PNGs.");
  process.exit(1);
}
console.log(
  `\ndocs-shots freshness OK (${names.length} shots ${EXACT ? 'pixel-exact' : `within ${(MAX_RATIO * 100).toFixed(2)}%`})`,
);
