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
export const DEFAULT_MAX_RATIO = 0;

export function compareShot(committedBuf, freshBuf, { maxRatio = DEFAULT_MAX_RATIO } = {}) {
  const a = PNG.sync.read(committedBuf);
  const b = PNG.sync.read(freshBuf);
  if (a.width !== b.width || a.height !== b.height) {
    return { status: 'size', a: { width: a.width, height: a.height }, b: { width: b.width, height: b.height } };
  }
  const diff = pixelmatch(a.data, b.data, null, a.width, a.height, { threshold: 0.1 });
  const ratio = diff / (a.width * a.height);
  return { status: ratio > maxRatio ? 'changed' : 'ok', ratio, diffPixels: diff };
}
