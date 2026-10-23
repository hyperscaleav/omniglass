// The command line of walkthroughpr.mjs (#925), run by the run:walkthrough
// workflow on the runner's node (no dependencies, no build step):
//
//   node web/e2e/walkthrough-pr.mjs select < changed.txt
//       the ids to record, space-separated: the scripts among the changed paths
//       (one per line on stdin), else every walkthrough in the tree
//   node web/e2e/walkthrough-pr.mjs ref <pr>
//       the ref the PR's walkthrough evidence is committed to
//   node web/e2e/walkthrough-pr.mjs files <id>
//       the files that walkthrough's evidence carries, one per line, each a
//       path both under out/ and in the evidence commit's tree (read from its
//       out/<id>/walkthrough.json)
//   node web/e2e/walkthrough-pr.mjs prune <days> < state.json
//       the evidence refs to delete, one per line, from {"refs": [...], "prs":
//       [{number, state, closed_at}, ...]} on stdin: those of PRs closed more
//       than <days> ago (walkthrough-prune.yml)
//   node web/e2e/walkthrough-pr.mjs comment
//       the PR comment body, from env REPO, PR, SHA (the evidence commit),
//       HEAD_SHA, RUN_URL, ARTIFACT_URL and IDS (the selected ids). A
//       selected walkthrough counts as recorded only if walkthroughs.sh did not
//       list it in out/failed.txt and its walkthrough.json carries the budget
//       mark the recorder writes only on a pass; with no
//       out/stack-up (walkthroughs.sh writes it once the stack is up), the
//       comment says the stack never came up instead of blaming each one.
import { existsSync, readFileSync, readdirSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { commentBody, evidenceFiles, evidenceRef, evidenceRefsToPrune, selectWalkthroughs } from './walkthroughpr.mjs';

const DIR = join(dirname(fileURLToPath(import.meta.url)), 'walkthroughs');
const OUT = join(DIR, 'out');

function main([cmd, ...args]) {
  switch (cmd) {
    case 'select': {
      const changed = readFileSync(0, 'utf8').split('\n').map((l) => l.trim()).filter(Boolean);
      const all = readdirSync(DIR).filter((f) => f.endsWith('.yaml')).map((f) => f.slice(0, -5)).sort();
      console.log(selectWalkthroughs(changed, all).join(' '));
      return 0;
    }
    case 'ref':
      console.log(evidenceRef(Number(args[0])));
      return 0;
    case 'files': {
      const id = args[0];
      const m = JSON.parse(readFileSync(join(OUT, id, 'walkthrough.json'), 'utf8'));
      if (m.id !== id) throw new Error(`out/${id}/walkthrough.json is walkthrough "${m.id}", not "${id}"`);
      console.log(evidenceFiles(m).join('\n'));
      return 0;
    }
    case 'prune': {
      const { refs, prs } = JSON.parse(readFileSync(0, 'utf8'));
      const out = evidenceRefsToPrune(refs, prs, new Date(), Number(args[0]));
      if (out.length) console.log(out.join('\n'));
      return 0;
    }
    case 'comment': {
      const env = (k) => {
        const v = process.env[k];
        if (!v) throw new Error(`walkthrough-pr comment: ${k} is not set`);
        return v;
      };
      const ids = env('IDS').split(/\s+/).filter(Boolean);
      const failedFile = join(OUT, 'failed.txt');
      const listed = existsSync(failedFile) ? readFileSync(failedFile, 'utf8').split(/\s+/).filter(Boolean) : [];
      const recorded = [];
      const failed = [];
      for (const id of ids) {
        const manifest = join(OUT, id, 'walkthrough.json');
        const m = !listed.includes(id) && existsSync(manifest) ? JSON.parse(readFileSync(manifest, 'utf8')) : null;
        // Only a recording that passed its budget carries the mark (walkthrough.mjs budget).
        if (m && m.budget) recorded.push(m);
        else failed.push(id);
      }
      process.stdout.write(
        commentBody({
          repo: env('REPO'),
          pr: Number(env('PR')),
          sha: env('SHA'),
          headSha: env('HEAD_SHA'),
          runUrl: env('RUN_URL'),
          artifactUrl: env('ARTIFACT_URL'),
          recorded,
          failed,
          stackUp: existsSync(join(OUT, 'stack-up')),
        }),
      );
      return 0;
    }
    default:
      console.error('usage: walkthrough-pr.mjs select | ref <pr> | files <id> | prune <days> | comment');
      return 2;
  }
}

try {
  process.exitCode = main(process.argv.slice(2));
} catch (e) {
  console.error(e.message);
  process.exitCode = 1;
}
