// Dev tool, not a gate: diff two capture dirs of the SAME code and report where
// the rasters jitter, as bounding boxes of connected diff clusters. Used to
// locate every nondeterministic region so its page can mask it (#398, #623).
// Usage: node web/e2e/shot-jitter.mjs <dirA> <dirB>
import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import { readdir } from 'node:fs/promises';
import { PNG } from 'pngjs';
import pixelmatch from 'pixelmatch';

const [a, b] = process.argv.slice(2);
const names = (await readdir(a)).filter((f) => f.endsWith('.png'));

for (const name of names) {
  const A = PNG.sync.read(readFileSync(join(a, name)));
  const B = PNG.sync.read(readFileSync(join(b, name)));
  if (A.width !== B.width || A.height !== B.height) { console.log(`${name}: SIZE ${A.width}x${A.height} vs ${B.width}x${B.height}`); continue; }
  const out = new PNG({ width: A.width, height: A.height });
  const n = pixelmatch(A.data, B.data, out.data, A.width, A.height, { threshold: 0.1 });
  if (n === 0) { console.log(`${name}: clean`); continue; }
  // Cluster diff pixels into coarse boxes on a 40px grid.
  const cell = 40, cols = Math.ceil(A.width / cell), rows = Math.ceil(A.height / cell);
  const hit = new Set();
  for (let y = 0; y < A.height; y++) for (let x = 0; x < A.width; x++) {
    const i = (y * A.width + x) * 4;
    if (out.data[i] === 255 && out.data[i + 1] === 0) hit.add(`${Math.floor(x / cell)},${Math.floor(y / cell)}`);
  }
  // Merge hit cells into row-spans for readability.
  const boxes = [];
  for (let r = 0; r < rows; r++) {
    let start = null;
    for (let c = 0; c <= cols; c++) {
      const h = hit.has(`${c},${r}`);
      if (h && start === null) start = c;
      if (!h && start !== null) { boxes.push(`x${start * cell}-${c * cell} y${r * cell}-${(r + 1) * cell}`); start = null; }
    }
  }
  console.log(`${name}: ${n}px in ${hit.size} cells -> ${boxes.slice(0, 12).join(' | ')}${boxes.length > 12 ? ` (+${boxes.length - 12})` : ''}`);
}
