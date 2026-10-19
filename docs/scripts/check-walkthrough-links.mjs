// check-walkthrough-links runs after `astro build` (#926): every walkthrough
// asset the built pages reference (the poster, the stills, and the video and
// its link) must exist in the build output. The embed decides whether to render
// the video by whether the mp4 is present when the page renders, and Astro
// caches rendered pages between builds, so a page rendered while a local mp4
// existed could otherwise publish a link to a video this build does not ship.
// This makes "never link a file that is not there" a gate on the HTML itself.
import { existsSync, readFileSync, readdirSync, statSync } from 'node:fs';
import { join, relative } from 'node:path';
import { fileURLToPath } from 'node:url';

const dist = fileURLToPath(new URL('../dist', import.meta.url));
const ref = /(?:src|href|poster)="(\/walkthroughs\/[^"]+)"/g;

function* html(dir) {
  for (const name of readdirSync(dir)) {
    const path = join(dir, name);
    if (statSync(path).isDirectory()) yield* html(path);
    else if (name.endsWith('.html')) yield path;
  }
}

const dead = [];
let refs = 0;
for (const page of html(dist)) {
  const text = readFileSync(page, 'utf8');
  for (const m of text.matchAll(ref)) {
    refs++;
    if (!existsSync(join(dist, decodeURIComponent(m[1])))) dead.push(`${relative(dist, page)}: ${m[1]}`);
  }
}

if (dead.length) {
  console.error(
    `${dead.length} walkthrough reference(s) to a file the build did not ship (a stale cached render? rebuild with \`astro build --force\`):`,
  );
  for (const d of [...new Set(dead)]) console.error('  ' + d);
  process.exit(1);
}
console.log(`check-walkthrough-links: OK (${refs} walkthrough references, all shipped)`);
