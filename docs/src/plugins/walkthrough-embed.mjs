// @ts-check
// The pure core of the docs walkthrough embed (#926): which recorder outputs a
// docs page carries, the HTML `::walkthrough{#id}` renders from the recorder's
// manifest, and how a built page's embeds are found again. Plain ESM with no
// dependencies, so the remark plugin (remark-walkthroughs.mjs), the asset copy
// (scripts/copy-walkthroughs.mjs) and the post-build check
// (scripts/check-walkthrough-embeds.mjs) share it, and the web unit tests pin it
// (web/src/docs-walkthrough.test.ts).
//
// The manifest is web/e2e/walkthroughs/out/<id>/walkthrough.json, written by the
// recorder; its types and the id and still rules are the shared core the
// run:walkthrough comment also reads (web/e2e/walkthroughcore.mjs). The docs
// never restate a caption: they copy the manifest and render from it.
import { PREVIEW, checkId, checkPreview, checkStep, checkStill, plural } from '../../../web/e2e/walkthroughcore.mjs';

/** @typedef {import('../../../web/e2e/walkthroughcore.mjs').Manifest} Manifest */

// Where a walkthrough's assets are served from, under docs/public/.
export const BASE = '/walkthroughs';

// The video's file name, as the recorder writes it beside the stills.
/** @param {string} id @returns {string} */
export function videoName(id) {
  return `${checkId(id)}.mp4`;
}

// The files a docs page carries for one walkthrough, in docs/public/walkthroughs/<id>/:
// the manifest, a still per step, and the animated preview when the manifest
// records one (preview.webp, #937). Not the recorder's poster.png, a byte copy
// of the poster step's still (the manifest names that step), and never the
// video, which is too large to commit; a build renders it only when present.
// A manifest whose recording never passed the budget gate is refused.
/** @param {Manifest} m @returns {string[]} */
export function walkthroughAssets(m) {
  const id = checkId(m.id);
  if (!m.budget) {
    throw new Error(`walkthrough ${id}: the manifest has no budget mark, so its recording did not pass the budget gate; re-run make walkthroughs ID=${id}`);
  }
  for (const s of m.steps) {
    checkStep(id, s.n);
    checkStill(id, s.still);
  }
  const stills = m.steps.map((s) => s.still);
  if (!stills.includes(m.poster)) {
    throw new Error(`walkthrough ${id}: poster "${m.poster}" is not one of its step stills`);
  }
  if (m.preview !== undefined) checkPreview(id, m.preview);
  return ['walkthrough.json', ...stills, ...(m.preview !== undefined ? [PREVIEW] : [])];
}

/** @param {unknown} s @returns {string} */
function esc(s) {
  return String(s)
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&#39;');
}

// The embed: the animated preview when the manifest records one, else the
// poster step's still (or, when the mp4 is present at build time, the video
// over the poster step's still and a link to it), the title and description, then
// every step's caption over its still, folded. `video` must be true only when
// the mp4 exists, so the page never links a file that is not there.
/** @param {Manifest} m @param {{ video: boolean }} opts @returns {string} */
export function renderWalkthrough(m, { video }) {
  walkthroughAssets(m); // validates the id, step numbers and still names before any reaches the HTML
  const dir = `${BASE}/${m.id}`;
  const title = esc(m.title);
  const poster = `${dir}/${m.poster}`;
  const media = video
    ? `<video controls preload="none" poster="${poster}" src="${dir}/${videoName(m.id)}">` +
      `<img src="${poster}" alt="${title}: poster" />` +
      `</video>`
    : m.preview !== undefined
      ? `<img src="${dir}/${PREVIEW}" alt="${title}: animated preview" loading="lazy" />`
      : `<img src="${poster}" alt="${title}: poster" loading="lazy" />`;
  const caption =
    `<strong>${title}</strong>` +
    (m.description ? ` ${esc(m.description)}` : '') +
    (video ? ` <a href="${dir}/${videoName(m.id)}">Watch the video (mp4)</a>` : '');
  const steps = m.steps
    .map(
      (s) =>
        `<li><p>${esc(s.caption)}</p>` +
        `<img src="${dir}/${s.still}" alt="Step ${s.n}: ${esc(s.caption)}" loading="lazy" /></li>`,
    )
    .join('');
  return (
    `<figure class="walkthrough" data-walkthrough="${m.id}">` +
    media +
    `<figcaption>${caption}</figcaption>` +
    `<details><summary>Every step, captioned (${plural(m.steps.length, 'step')})</summary>` +
    `<ol>${steps}</ol>` +
    `</details>` +
    `</figure>`
  );
}

// Every embed in a built page, verbatim, by id. The embed holds no nested
// figure, so each runs from its opening tag to the first </figure> after it.
/** @param {string} html @returns {{ id: string, html: string }[]} */
export function findEmbeds(html) {
  const out = [];
  const open = /<figure class="walkthrough" data-walkthrough="([^"]*)">/g;
  for (const m of html.matchAll(open)) {
    const end = html.indexOf('</figure>', m.index);
    if (end < 0) continue;
    out.push({ id: m[1], html: html.slice(m.index, end + '</figure>'.length) });
  }
  return out;
}

/** @param {string} s @returns {string} */
function decode(s) {
  return s
    .replace(/&#x([0-9a-f]+);/gi, (_, h) => String.fromCodePoint(parseInt(h, 16)))
    .replace(/&#(\d+);/g, (_, d) => String.fromCodePoint(Number(d)))
    .replace(/&quot;/g, '"')
    .replace(/&apos;/g, "'")
    .replace(/&lt;/g, '<')
    .replace(/&gt;/g, '>')
    .replace(/&amp;/g, '&');
}

// An embed's markup in a canonical form, so the HTML renderWalkthrough returns
// and the same embed after Astro's serializer (which drops the self-closing
// slash and writes hex entities) compare equal, while any difference in a tag,
// an attribute value or a text compares unequal. Every tag becomes its name and
// its attributes sorted with decoded values; every text run is decoded.
/** @param {string} html @returns {string} */
export function canonicalEmbed(html) {
  const out = [];
  const tag = /<(\/?)([a-zA-Z][a-zA-Z0-9-]*)((?:\s+[a-zA-Z-]+(?:="[^"]*")?)*)\s*\/?>/g;
  let last = 0;
  for (const m of html.matchAll(tag)) {
    const text = html.slice(last, m.index);
    if (text) out.push(`text(${JSON.stringify(decode(text))})`);
    last = (m.index ?? 0) + m[0].length;
    if (m[1]) {
      out.push(`</${m[2].toLowerCase()}>`);
      continue;
    }
    const attrs = [...m[3].matchAll(/([a-zA-Z-]+)(?:="([^"]*)")?/g)]
      .map((a) => `${a[1].toLowerCase()}=${JSON.stringify(decode(a[2] ?? ''))}`)
      .sort();
    out.push(`<${m[2].toLowerCase()} ${attrs.join(' ')}>`);
  }
  const tail = html.slice(last);
  if (tail) out.push(`text(${JSON.stringify(decode(tail))})`);
  return out.join('');
}
