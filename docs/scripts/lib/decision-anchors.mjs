// @ts-check
// The pure half of check-decision-anchors.mjs (#1096): reading the heading ids
// of the built decision log and every fragment a built page aims at it.

const LOG = '/architecture/decisions/';

/**
 * Every `id` attribute in the built page.
 * @param {string} html
 * @returns {Set<string>}
 */
export function headingIds(html) {
  return new Set([...html.matchAll(/\sid="([^"]*)"/g)].map((m) => m[1]));
}

/**
 * The fragments the built page aims at the decision log: an absolute link into
 * the log (`/architecture/decisions/#x`, with or without `index.html`), and,
 * when `isLog`, an in-page `#x`. A bare `#` is the top of the page, not an anchor.
 * @param {string} html
 * @param {boolean} isLog true when `html` is the decision log itself
 * @returns {string[]}
 */
export function logFragments(html, isLog) {
  const out = [];
  for (const m of html.matchAll(/\shref="([^"]*)"/g)) {
    const href = m[1].replaceAll('&#38;', '&').replaceAll('&amp;', '&');
    let frag;
    if (href.startsWith('#')) frag = isLog ? href.slice(1) : undefined;
    else if (href.startsWith(LOG)) {
      const hash = href.indexOf('#');
      const path = hash < 0 ? href : href.slice(0, hash);
      if (hash >= 0 && (path === LOG || path === LOG + 'index.html')) frag = href.slice(hash + 1);
    }
    if (frag) out.push(decodeURIComponent(frag));
  }
  return out;
}

/**
 * The fragments in `html` that are not a heading id on the decision log.
 * @param {string} html
 * @param {boolean} isLog
 * @param {Set<string>} ids
 * @returns {string[]}
 */
export function brokenFragments(html, isLog, ids) {
  return logFragments(html, isLog).filter((f) => !ids.has(f));
}
