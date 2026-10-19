// @ts-check
// The render stamp (#937): what the walkthrough embed reads that Astro's content
// cache cannot see. Astro keys a cached page render on the page's own content,
// so a re-copied walkthrough.json, a video added or removed, or a change to the
// plugin that renders the embed leaves a cached page saying the old thing. The
// docs build (scripts/astro-build.mjs) digests these inputs, compares the digest
// with the stamp it wrote beside Astro's cache after the last good build, and
// forces the build only when they differ, so a restored cache (a Cloudflare
// Pages build, a local re-copy) can never ship a stale embed.
import { createHash } from 'node:crypto';
import { existsSync, readFileSync, readdirSync } from 'node:fs';
import { join } from 'node:path';

/** @typedef {{ name: string, bytes: string | Uint8Array }} RenderInput */

// The embed's renderer and everything it imports, relative to the repo root.
export const PLUGIN_SOURCES = [
  'docs/src/plugins/remark-walkthroughs.mjs',
  'docs/src/plugins/frontmatter-directive.mjs',
  'docs/src/plugins/walkthrough-embed.mjs',
  'web/e2e/walkthroughcore.mjs',
];

const WALKTHROUGHS = 'docs/public/walkthroughs';

/**
 * One digest over the inputs, independent of their order. Each input is framed
 * by its name and byte length, so no two different input sets hash alike by
 * moving bytes across a boundary.
 * @param {RenderInput[]} inputs
 * @returns {string} hex sha256
 */
export function renderDigest(inputs) {
  const enc = new TextEncoder();
  const h = createHash('sha256');
  const sorted = [...inputs].sort((a, b) => (a.name < b.name ? -1 : a.name > b.name ? 1 : 0));
  for (const { name, bytes } of sorted) {
    const body = typeof bytes === 'string' ? enc.encode(bytes) : bytes;
    h.update(`${enc.encode(name).length}:${name}\0${body.length}:`);
    h.update(body);
  }
  return h.digest('hex');
}

/**
 * The astro arguments for this build: a plain build when the stamp from the last
 * good build matches, else a forced one (no stamp means no trustworthy cache).
 * @param {string} digest
 * @param {string | null} stamp
 * @returns {string[]}
 */
export function astroBuildArgs(digest, stamp) {
  return stamp !== null && stamp.trim() === digest ? ['build'] : ['build', '--force'];
}

/**
 * The inputs under `repo`: every copied walkthrough.json, each walkthrough
 * directory's file listing (the video renders only when its file is present),
 * and the plugin sources. Names are repo-relative.
 * @param {string} repo the repository root
 * @returns {RenderInput[]}
 */
export function collectRenderInputs(repo) {
  /** @type {RenderInput[]} */
  const inputs = PLUGIN_SOURCES.map((name) => ({ name, bytes: readFileSync(join(repo, name)) }));
  const root = join(repo, WALKTHROUGHS);
  if (!existsSync(root)) return inputs;
  for (const id of readdirSync(root, { withFileTypes: true })) {
    if (!id.isDirectory()) continue;
    const dir = `${WALKTHROUGHS}/${id.name}`;
    const files = readdirSync(join(repo, dir)).sort();
    inputs.push({ name: `${dir}/`, bytes: files.join('\n') });
    if (files.includes('walkthrough.json')) {
      inputs.push({ name: `${dir}/walkthrough.json`, bytes: readFileSync(join(repo, dir, 'walkthrough.json')) });
    }
  }
  return inputs;
}
