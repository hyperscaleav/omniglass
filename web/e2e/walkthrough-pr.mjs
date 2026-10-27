// The command line of walkthroughpr.mjs (#925, #1132, #1180), run by the
// run:demo workflow on the runner's node (no dependencies, no build step). A
// demo id names a tape (scripts/demo/<id>.tape, a terminal demo) or a
// walkthrough script (web/e2e/walkthroughs/<id>.yaml), never both:
//
//   node web/e2e/walkthrough-pr.mjs select < changed.txt
//       the demos a PR records, as JSON [{id, kind, why}, ...], tapes first:
//       those its body (env PR_BODY) tags with demo: <id>, plus any tape or
//       walkthrough script among the changed paths (one per line on stdin);
//       a tag naming no demo in the tree fails, naming it
//   node web/e2e/walkthrough-pr.mjs every
//       every demo in the tree, the same JSON: what the freshness run records
//   node web/e2e/walkthrough-pr.mjs tag
//       the demo tag lint: the PR body (env PR_BODY) must tag its demos
//       (demo: <id> ...), each naming a demo in the tree, or say demo: none
//       (<reason>); prints what it read, and exits 1 naming any problem
//   node web/e2e/walkthrough-pr.mjs ref <pr>
//       the ref the PR's demo evidence is committed to
//   node web/e2e/walkthrough-pr.mjs files <id>
//       the files that demo's evidence carries, one per line, each a path both
//       under its recorder's out/ and in the evidence commit's tree (read from
//       its out/<id>/demo.json or walkthrough.json)
//   node web/e2e/walkthrough-pr.mjs stage <dir>
//       copies the evidence of every selected demo that passed (env IDS) into
//       <dir>, at its path in the evidence commit's tree; a file the manifest
//       names but the recorder left out is warned about and skipped
//   node web/e2e/walkthrough-pr.mjs prune <days> < state.json
//       the evidence refs to delete, one per line, from {"refs": [...], "prs":
//       [{number, state, closed_at}, ...]} on stdin: those of PRs closed more
//       than <days> ago (demo-prune.yml)
//   node web/e2e/walkthrough-pr.mjs comment
//       the PR comment body, from env REPO, PR, SHA (the evidence commit),
//       HEAD_SHA, RUN_URL, ARTIFACT_URL and IDS (the selected ids)
//   node web/e2e/walkthrough-pr.mjs clip
//       the clip for the PR body's Demo section, from env REPO, SHA,
//       HEAD_SHA and IDS
//   node web/e2e/walkthrough-pr.mjs report <dir>
//       the freshness run's verdict, from env RUN_URL, HEAD_SHA and IDS, into
//       <dir>: status (pass or fail), summary.md and body.md (the issue body)
//
// Where a demo's recording is read from: with env LEGS set (the run:demo
// workflow, which records each demo on a runner of its own and downloads each
// runner's out/ as LEGS/demo-leg-<id>/), that directory; else its recorder's
// out/ (make demo). Either holds the recorder's <id>/ directory, its
// failed.txt and its stack-up mark; a leg also holds head, the commit it
// recorded, and one marking another commit than HEAD_SHA (an earlier
// attempt's, left in the run) counts as never run. A selected demo counts as recorded only if
// failed.txt does not list it and its manifest carries the budget mark the
// recorder writes only on a pass; with no stack-up mark beside it, it never ran
// (its lab never came up, or its job ended first), which the comment, the
// clip and the report name as the lab's fault rather than the demo's.
import { copyFileSync, existsSync, mkdirSync, readFileSync, readdirSync, writeFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import {
  clipBody,
  commentBody,
  demoTag,
  everyDemo,
  evidenceFiles,
  evidenceRef,
  evidenceRefsToPrune,
  freshnessReport,
  legOutcome,
  selectDemos,
  tapeEvidenceFiles,
} from './walkthroughpr.mjs';

const ROOT = join(dirname(fileURLToPath(import.meta.url)), '..', '..');
// Each recorder: where its scripts are, their extension, the manifest a pass
// writes, and the files that manifest's evidence carries.
const RECORDERS = {
  tape: { dir: join(ROOT, 'scripts', 'demo'), ext: '.tape', manifest: 'demo.json', files: tapeEvidenceFiles },
  walkthrough: { dir: join(ROOT, 'web', 'e2e', 'walkthroughs'), ext: '.yaml', manifest: 'walkthrough.json', files: evidenceFiles },
};
const out = (/** @type {'tape' | 'walkthrough'} */ kind) => join(RECORDERS[kind].dir, 'out');

/** @param {'tape' | 'walkthrough'} kind @returns {string[]} */
function ids(kind) {
  const { dir, ext } = RECORDERS[kind];
  return readdirSync(dir)
    .filter((f) => f.endsWith(ext))
    .map((f) => f.slice(0, -ext.length))
    .sort();
}

/** @param {string} id @returns {'tape' | 'walkthrough'} */
function kindOf(id) {
  return existsSync(join(RECORDERS.tape.dir, `${id}.tape`)) ? 'tape' : 'walkthrough';
}

// Where demo `id`'s recording is read from: its runner's downloaded out/
// under LEGS, else its recorder's out/.
/** @param {string} id @returns {string} */
function outOf(id) {
  const legs = process.env.LEGS;
  return legs ? join(legs, `demo-leg-${id}`) : out(kindOf(id));
}

/** @param {string} id @returns {string[]} */
function listedFailed(id) {
  const f = join(outOf(id), 'failed.txt');
  return existsSync(f) ? readFileSync(f, 'utf8').split(/\s+/).filter(Boolean) : [];
}

// Demo `id`'s manifest, checked to be its own.
/** @param {string} id @returns {any} */
function manifestOf(id) {
  const kind = kindOf(id);
  const m = JSON.parse(readFileSync(join(outOf(id), id, RECORDERS[kind].manifest), 'utf8'));
  if (m.id !== id) throw new Error(`${kind} out/${id}/${RECORDERS[kind].manifest} is demo "${m.id}", not "${id}"`);
  return m;
}

// Demo `id`'s manifest if it passed (not listed as failed, and carrying the
// budget mark only a pass writes), else null.
/** @param {string} id @returns {any} */
function passed(id) {
  const kind = kindOf(id);
  if (listedFailed(id).includes(id) || !existsSync(join(outOf(id), id, RECORDERS[kind].manifest))) return null;
  const m = manifestOf(id);
  return m.budget ? m : null;
}

// The commit demo `id`'s leg marked (its out/head), read only under LEGS,
// where it is checked against the run's HEAD_SHA; empty when it marked none.
/** @param {string} id @returns {string | undefined} */
function legHead(id) {
  if (!process.env.LEGS) return undefined;
  const f = join(outOf(id), 'head');
  return existsSync(f) ? readFileSync(f, 'utf8').trim() : '';
}

// Every selected demo's result, by recorder (walkthroughpr.mjs legOutcome):
// recorded (its manifest), failed, or never ran.
function results() {
  /** @type {Record<'tape' | 'walkthrough', { recorded: any[], failed: string[], unstarted: string[] }>} */
  const r = { tape: { recorded: [], failed: [], unstarted: [] }, walkthrough: { recorded: [], failed: [], unstarted: [] } };
  for (const id of selected()) {
    const kind = kindOf(id);
    const head = legHead(id);
    const outcome = legOutcome({
      manifest: passed(id),
      listedFailed: listedFailed(id).includes(id),
      stackUp: existsSync(join(outOf(id), 'stack-up')),
      legHead: head,
      headSha: head === undefined ? undefined : env('HEAD_SHA'),
    });
    if (outcome === 'recorded') r[kind].recorded.push(passed(id));
    else r[kind][outcome].push(id);
  }
  return { tapes: r.tape, walkthroughs: r.walkthrough };
}

/** @returns {string} */
const body = () => process.env.PR_BODY ?? '';

/** @param {string} k @returns {string} */
function env(k) {
  const v = process.env[k];
  if (!v) throw new Error(`walkthrough-pr: ${k} is not set`);
  return v;
}

/** @returns {string[]} */
const selected = () => env('IDS').split(/\s+/).filter(Boolean);

function main([cmd, ...args]) {
  switch (cmd) {
    case 'select': {
      const changed = readFileSync(0, 'utf8').split('\n').map((l) => l.trim()).filter(Boolean);
      console.log(JSON.stringify(selectDemos(changed, demoTag(body()), ids('tape'), ids('walkthrough'))));
      return 0;
    }
    case 'every':
      console.log(JSON.stringify(everyDemo(ids('tape'), ids('walkthrough'))));
      return 0;
    case 'tag': {
      const tag = demoTag(body());
      if (!tag) {
        console.error(
          "demo tag: the PR body tags no demo. Add a line to its Demo section: demo: <id> (the tape or walkthrough this PR is about, " +
            'several ids allowed), or demo: none (<reason>) for a PR with nothing to demo (docs-only, test-only, a refactor no one can observe).',
        );
        return 1;
      }
      if ('none' in tag) {
        console.log(`demo tag: none (${tag.none})`);
        return 0;
      }
      const picked = selectDemos([], tag, ids('tape'), ids('walkthrough'));
      console.log(`demo tag: ${picked.map((d) => `${d.id} (${d.kind === 'tape' ? 'terminal demo' : 'walkthrough'})`).join(', ')}`);
      return 0;
    }
    case 'ref':
      console.log(evidenceRef(Number(args[0])));
      return 0;
    case 'files': {
      const id = args[0];
      console.log(RECORDERS[kindOf(id)].files(manifestOf(id)).join('\n'));
      return 0;
    }
    case 'stage': {
      const tree = args[0];
      if (!tree) throw new Error('walkthrough-pr stage: name the directory to stage into');
      for (const id of selected()) {
        const m = passed(id);
        if (!m) continue;
        for (const f of RECORDERS[kindOf(id)].files(m)) {
          const src = join(outOf(id), f);
          if (!existsSync(src)) {
            console.log(`::warning::${id} has no ${f}; it is left out of the evidence`);
            continue;
          }
          mkdirSync(dirname(join(tree, f)), { recursive: true });
          copyFileSync(src, join(tree, f));
          console.log(`staged ${f}`);
        }
      }
      return 0;
    }
    case 'prune': {
      const { refs, prs } = JSON.parse(readFileSync(0, 'utf8'));
      const doomed = evidenceRefsToPrune(refs, prs, new Date(), Number(args[0]));
      if (doomed.length) console.log(doomed.join('\n'));
      return 0;
    }
    case 'comment':
      process.stdout.write(
        commentBody({
          repo: env('REPO'),
          pr: Number(env('PR')),
          sha: env('SHA'),
          headSha: env('HEAD_SHA'),
          runUrl: env('RUN_URL'),
          artifactUrl: env('ARTIFACT_URL'),
          ...results(),
        }),
      );
      return 0;
    case 'clip':
      process.stdout.write(clipBody({ repo: env('REPO'), sha: env('SHA'), headSha: env('HEAD_SHA'), ...results() }));
      return 0;
    case 'report': {
      const dir = args[0];
      if (!dir) throw new Error('walkthrough-pr report: name the directory to write the report into');
      const r = freshnessReport({ runUrl: env('RUN_URL'), headSha: env('HEAD_SHA'), ...results() });
      mkdirSync(dir, { recursive: true });
      writeFileSync(join(dir, 'status'), r.ok ? 'pass\n' : 'fail\n');
      writeFileSync(join(dir, 'summary.md'), r.summary + '\n');
      writeFileSync(join(dir, 'body.md'), r.body);
      console.log(r.summary);
      return 0;
    }
    default:
      console.error('usage: walkthrough-pr.mjs select | every | tag | ref <pr> | files <id> | stage <dir> | prune <days> | comment | clip | report <dir>');
      return 2;
  }
}

try {
  process.exitCode = main(process.argv.slice(2));
} catch (e) {
  console.error(e.message);
  process.exitCode = 1;
}
