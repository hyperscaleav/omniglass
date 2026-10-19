// astro-build runs `astro build` for `pnpm build` (#937), forced exactly when
// what the walkthrough embed reads has changed since the cached render: the
// copied manifests, which files sit beside them, and the plugin sources
// (lib/render-stamp.mjs). Astro's content cache (node_modules/.astro, which a
// Cloudflare Pages build restores from the last build) keys a page render on
// the page's own content alone, so without this a re-copied walkthrough ships
// its old embed. The stamp is written beside that cache only after a build
// succeeds, so a failed build forces the next one too.
// check-walkthrough-embeds.mjs still runs after this and fails any stale embed.
import { spawnSync } from 'node:child_process';
import { existsSync, mkdirSync, readFileSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { astroBuildArgs, collectRenderInputs, renderDigest } from './lib/render-stamp.mjs';

const docs = fileURLToPath(new URL('..', import.meta.url));
const repo = join(docs, '..');
// Astro's default cacheDir, which holds the content store with the page renders.
const cacheDir = join(docs, 'node_modules', '.astro');
const stampPath = join(cacheDir, 'walkthrough-render.stamp');

const digest = renderDigest(collectRenderInputs(repo));
const stamp = existsSync(stampPath) ? readFileSync(stampPath, 'utf8') : null;
const args = astroBuildArgs(digest, stamp);
console.log(
  args.includes('--force')
    ? `astro-build: walkthrough render inputs ${stamp === null ? 'have no stamp' : 'changed'}; forcing the build (astro ${args.join(' ')})`
    : 'astro-build: walkthrough render inputs unchanged; building from the cache',
);

const run = spawnSync(join(docs, 'node_modules', '.bin', 'astro'), args, { cwd: docs, stdio: 'inherit' });
if (run.error) {
  console.error(`astro-build: could not run astro: ${run.error.message}`);
  process.exit(1);
}
if (run.status !== 0) process.exit(run.status ?? 1);
mkdirSync(cacheDir, { recursive: true });
writeFileSync(stampPath, digest + '\n');
