// @ts-check
// Walkthroughs as PR evidence (#925): the pure choices the run:walkthrough
// workflow (.github/workflows/walkthrough.yml) makes, so they are unit-tested
// (web/src/walkthrough-pr.test.ts) rather than buried in YAML. Plain ESM with no
// dependencies, so the workflow runs it on the runner's node with no build step;
// web/e2e/walkthrough-pr.mjs is its command line. Its types are the JSDoc below,
// checked by tsc (web/tsconfig.json).
//
// - which walkthroughs a PR records: the scripts it adds or changes, else all;
// - where their stills are committed: .github/screenshots/<pr>/walkthroughs/<id>/
//   (ruled on #884), so the evidence that matters for review renders inline and
//   outlives the workflow artifact that carries the video;
// - the raw URL each still is embedded by: an immutable commit SHA, never a
//   branch, so the link survives the branch being deleted on squash-merge;
// - the one comment the workflow upserts, found again by its marker.

/** @typedef {{ n: number, kind: string, caption: string, still: string }} ManifestStep */
/** @typedef {{ id: string, title: string, description?: string, steps: ManifestStep[], poster: string }} Manifest */

export const WALKTHROUGH_MARKER = '<!-- omniglass-walkthrough -->';

const SCRIPT_DIR = 'web/e2e/walkthroughs/';
const ID = /^[a-z0-9][a-z0-9-]*$/;
const SHA = /^[0-9a-f]{40}$/;
const REPO = /^[A-Za-z0-9_.-]+\/[A-Za-z0-9_.-]+$/;
const STILL = /^step-\d+\.png$/;

/** @param {unknown} id @returns {string} */
function checkId(id) {
  if (typeof id !== 'string' || !ID.test(id)) {
    throw new Error(`walkthrough id "${id}" must be lowercase letters, digits and dashes (it names a directory)`);
  }
  return id;
}

// The ids to record, in the tree's order. `changed` is the PR's changed paths
// (repo-relative), `all` every script's id in the tree. A script the PR adds or
// changes is recorded alone; a PR that touches no script records every one,
// since any console change can move a flow a walkthrough walks.
/** @param {string[]} changed @param {string[]} all @returns {string[]} */
export function selectWalkthroughs(changed, all) {
  all.forEach(checkId);
  if (all.length === 0) throw new Error(`walkthroughs: no walkthroughs under ${SCRIPT_DIR}`);
  const touched = new Set();
  for (const p of changed) {
    if (!p.startsWith(SCRIPT_DIR) || !p.endsWith('.yaml')) continue;
    const rest = p.slice(SCRIPT_DIR.length, -'.yaml'.length);
    if (!rest.includes('/')) touched.add(rest);
  }
  const picked = all.filter((id) => touched.has(id));
  return picked.length ? picked : [...all];
}

// Where a walkthrough's poster and stills are committed for PR `pr`.
/** @param {number} pr @param {string} id @returns {string} */
export function stillsDir(pr, id) {
  if (!Number.isInteger(pr) || pr < 1) throw new Error(`PR number "${pr}" must be a positive integer`);
  return `.github/screenshots/${pr}/walkthroughs/${checkId(id)}`;
}

// The raw URL of `path` at commit `sha` of `repo` (owner/name).
/** @param {string} repo @param {string} sha @param {string} path @returns {string} */
export function rawUrl(repo, sha, path) {
  if (!REPO.test(repo)) throw new Error(`repository "${repo}" must be owner/name`);
  if (!SHA.test(sha)) throw new Error(`"${sha}" must be a full commit SHA (an immutable link, never a branch)`);
  return `https://raw.githubusercontent.com/${repo}/${sha}/${path.split('/').map(encodeURIComponent).join('/')}`;
}

// Script text into Markdown: no HTML, and no brackets that could open a link.
/** @param {string} s @returns {string} */
function esc(s) {
  return String(s)
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/([[\]])/g, '\\$1');
}

/** @type {(n: number, word: string) => string} */
const plural = (n, word) => `${n} ${word}${n === 1 ? '' : 's'}`;

/** @param {string} id @param {string} name */
function checkStill(id, name) {
  if (typeof name !== 'string' || !STILL.test(name)) {
    throw new Error(`walkthrough ${id}: still "${name}" is not a recorder still name (step-NN.png)`);
  }
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
// of those that did not.
/**
 * @param {{ repo: string, pr: number, sha: string, headSha: string, runUrl: string,
 *   artifactUrl: string, recorded: Manifest[], failed: string[] }} args
 * @returns {string}
 */
export function commentBody({ repo, pr, sha, headSha, runUrl, artifactUrl, recorded, failed }) {
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
  if (failed.length) {
    lines.push(
      `**Failed** (a walkthrough is a test: a step whose result never appeared, or a recording over budget; the run log names the step, and the artifact keeps its partial stills):`,
      '',
      ...failed.map((id) => `- \`${checkId(id)}\``),
      '',
    );
  }
  for (const m of recorded) lines.push(...section(repo, pr, sha, m));
  if (total === 0) lines.push('Nothing was recorded.', '');
  lines.push('Re-run the workflow, or remove and re-add the `run:walkthrough` label, to record the current head again; this comment is updated in place.');
  return lines.join('\n') + '\n';
}
