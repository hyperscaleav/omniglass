// check-fragment-pages runs after `astro build` and fails it when a log page
// did not publish what its fragment directory holds (#1095). A render error on
// a .md page is swallowed (logged, page rendered empty, exit zero), and the
// decision index's anchors are computed apart from the headings they link, so
// the built pages are the only proof: every decision's index anchor must name a
// heading on the decision log, and the build log must carry one heading per
// section entry.
import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { missingLogIds, parseFragment, readFragmentDir } from '../src/plugins/fragments.mjs';

const docs = fileURLToPath(new URL('..', import.meta.url));
const page = (slug) => readFileSync(join(docs, 'dist', slug, 'index.html'), 'utf8');
const dir = (name) => readFragmentDir(join(docs, 'src', 'content', name));

const failures = missingLogIds(page('architecture/decisions'), dir('decisions'));

const sections = dir('build-log').filter((f) => parseFragment(f.name, f.text).data.form === undefined).length;
const headings = (page('architecture/build-log').match(/<h3 id="/g) ?? []).length;
if (headings !== sections) {
  failures.push(`the built build log carries ${headings} entry headings for ${sections} section entries: its render failed or an entry lost its heading`);
}

if (failures.length > 0) {
  console.error(`check-fragment-pages: ${failures.length} problem(s):\n  ${failures.join('\n  ')}`);
  process.exit(1);
}
console.log(`check-fragment-pages: OK (${dir('decisions').length} decisions linked, ${sections} build-log sections)`);
