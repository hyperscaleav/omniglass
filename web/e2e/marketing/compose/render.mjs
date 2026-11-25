// @ts-check
// The camera pass of a marketing loop (#2299):
//
//   node render.mjs <in.mp4> <camera.json> <seconds> <out.mp4>
//
// <in.mp4> is the trimmed recording, <camera.json> the recorder's focus log
// ({ viewport, events, lapses }), <seconds> the clip's length. The moves come from
// cameraTrack (web/e2e/marketinglib.mjs, unit tested); Revideo renders them at
// the viewport's size. web/e2e/marketing.sh runs this offline in the
// Playwright image, after a pnpm install here (compose_deps).
import { copyFileSync, mkdirSync, readFileSync, rmSync } from 'node:fs';
import { basename, dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { CAMERA, cameraTrack, lapseBadges } from '../../marketinglib.mjs';

// Revideo reports usage to its maintainers unless told not to; a capture run
// sends nothing anywhere.
process.env.DISABLE_TELEMETRY = 'true';
const { renderVideo } = await import('@revideo/renderer');

const [input, cameraFile, seconds, output] = process.argv.slice(2);
if (!input || !cameraFile || !seconds || !output || !output.endsWith('.mp4')) {
  console.error('usage: node render.mjs <in.mp4> <camera.json> <seconds> <out.mp4>');
  process.exit(2);
}
const duration = Number(seconds);
const { viewport, events, lapses = [] } = JSON.parse(readFileSync(cameraFile, 'utf8'));
const moves = cameraTrack(events, viewport, duration);
const badges = lapseBadges(lapses);

// The Playwright image's Chromium, unless CHROME names another.
const chrome = process.env.CHROME ?? (await import('@playwright/test')).chromium.executablePath();
// Paths are taken from the caller's directory; the render runs from this one.
const src = resolve(input);
const out = resolve(output);
const here = dirname(fileURLToPath(import.meta.url));
process.chdir(here);

// Revideo serves media from the project's public/ directory and probes it
// there, so the recording is staged into it for the render and removed after.
const staged = `${process.pid}-${basename(input)}`;
mkdirSync('public', { recursive: true });
copyFileSync(src, `public/${staged}`);
// Revideo looks for a clip's audio at <outDir>/../public/<src>, so the render
// writes to ./out here, beside the public/ the recording is staged in, and the
// result is moved to where the caller asked.
const made = `${process.pid}-${basename(out)}`;
mkdirSync('out', { recursive: true });
const file = await renderVideo({
  projectFile: './src/project.tsx',
  variables: { src: `/${staged}`, width: viewport.width, height: viewport.height, duration, moves, badges, dim: CAMERA.dim },
  settings: {
    outFile: /** @type {`${string}.mp4`} */ (made),
    outDir: './out',
    logProgress: false,
    projectSettings: { size: { x: viewport.width, y: viewport.height }, background: CAMERA.backdrop },
    puppeteer: { executablePath: chrome, args: ['--no-sandbox'] },
    viteConfig: { logLevel: 'error' },
  },
}).finally(() => rmSync(`public/${staged}`, { force: true }));
copyFileSync(resolve(file), out);
rmSync(resolve(file), { force: true });
console.log(`marketing: camera ${basename(out)}: ${moves.length} moves, ${badges.length} sped-up stretches`);
