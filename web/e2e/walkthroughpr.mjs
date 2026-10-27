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
// - which demos a PR records (#1180): exactly the demos its body tags
//   (`demo: <id> ...` in its Demo section), plus any tape or walkthrough
//   script it adds or changes; nothing fans out from what else it touches,
//   and the freshness run on main records every demo instead;
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
//   its marker (or the walkthrough comment's earlier one), and the clip it
//   writes into the PR body's Demo section between its own markers (#1180);
// - the freshness report the nightly run on main files when a demo fails.

import { ID, PREVIEW, checkId, checkPreview, checkStill, plural, videoName } from './walkthroughcore.mjs';
import { checkTapeId } from './tapelib.mjs';

/** @typedef {import('./walkthroughcore.mjs').ManifestStep} ManifestStep */
/** @typedef {import('./walkthroughcore.mjs').Manifest} Manifest */
/** @typedef {import('./tapelib.mjs').TapeManifest} TapeManifest */
/**
 * One recorder's results: the manifests of the demos that passed, the ids of
 * those that ran and failed, and the ids of those that never ran (their lab
 * never came up, or their job ended before recording).
 * @template M
 * @typedef {{ recorded: M[], failed: string[], unstarted: string[] }} Results
 */
/**
 * What a PR body's demo tag says: the demos it names, or none, with why.
 * @typedef {{ ids: string[] } | { none: string }} DemoTag
 */
/**
 * One demo to record, the recorder that plays it, and why it was chosen.
 * @typedef {{ id: string, kind: 'tape' | 'walkthrough', why: string }} Selected
 */

export const DEMO_MARKER = '<!-- omniglass-demo -->';
// The marker the comment carried when walkthroughs were its only demos (#925):
// the upsert finds a comment by either, so a PR labelled before demos keeps
// one comment, updated in place.
export const WALKTHROUGH_MARKER = '<!-- omniglass-walkthrough -->';
export const DEMO_MARKERS = [DEMO_MARKER, WALKTHROUGH_MARKER];
// The edges of the clip the publish job writes into the PR body's Demo
// section (#1180): it replaces only what lies between them, so the author's
// text around the block is never touched. demo.yml spells them out too.
export const CLIP_START = '<!-- omniglass-demo-clip -->';
export const CLIP_END = '<!-- /omniglass-demo-clip -->';
// The marker the freshness run's issue opens with, which it is found again by.
export const FRESHNESS_MARKER = '<!-- omniglass-demo-freshness -->';

// The SHA the record job writes the comment against, before the evidence
// commit exists; the publish job (which runs nothing from the PR) replaces
// every occurrence with the evidence commit's SHA. demo.yml spells it out in
// both jobs.
export const PENDING_SHA = '0'.repeat(40);

