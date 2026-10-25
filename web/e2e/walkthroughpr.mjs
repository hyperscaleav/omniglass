// @ts-check
// Demos as PR evidence (#925, #1132): the pure choices the run:demo workflow
// (.github/workflows/demo.yml) makes, so they are unit-tested
// (web/src/walkthrough-pr.test.ts) rather than buried in YAML. Plain ESM with no
// dependencies, so the workflow runs it on the runner's node with no build step;
// web/e2e/walkthrough-pr.mjs is its command line. Its types are the JSDoc below,
// checked by tsc (web/tsconfig.json).
//
// A demo is a recording of a PR's feature working, made by one of two
// recorders: a terminal demo (a VHS tape, scripts/demo/<id>.tape, for CLI and
// API work; web/e2e/tapelib.mjs) or a walkthrough (a Playwright script,
// web/e2e/walkthroughs/<id>.yaml, for a console flow). A demo id names one demo
// of either kind and its evidence directory.
//
// - which demos a PR records: the tapes and scripts it adds or changes, every
//   tape when it changes the terminal recorder, every walkthrough when it
//   changes anything else under web/, and every demo when it names none;
// - where their evidence is committed (#939): the PR's own ref,
//   refs/walkthroughs/pr-<n> (named when walkthroughs were the only demos),
//   never the PR branch, so the PR head's checks are untouched and a
//   squash-merge carries none of it into main; a ref outside refs/heads/ is not
//   fetched by a default clone either, yet GitHub's raw host and jsDelivr serve
//   its commits by SHA like any other;
// - which files that commit carries per demo, under <id>/: a terminal demo's
//   GIF and MP4; a walkthrough's animated preview, poster, every step's still
//   and mp4;
// - the URLs they are reached by, each pinned to the evidence commit's SHA,
//   never a ref: the raw host for the images that render inline (a terminal
//   demo's GIF; a walkthrough's preview, an animated WebP, #937), and jsDelivr
//   for every video, which it serves as video/mp4 with range requests, so the
//   browser's own player plays and seeks it (GitHub plays no inline video a
//   workflow can attach);
// - the one comment the workflow upserts, listing every demo, found again by
//   its marker (or the walkthrough comment's earlier one).

import { PREVIEW, checkId, checkPreview, checkStill, plural, videoName } from './walkthroughcore.mjs';
import { checkTapeId } from './tapelib.mjs';

/** @typedef {import('./walkthroughcore.mjs').ManifestStep} ManifestStep */
/** @typedef {import('./walkthroughcore.mjs').Manifest} Manifest */
/** @typedef {import('./tapelib.mjs').TapeManifest} TapeManifest */
/**
 * One recorder's results: the manifests of the demos that passed, the ids of
 * those that did not, and whether its stack came up at all.
 * @template M
 * @typedef {{ recorded: M[], failed: string[], stackUp?: boolean }} Results
 */

export const DEMO_MARKER = '<!-- omniglass-demo -->';
// The marker the comment carried when walkthroughs were its only demos (#925):
// the upsert finds a comment by either, so a PR labelled before demos keeps
// one comment, updated in place.
export const WALKTHROUGH_MARKER = '<!-- omniglass-walkthrough -->';
export const DEMO_MARKERS = [DEMO_MARKER, WALKTHROUGH_MARKER];

// The SHA the record job writes the comment against, before the evidence
// commit exists; the publish job (which runs nothing from the PR) replaces
// every occurrence with the evidence commit's SHA. walkthrough.yml spells it
// out in both jobs.
export const PENDING_SHA = '0'.repeat(40);

const SCRIPT_DIR = 'web/e2e/walkthroughs/';
const TAPE_DIR = 'scripts/demo/';
const SHA = /^[0-9a-f]{40}$/;
const REPO = /^[A-Za-z0-9_.-]+\/[A-Za-z0-9_.-]+$/;
// The terminal recorder's own files under web/: a change to one re-records
// every tape, and is no reason to re-record a walkthrough.
const TAPE_RECORDER = new Set(['web/e2e/tapes.sh', 'web/e2e/tapelib.mjs', 'web/e2e/tape.mjs']);
const TAPE_RECORDER_DIR = 'web/e2e/tape/';
// The stack both recorders play on: a change to it re-records every demo.
const SHARED = new Set(['web/e2e/lib/stack.sh', 'web/e2e/demo.sh']);

// The id of the top-level script under `dir` with extension `ext` that path
// `p` names, or null.
/** @param {string} p @param {string} dir @param {string} ext @returns {string | null} */
function scriptId(p, dir, ext) {
  if (!p.startsWith(dir) || !p.endsWith(ext)) return null;
  const rest = p.slice(dir.length, -ext.length);
  return rest.includes('/') ? null : rest;
}

