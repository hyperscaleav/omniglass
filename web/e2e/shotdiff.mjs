import { PNG } from 'pngjs';

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
// The zero is a zero COUNT of pixels that differ in any channel (#774): a byte-
// equal pair is ok without decoding, and a byte mismatch is decoded and counted
// exactly, every pixel, every channel (alpha included). The gate used to count
// the pixels pixelmatch judged different at threshold 0.1, which cannot see a
// change confined to the console's dark surface tones (#0d1117 against #0c1320
// scores 0): a mask box resizing moved 8,400 pixels and scored 0.000% (#773), a
// mask edge moving a column scored zero too (#1081). pixelmatch stays in
// web/e2e/shot-jitter.mjs, the dev locator tool, where a perceptual diff is the
// point. DOCS_SHOTS_MAX_RATIO applies to the exact count.
export const DEFAULT_MAX_RATIO = 0;

function countExact(a, b) {
  let n = 0;
  for (let i = 0; i < a.length; i += 4) {
    if (a[i] !== b[i] || a[i + 1] !== b[i + 1] || a[i + 2] !== b[i + 2] || a[i + 3] !== b[i + 3]) n++;
  }
  return n;
}

export function compareShot(committedBuf, freshBuf, { maxRatio = DEFAULT_MAX_RATIO } = {}) {
  if (Buffer.compare(committedBuf, freshBuf) === 0) return { status: 'ok', ratio: 0, diffPixels: 0 };
  const a = PNG.sync.read(committedBuf);
  const b = PNG.sync.read(freshBuf);
  if (a.width !== b.width || a.height !== b.height) {
    return { status: 'size', a: { width: a.width, height: a.height }, b: { width: b.width, height: b.height } };
  }
  const diff = countExact(a.data, b.data);
  const ratio = diff / (a.width * a.height);
  return { status: ratio > maxRatio ? 'changed' : 'ok', ratio, diffPixels: diff };
}
