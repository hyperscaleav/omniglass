// Structural gate for the docs screenshots. Deterministic (no browser, no build):
// asserts the `screenshots` frontmatter and the committed PNGs are in sync, so a
// declared shot without an image, an orphan image with no declaration, or a
// duplicate id fails fast. It does NOT check the pixels are current (that is the
// job of `make docs-shots-check`, which re-captures and diffs).
//
// Run via `make docs-shots-verify`.
import matter from 'gray-matter';
import { readdir, readFile } from 'node:fs/promises';
import { join } from 'node:path';

const CONTENT = 'docs/src/content/docs';
const SHOTS = 'docs/public/screenshots';

async function walk(dir) {
  const files = [];
  for (const entry of await readdir(dir, { withFileTypes: true })) {
    const p = join(dir, entry.name);
    if (entry.isDirectory()) files.push(...(await walk(p)));
    else if (entry.name.endsWith('.md') || entry.name.endsWith('.mdx')) files.push(p);
  }
  return files;
}

const declared = new Map(); // id -> page
const errors = [];

for (const file of await walk(CONTENT)) {
  const { data } = matter(await readFile(file, 'utf8'));
  for (const s of data.screenshots ?? []) {
    if (declared.has(s.id)) {
      errors.push(`duplicate screenshot id "${s.id}" (in ${file} and ${declared.get(s.id)})`);
    }
    declared.set(s.id, file);
  }
}

const onDisk = new Set(
  (await readdir(SHOTS).catch(() => []))
    .filter((f) => f.endsWith('.png'))
    .map((f) => f.replace(/\.png$/, '')),
);

for (const [id, page] of declared) {
  if (!onDisk.has(id)) {
    errors.push(`declared screenshot "${id}" (${page}) has no image at ${SHOTS}/${id}.png; run 'make docs-shots'`);
  }
}
for (const id of onDisk) {
  if (!declared.has(id)) {
    errors.push(`orphan image ${SHOTS}/${id}.png has no screenshots frontmatter entry; remove it or declare it`);
  }
}

if (errors.length > 0) {
  console.error('docs-shots-verify: FAIL');
  for (const e of errors) console.error('  - ' + e);
  process.exit(1);
}
console.log(`docs-shots-verify: OK (${declared.size} screenshots, all present, no orphans)`);