const SCRIPT_DIR = 'web/e2e/walkthroughs/';
const TAPE_DIR = 'scripts/demo/';
const SHA = /^[0-9a-f]{40}$/;
const REPO = /^[A-Za-z0-9_.-]+\/[A-Za-z0-9_.-]+$/;
// A tag line: `demo:` opening a line of its own (a list bullet allowed), and
// what follows it.
const TAG_LINE = /^[ \t]*(?:[-*+][ \t]+)?demo:(.*)$/;
const FENCE = /^[ \t]*(?:```|~~~)/;
const NONE = /^none[ \t]*\((.*)\)$/;

// The id of the top-level script under `dir` with extension `ext` that path
// `p` names, or null.
/** @param {string} p @param {string} dir @param {string} ext @returns {string | null} */
function scriptId(p, dir, ext) {
  if (!p.startsWith(dir) || !p.endsWith(ext)) return null;
  const rest = p.slice(dir.length, -ext.length);
  return rest.includes('/') ? null : rest;
}

// The lines of `body` a reader sees as text: an HTML comment blanked out (its
// characters, not its newlines, so line numbers hold) and a fenced code
// block's lines dropped, so a tag quoted by the PR template or shown as an
// example is never read as the PR's own. Each entry is [line number, text].
/** @param {string} body @returns {[number, string][]} */
export function visibleLines(body) {
  const masked = String(body ?? '').replace(/<!--[\s\S]*?-->/g, (c) => c.replace(/[^\n]/g, ' '));
  /** @type {[number, string][]} */
  const out = [];
  let fenced = false;
  masked.split('\n').forEach((raw, i) => {
    const line = raw.replace(/\r$/, '');
    if (FENCE.test(line)) fenced = !fenced;
    else if (!fenced) out.push([i, line]);
  });
  return out;
}

// The demo tag `body` (a PR body) carries, or null when no line is one. A tag
// is a line of its own in the Demo section: `demo: <id> [<id>...]` (spaces or
// commas between), or `demo: none (<reason>)`. Every tag line counts, ids
// once each in order; a word that is not a demo id, `none` with no reason in
// parentheses, and `none` beside a demo are refused, naming the problem.
// Whether each id names a demo in the tree is selectDemos's to check.
/** @param {string} body @returns {DemoTag | null} */
export function demoTag(body) {
  /** @type {string[]} */
  const ids = [];
  /** @type {string[]} */
  const nones = [];
  for (const [, line] of visibleLines(body)) {
    const m = TAG_LINE.exec(line);
    if (!m) continue;
    const value = m[1].trim();
    if (value === 'none' || value.startsWith('none ') || value.startsWith('none(')) {
      const n = NONE.exec(value);
      const reason = n ? n[1].trim() : '';
      if (!reason) throw new Error('demo tag: say why a PR has no demo, as demo: none (<reason>)');
      nones.push(reason);
      continue;
    }
    const words = value.split(/[\s,]+/).filter(Boolean);
    if (!words.length) throw new Error('demo tag: demo: names no demo (write demo: <id>, or demo: none (<reason>))');
    for (const w of words) {
      if (!ID.test(w)) throw new Error(`demo tag: "${w}" is not a demo id (lowercase letters, digits and hyphens)`);
      if (!ids.includes(w)) ids.push(w);
    }
  }
  if (nones.length && ids.length) throw new Error('demo tag: a PR either names demos or says none, not both');
  if (nones.length > 1) throw new Error('demo tag: a PR says none once');
  if (nones.length) return { none: nones[0] };
  return ids.length ? { ids } : null;
}

/** @param {string[]} tapes @param {string[]} walkthroughs */
function checkTree(tapes, walkthroughs) {
  tapes.forEach(checkTapeId);
  walkthroughs.forEach(checkId);
  const both = tapes.find((id) => walkthroughs.includes(id));
  if (both !== undefined) throw new Error(`demos: demo id "${both}" names both a tape and a walkthrough; a demo id names one demo`);
  if (tapes.length + walkthroughs.length === 0) throw new Error(`demos: no demos under ${TAPE_DIR} or ${SCRIPT_DIR}`);
}

// The demos a PR records (#1180), tapes first, each in the tree's order, with
// the recorder that plays each and why. `changed` is the PR's changed paths
// (repo-relative), `tag` its body's demo tag (demoTag), and `tapes` and
// `walkthroughs` every tape's and walkthrough script's id in the tree. A PR
// records exactly the demos it tags, plus any tape or walkthrough script it
// adds or changes (a demo is a test, so a changed one is played). Nothing
// else: a change to the console, a recorder, the lab or this pipeline
// records no demo it did not name, since a demo it could break is the
// freshness run's to catch (everyDemo). A tag naming a demo the tree does not
// have is refused, naming it.
/**
 * @param {string[]} changed
 * @param {DemoTag | null} tag
 * @param {string[]} tapes
 * @param {string[]} walkthroughs
 * @returns {Selected[]}
 */
export function selectDemos(changed, tag, tapes, walkthroughs) {
  checkTree(tapes, walkthroughs);
  const tagged = new Set(tag && 'ids' in tag ? tag.ids : []);
  for (const id of tagged) {
    if (!tapes.includes(id) && !walkthroughs.includes(id)) {
      throw new Error(`demo tag names no demo "${id}" (neither ${TAPE_DIR}${id}.tape nor ${SCRIPT_DIR}${id}.yaml)`);
    }
  }
  const touched = new Set();
  for (const p of changed) {
    const id = scriptId(p, TAPE_DIR, '.tape') ?? scriptId(p, SCRIPT_DIR, '.yaml');
    if (id !== null) touched.add(id);
  }
  /** @type {(id: string) => string} */
  const why = (id) => [...(tagged.has(id) ? ['tagged'] : []), ...(touched.has(id) ? ['changed by the PR'] : [])].join(', and ');
  return [
    ...tapes.filter((id) => tagged.has(id) || touched.has(id)).map((id) => ({ id, kind: /** @type {const} */ ('tape'), why: why(id) })),
    ...walkthroughs.filter((id) => tagged.has(id) || touched.has(id)).map((id) => ({ id, kind: /** @type {const} */ ('walkthrough'), why: why(id) })),
  ];
}

// Every demo in the tree, tapes first, as the freshness run on main records
// them: the run that catches a demo a change broke without naming it.
/** @param {string[]} tapes @param {string[]} walkthroughs @returns {Selected[]} */
export function everyDemo(tapes, walkthroughs) {
  checkTree(tapes, walkthroughs);
  const why = 'the freshness run records every demo';
  return [
    ...tapes.map((id) => ({ id, kind: /** @type {const} */ ('tape'), why })),
    ...walkthroughs.map((id) => ({ id, kind: /** @type {const} */ ('walkthrough'), why })),
  ];
}

// What a selected demo's recording says happened: recorded (a manifest with
// the budget mark only a pass writes, not listed as failed), failed (it ran
// and did not pass), or unstarted (its lab never came up, or its job ended
// first). In the run:demo workflow each demo's leg marks the commit it
// checked out (`legHead`) before anything can fail, and a leg that marked
// another commit than the run's (`headSha`), or none, is an earlier attempt's
// recording left in the run, never this run's: it counts as unstarted (#1180).
// With no leg (make demo on this machine) there is no commit to compare.
/**
 * @param {{ manifest: { budget?: unknown } | null, listedFailed: boolean, stackUp: boolean,
 *   legHead?: string, headSha?: string }} rec
 * @returns {'recorded' | 'failed' | 'unstarted'}
 */
export function legOutcome({ manifest, listedFailed, stackUp, legHead, headSha }) {
  if (headSha !== undefined && legHead !== headSha) return 'unstarted';
  if (manifest && manifest.budget && !listedFailed) return 'recorded';
  return stackUp ? 'failed' : 'unstarted';
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

// The ids of a run's demos that failed and that never ran, each with its
// kind, as Markdown list items; every id checked first.
/**
 * @param {Results<TapeManifest>} tapes
 * @param {Results<Manifest>} walkthroughs
 * @returns {{ failed: string[], unstarted: string[] }}
 */
function misses(tapes, walkthroughs) {
  const item = (/** @type {string} */ id, /** @type {string} */ kind) => `- \`${id}\` (${kind})`;
  return {
    failed: [...tapes.failed.map((id) => item(checkTapeId(id), 'terminal demo')), ...walkthroughs.failed.map((id) => item(checkId(id), 'walkthrough'))],
    unstarted: [...tapes.unstarted.map((id) => item(checkTapeId(id), 'terminal demo')), ...walkthroughs.unstarted.map((id) => item(checkId(id), 'walkthrough'))],
  };
}

