// A page is settled when its network goes quiet, but a console page bound to
// live data holds a /views:watch stream open for as long as it shows (#1830),
// so Playwright's networkidle never comes. settle waits for quiet among the
// requests that end: every one but an event stream. Plain ESM so the .mjs
// capture scripts run it without a build step.

// isStreamRequest reports whether a request is a live stream, which stays
// open by design and so never counts against the page going quiet.
export function isStreamRequest(url, accept) {
  if ((accept ?? '').includes('text/event-stream')) return true;
  try {
    return new URL(url).pathname.endsWith('/views:watch');
  } catch {
    return false;
  }
}

const trackers = new WeakMap();

// trackRequests counts a page's requests in flight, streams aside. Attach it
// before the navigation it waits on, so a request already under way counts.
export function trackRequests(page) {
  let t = trackers.get(page);
  if (t) return t;
  const inflight = new Set();
  t = { inflight };
  page.on('request', (r) => {
    if (!isStreamRequest(r.url(), r.headers()['accept'])) inflight.add(r);
  });
  page.on('requestfinished', (r) => inflight.delete(r));
  page.on('requestfailed', (r) => inflight.delete(r));
  trackers.set(page, t);
  return t;
}

// settle resolves once no request but a stream has been in flight for
// quietMs, and rejects past timeout.
export async function settle(page, { quietMs = 500, timeout = 30_000 } = {}) {
  const { inflight } = trackRequests(page);
  const deadline = Date.now() + timeout;
  let quietFrom = inflight.size === 0 ? Date.now() : 0;
  for (;;) {
    if (inflight.size > 0) quietFrom = 0;
    else if (!quietFrom) quietFrom = Date.now();
    if (quietFrom && Date.now() - quietFrom >= quietMs) return;
    if (Date.now() > deadline) throw new Error(`the network was still busy after ${timeout / 1000}s: ${[...inflight].map((r) => r.url()).join(', ')}`);
    await new Promise((r) => setTimeout(r, 50));
  }
}

// gotoSettled navigates to url, waits for the load event, then for the page
// to settle.
export async function gotoSettled(page, url, opts = {}) {
  trackRequests(page);
  await page.goto(url, { waitUntil: 'load', timeout: opts.timeout });
  await settle(page, opts);
}

// settleLayout resolves once the page's geometry holds still: every scroll
// offset, the document's size, any open tooltip's box and the count of running
// animations are identical for `frames` consecutive animation frames, and the
// fonts are loaded. A shot taken while a hover is still scrolling a table, a
// tooltip is still being placed or a transition is still running differs from
// one taken a frame later, and the zero-tolerance freshness gate reads that as
// UI drift (#2168). It rejects past timeout, naming what kept moving.
export async function settleLayout(page, { frames = 30, timeout = 10_000 } = {}) {
  await page.evaluate(
    ({ frames, timeout }) =>
      new Promise((resolve, reject) => {
        const signature = () => {
          const parts = [innerWidth, innerHeight, scrollX, scrollY, document.documentElement.scrollWidth, document.documentElement.scrollHeight];
          for (const el of document.querySelectorAll('*')) {
            if (el.scrollLeft || el.scrollTop) parts.push(el.tagName, el.scrollLeft, el.scrollTop);
          }
          for (const el of document.querySelectorAll('[role=tooltip]')) {
            const r = el.getBoundingClientRect();
            parts.push('tip', r.x, r.y, r.width, r.height);
          }
          parts.push('anim', document.getAnimations().filter((a) => a.playState === 'running').length);
          return parts.join('|');
        };
        const deadline = performance.now() + timeout;
        let last = '';
        let steady = 0;
        const tick = () => {
          const now = signature();
          steady = now === last ? steady + 1 : 0;
          last = now;
          if (steady >= frames) resolve(undefined);
          else if (performance.now() > deadline) reject(new Error(`the layout was still moving after ${timeout / 1000}s: ${now}`));
          else requestAnimationFrame(tick);
        };
        document.fonts.ready.then(() => requestAnimationFrame(tick));
      }),
    { frames, timeout },
  );
}
