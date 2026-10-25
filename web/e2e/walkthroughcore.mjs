// @ts-check
// The shared pure core of a recorded walkthrough's manifest (walkthrough.json,
// written by web/e2e/walkthrough.mjs): its types, the id and still-name rules,
// and their checks. Both of the manifest's readers import it: the
// run:demo PR comment (walkthroughpr.mjs) and the docs embed
// (docs/src/plugins/walkthrough-embed.mjs). Plain ESM with no dependencies, so
// the runner's node and the docs build load it with no install or build step.

/** @typedef {{ n: number, kind: string, caption: string, still: string }} ManifestStep */
/**
 * `budget` and `preview` are written by the recorder's budget gate only when the
 * mp4 and its animated preview both passed it, so a manifest without the budget
 * mark never came from a recording that passed, and `preview` names the
 * preview only once it is known to be within its budget.
 * @typedef {{ id: string, title: string, description?: string, steps: ManifestStep[], poster: string,
 *   preview?: string, budget?: { seconds: number, bytes: number, previewBytes?: number } }} Manifest
 */

export const ID = /^[a-z0-9][a-z0-9-]*$/;
export const STILL = /^step-\d+\.png$/;
// The animated preview the recorder encodes from the mp4, beside the poster.
export const PREVIEW = 'preview.webp';

/** @param {unknown} id @returns {string} */
export function checkId(id) {
  if (typeof id !== 'string' || !ID.test(id)) {
    throw new Error(`walkthrough id "${id}" must be lowercase letters, digits and dashes (it names a directory)`);
  }
  return id;
}

// The video's file name, as the recorder writes it beside the stills.
/** @param {string} id @returns {string} */
export function videoName(id) {
  return `${checkId(id)}.mp4`;
}

/** @param {string} id @param {unknown} name */
export function checkStill(id, name) {
  if (typeof name !== 'string' || !STILL.test(name)) {
    throw new Error(`walkthrough ${id}: still "${name}" is not a recorder still name (step-NN.png)`);
  }
}

/** @param {string} id @param {unknown} name */
export function checkPreview(id, name) {
  if (name !== PREVIEW) {
    throw new Error(`walkthrough ${id}: preview "${name}" is not the recorder's preview name (${PREVIEW})`);
  }
}

/** @param {string} id @param {unknown} n @returns {number} */
export function checkStep(id, n) {
  if (typeof n !== 'number' || !Number.isInteger(n) || n < 1) {
    throw new Error(`walkthrough ${id}: step number "${n}" must be a positive integer`);
  }
  return n;
}

/** @type {(n: number, word: string) => string} */
export const plural = (n, word) => `${n} ${word}${n === 1 ? '' : 's'}`;
