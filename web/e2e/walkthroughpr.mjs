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
// - where their evidence is committed (#939): the PR's own ref,
//   refs/walkthroughs/pr-<n>, never the PR branch, so the PR head's checks are
//   untouched and a squash-merge carries none of it into main; a ref outside
//   refs/heads/ is not fetched by a default clone either, yet GitHub's raw host
//   and jsDelivr serve its commits by SHA like any other;
// - which files that commit carries per walkthrough: the animated preview, the
//   poster, every step's still and the mp4, under <id>/;
// - the URLs they are reached by, each pinned to the evidence commit's SHA,
//   never a ref: the raw host for the images that render inline (the preview,
//   an animated WebP, is what plays on the PR page, #937), and jsDelivr for the
//   video, which it serves as video/mp4 with range requests, so the browser's
//   own player plays and seeks it (GitHub plays no inline video a workflow can
//   attach);
// - the one comment the workflow upserts, found again by its marker.

import { PREVIEW, checkId, checkPreview, checkStill, plural, videoName } from './walkthroughcore.mjs';

/** @typedef {import('./walkthroughcore.mjs').ManifestStep} ManifestStep */
/** @typedef {import('./walkthroughcore.mjs').Manifest} Manifest */

export const WALKTHROUGH_MARKER = '<!-- omniglass-walkthrough -->';

// The SHA the record job writes the comment against, before the evidence
// commit exists; the publish job (which runs nothing from the PR) replaces
// every occurrence with the evidence commit's SHA. walkthrough.yml spells it
// out in both jobs.
export const PENDING_SHA = '0'.repeat(40);

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

// The ref PR `pr`'s evidence is committed to. Each run commits a fresh tree on
// top of the ref's last commit, so a renamed or deleted script leaves nothing
// behind while an older comment's SHA stays reachable.
/** @param {number} pr @returns {string} */
export function evidenceRef(pr) {
  if (!Number.isInteger(pr) || pr < 1) throw new Error(`PR number "${pr}" must be a positive integer`);
  return `refs/walkthroughs/pr-${pr}`;
}

// The evidence refs the scheduled prune (walkthrough-prune.yml) deletes: those
// of PRs closed more than `days` ago. A ref outside refs/walkthroughs/pr-<n>,
// one whose PR is not in `prs`, an open PR's, and a closed PR's with no
// readable close time are all kept: the prune only ever deletes what it can
// prove is stale. A deleted ref's files go stale in its PR's comment.
/**
 * @param {string[]} refs
 * @param {{ number: number, state: string, closed_at: string | null }[]} prs
 * @param {Date} now
 * @param {number} days
 * @returns {string[]}
 */
export function evidenceRefsToPrune(refs, prs, now, days) {
  if (!Number.isInteger(days) || days < 1) throw new Error(`retention "${days}" must be a positive whole number of days`);
  const byNumber = new Map(prs.map((p) => [p.number, p]));
  const cutoff = now.getTime() - days * 86_400_000;
  return refs.filter((ref) => {
    const m = /^refs\/walkthroughs\/pr-([1-9][0-9]*)$/.exec(ref);
    const pr = m ? byNumber.get(Number(m[1])) : undefined;
    if (!pr || pr.state !== 'closed' || !pr.closed_at) return false;
    const closed = Date.parse(pr.closed_at);
    return Number.isFinite(closed) && closed < cutoff;
  });
}

// The files a passing walkthrough's evidence carries, as paths in the evidence
// commit's tree (each the recorder's file of that name under out/<id>/): the
// animated preview when the recording has one, the poster, every step's
// still, then the video.
/** @param {Manifest} m @returns {string[]} */
export function evidenceFiles(m) {
  const id = checkId(m.id);
  checkStill(id, m.poster);
  m.steps.forEach((s) => checkStill(id, s.still));
  if (m.preview !== undefined) checkPreview(id, m.preview);
  const names = [...(m.preview !== undefined ? [PREVIEW] : []), 'poster.png', ...m.steps.map((s) => s.still), videoName(id)];
  return names.map((f) => `${id}/${f}`);
}

/** @param {string} repo @param {string} sha */
function checkPin(repo, sha) {
  if (!REPO.test(repo)) throw new Error(`repository "${repo}" must be owner/name`);
  if (!SHA.test(sha)) throw new Error(`"${sha}" must be a full commit SHA (an immutable link, never a branch)`);
}

/** @param {string} path @returns {string} */
const encodePath = (path) => path.split('/').map(encodeURIComponent).join('/');

// The raw URL of `path` at commit `sha` of `repo` (owner/name).
/** @param {string} repo @param {string} sha @param {string} path @returns {string} */
export function rawUrl(repo, sha, path) {
  checkPin(repo, sha);
  return `https://raw.githubusercontent.com/${repo}/${sha}/${encodePath(path)}`;
}

// The jsDelivr URL of `path` at commit `sha` of `repo`: what a video is linked
// by, since jsDelivr serves a public repository's mp4 as video/mp4 with range
// requests (the raw host serves it as an octet stream, which a browser
// downloads rather than plays).
/** @param {string} repo @param {string} sha @param {string} path @returns {string} */
export function cdnUrl(repo, sha, path) {
  checkPin(repo, sha);
  return `https://cdn.jsdelivr.net/gh/${repo}@${sha}/${encodePath(path)}`;
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

// One walkthrough's section: title, description, the scrubbable video's link,
// the animated preview when the recording has one, the poster, then every
// step's caption over its still, folded so the comment stays scannable.
/** @param {string} repo @param {string} sha @param {Manifest} m @returns {string[]} */
function section(repo, sha, m) {
  evidenceFiles(m); // validates the id, stills and preview before any reaches a URL
  const url = (/** @type {string} */ f) => rawUrl(repo, sha, `${m.id}/${f}`);
  const lines = [`#### ${esc(m.title)}`, ''];
  if (m.description) lines.push(esc(m.description), '');
  lines.push(`\`${m.id}\`, ${plural(m.steps.length, 'step')}: [Watch (scrubbable)](${cdnUrl(repo, sha, `${m.id}/${videoName(m.id)}`)})`, '');
  if (m.preview !== undefined) lines.push(`![${esc(m.title)}: animated preview](${url(PREVIEW)})`, '');
  lines.push(`![${esc(m.title)}: poster](${url('poster.png')})`, '');
  lines.push('<details><summary>Every step, as recorded</summary>', '');
  for (const s of m.steps) {
    lines.push(`${s.n}. ${esc(s.caption)}`, '', `   ![${m.id} step ${s.n}](${url(s.still)})`, '');
  }
  lines.push('</details>', '');
  return lines;
}

// The comment body. `sha` is the evidence commit (what every file is reached
// by), `headSha` the PR head they were recorded from; `recorded` the
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
      `Each walkthrough's video, animated preview, poster and stills are committed to \`${evidenceRef(pr)}\` (off the PR branch, so none of it reaches main) ` +
      `and reached by that commit's SHA; the [workflow artifact](${artifactUrl}) also keeps a failed walkthrough's partial stills.`,
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
  for (const m of recorded) lines.push(...section(repo, sha, m));
  if (total === 0 && stackUp) lines.push('Nothing was recorded.', '');
  lines.push('Re-run the workflow, or remove and re-add the `run:walkthrough` label, to record the current head again; this comment is updated in place.');
  return lines.join('\n') + '\n';
}