// The ids to record, tapes first, each in the tree's order. `changed` is the
// PR's changed paths (repo-relative); `tapes` and `walkthroughs` every tape's
// and every walkthrough script's id in the tree. A PR records the tapes and
// scripts it adds or changes; every tape when it changes the terminal recorder
// (anything under scripts/demo/ but a tape, or the recorder's own files);
// every walkthrough when it changes anything else under web/ (the console,
// the walkthrough recorder, a fixture), since that can move a flow a
// walkthrough walks; every demo when it changes the stack both play on, or
// names no demo and moves no recorder (as the walkthrough label always
// recorded every walkthrough).
/** @param {string[]} changed @param {string[]} tapes @param {string[]} walkthroughs @returns {string[]} */
export function selectDemos(changed, tapes, walkthroughs) {
  tapes.forEach(checkTapeId);
  walkthroughs.forEach(checkId);
  const both = tapes.find((id) => walkthroughs.includes(id));
  if (both !== undefined) throw new Error(`demos: demo id "${both}" names both a tape and a walkthrough; a demo id names one demo`);
  if (tapes.length + walkthroughs.length === 0) throw new Error(`demos: no demos under ${TAPE_DIR} or ${SCRIPT_DIR}`);
  const touchedTapes = new Set();
  const touchedWalks = new Set();
  let allTapes = false;
  let allWalks = false;
  for (const p of changed) {
    const tape = scriptId(p, TAPE_DIR, '.tape');
    const walk = scriptId(p, SCRIPT_DIR, '.yaml');
    if (tape !== null) touchedTapes.add(tape);
    else if (walk !== null) touchedWalks.add(walk);
    else if (SHARED.has(p)) allTapes = allWalks = true;
    else if (p.startsWith(TAPE_DIR) || TAPE_RECORDER.has(p) || p.startsWith(TAPE_RECORDER_DIR)) allTapes = true;
    else if (p.startsWith('web/')) allWalks = true;
  }
  const pickedTapes = allTapes ? [...tapes] : tapes.filter((id) => touchedTapes.has(id));
  const pickedWalks = allWalks ? [...walkthroughs] : walkthroughs.filter((id) => touchedWalks.has(id));
  if (pickedTapes.length + pickedWalks.length === 0) return [...tapes, ...walkthroughs];
  return [...pickedTapes, ...pickedWalks];
}

// The ref PR `pr`'s evidence is committed to. Each run commits a fresh tree on
// top of the ref's last commit, so a renamed or deleted script leaves nothing
// behind while an older comment's SHA stays reachable.
/** @param {number} pr @returns {string} */
export function evidenceRef(pr) {
  if (!Number.isInteger(pr) || pr < 1) throw new Error(`PR number "${pr}" must be a positive integer`);
  return `refs/walkthroughs/pr-${pr}`;
}

// The evidence refs the scheduled prune (demo-prune.yml) deletes: those
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