// The tally of a run: what it recorded by kind, then how many failed and how
// many never ran.
/** @param {Results<TapeManifest>} tapes @param {Results<Manifest>} walkthroughs @param {number} failed @param {number} unstarted */
function tally(tapes, walkthroughs, failed, unstarted) {
  const kinds = [
    ...(tapes.recorded.length ? [plural(tapes.recorded.length, 'terminal demo')] : []),
    ...(walkthroughs.recorded.length ? [plural(walkthroughs.recorded.length, 'walkthrough')] : []),
  ];
  return `${kinds.length ? kinds.join(' and ') : '0 demos'} recorded${failed ? `, ${failed} failed` : ''}${unstarted ? `, ${unstarted} did not run` : ''}`;
}

const FAILED_NOTE =
  '**Failed** (a demo is a test: a terminal demo fails when a command it typed exited non-zero or a wait ran out, a walkthrough when a ' +
  "step's result never appeared, and either when its recording is over budget; the run log names the cause, and the artifact keeps the partial output):";
const UNSTARTED_NOTE =
  '**Did not run** (its lab never came up, or its record job ended before recording: this is the lab or the runner, not the demo, ' +
  "and that demo's record job log says why):";

// The comment body: one comment listing every demo the run recorded, terminal
// demos first, then walkthroughs. `sha` is the evidence commit (what every
// file is reached by), `headSha` the PR head they were recorded from;
// `tapes` and `walkthroughs` each recorder's results: the manifests
// (demo.json, walkthrough.json) of the demos that passed, the ids of those
// that failed, and the ids of those that never ran (each demo records on a
// lab of its own, #1180, #1197, so a lab that never came up costs that demo
// alone, and is named as the lab's fault rather than the demo's).
/**
 * @param {{ repo: string, pr: number, sha: string, headSha: string, runUrl: string,
 *   artifactUrl: string, tapes: Results<TapeManifest>, walkthroughs: Results<Manifest> }} args
 * @returns {string}
 */
