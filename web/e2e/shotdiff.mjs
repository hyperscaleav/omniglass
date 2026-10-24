import { PNG } from 'pngjs';
import pixelmatch from 'pixelmatch';

// The freshness comparison, extracted so the gate's contract is unit-testable
// (#398, #623): compare two PNG buffers and report ok / changed / size. The
// shipped default tolerance is ZERO. The old 0.5% ceiling existed to absorb
// dev-seed jitter (v7 uuids, now-relative timestamps) leaking into rendered
// pixels; those regions are masked at capture instead (each page's
// `screenshots` frontmatter, painted as constant-color boxes in the PNG), so a
// nonzero diff is a real UI change by construction and the gate that used to
// pass a renamed label (0.033%, #398) or a collapsed rail (0.13%, #623) now
// fails on a single unmasked pixel. DOCS_SHOTS_MAX_RATIO stays as an escape
// hatch for unpinned local browsers, never for CI.
//
// The zero is a zero COUNT of pixels pixelmatch judges different, and pixelmatch
// runs at threshold 0.1 below: against this console's dark surfaces that absorbs
// a whole surface swap, so a dark-on-dark change still scores 0.000%. Measured
// and tracked in #774; when byte stability is the claim, compare the bytes
// (web/e2e/shotmask.spec.ts does).
//
// `exact` is that comparison for whole captures: every pixel equal in every
// channel, any difference a failure whatever maxRatio says. It is how a mask is
// proven to hold (DOCS_SHOTS_EXACT=1 with the clock moved, #1081): a mask edge
// moving one pixel over a dark surface scores zero at threshold 0.1, so the
// default comparison cannot tell a mask that holds from one that does not.
export const DEFAULT_MAX_RATIO = 0;

function countExact(a, b) {
  let n = 0;
  for (let i = 0; i < a.length; i += 4) {
    if (a[i] !== b[i] || a[i + 1] !== b[i + 1] || a[i + 2] !== b[i + 2] || a[i + 3] !== b[i + 3]) n++;
  }
  return n;
}

export function compareShot(committedBuf, freshBuf, { maxRatio = DEFAULT_MAX_RATIO, exact = false } = {}) {
  const a = PNG.sync.read(committedBuf);
  const b = PNG.sync.read(freshBuf);
  if (a.width !== b.width || a.height !== b.height) {
    return { status: 'size', a: { width: a.width, height: a.height }, b: { width: b.width, height: b.height } };
  }
  if (exact) {
    const n = countExact(a.data, b.data);
    return { status: n > 0 ? 'changed' : 'ok', ratio: n / (a.width * a.height), diffPixels: n };
  }
  const diff = pixelmatch(a.data, b.data, null, a.width, a.height, { threshold: 0.1 });
  const ratio = diff / (a.width * a.height);
  return { status: ratio > maxRatio ? 'changed' : 'ok', ratio, diffPixels: diff };
}
