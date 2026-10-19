// @ts-check
// The pure core of the docs walkthrough embed (#926): which recorder outputs a
// docs page carries, and the HTML `::walkthrough{#id}` renders from the
// recorder's manifest. Plain ESM with no dependencies, so the remark plugin
// (remark-walkthroughs.mjs) and the asset copy (scripts/copy-walkthroughs.mjs)
// share it, and the web unit tests pin it (web/src/docs-walkthrough.test.ts).
//
// The manifest is web/e2e/walkthroughs/out/<id>/walkthrough.json, written by the
// recorder; its shape is the one the run:walkthrough comment reads
// (web/e2e/walkthroughpr.mjs). The docs never restate a caption: they copy the
// manifest and render from it.

/** @typedef {{ n: number, kind: string, caption: string, still: string }} ManifestStep */
/** @typedef {{ id: string, title: string, description?: string, steps: ManifestStep[], poster: string }} Manifest */

// Where a walkthrough's assets are served from, under docs/public/.
export const BASE = '/walkthroughs';

const ID = /^[a-z0-9][a-z0-9-]*$/;
const STILL = /^step-\d+\.png$/;

/** @param {unknown} id @returns {string} */
function checkId(id) {
  if (typeof id !== 'string' || !ID.test(id)) {
    throw new Error(`walkthrough id "${id}" must be lowercase letters, digits and dashes (it names a directory)`);
  }
  return id;
}

/** @param {string} id @param {unknown} name */
function checkStill(id, name) {
  if (typeof name !== 'string' || !STILL.test(name)) {
    throw new Error(`walkthrough ${id}: still "${name}" is not a recorder still name (step-NN.png)`);
  }
}

// The video's file name, as the recorder writes it beside the stills.
/** @param {string} id @returns {string} */
export function videoName(id) {
  return `${checkId(id)}.mp4`;
}

// The files a docs page carries for one walkthrough, in docs/public/walkthroughs/<id>/:
// the manifest, the poster and a still per step. Never the video, which is too
// large to commit; a build renders it only when it is present.
/** @param {Manifest} m @returns {string[]} */
export function walkthroughAssets(m) {
  const id = checkId(m.id);
  m.steps.forEach((s) => checkStill(id, s.still));
  return ['walkthrough.json', 'poster.png', ...m.steps.map((s) => s.still)];
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

/** @type {(n: number, word: string) => string} */
const plural = (n, word) => `${n} ${word}${n === 1 ? '' : 's'}`;

// The embed: the poster (or, when the mp4 is present at build time, the video
// over the poster and a link to it), the title and description, then every
// step's caption over its still, folded. `video` must be true only when the mp4
// exists, so the page never links a file that is not there.
/** @param {Manifest} m @param {{ video: boolean }} opts @returns {string} */
export function renderWalkthrough(m, { video }) {
  walkthroughAssets(m); // validates the id and every still name before any reaches a path
  const dir = `${BASE}/${m.id}`;
  const title = esc(m.title);
  const poster = `${dir}/poster.png`;
  const media = video
    ? `<video controls preload="none" poster="${poster}" src="${dir}/${videoName(m.id)}">` +
      `<img src="${poster}" alt="${title}: poster" />` +
      `</video>`
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