export function commentBody({ repo, pr, sha, headSha, runUrl, artifactUrl, tapes, walkthroughs }) {
  const { failed, unstarted } = misses(tapes, walkthroughs);
  const lines = [
    DEMO_MARKER,
    '### Demos',
    '',
    `${tally(tapes, walkthroughs, failed.length, unstarted.length)} from \`${String(headSha).slice(0, 7)}\` in [this run](${runUrl}). ` +
      `Each demo's files are committed to \`${evidenceRef(pr)}\` (off the PR branch, so none of it reaches main) ` +
      `and reached by that commit's SHA; the [workflow artifacts](${artifactUrl}) also keep a failed demo's partial output.`,
    '',
  ];
  if (failed.length) lines.push(FAILED_NOTE, '', ...failed, '');
  if (unstarted.length) lines.push(UNSTARTED_NOTE, '', ...unstarted, '');
  for (const m of tapes.recorded) lines.push(...tapeSection(repo, sha, m));
  for (const m of walkthroughs.recorded) lines.push(...section(repo, sha, m));
  if (!tapes.recorded.length && !walkthroughs.recorded.length && !failed.length && !unstarted.length) lines.push('Nothing was recorded.', '');
  lines.push(
    "The PR body's Demo section carries each recording's clip. Re-run the workflow, or remove and re-add the `run:demo` label, " +
      'to record the current head again; this comment and the clip are updated in place.',
  );
  return lines.join('\n') + '\n';
}

// The clip the publish job writes into the PR body's Demo section, between
// CLIP_START and CLIP_END (#1180): per demo, terminal demos first, a line
// with its title, id, kind and length linking its scrubbable video, then what
// plays inline (a terminal demo's GIF; a walkthrough's animated preview, else
// its poster), all pinned to the evidence commit's SHA; then the demos that
// failed or never ran. The full record (every step's still, the run, the
// artifacts) stays in the demo comment, which the publish job links below the
// clip. Every title is escaped, so the clip carries no HTML and can never
// close its own block.
/**
 * @param {{ repo: string, sha: string, headSha: string, tapes: Results<TapeManifest>, walkthroughs: Results<Manifest> }} args
 * @returns {string}
 */
