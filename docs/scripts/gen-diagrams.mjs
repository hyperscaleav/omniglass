// Pre-render every ```d2 block in the docs to a committed SVG under public/d2/.
//
// astro-d2 runs with `skipGeneration: true` (see astro.config.mjs): the Cloudflare
// Pages build only inlines these committed SVGs, so it needs no `d2` binary and avoids
// the D2 WASM parser, which mis-parses our source. Run this whenever a diagram changes:
//
//   pnpm diagrams        (needs the `d2` binary on PATH: go install oss.terrastruct.com/d2@latest)
//
// Output path and the d2 flags mirror astro-d2 so the committed SVG matches what the
// integration expects (it reads the file, checks the d2 version attribute, inlines it).
import { execFileSync } from 'node:child_process';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const contentDir = path.join(root, 'src/content/docs');
const outRoot = path.join(root, 'public/d2');

// Keep these in sync with the d2() options in astro.config.mjs.
const D2_FLAGS = ['--layout=elk', '--theme=0', '--dark-theme=200', '--sketch=false', '--pad=24'];

/** Every .md/.mdx under the content dir, recursively. */
function* mdFiles(dir) {
  for (const e of fs.readdirSync(dir, { withFileTypes: true })) {
    const p = path.join(dir, e.name);
    if (e.isDirectory()) yield* mdFiles(p);
    else if (/\.mdx?$/.test(e.name)) yield p;
  }
}

const fence = /```d2\n([\s\S]*?)\n```/g;
let count = 0;

for (const file of mdFiles(contentDir)) {
  // Mirror astro-d2: strip the src/content/(docs|pages)/ prefix, name blocks <stem>-<i>.svg.
  const rel = path.relative(contentDir, file).replace(/\.mdx?$/, '');
  const src = fs.readFileSync(file, 'utf8');
  let i = 0;
  for (const m of src.matchAll(fence)) {
    const outPath = path.join(outRoot, 'docs', `${rel}-${i}.svg`);
    fs.mkdirSync(path.dirname(outPath), { recursive: true });
    try {
      execFileSync('d2', [...D2_FLAGS, '-', outPath], { input: m[1], stdio: ['pipe', 'inherit', 'inherit'] });
    } catch {
      console.error(`\nFailed to render ${path.relative(root, file)} block #${i}`);
      process.exit(1);
    }
    count++;
    i++;
  }
}

console.log(`Rendered ${count} D2 diagram(s) to ${path.relative(root, outRoot)}/`);
