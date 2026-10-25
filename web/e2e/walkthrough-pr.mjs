// The command line of walkthroughpr.mjs (#925, #1132), run by the run:demo
// workflow on the runner's node (no dependencies, no build step). A demo id
// names a tape (scripts/demo/<id>.tape, a terminal demo) or a walkthrough
// script (web/e2e/walkthroughs/<id>.yaml), never both:
//
//   node web/e2e/walkthrough-pr.mjs select < changed.txt
//       the ids to record, space-separated, tapes first: the demos among the
//       changed paths (one per line on stdin), widened by any recorder they
//       change, else every demo in the tree
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
//       HEAD_SHA, RUN_URL, ARTIFACT_URL and IDS (the selected ids). A selected
//       demo counts as recorded only if its recorder did not list it in its
//       out/failed.txt and its manifest carries the budget mark the recorder
//       writes only on a pass; with no out/stack-up from its recorder (each
//       writes one once its stack is up), the comment says that recorder's
//       stack never came up instead of blaming each demo.
import { copyFileSync, existsSync, mkdirSync, readFileSync, readdirSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { commentBody, evidenceFiles, evidenceRef, evidenceRefsToPrune, selectDemos, tapeEvidenceFiles } from './walkthroughpr.mjs';

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

/** @param {'tape' | 'walkthrough'} kind @returns {string[]} */
function listedFailed(kind) {
  const f = join(out(kind), 'failed.txt');
  return existsSync(f) ? readFileSync(f, 'utf8').split(/\s+/).filter(Boolean) : [];
}

// Demo `id`'s manifest, checked to be its own.
/** @param {string} id @returns {any} */
function manifestOf(id) {
  const kind = kindOf(id);
  const m = JSON.parse(readFileSync(join(out(kind), id, RECORDERS[kind].manifest), 'utf8'));
  if (m.id !== id) throw new Error(`${kind} out/${id}/${RECORDERS[kind].manifest} is demo "${m.id}", not "${id}"`);
  return m;
}

// Demo `id`'s manifest if it passed (not listed as failed, and carrying the
// budget mark only a pass writes), else null.
/** @param {string} id @returns {any} */
function passed(id) {
  const kind = kindOf(id);
  if (listedFailed(kind).includes(id) || !existsSync(join(out(kind), id, RECORDERS[kind].manifest))) return null;
  const m = manifestOf(id);
  return m.budget ? m : null;
}

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
      console.log(selectDemos(changed, ids('tape'), ids('walkthrough')).join(' '));
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
        const kind = kindOf(id);
        for (const f of RECORDERS[kind].files(m)) {
          const src = join(out(kind), f);
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
    case 'comment': {
      /** @type {Record<'tape' | 'walkthrough', { recorded: any[], failed: string[] }>} */
      const results = { tape: { recorded: [], failed: [] }, walkthrough: { recorded: [], failed: [] } };
      for (const id of selected()) {
        const m = passed(id);
        if (m) results[kindOf(id)].recorded.push(m);
        else results[kindOf(id)].failed.push(id);
      }
      process.stdout.write(
        commentBody({
          repo: env('REPO'),
          pr: Number(env('PR')),
          sha: env('SHA'),
          headSha: env('HEAD_SHA'),
          runUrl: env('RUN_URL'),
          artifactUrl: env('ARTIFACT_URL'),
          tapes: { ...results.tape, stackUp: existsSync(join(out('tape'), 'stack-up')) },
          walkthroughs: { ...results.walkthrough, stackUp: existsSync(join(out('walkthrough'), 'stack-up')) },
        }),
      );
      return 0;
    }
    default:
      console.error('usage: walkthrough-pr.mjs select | ref <pr> | files <id> | stage <dir> | prune <days> | comment');
      return 2;
  }
}

try {
  process.exitCode = main(process.argv.slice(2));
} catch (e) {
  console.error(e.message);
  process.exitCode = 1;
}
