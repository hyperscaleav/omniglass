// @ts-check
// The one way explorer.mjs runs a child process (#1777). gh's open-issue
// listing (bodies, limit 1000) runs to megabytes, so the buffer is raised
// well past Node's 1 MiB execFileSync default, which fails with ENOBUFS.
import { execFileSync } from 'node:child_process';

export const MAX_BUFFER = 256 * 1024 * 1024;

/**
 * @param {string} bin
 * @param {string[]} a
 * @param {string} [input]
 * @returns {string}
 */
export const run = (bin, a, input) =>
  execFileSync(bin, a, { encoding: 'utf8', input, maxBuffer: MAX_BUFFER, stdio: [input ? 'pipe' : 'ignore', 'pipe', 'inherit'] });
