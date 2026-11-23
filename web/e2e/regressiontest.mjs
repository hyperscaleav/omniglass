// The regression-test gate (#2380): a PR that closes a bug or flaky-test
// issue (label or Bug type) must add or change a test, or say why not with a line of its own,
// `regression-test: none (<reason>)`. Pure functions, no I/O; the command line
// is regression-test.mjs, run by regression-test.yml.
import { visibleLines } from './walkthroughpr.mjs';

// An issue is gated by either signal: the native Issue Type `Bug`, or a `bug` or
// `flaky-test` label (tickets filed by hand often carry the label and no type).
export const GATED_TYPE = 'Bug';
export const GATED_LABELS = ['bug', 'flaky-test'];

/** @param {{ type?: string, labels?: string[] }} i @returns {boolean} */
export const isGated = (i) => i.type === GATED_TYPE || (i.labels ?? []).some((l) => GATED_LABELS.includes(l));

const CLOSING = /\b(?:close[sd]?|fix(?:e[sd])?|resolve[sd]?)\b:?\s+(?:[\w.-]+\/[\w.-]+)?#(\d+)/gi;
const EXEMPT = /^\s*regression-test:\s*none\b\s*(?:\(([^)]*)\))?\s*$/i;

// The issue numbers the body closes by a closing keyword, in order, once each.
// Comments and fenced code are not the PR's own words (visibleLines).
/** @param {string} body @returns {number[]} */
export function closedIssues(body) {
  /** @type {number[]} */
  const out = [];
  for (const [, line] of visibleLines(body)) {
    for (const m of line.matchAll(CLOSING)) {
      const n = Number(m[1]);
      if (!out.includes(n)) out.push(n);
    }
  }
  return out;
}

// The reason of the body's `regression-test: none (<reason>)` line: a string
// (trimmed, possibly empty when the parentheses are empty or missing), or null
// when no line is an exemption.
/** @param {string} body @returns {string | null} */
export function exemption(body) {
  for (const [, line] of visibleLines(body)) {
    const m = EXEMPT.exec(line);
    if (m) return (m[1] ?? '').trim();
  }
  return null;
}

const TEST_FILE = [/_test\.go$/, /\.test\.tsx?$/, /\.spec\.[^/]+$/, /^web\/e2e\//];

/** @param {string} p @returns {boolean} */
export const isTestFile = (p) => TEST_FILE.some((re) => re.test(p));

// Whether any changed path is a test file (testdata alone is not one).
/** @param {string[]} changed @returns {boolean} */
export function touchesTest(changed) {
  return changed.some(isTestFile);
}

// The verdict: { ok, message }. `closed` is [{number, type, labels}] for each closed
// issue the caller looked up.
/**
 * @param {{ body: string, changed: string[], closed: { number: number, type: string, labels?: string[] }[] }} a
 * @returns {{ ok: boolean, message: string }}
 */
export function check({ body, changed, closed }) {
  const gated = closed.filter(isGated);
  if (!gated.length) return { ok: true, message: 'regression-test: the PR closes no bug or flaky-test issue' };
  const named = gated.map((i) => `#${i.number}`).join(', ');
  if (touchesTest(changed)) return { ok: true, message: `regression-test: the diff changes a test (closes ${named})` };
  const reason = exemption(body);
  if (reason) return { ok: true, message: `regression-test: none (${reason}) (closes ${named})` };
  const why =
    reason === ''
      ? 'its `regression-test: none ()` line gives no reason'
      : 'its diff adds or changes no test file';
  return {
    ok: false,
    message:
      `regression-test: this PR closes ${named}, a bug or flaky-test ticket, and ${why}. ` +
      'Either add or change a test (*_test.go, *.test.ts(x), *.spec.*, or a file under web/e2e/) that fails without the fix, ' +
      'or add a line of its own to the PR body: regression-test: none (<why no test can show this>).',
  };
}
