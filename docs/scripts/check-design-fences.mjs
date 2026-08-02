// check-design-fences fails the docs build when a `:::design` fence has no
// owner: every fence's label must name the issue or ADR that tracks the gap
// (the trackerRef contract shared with the remark plugin that renders the
// fence). It runs before `astro build` because a remark `file.fail` on a .md
// page is swallowed by the content loader (logged, page rendered empty, exit
// zero), which would let an unowned fence publish silently.
//
// Scans source lines rather than the remark tree so .md and .mdx behave
// identically; a fence opener inside a fenced code block is an example, not a
// fence, and is skipped.
import { readFileSync, readdirSync, statSync } from 'node:fs';
import { join, relative } from 'node:path';
import { fileURLToPath } from 'node:url';
import { trackerRef } from '../src/plugins/remark-design-fence.mjs';

const root = fileURLToPath(new URL('../src/content/docs', import.meta.url));
const fenceOpen = /^\s*:{3,}design\b(.*)$/;
const codeFence = /^\s*(`{3,}|~{3,})/;

function* walk(dir) {
  for (const name of readdirSync(dir)) {
    const path = join(dir, name);
    if (statSync(path).isDirectory()) yield* walk(path);
    else if (/\.(md|mdx)$/.test(name)) yield path;
  }
}

const failures = [];
for (const path of walk(root)) {
  let inCode = false;
  let delim = '';
  readFileSync(path, 'utf8')
    .split('\n')
    .forEach((line, i) => {
      if (inCode) {
        // A closing code fence has no info string (CommonMark): "```go"
        // inside a "```" block is content, not a close.
        const t = line.trimStart();
        if (t.startsWith(delim) && /^[`~]*\s*$/.test(t.slice(delim.length))) inCode = false;
        return;
      }
      const code = codeFence.exec(line);
      if (code) {
        inCode = true;
        delim = code[1];
        return;
      }
      const open = fenceOpen.exec(line);
      if (open && !trackerRef.test(open[1])) {
        failures.push(`${relative(root, path)}:${i + 1}: :::design has no issue or ADR reference in its label`);
      }
    });
}

if (failures.length > 0) {
  console.error(`${failures.length} unowned design fence(s); every fence names the issue or ADR that tracks its gap, e.g. :::design[Target design, tracked in #434]`);
  for (const f of failures) console.error('  ' + f);
  process.exit(1);
}
