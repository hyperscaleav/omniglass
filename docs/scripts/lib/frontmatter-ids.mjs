// The docs scripts' shared reading of a page's embed declarations: the ids under
// a top-level frontmatter list (`screenshots:`, `walkthroughs:`). A line-shape
// parse, not a YAML library: each block is a flat list of maps and its real
// consumers read the same shape, so anything fancier would be looser than they
// are. Used by the pre-build directive gate (check-embeds.mjs) and the
// walkthrough asset copy (copy-walkthroughs.mjs).
import { readdirSync, statSync } from 'node:fs';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';

export const contentRoot = fileURLToPath(new URL('../../src/content/docs', import.meta.url));

const frontmatterId = /^\s+(?:-\s+)?id:\s*['"]?([A-Za-z0-9_-]+)['"]?\s*$/;

/** Every .md/.mdx page under dir. */
export function* walkPages(dir = contentRoot) {
  for (const name of readdirSync(dir)) {
    const path = join(dir, name);
    if (statSync(path).isDirectory()) yield* walkPages(path);
    else if (/\.(md|mdx)$/.test(name)) yield path;
  }
}

/**
 * The ids declared under the page's `<key>:` frontmatter block.
 * @param {string[]} lines the page's lines
 * @param {string} key e.g. 'screenshots'
 * @returns {Set<string>}
 */
export function declaredIds(lines, key) {
  const ids = new Set();
  const opener = new RegExp(`^${key}:\\s*$`);
  let inFrontmatter = false;
  let inBlock = false;
  for (const line of lines) {
    if (line.trim() === '---') {
      if (inFrontmatter) break;
      inFrontmatter = true;
      continue;
    }
    if (!inFrontmatter) break;
    if (opener.test(line)) {
      inBlock = true;
      continue;
    }
    if (inBlock) {
      if (/^\S/.test(line)) inBlock = false; // the next top-level key
      else {
        const m = frontmatterId.exec(line);
        if (m) ids.add(m[1]);
      }
    }
  }
  return ids;
}
