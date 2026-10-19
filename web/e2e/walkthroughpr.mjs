// @ts-check
// Walkthroughs as PR evidence (#925): the pure choices the run:walkthrough
// workflow (.github/workflows/walkthrough.yml) makes, so they are unit-tested
// (web/src/walkthrough-pr.test.ts) rather than buried in YAML. Plain ESM with no
// dependencies, so the workflow runs it on the runner's node with no build step;
// web/e2e/walkthrough-pr.mjs is its command line. Its types are the JSDoc below,
// checked by tsc (web/tsconfig.json).
//
// - which walkthroughs a PR records: the scripts a scripts-only PR adds or
//   changes, else all;
// - where their stills are committed: .github/screenshots/<pr>/walkthroughs/<id>/
//   (ruled on #884), so the evidence that matters for review renders inline and
//   outlives the workflow artifact that carries the video;
// - the raw URL each still is embedded by: an immutable commit SHA, never a
//   branch, so the link survives the branch being deleted on squash-merge;
// - the one comment the workflow upserts, found again by its marker.

import { checkId, checkStill, plural } from './walkthroughcore.mjs';

/** @typedef {import('./walkthroughcore.mjs').ManifestStep} ManifestStep */
/** @typedef {import('./walkthroughcore.mjs').Manifest} Manifest */

export const WALKTHROUGH_MARKER = '<!-- omniglass-walkthrough -->';

const SCRIPT_DIR = 'web/e2e/walkthroughs/';
const SHA = /^[0-9a-f]{40}$/;
const REPO = /^[A-Za-z0-9_.-]+\/[A-Za-z0-9_.-]+$/;

// The ids to record, in the tree's order. `changed` is the PR's changed paths
// (repo-relative), `all` every script's id in the tree. A PR that changes
// nothing under web/ but scripts records just the scripts it adds or changes;
// any other change under web/ (the console, the recorder, a fixture) records
// every one, since it can move a flow a walkthrough walks; so does a PR that
// touches no existing script.
/** @param {string[]} changed @param {string[]} all @returns {string[]} */
export function selectWalkthroughs(changed, all) {
  all.forEach(checkId);
  if (all.length === 0) throw new Error(`walkthroughs: no walkthroughs under ${SCRIPT_DIR}`);
  const touched = new Set();
  for (const p of changed) {
    const rest = p.startsWith(SCRIPT_DIR) && p.endsWith('.yaml') ? p.slice(SCRIPT_DIR.length, -'.yaml'.length) : null;
    if (rest !== null && !rest.includes('/')) touched.add(rest);
    else if (p.startsWith('web/')) return [...all];
  }
  const picked = all.filter((id) => touched.has(id));
  return picked.length ? picked : [...all];
}

// The directory every walkthrough still of PR `pr` sits under. A run clears it
// before writing, so a renamed or deleted script leaves no stale stills.
/** @param {number} pr @returns {string} */
export function stillsRoot(pr) {
  if (!Number.isInteger(pr) || pr < 1) throw new Error(`PR number "${pr}" must be a positive integer`);
  return `.github/screenshots/${pr}/walkthroughs`;
}

// Where a walkthrough's poster and stills are committed for PR `pr`.
/** @param {number} pr @param {string} id @returns {string} */
export function stillsDir(pr, id) {
  return `${stillsRoot(pr)}/${checkId(id)}`;
}

// The raw URL of `path` at commit `sha` of `repo` (owner/name).
/** @param {string} repo @param {string} sha @param {string} path @returns {string} */
export function rawUrl(repo, sha, path) {
  if (!REPO.test(repo)) throw new Error(`repository "${repo}" must be owner/name`);
  if (!SHA.test(sha)) throw new Error(`"${sha}" must be a full commit SHA (an immutable link, never a branch)`);
  return `https://raw.githubusercontent.com/${repo}/${sha}/${path.split('/').map(encodeURIComponent).join('/')}`;
}

