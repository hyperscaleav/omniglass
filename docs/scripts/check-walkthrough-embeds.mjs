// check-walkthrough-embeds runs after `astro build` (#926) and holds every
// built walkthrough embed to the files this build shipped:
//
//   - every walkthrough asset a built page references (a still, the video and
//     its link) exists in the build output; and
//   - every embed is what the shipped manifest renders now, with the video if
//     and only if the build ships the mp4 (compared in canonical form, since
//     Astro's serializer rewrites the same markup: canonicalEmbed).
//
// Astro caches each rendered page keyed on the page's own content; it does not
// know the embed depends on walkthrough.json or on whether the mp4 exists. So a
// re-copied walkthrough (new captions, a new still count) or a video added or
// removed leaves a cached render that says the old thing, and this check is
// what turns that staleness into a failed build. The fix is a forced build:
// `corepack pnpm astro build --force` in docs/.
import { existsSync, readFileSync, readdirSync, statSync } from 'node:fs';
import { join, relative } from 'node:path';
import { fileURLToPath } from 'node:url';
import { BASE, canonicalEmbed, findEmbeds, renderWalkthrough, videoName } from '../src/plugins/walkthrough-embed.mjs';

const dist = fileURLToPath(new URL('../dist', import.meta.url));
const ref = /(?:src|href|poster)="(\/walkthroughs\/[^"]+)"/g;

function* html(dir) {
  for (const name of readdirSync(dir)) {
    const path = join(dir, name);
    if (statSync(path).isDirectory()) yield* html(path);
    else if (name.endsWith('.html')) yield path;
  }
}

const problems = [];
let refs = 0;
let embeds = 0;
for (const page of html(dist)) {
  const text = readFileSync(page, 'utf8');
  const where = relative(dist, page);
  for (const m of text.matchAll(ref)) {
    refs++;
    if (!existsSync(join(dist, decodeURIComponent(m[1])))) problems.push(`${where}: references ${m[1]}, which the build did not ship`);
  }
  for (const { id, html: built } of findEmbeds(text)) {
    embeds++;
    const dir = join(dist, BASE, id);
    let want;
    try {
      const manifest = JSON.parse(readFileSync(join(dir, 'walkthrough.json'), 'utf8'));
      want = renderWalkthrough(manifest, { video: existsSync(join(dir, videoName(id))) });
    } catch (e) {
      problems.push(`${where}: walkthrough "${id}" cannot be re-rendered from the shipped manifest: ${e.message}`);
      continue;
    }
    if (canonicalEmbed(built) !== canonicalEmbed(want)) problems.push(`${where}: walkthrough "${id}" is not what its shipped manifest renders (a stale cached render)`);
  }
}

if (problems.length) {
  console.error(`${problems.length} walkthrough embed problem(s); after copying walkthroughs, force the build (cd docs && corepack pnpm astro build --force):`);
  for (const p of [...new Set(problems)]) console.error('  ' + p);
  process.exit(1);
}
console.log(`check-walkthrough-embeds: OK (${embeds} embeds match their manifests, ${refs} walkthrough references all shipped)`);
