// The command line of regressiontest.mjs (#2380), run by regression-test.yml:
//
//   node web/e2e/regression-test.mjs
//       env PR_BODY, PR_NUMBER, GH_TOKEN, GITHUB_REPOSITORY; reads the PR's
//       changed files and each closed issue's labels through the gh CLI,
//       prints the verdict, exits 1 on a failure.
import { execFileSync } from 'node:child_process';
import { check, closedIssues } from './regressiontest.mjs';

/** @param {string[]} args @returns {any} */
const gh = (args) => JSON.parse(execFileSync('gh', args, { encoding: 'utf8', maxBuffer: 1 << 28 }));

function main() {
  const body = process.env.PR_BODY ?? '';
  const repo = process.env.GITHUB_REPOSITORY;
  const issues = closedIssues(body);
  if (!issues.length) {
    console.log('regression-test: the PR closes no issue');
    return 0;
  }
  const closed = issues.map((number) => {
    try {
      const r = gh(['api', `repos/${repo}/issues/${number}`]);
      // A pull request number in a closing keyword is not a ticket.
      return { number, labels: r.pull_request ? [] : (r.labels ?? []).map((/** @type {{name: string}} */ l) => l.name) };
    } catch {
      return { number, labels: [] };
    }
  });
  const files = gh(['api', '--paginate', '--slurp', `repos/${repo}/pulls/${process.env.PR_NUMBER}/files?per_page=100`])
    .flat()
    .flatMap((/** @type {{filename: string, previous_filename?: string}} */ f) => [f.filename, f.previous_filename ?? '']);
  const v = check({ body, changed: files.filter(Boolean), closed });
  (v.ok ? console.log : console.error)(v.message);
  return v.ok ? 0 : 1;
}

process.exit(main());
