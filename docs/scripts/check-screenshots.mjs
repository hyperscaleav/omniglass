// check-screenshots fails the docs build when a `::screenshot` directive does
// not resolve: every `::screenshot{#id}` must have a matching entry in its own
// page's `screenshots` frontmatter, and a directive with no id at all is
// equally broken. It runs before `astro build` for the same reason the design
// fence gate does (#519, the `swallowed-build-gate` class): a remark
// `file.fail` on a `.md` page is caught by the content loader, logged, and the
// page publishes empty with exit zero, so the render-path check cannot gate.
// The remark plugin keeps its `file.fail` for the dev overlay; this script is
// what makes "a #id with no frontmatter entry fails the build" true.
//
// Scans source lines rather than the remark tree so .md and .mdx behave
// identically; a directive inside a fenced code block is an example, not an
// embed, and is skipped. Whether the captured PNG exists stays
// `make docs-shots-verify`'s job, exactly as the plugin documents.
import { readFileSync, readdirSync, statSync } from 'node:fs';
import { join, relative } from 'node:path';
import { fileURLToPath } from 'node:url';

const root = fileURLToPath(new URL('../src/content/docs', import.meta.url));
const directive = /::screenshot(\{#([A-Za-z0-9_-]+)\})?/g;
const codeFence = /^\s*(`{3,}|~{3,})/;
const frontmatterId = /^\s+(?:-\s+)?id:\s*['"]?([A-Za-z0-9_-]+)['"]?\s*$/;

function* walk(dir) {
  for (const name of readdirSync(dir)) {
    const path = join(dir, name);
    if (statSync(path).isDirectory()) yield* walk(path);
    else if (/\.(md|mdx)$/.test(name)) yield path;
  }
}

// declaredIds reads the ids under the page's `screenshots:` frontmatter block.
// A line-shape parse, not a YAML library: the block is a flat list of maps and
// the capture generator reads the same shape, so anything fancier would be
// looser than the real consumer.
function declaredIds(lines) {
  const ids = new Set();
  let inFrontmatter = false;
  let inScreenshots = false;
  for (const line of lines) {
    if (line.trim() === '---') {
      if (inFrontmatter) break;
      inFrontmatter = true;
      continue;
    }
    if (!inFrontmatter) break;
    if (/^screenshots:\s*$/.test(line)) {
      inScreenshots = true;
      continue;
    }
    if (inScreenshots) {
      if (/^\S/.test(line)) inScreenshots = false; // the next top-level key
      else {
        const m = frontmatterId.exec(line);
        if (m) ids.add(m[1]);
      }
    }
  }
  return ids;
}

const failures = [];
for (const path of walk(root)) {
  const lines = readFileSync(path, 'utf8').split('\n');
  const ids = declaredIds(lines);
  let inCode = false;
  let delim = '';
  lines.forEach((line, i) => {
    if (inCode) {
      const t = line.trimStart();
      if (t.startsWith(delim) && /^[`~]*\s*$/.test(t.slice(delim.length))) inCode = false;
      return;
    }
    const code = codeFence.exec(line);
    if (code) {
      inCode = true;
      delim = code[1];
      return;
    }
    // An inline code span quoting the syntax is teaching, not an embed.
    const prose = line.replace(/`[^`]*`/g, '');
    for (const m of prose.matchAll(directive)) {
      const where = `${relative(root, path)}:${i + 1}`;
      if (!m[2]) failures.push(`${where}: ::screenshot has no id (write ::screenshot{#some-id})`);
      else if (!ids.has(m[2])) failures.push(`${where}: ::screenshot{#${m[2]}} has no matching entry in this page's screenshots frontmatter`);
    }
  });
}

if (failures.length > 0) {
  console.error(`${failures.length} unresolved screenshot directive(s); each ::screenshot{#id} needs a screenshots frontmatter entry on its own page`);
  for (const f of failures) console.error('  ' + f);
  process.exit(1);
}