// The files a passing terminal demo's evidence carries, as paths in the
// evidence commit's tree (each the recorder's file of that name under
// scripts/demo/out/<id>/): the GIF, then the MP4. A manifest without the budget
// mark never came from a recording that passed, and is refused.
/** @param {TapeManifest} m @returns {string[]} */
export function tapeEvidenceFiles(m) {
  const id = checkTapeId(m.id);
  if (m.gif !== `${id}.gif`) throw new Error(`tape ${id}: gif "${m.gif}" is not the recorder's name (${id}.gif)`);
  if (m.mp4 !== `${id}.mp4`) throw new Error(`tape ${id}: mp4 "${m.mp4}" is not the recorder's name (${id}.mp4)`);
  if (!m.budget) throw new Error(`tape ${id}: its manifest has no budget mark, so the recording never passed`);
  return [`${id}/${m.gif}`, `${id}/${m.mp4}`];
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

// A length in seconds as minutes and seconds ("1m 8s", "42s").
/** @param {number} seconds @returns {string} */
function length(seconds) {
  const s = Math.round(seconds);
  return s >= 60 ? `${Math.floor(s / 60)}m ${s % 60}s` : `${s}s`;
}

// One terminal demo's section: title, description, the scrubbable video's
// link, then the GIF, which plays inline.
/** @param {string} repo @param {string} sha @param {TapeManifest} m @returns {string[]} */
function tapeSection(repo, sha, m) {
  const [gif, mp4] = tapeEvidenceFiles(m); // validates the id, names and mark before any reaches a URL
  const lines = [`#### ${esc(m.title)}`, ''];
  if (m.description) lines.push(esc(m.description), '');
  const took = m.budget ? `, ${length(m.budget.seconds)}` : '';
  lines.push(`\`${m.id}\`, terminal demo${took}: [Watch (scrubbable)](${cdnUrl(repo, sha, mp4)})`, '');
  lines.push(`![${esc(m.title)}: terminal recording](${rawUrl(repo, sha, gif)})`, '');
  return lines;
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
  lines.push(`\`${m.id}\`, walkthrough, ${plural(m.steps.length, 'step')}: [Watch (scrubbable)](${cdnUrl(repo, sha, `${m.id}/${videoName(m.id)}`)})`, '');
  if (m.preview !== undefined) lines.push(`![${esc(m.title)}: animated preview](${url(PREVIEW)})`, '');
  lines.push(`![${esc(m.title)}: poster](${url('poster.png')})`, '');
  lines.push('<details><summary>Every step, as recorded</summary>', '');
  for (const s of m.steps) {
    lines.push(`${s.n}. ${esc(s.caption)}`, '', `   ![${m.id} step ${s.n}](${url(s.still)})`, '');
  }
  lines.push('</details>', '');
  return lines;
}

// The comment body: one comment listing every demo the run recorded, terminal
// demos first, then walkthroughs. `sha` is the evidence commit (what every
// file is reached by), `headSha` the PR head they were recorded from;
// `tapes` and `walkthroughs` each recorder's results: the manifests
// (demo.json, walkthrough.json) of the demos that passed and the ids of those
// that did not. A recorder whose `stackUp` is false never came up, so none of
// its demos ran: the comment says so instead of blaming each one.
/**
 * @param {{ repo: string, pr: number, sha: string, headSha: string, runUrl: string,
 *   artifactUrl: string, tapes: Results<TapeManifest>, walkthroughs: Results<Manifest> }} args
 * @returns {string}
 */
export function commentBody({ repo, pr, sha, headSha, runUrl, artifactUrl, tapes, walkthroughs }) {
  const tapesUp = tapes.stackUp !== false;
  const walksUp = walkthroughs.stackUp !== false;
  const failed = [
    ...(tapesUp ? tapes.failed.map((id) => `- \`${checkTapeId(id)}\` (terminal demo)`) : []),
    ...(walksUp ? walkthroughs.failed.map((id) => `- \`${checkId(id)}\` (walkthrough)`) : []),
  ];
  const kinds = [
    ...(tapes.recorded.length ? [plural(tapes.recorded.length, 'terminal demo')] : []),
    ...(walkthroughs.recorded.length ? [plural(walkthroughs.recorded.length, 'walkthrough')] : []),
  ];
  const tally = `${kinds.length ? kinds.join(' and ') : '0 demos'} recorded${failed.length ? `, ${failed.length} failed` : ''}`;
  const lines = [
    DEMO_MARKER,
    '### Demos',
    '',
    `${tally} from \`${String(headSha).slice(0, 7)}\` in [this run](${runUrl}). ` +
      `Each demo's files are committed to \`${evidenceRef(pr)}\` (off the PR branch, so none of it reaches main) ` +
      `and reached by that commit's SHA; the [workflow artifact](${artifactUrl}) also keeps a failed demo's partial output.`,
    '',
  ];
  if (!tapesUp) {
    lines.push(
      `**The terminal demos' stack never came up**, so no terminal demo ran (${tapes.failed.length} selected). ` +
        'This is the stack, not a tape: the Record step of the run log says why.',
      '',
    );
  }
  if (!walksUp) {
    lines.push(
      `**The walkthrough stack never came up**, so no walkthrough ran (${walkthroughs.failed.length} selected). ` +
        'This is the stack, not a walkthrough: the Record step of the run log says why.',
      '',
    );
  }
  if (failed.length) {
    lines.push(
      '**Failed** (a demo is a test: a terminal demo fails when a command it typed exited non-zero or a wait ran out, a walkthrough when a ' +
        "step's result never appeared, and either when its recording is over budget; the run log names the cause, and the artifact keeps the partial output):",
      '',
      ...failed,
      '',
    );
  }
  for (const m of tapes.recorded) lines.push(...tapeSection(repo, sha, m));
  for (const m of walkthroughs.recorded) lines.push(...section(repo, sha, m));
  if (!kinds.length && !failed.length && tapesUp && walksUp) lines.push('Nothing was recorded.', '');
  lines.push('Re-run the workflow, or remove and re-add the `run:demo` label, to record the current head again; this comment is updated in place.');
  return lines.join('\n') + '\n';
}
