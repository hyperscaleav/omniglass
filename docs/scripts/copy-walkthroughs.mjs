// copy-walkthroughs brings the walkthroughs the docs declare into the site
// (#926): for every id under a page's `walkthroughs` frontmatter, it copies the
// recorder's manifest, per-step stills and video (<id>.mp4, #939) from
// web/e2e/walkthroughs/out/<id>/
// (written by `make walkthroughs`) into public/walkthroughs/<id>/, which is
// committed like public/screenshots/. The recorder's poster.png is not copied:
// it is a byte copy of the poster step's still, which the manifest names. Nor
// is the animated preview: the embed plays the video, so the preview is only
// the PR comment's glance (one left from an older copy is removed).
//
// Only a recording that passed is accepted: its manifest must carry the budget
// mark the recorder writes after the budget gate passes, so a failed or
// over-budget recording is refused even when out/failed.txt does not list it.
//
// The mp4 is committed with the rest (about 1 MB per walkthrough per
// re-record; contributing/walkthroughs states the cost), so the embed plays it
// with controls on the deployed docs and their PR previews.
//
// Astro caches each rendered page by the page's own content, not by the
// manifest it renders; the docs build (`corepack pnpm build`) sees the copy
// change its render stamp and forces itself (astro-build.mjs, #937), and
// check-walkthrough-embeds.mjs fails any build that still rendered stale.
//
// Run via `make docs-walkthroughs`.
import { copyFileSync, existsSync, mkdirSync, readFileSync, readdirSync, unlinkSync } from 'node:fs';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { checkId } from '../../web/e2e/walkthroughcore.mjs';
import { walkthroughAssets } from '../src/plugins/walkthrough-embed.mjs';
import { declaredIds, walkPages } from './lib/frontmatter-ids.mjs';

const OUT = fileURLToPath(new URL('../../web/e2e/walkthroughs/out', import.meta.url));
const DEST = fileURLToPath(new URL('../public/walkthroughs', import.meta.url));

const ids = new Set();
for (const path of walkPages()) {
  for (const id of declaredIds(readFileSync(path, 'utf8').split('\n'), 'walkthroughs')) ids.add(id);
}

const failedFile = join(OUT, 'failed.txt');
const failed = existsSync(failedFile) ? readFileSync(failedFile, 'utf8').split(/\s+/).filter(Boolean) : [];

// One walkthrough's copy; throws an Error whose message names what is wrong.
/** @param {string} id */
function copyOne(id) {
  checkId(id);
  const src = join(OUT, id);
  const manifestPath = join(src, 'walkthrough.json');
  if (failed.includes(id)) {
    throw new Error(`walkthrough "${id}" failed its last recording (out/failed.txt); fix it and re-run make walkthroughs ID=${id}`);
  }
  if (!existsSync(manifestPath)) {
    throw new Error(`walkthrough "${id}" is not recorded (no ${manifestPath}); run make walkthroughs ID=${id}`);
  }
  let manifest;
  try {
    manifest = JSON.parse(readFileSync(manifestPath, 'utf8'));
  } catch (e) {
    throw new Error(`${manifestPath} does not parse: ${e.message}`);
  }
  if (manifest.id !== id) throw new Error(`${manifestPath} is walkthrough "${manifest.id}", not "${id}"`);
  const files = walkthroughAssets(manifest);
  const missing = files.filter((f) => !existsSync(join(src, f)));
  if (missing.length) {
    throw new Error(`walkthrough "${id}" is missing ${missing.join(', ')} under ${src}; re-run make walkthroughs ID=${id}`);
  }
  const dest = join(DEST, id);
  mkdirSync(dest, { recursive: true });
  // Nothing but the copy lives here: a directory is refused rather than
  // deleted, and a file from an older, longer recording is removed so it cannot
  // outlive the recording it came from.
  const stale = [];
  for (const entry of readdirSync(dest, { withFileTypes: true })) {
    if (entry.isDirectory()) {
      throw new Error(`public/walkthroughs/${id}/${entry.name}/ is a directory, which a walkthrough copy never holds; move it out and re-run`);
    }
    if (!files.includes(entry.name)) stale.push(entry.name);
  }
  for (const f of stale) unlinkSync(join(dest, f));
  for (const f of files) copyFileSync(join(src, f), join(dest, f));
  console.log(`docs-walkthroughs: ${id}: ${files.length} files into public/walkthroughs/${id}/`);
}

const errors = [];
for (const id of [...ids].sort()) {
  try {
    copyOne(id);
  } catch (e) {
    errors.push(e instanceof Error ? e.message : String(e));
  }
}

const orphans = existsSync(DEST) ? readdirSync(DEST).filter((d) => !ids.has(d)) : [];
for (const d of orphans) {
  errors.push(`public/walkthroughs/${d}/ is declared by no page; git rm it, or declare it in a page's walkthroughs frontmatter`);
}

if (errors.length) {
  console.error('docs-walkthroughs: FAIL');
  for (const e of errors) console.error('  - ' + e);
  process.exit(1);
}
console.log(`docs-walkthroughs: OK (${ids.size} walkthrough${ids.size === 1 ? '' : 's'})`);