// Script text into Markdown, on one line: no HTML, no brackets that could open
// a link (the backslash escaped first, so "\[" cannot turn back into "["), and
// no newline that could start a heading or close the fold.
/** @param {string} s @returns {string} */
function esc(s) {
  return String(s)
    .replace(/\r?\n|\r/g, ' ')
    .replace(/\\/g, '\\\\')
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/([[\]])/g, '\\$1');
}

// One walkthrough's section: title, description, poster, then every step's
// caption over its still, folded so the comment stays scannable.
/** @param {string} repo @param {number} pr @param {string} sha @param {Manifest} m @returns {string[]} */
function section(repo, pr, sha, m) {
  checkStill(m.id, m.poster);
  m.steps.forEach((s) => checkStill(m.id, s.still));
  const dir = stillsDir(pr, m.id);
  const url = (/** @type {string} */ f) => rawUrl(repo, sha, `${dir}/${f}`);
  const lines = [`#### ${esc(m.title)}`, ''];
  if (m.description) lines.push(esc(m.description), '');
  lines.push(`\`${m.id}\`, ${plural(m.steps.length, 'step')}`, '', `![${esc(m.title)}: poster](${url('poster.png')})`, '');
  lines.push('<details><summary>Every step, as recorded</summary>', '');
  for (const s of m.steps) {
    lines.push(`${s.n}. ${esc(s.caption)}`, '', `   ![${m.id} step ${s.n}](${url(s.still)})`, '');
  }
  lines.push('</details>', '');
  return lines;
}

// The comment body. `sha` is the commit holding the stills (what they are
// embedded by), `headSha` the PR head they were recorded from; `recorded` the
// manifests (walkthrough.json) of the walkthroughs that passed, `failed` the ids
// of those that did not. `stackUp` false means the walkthrough stack never came
// up, so no walkthrough ran: the comment says so instead of blaming each one.
/**
 * @param {{ repo: string, pr: number, sha: string, headSha: string, runUrl: string,
 *   artifactUrl: string, recorded: Manifest[], failed: string[], stackUp?: boolean }} args
 * @returns {string}
 */
export function commentBody({ repo, pr, sha, headSha, runUrl, artifactUrl, recorded, failed: listed, stackUp = true }) {
  const failed = stackUp ? listed : [];
  const total = recorded.length + failed.length;
  const tally = failed.length
    ? `${plural(recorded.length, 'walkthrough')} recorded, ${failed.length} failed`
    : `${plural(recorded.length, 'walkthrough')} recorded`;
  const lines = [
    WALKTHROUGH_MARKER,
    '### Walkthroughs',
    '',
    `${tally} from \`${String(headSha).slice(0, 7)}\` in [this run](${runUrl}). ` +
      `The videos are a [workflow artifact](${artifactUrl}) (an mp4 per walkthrough, kept while the artifact lives); ` +
      `the poster and stills are committed under \`.github/screenshots/${pr}/walkthroughs/\` and embedded by commit SHA.`,
    '',
  ];
  if (!stackUp) {
    lines.push(
      `**The walkthrough stack never came up**, so no walkthrough ran (${listed.length} selected). ` +
        'This is the stack, not a walkthrough: the Record step of the run log says why.',
      '',
    );
  }
  if (failed.length) {
    lines.push(
      `**Failed** (a walkthrough is a test: a step whose result never appeared, or a recording over budget; the run log names the step, and the artifact keeps its partial stills):`,
      '',
      ...failed.map((id) => `- \`${checkId(id)}\``),
      '',
    );
  }
  for (const m of recorded) lines.push(...section(repo, pr, sha, m));
  if (total === 0 && stackUp) lines.push('Nothing was recorded.', '');
  lines.push('Re-run the workflow, or remove and re-add the `run:walkthrough` label, to record the current head again; this comment is updated in place.');
  return lines.join('\n') + '\n';
}
