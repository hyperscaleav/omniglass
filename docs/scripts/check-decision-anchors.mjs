// check-decision-anchors runs after `astro build` (#1096) and holds every link
// into the decision log to a heading that exists on the built page: an absolute
// `/architecture/decisions/#adr-NNNN-...` from any page, and an in-page
// `#adr-NNNN-...` on the log itself.
//
// An ADR's heading id is github-slugger's slug of its title (including the `.2`
// suffix rule on a repeated heading), so a link typed by hand drifts the moment
// a title is reworded and the browser silently stays at the top of the page.
// Cite an ADR by its current title slug; this check reads the ids off the built
// log, the one place that knows what the slugger emitted.
import { existsSync, readFileSync, readdirSync, statSync } from 'node:fs';
import { join, relative } from 'node:path';
import { fileURLToPath } from 'node:url';
import { brokenFragments, headingIds } from './lib/decision-anchors.mjs';

const dist = fileURLToPath(new URL('../dist', import.meta.url));
const logPage = join(dist, 'architecture', 'decisions', 'index.html');

function* html(dir) {
  for (const name of readdirSync(dir)) {
    const path = join(dir, name);
    if (statSync(path).isDirectory()) yield* html(path);
    else if (name.endsWith('.html')) yield path;
  }
}

if (!existsSync(logPage)) {
  console.error(`check-decision-anchors: ${relative(dist, logPage)} is not in the build output; run it after \`astro build\``);
  process.exit(1);
}
const ids = headingIds(readFileSync(logPage, 'utf8'));
const problems = [];
for (const page of html(dist)) {
  const where = relative(dist, page);
  const text = readFileSync(page, 'utf8');
  const isLog = page === logPage;
  const bad = brokenFragments(text, isLog, ids);
  for (const f of bad) problems.push(`/${where.replace(/index\.html$/, '')} -> /architecture/decisions/#${f}`);
}

if (problems.length) {
  console.error(`${problems.length} link(s) into the decision log land on no heading; cite the ADR by the slug of its current title:`);
  for (const p of [...new Set(problems)]) console.error('  ' + p);
  process.exit(1);
}
console.log(`check-decision-anchors: OK (every link into the decision log lands on a heading, ${ids.size} ids)`);
