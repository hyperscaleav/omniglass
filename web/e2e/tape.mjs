// The command line of tapelib.mjs (#1132), run by the terminal recorder
// (web/e2e/tapes.sh) in the Playwright container (node, no install):
//
//   node e2e/tape.mjs wrap <tape>
//       the tape the recorder plays, on stdout: <tape> held to the house rules
//       (a refusal names the line and exits 1) and wrapped in the house
//       settings
//   node e2e/tape.mjs finish <tape> <out> <seconds>
//       the verdict on <tape>'s recording in directory <out>: the commands the
//       prelude listed as failed (failed-commands.txt), the GIF and MP4, and
//       the MP4's length in <seconds> (empty or junk when unread). A pass
//       writes <out>/demo.json, the manifest with the budget mark; a failure
//       prints each problem, removes any demo.json an earlier pass left, and
//       exits 1. OG_TAPE_MAX_GIF_BYTES and OG_TAPE_MAX_SECONDS lower the budget.
import { existsSync, readFileSync, statSync, unlinkSync, writeFileSync } from 'node:fs';
import { basename, join } from 'node:path';
import { parseTape, tapeLimits, tapeManifest, tapeProblems, wrapTape } from './tapelib.mjs';

/** @param {string} file */
function read(file) {
  return parseTape(basename(file, '.tape'), readFileSync(file, 'utf8'), file.replace(/^\.\.\//, ''));
}

/** @param {string} f @returns {number} */
const size = (f) => (existsSync(f) ? statSync(f).size : 0);

function main([cmd, ...args]) {
  switch (cmd) {
    case 'wrap':
      process.stdout.write(wrapTape(read(args[0])) + '\n');
      return 0;
    case 'finish': {
      const [file, out, secs] = args;
      const tape = read(file);
      const failuresFile = join(out, 'failed-commands.txt');
      const rec = {
        failures: existsSync(failuresFile) ? readFileSync(failuresFile, 'utf8') : '',
        gifBytes: size(join(out, `${tape.id}.gif`)),
        mp4Bytes: size(join(out, `${tape.id}.mp4`)),
        seconds: secs === undefined || secs.trim() === '' ? NaN : Number(secs),
      };
      const manifest = join(out, 'demo.json');
      const problems = tapeProblems(tape.id, rec, tapeLimits(process.env));
      if (problems.length) {
        if (existsSync(manifest)) unlinkSync(manifest);
        for (const p of problems) console.error(p);
        return 1;
      }
      writeFileSync(manifest, JSON.stringify(tapeManifest(tape, rec), null, 2) + '\n');
      console.log(`tape ${tape.id}: recorded, ${rec.seconds.toFixed(1)}s, GIF ${rec.gifBytes} bytes, MP4 ${rec.mp4Bytes} bytes`);
      return 0;
    }
    default:
      console.error('usage: tape.mjs wrap <tape> | finish <tape> <out> <seconds>');
      return 2;
  }
}

try {
  process.exitCode = main(process.argv.slice(2));
} catch (e) {
  console.error(e.message);
  process.exitCode = 1;
}
