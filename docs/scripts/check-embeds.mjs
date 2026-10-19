// check-embeds fails the docs build when a generated-resource directive does not
// resolve: every `::screenshot{#id}` must have a matching entry in its own page's
// `screenshots` frontmatter, every `::walkthrough{#id}` one in its `walkthroughs`
// frontmatter, and a directive with no id at all is equally broken. A declared
// walkthrough also needs its copied manifest, public/walkthroughs/<id>/walkthrough.json,
// because the embed renders from it (`make docs-walkthroughs`).
//
// It runs before `astro build` for the same reason the design fence gate does
// (#519, the `swallowed-build-gate` class): a remark `file.fail` on a `.md` page
// is caught by the content loader, logged, and the page publishes empty with exit
// zero, so the render-path check cannot gate. The remark plugins keep their
// `file.fail` for the dev overlay; this script is what makes "a #id with no
// frontmatter entry fails the build" true.
//
// Scans source lines rather than the remark tree so .md and .mdx behave
// identically. Only a leaf directive alone on its line embeds; a text
// directive, a container, or a quote in code or an HTML comment is skipped
// (lib/embed-scan.mjs, the one definition, unit-tested in make test-web).
// Whether a captured screenshot PNG exists stays `make docs-shots-verify`'s job, exactly as its
// plugin documents; the walkthrough's other invariants (a script per declared
// id, every declaration used) are the docs lint suite's (internal/docslint).
import { existsSync, readFileSync } from 'node:fs';
import { join, relative } from 'node:path';
import { fileURLToPath } from 'node:url';
import { scanDirectives } from './lib/embed-scan.mjs';
import { contentRoot, declaredIds, walkPages } from './lib/frontmatter-ids.mjs';

const publicDir = fileURLToPath(new URL('../public', import.meta.url));

const embeds = [
  { name: 'screenshot', key: 'screenshots' },
  { name: 'walkthrough', key: 'walkthroughs' },
];

const failures = [];
for (const path of walkPages()) {
  const lines = readFileSync(path, 'utf8').split('\n');
  const declared = Object.fromEntries(embeds.map((e) => [e.name, declaredIds(lines, e.key)]));
  const page = relative(contentRoot, path);
  for (const { name, key } of embeds) {
    for (const { line, id } of scanDirectives(lines, name)) {
      if (!id) failures.push(`${page}:${line}: ::${name} has no id (write ::${name}{#some-id})`);
      else if (!declared[name].has(id)) {
        failures.push(`${page}:${line}: ::${name}{#${id}} has no matching entry in this page's ${key} frontmatter`);
      }
    }
  }
  for (const id of declared.walkthrough) {
    if (!existsSync(join(publicDir, 'walkthroughs', id, 'walkthrough.json'))) {
      failures.push(
        `${relative(contentRoot, path)}: walkthrough "${id}" has no public/walkthroughs/${id}/walkthrough.json; ` +
          `record it (make walkthroughs ID=${id}) and copy it (make docs-walkthroughs)`,
      );
    }
  }
}

if (failures.length > 0) {
  console.error(
    `${failures.length} unresolved embed(s); each ::screenshot{#id} and ::walkthrough{#id} needs a frontmatter entry on its own page, and a walkthrough its copied manifest`,
  );
  for (const f of failures) console.error('  ' + f);
  process.exit(1);
}
