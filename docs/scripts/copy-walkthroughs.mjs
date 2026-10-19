// copy-walkthroughs brings the walkthroughs the docs declare into the site
// (#926): for every id under a page's `walkthroughs` frontmatter, it copies the
// recorder's manifest, poster and per-step stills from
// web/e2e/walkthroughs/out/<id>/ (written by `make walkthroughs`) into
// public/walkthroughs/<id>/, which is committed like public/screenshots/.
//
// The mp4 is not committed (size). DOCS_WALKTHROUGH_VIDEO=1 also copies it, for
// a local build that plays the video; that copy is gitignored, and without the
// flag a stale one is removed so the video never disagrees with the stills
// beside it. A declared walkthrough that has not been recorded, or whose last
// recording failed, fails the copy naming it.
//
// Run via `make docs-walkthroughs`.
import { copyFileSync, existsSync, mkdirSync, readFileSync, readdirSync, unlinkSync } from 'node:fs';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { declaredIds, walkPages } from './lib/frontmatter-ids.mjs';
import { videoName, walkthroughAssets } from '../src/plugins/walkthrough-embed.mjs';

const OUT = fileURLToPath(new URL('../../web/e2e/walkthroughs/out', import.meta.url));
const DEST = fileURLToPath(new URL('../public/walkthroughs', import.meta.url));
const withVideo = process.env.DOCS_WALKTHROUGH_VIDEO === '1';

const ids = new Set();
for (const path of walkPages()) {
  for (const id of declaredIds(readFileSync(path, 'utf8').split('\n'), 'walkthroughs')) ids.add(id);
}

const failedFile = join(OUT, 'failed.txt');
const failed = existsSync(failedFile) ? readFileSync(failedFile, 'utf8').split(/\s+/).filter(Boolean) : [];

const errors = [];
for (const id of [...ids].sort()) {
  const src = join(OUT, id);
  const manifestPath = join(src, 'walkthrough.json');
  if (failed.includes(id)) {
    errors.push(`walkthrough "${id}" failed its last recording (out/failed.txt); fix it and re-run make walkthroughs ID=${id}`);
    continue;
  }
  if (!existsSync(manifestPath)) {
    errors.push(`walkthrough "${id}" is not recorded (no ${manifestPath}); run make walkthroughs ID=${id}`);
    continue;
  }
  const manifest = JSON.parse(readFileSync(manifestPath, 'utf8'));
  if (manifest.id !== id) {
    errors.push(`${manifestPath} is walkthrough "${manifest.id}", not "${id}"`);
    continue;
  }
  const files = walkthroughAssets(manifest);
  const video = videoName(id);
  if (withVideo) files.push(video);
  const missing = files.filter((f) => !existsSync(join(src, f)));
  if (missing.length) {
    errors.push(`walkthrough "${id}" is missing ${missing.join(', ')} under ${src}; re-run make walkthroughs ID=${id}`);
    continue;
  }
  const dest = join(DEST, id);
  mkdirSync(dest, { recursive: true });
  // A still from an older, longer recording, or a video the flag did not ask
  // for, would outlive the recording it came from.
  for (const f of readdirSync(dest)) {
    if (!files.includes(f)) unlinkSync(join(dest, f));
  }
  for (const f of files) copyFileSync(join(src, f), join(dest, f));
  console.log(`docs-walkthroughs: ${id}: ${files.length} files into public/walkthroughs/${id}/${withVideo ? ' (with the video)' : ''}`);
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
