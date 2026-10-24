// withFragments wraps the docs collection's loader so a page that splices a
// fragment directory (remark-fragments.mjs) re-renders when the directory
// changes (#1095). Astro keys a cached page render on the page file's own
// digest, and a host page's file does not change when an entry is added, so
// without this a restored cache (a Cloudflare Pages build, a second local
// build) or a running `pnpm dev` would keep serving the log without it.
//
// Two seams of the public loader context: generateDigest mixes the fragment
// files (and the renderer's source) into a host page's digest, and in dev the
// watcher re-announces the host page whenever a fragment changes.
import { createHash } from 'node:crypto';
import { readdirSync, readFileSync, statSync } from 'node:fs';
import { basename, join, sep } from 'node:path';
import { fileURLToPath } from 'node:url';
import { DIRECTIVES, directivesIn, readFragmentDir } from './fragments.mjs';

const RENDERER = ['fragments.mjs', 'remark-fragments.mjs'];

/**
 * A digest over everything a page's directives render from.
 * @param {string} srcDir the docs src/ directory
 * @param {string} text the page file
 */
export function fragmentDigest(srcDir, text) {
  const h = createHash('sha256');
  for (const name of RENDERER) h.update(readFileSync(join(srcDir, 'plugins', name)));
  const dirs = [...new Set(directivesIn(text).map((d) => DIRECTIVES[d].dir))].sort();
  for (const dir of dirs) {
    for (const f of readFragmentDir(join(srcDir, 'content', dir))) {
      h.update(`${dir}/${f.name}\0${f.text.length}\0`).update(f.text);
    }
  }
  return h.digest('hex');
}

/** Every .md/.mdx page under dir whose directives read the fragment directory `fragDir`. */
function* pagesReading(dir, fragDir) {
  for (const name of readdirSync(dir)) {
    const path = join(dir, name);
    if (statSync(path).isDirectory()) yield* pagesReading(path, fragDir);
    else if (/\.mdx?$/.test(name) && directivesIn(readFileSync(path, 'utf8')).some((d) => DIRECTIVES[d].dir === fragDir)) {
      yield path;
    }
  }
}

/**
 * @param {import('astro/loaders').Loader} loader
 * @returns {import('astro/loaders').Loader}
 */
export function withFragments(loader) {
  return {
    ...loader,
    load: (ctx) => {
      const srcDir = fileURLToPath(ctx.config.srcDir);
      /** @type {typeof ctx.generateDigest} */
      const generateDigest = (data) =>
        typeof data === 'string' && directivesIn(data).length > 0
          ? ctx.generateDigest(`${data}\0fragments:${fragmentDigest(srcDir, data)}`)
          : ctx.generateDigest(data);
      // Registered on every load, as Astro's glob loader does: a full content
      // re-sync removes every listener a loader added, then loads again.
      const { watcher } = ctx;
      if (watcher) {
        const dirs = [...new Set(Object.values(DIRECTIVES).map((d) => join(srcDir, 'content', d.dir)))];
        watcher.add(dirs);
        watcher.on('all', (_event, path) => {
          const dir = dirs.find((d) => path.startsWith(d + sep));
          if (!dir) return;
          for (const page of pagesReading(join(srcDir, 'content', 'docs'), basename(dir))) watcher.emit('change', page);
        });
      }
      return loader.load({ ...ctx, generateDigest });
    },
  };
}
