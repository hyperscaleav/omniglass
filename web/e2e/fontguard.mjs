// The capture's refusal to photograph the wrong typeface (#775).
//
// The docs capture opens a new browser context per shot and compares the result at
// zero tolerance. When the faces came from a font CDN, a context that lost the
// race rendered in fallback metrics and the capture wrote that PNG as though it
// were real; the freshness gate then reported it three steps downstream as UI
// drift on a PR that changed no UI. Self-hosting removes the race. This removes
// the silence: a shot that is not rendering in the console's own faces aborts the
// capture, naming what is missing.
//
// The decision is a pure function over what the page reports, so it is unit-tested
// without a browser (web/src/fontguard.test.ts); only the probe touches the page.

// The nine faces the type system declares (app.css: --og-font-ui, --og-font-data).
// Same list as web/src/font-hosting-guard.test.ts asserts src/fonts.css declares:
// there it is the stylesheet's contract, here it is the render's.
export const REQUIRED_FACES = [
  { family: 'IBM Plex Sans', weight: '400', style: 'normal' },
  { family: 'IBM Plex Sans', weight: '400', style: 'italic' },
  { family: 'IBM Plex Sans', weight: '500', style: 'normal' },
  { family: 'IBM Plex Sans', weight: '600', style: 'normal' },
  { family: 'IBM Plex Sans', weight: '700', style: 'normal' },
  { family: 'JetBrains Mono', weight: '400', style: 'normal' },
  { family: 'JetBrains Mono', weight: '500', style: 'normal' },
  { family: 'JetBrains Mono', weight: '600', style: 'normal' },
  { family: 'JetBrains Mono', weight: '700', style: 'normal' },
];

// Runs IN THE PAGE, so it is self-contained: Playwright serializes the source and
// module scope does not travel with it.
//
// Reported per required face: how many @font-face rules declare it (the families
// are split by unicode-range, so one face is several rules), whether any of them
// actually loaded, and whether any of them errored. Loadedness is reported, not
// judged, here: a weight the page never renders legitimately stays unloaded.
function probe(required) {
  return document.fonts.ready.then(() =>
    required.map((face) => {
      let declared = 0;
      let loaded = false;
      let errored = false;
      document.fonts.forEach((f) => {
        if (f.family.replace(/["']/g, '') !== face.family) return;
        if (f.weight !== face.weight || f.style !== face.style) return;
        declared += 1;
        if (f.status === 'loaded') loaded = true;
        if (f.status === 'error') errored = true;
      });
      return { ...face, declared, loaded, errored };
    }),
  );
}

const name = (f) => `${f.family} ${f.weight}${f.style === 'italic' ? ' italic' : ''}`;

// The pure decision: every reason this render must not be photographed, or [].
//
// Three properties, each catching a different way the fix can rot:
//
//   - Every required face is DECLARED. document.fonts.check() answers true for a
//     family nothing declares (no face matched, so no face is unloaded), so a
//     dropped @font-face or a stylesheet that 404s would otherwise read as clean.
//   - Each family RENDERED from at least one of its own files. This is the #775
//     signal itself: when the stylesheet was late, zero faces of both families
//     were loaded when the shutter fired. It is per family rather than per face
//     because a weight the page does not render never loads, which is not a fault.
//   - No font file errored or failed in flight.
export function fontFailures({ faces, failedRequests = [] }) {
  const failures = [];
  for (const f of faces) {
    if (f.declared === 0) failures.push(`no @font-face declares ${name(f)}`);
    else if (f.errored) failures.push(`${name(f)} failed to load (FontFace status "error")`);
  }
  for (const family of [...new Set(faces.map((f) => f.family))]) {
    const own = faces.filter((f) => f.family === family);
    if (own.every((f) => f.declared === 0)) continue; // already reported, once per face
    if (!own.some((f) => f.loaded)) {
      failures.push(
        `nothing rendered in ${family}: the page painted in fallback metrics, which is not what the shot is of`,
      );
    }
  }
  for (const req of failedRequests) failures.push(`font request failed: ${req}`);
  return failures;
}

// Collects font requests that never arrived. Attach BEFORE navigation; the array
// it returns is filled as the page loads and read by assertFontsRendered.
export function watchFontRequests(page) {
  const failed = [];
  const isFont = (url) => url.endsWith('.woff2') || url.includes('/fonts/');
  page.on('requestfailed', (r) => {
    if (isFont(r.url())) failed.push(`${r.url()} (${r.failure()?.errorText ?? 'failed'})`);
  });
  page.on('response', (r) => {
    if (isFont(r.url()) && r.status() >= 400) failed.push(`${r.url()} (${r.status()})`);
  });
  return failed;
}

// Throws unless this page is rendering in the console's own typefaces.
export async function assertFontsRendered(page, shotId, failedRequests = []) {
  const faces = await page.evaluate(probe, REQUIRED_FACES);
  const failures = fontFailures({ faces, failedRequests });
  if (failures.length > 0) {
    throw new Error(
      `docs-shots: ${shotId} is not rendering in the console's typefaces, refusing to write it (#775)\n` +
        failures.map((f) => `  - ${f}`).join('\n'),
    );
  }
}