export function clipBody({ repo, sha, headSha, tapes, walkthroughs }) {
  const lines = [`Recorded from \`${String(headSha).slice(0, 7)}\` by \`run:demo\`:`, ''];
  for (const m of tapes.recorded) {
    const [gif, mp4] = tapeEvidenceFiles(m);
    const took = m.budget ? `, ${length(m.budget.seconds)}` : '';
    lines.push(`**${esc(m.title)}** (\`${m.id}\`, terminal demo${took}): [Watch (scrubbable)](${cdnUrl(repo, sha, mp4)})`, '');
    lines.push(`![${esc(m.title)}: terminal recording](${rawUrl(repo, sha, gif)})`, '');
  }
  for (const m of walkthroughs.recorded) {
    evidenceFiles(m);
    lines.push(
      `**${esc(m.title)}** (\`${m.id}\`, walkthrough, ${plural(m.steps.length, 'step')}): [Watch (scrubbable)](${cdnUrl(repo, sha, `${m.id}/${videoName(m.id)}`)})`,
      '',
    );
    lines.push(
      m.preview !== undefined
        ? `![${esc(m.title)}: animated preview](${rawUrl(repo, sha, `${m.id}/${PREVIEW}`)})`
        : `![${esc(m.title)}: poster](${rawUrl(repo, sha, `${m.id}/poster.png`)})`,
      '',
    );
  }
  const { failed, unstarted } = misses(tapes, walkthroughs);
  const inline = (/** @type {string[]} */ items) => items.map((i) => i.slice(2)).join(', ');
  if (failed.length) lines.push(`**Failed:** ${inline(failed)}.`, '');
  if (unstarted.length) lines.push(`**Did not run:** ${inline(unstarted)}.`, '');
  return lines.join('\n').trimEnd() + '\n';
}

// The freshness run's verdict on main (#1180): ok when every demo recorded
// green; a one-line summary (the closing note on a green run); and the body
// of the issue it opens or updates when one did not, naming each demo that
// failed or never ran with its kind, and how to reproduce it.
/**
 * @param {{ runUrl: string, headSha: string, tapes: Results<TapeManifest>, walkthroughs: Results<Manifest> }} args
 * @returns {{ ok: boolean, summary: string, body: string }}
 */
export function freshnessReport({ runUrl, headSha, tapes, walkthroughs }) {
  const { failed, unstarted } = misses(tapes, walkthroughs);
  const total = tapes.recorded.length + walkthroughs.recorded.length + failed.length + unstarted.length;
  const head = `\`${String(headSha).slice(0, 7)}\``;
  const ok = failed.length + unstarted.length === 0;
  const summary = ok
    ? `All ${plural(total, 'demo')} recorded green on \`main\` at ${head} in [this run](${runUrl}).`
    : `${failed.length + unstarted.length} of ${plural(total, 'demo')} did not record green on \`main\` at ${head} in [this run](${runUrl}).`;
  const first = [...tapes.failed, ...walkthroughs.failed, ...tapes.unstarted, ...walkthroughs.unstarted][0];
  const lines = [
    FRESHNESS_MARKER,
    'The freshness run re-records every demo on `main` each night, since a PR records only the demos it tags (#1180), so a change ' +
      'can break a demo it never named.',
    '',
    summary,
    '',
  ];
  if (failed.length) lines.push(FAILED_NOTE, '', ...failed, '');
  if (unstarted.length) lines.push(UNSTARTED_NOTE, '', ...unstarted, '');
  if (!ok) {
    lines.push(
      `Each demo's partial output and its lab's logs are in the run's \`demo-leg-<id>\` artifact. Reproduce one with \`make demo ID=${first}\`, ` +
        'fix the feature or the demo, and the next green run closes this issue.',
    );
  }
  return { ok, summary, body: lines.join('\n').trimEnd() + '\n' };
}
