// check-fragments fails the docs build when a fragment directory cannot render
// (#1095): a malformed entry file, a missing field, or two decisions holding one
// number. It runs before `astro build` because the same error thrown inside the
// page render (remark-fragments.mjs) is swallowed by the content loader on a .md
// page (logged, page rendered empty, exit zero), which would publish an empty
// build log or decision log. It expands every page that uses a directive with
// the renderer the page render uses (which also refuses a directive line in an
// entry body, the one way an entry could fail a remark plugin);
// check-fragment-pages.mjs then proves the built pages carry every entry.
import { readFileSync } from 'node:fs';
import { join, relative } from 'node:path';
import { fileURLToPath } from 'node:url';
import { directivesIn, expandFragments, readFragmentDir } from '../src/plugins/fragments.mjs';
import { walkPages } from './lib/frontmatter-ids.mjs';

const contentDir = fileURLToPath(new URL('../src/content', import.meta.url));
const load = (dir) => readFragmentDir(join(contentDir, dir));

const failures = [];
const used = new Set();
for (const path of walkPages()) {
  const text = readFileSync(path, 'utf8');
  const directives = directivesIn(text);
  if (directives.length === 0) continue;
  directives.forEach((d) => used.add(d));
  try {
    expandFragments(text, load);
  } catch (err) {
    failures.push(`${relative(contentDir, path)}: ${err.message}`);
  }
}
for (const d of ['build-log', 'decision-index', 'decision-log']) {
  if (!used.has(d)) failures.push(`no page carries the ::${d} directive, so its entries would not publish`);
}

if (failures.length > 0) {
  console.error(`check-fragments: ${failures.length} problem(s):\n  ${failures.join('\n  ')}`);
  process.exit(1);
}
console.log(`check-fragments: OK (${[...used].map((d) => `::${d}`).join(', ')} render)`);
