// The fragment-directory primitive (#1095). An append-only log is a directory
// of one file per entry, never one shared file, so two PRs that each add an
// entry add two files and cannot conflict. A page names where the entries go
// with a directive on a line of its own, and the docs build splices the
// rendered entries in there before the page is parsed:
//
//   ::build-log        every file under src/content/build-log/, oldest first
//   ::decision-index   the decision log's index table, from src/content/decisions/
//   ::decision-log     the decision log's entries, one ### ADR-NNNN heading each
//
// Splicing text before the parse (remark-fragments.mjs) is what keeps the page
// one document: heading ids dedupe across the whole page, the table of contents
// lists every entry, and smartypants, lists and tables behave exactly as they
// did when the entries were written into the page by hand.
//
// A fragment is frontmatter plus a body. The frontmatter is a deliberately small
// line grammar rather than YAML, so the Go side (internal/docslint/fragments.go)
// reads it identically: one `key: value` per line, a value being a JSON string,
// a single-quoted string ('' escapes a quote), a JSON list of integers, or a
// bare word. Everything here except readFragmentDir is pure.
import { readdirSync, readFileSync } from 'node:fs';
import { join } from 'node:path';
import { slug } from 'github-slugger';

/** @typedef {{ name: string, text: string }} FragmentSource */
/** @typedef {{ name: string, data: Record<string, string | number[]>, body: string }} Fragment */

const keyLine = /^([a-z][a-z_]*):(?: (.*))?$/;
// An issue or PR number: no leading zero, at most nine digits.
const intList = /^\[\s*(?:[1-9]\d{0,8}\s*(?:,\s*[1-9]\d{0,8}\s*)*)?\]$/;
// Characters a frontmatter line or value may not carry: a lone carriage return
// and the Unicode line separators (JS and Go disagree on whether `.` matches
// them), and in a decoded string any control character, which would split the
// heading a title renders into.
const lineBreakers = /[\r\u2028\u2029]/;
const controlChar = /[\u0000-\u001f\u007f\u2028\u2029]/;

/**
 * Parse one fragment file. Throws an Error naming the file on any malformed line.
 * @param {string} name the file name, used in messages
 * @param {string} text the file contents
 * @returns {Fragment}
 */
export function parseFragment(name, text) {
  const lines = text.replace(/\r\n/g, '\n').split('\n');
  if (lines[0] !== '---') throw new Error(`${name}: a fragment must open with a --- frontmatter fence`);
  const close = lines.indexOf('---', 1);
  if (close === -1) throw new Error(`${name}: the frontmatter fence never closes`);
  /** @type {Record<string, string | number[]>} */
  const data = {};
  for (let i = 1; i < close; i++) {
    const line = lines[i];
    if (lineBreakers.test(line)) throw new Error(`${name}: line ${i + 1} carries a control character`);
    if (line.trim() === '') continue;
    const m = keyLine.exec(line);
    if (!m) throw new Error(`${name}: line ${i + 1} is not a "key: value" line: ${line}`);
    const [, key, raw = ''] = m;
    if (key in data) throw new Error(`${name}: duplicate key "${key}"`);
    data[key] = parseValue(name, key, raw.trim());
  }
  const body = lines
    .slice(close + 1)
    .join('\n')
    .replace(/^\n+/, '')
    .replace(/\s+$/, '');
  if (body === '') throw new Error(`${name}: empty body`);
  return { name, data, body };
}

function parseValue(name, key, raw) {
  if (raw.startsWith('"')) {
    let v;
    try {
      v = JSON.parse(raw);
    } catch {
      throw new Error(`${name}: ${key} is not a valid double-quoted string: ${raw}`);
    }
    if (typeof v !== 'string') throw new Error(`${name}: ${key} is not a string`);
    if (controlChar.test(v)) throw new Error(`${name}: ${key} carries a control character`);
    return v;
  }
  if (raw.startsWith("'")) {
    if (raw.length < 2 || !raw.endsWith("'") || raw.slice(1, -1).replace(/''/g, '').includes("'")) {
      throw new Error(`${name}: ${key} is not a valid single-quoted string: ${raw}`);
    }
    return raw.slice(1, -1).replace(/''/g, "'");
  }
  if (raw.startsWith('[')) {
    // Digits only, so this reader and the Go one accept exactly the same lists.
    if (!intList.test(raw)) throw new Error(`${name}: ${key} must be a list of positive integers, like [12, 34]`);
    return raw
      .slice(1, -1)
      .split(',')
      .map((t) => t.trim())
      .filter((t) => t !== '')
      .map(Number);
  }
  return raw;
}

/**
 * Every .md file in dir, sorted by name.
 * @param {string} dir
 * @returns {FragmentSource[]}
 */
export function readFragmentDir(dir) {
  return readdirSync(dir)
    .filter((n) => n.endsWith('.md'))
    .sort(byName)
    .map((name) => ({ name, text: readFileSync(join(dir, name), 'utf8') }));
}

// Code-point order, so the log's order never depends on the build host's locale.
const byName = (a, b) => (a < b ? -1 : a > b ? 1 : 0);

const isoDate = /^\d{4}-\d{2}-\d{2}$/;

const bodyDirective = /^ {0,3}:{2,}[A-Za-z]/;
const fenceOpen = /^ {0,3}(`{3,}|~{3,})/;

/**
 * Refuse a directive line (::screenshot, ::walkthrough, :::design, ...) in an
 * entry body. Those resolve against the page's own frontmatter and fail the
 * render when they cannot, and a render error on a .md page is swallowed (the
 * page publishes empty), so an entry may only show one inside a code fence.
 * @param {Fragment} f
 */
function checkNoDirectives(f) {
  let fence = '';
  for (const line of f.body.split('\n')) {
    const m = fenceOpen.exec(line);
    if (fence) {
      if (m && m[1][0] === fence[0] && m[1].length >= fence.length && line.trim() === m[1]) fence = '';
      continue;
    }
    if (m) {
      fence = m[1];
      continue;
    }
    if (bodyDirective.test(line)) {
      throw new Error(`${f.name}: an entry body cannot carry a directive (${line.trim()}); show one inside a code fence`);
    }
  }
}

/**
 * @param {Fragment} f
 * @param {string[]} required
 * @param {string[]} optional
 */
function checkKeys(f, required, optional = []) {
  for (const key of Object.keys(f.data)) {
    if (!required.includes(key) && !optional.includes(key)) throw new Error(`${f.name}: unknown key "${key}"`);
  }
  for (const key of required) {
    const v = f.data[key];
    if (v === undefined || v === '') throw new Error(`${f.name}: needs ${key === 'refs' ? 'refs' : `a ${key}`} in its frontmatter`);
  }
  if (typeof f.data.date === 'string' && !isoDate.test(f.data.date)) {
    throw new Error(`${f.name}: date "${f.data.date}" is not YYYY-MM-DD`);
  }
}

// The build log's files are <YYYY-MM-DD>-<slug>.md. The entries that predate
// the split carry a four-digit sequence instead (0001-<slug>.md), which sorts
// ahead of every date and keeps them in the order they were logged.
const buildLogName = /^(?:(\d{4}-\d{2}-\d{2})|\d{4})-[a-z0-9][a-z0-9-]*\.md$/;

/**
 * The build log, oldest first: a section entry renders under a ### heading made
 * from its title; a historical `form: bullet` entry is its own list item.
 * @param {FragmentSource[]} sources
 * @returns {string}
 */
export function renderBuildLog(sources) {
  return [...sources]
    .sort((a, b) => byName(a.name, b.name))
    .map(({ name, text }) => {
      const m = buildLogName.exec(name);
      if (!m) throw new Error(`${name}: a build-log entry is named <YYYY-MM-DD>-<slug>.md (lowercase slug)`);
      const f = parseFragment(name, text);
      checkKeys(f, ['title', 'date', 'refs'], ['form']);
      if (!Array.isArray(f.data.refs)) throw new Error(`${name}: refs must be a list, like [1095] or []`);
      if (m[1] && m[1] !== f.data.date) throw new Error(`${name}: the file name names ${m[1]} but its date is ${f.data.date}`);
      checkNoDirectives(f);
      if (f.data.form === undefined) return `### ${f.data.title}\n\n${f.body}`;
      if (f.data.form !== 'bullet') throw new Error(`${name}: form is "bullet" or absent`);
      // A bullet has no heading of its own, so after a section it would render
      // inside that section: only the history before the split keeps the form.
      if (m[1]) throw new Error(`${name}: form: bullet is the sequence-numbered history's; a new entry is a section (drop the form key)`);
      if (!f.body.startsWith('- ')) throw new Error(`${name}: a bullet entry's body must start with "- "`);
      return f.body;
    })
    .join('\n\n');
}

const adrName = /^adr-(\d{4})-[a-z0-9][a-z0-9-]*\.md$/;

/**
 * The decision log's entries, parsed, checked and in number order. Two files
 * holding one number (two PRs that each took the next free one) is refused by
 * name, since the second heading would silently take a -1 anchor.
 * @param {FragmentSource[]} sources
 */
function decisions(sources) {
  const byNumber = new Map();
  const out = sources.map(({ name, text }) => {
    const m = adrName.exec(name);
    if (!m) throw new Error(`${name}: a decision is named adr-NNNN-<slug>.md (four digits, lowercase slug)`);
    const f = parseFragment(name, text);
    checkKeys(f, ['title', 'date', 'status', 'summary']);
    checkNoDirectives(f);
    const taken = byNumber.get(m[1]);
    if (taken) {
      throw new Error(
        `ADR-${m[1]} is taken by both ${taken} and ${name}: renumber the one that landed second with ` +
          `make adr-renumber FROM=${m[1]} TO=<next free number> FILE=<its file name> (see /record-decision)`,
      );
    }
    byNumber.set(m[1], name);
    return { number: m[1], ...f };
  });
  return out.sort((a, b) => byName(a.number, b.number));
}

/**
 * The anchor a `### ADR-NNNN: <title>` heading renders with: github-slugger, as
 * Astro's heading ids use, over the heading's text with the inline markdown
 * (code spans, links, emphasis) reduced to its words.
 * @param {string} number four digits
 * @param {string} title the heading's markdown after "ADR-NNNN: "
 */
export function adrAnchor(number, title) {
  const text = `ADR-${number}: ${title}`
    .replace(/`([^`]*)`/g, '$1')
    .replace(/\[([^\]]*)\]\([^)]*\)/g, '$1')
    .replace(/\*\*([^*]+)\*\*/g, '$1')
    .replace(/\*([^*]+)\*/g, '$1');
  return slug(text);
}

// A bare | in a cell would end the cell, and GFM drops a row's surplus cells, so
// a summary carrying one would lose its tail from the index without a word.
const cell = (s) => s.replace(/(?<!\\)\|/g, '\\|');

/**
 * The index table: one row per decision, the ID linking to its entry.
 * @param {FragmentSource[]} sources
 */
export function renderDecisionIndex(sources) {
  const rows = decisions(sources).map(
    (d) =>
      `| [ADR-${d.number}](#${adrAnchor(d.number, d.data.title)}) | ${d.data.date} | ${cell(d.data.status)} | ${cell(d.data.summary)} |`,
  );
  return ['| ID | Date | Status | Decision |', '|---|---|---|---|', ...rows].join('\n');
}

/**
 * The entries, each under its numbered heading.
 * @param {FragmentSource[]} sources
 */
export function renderDecisionLog(sources) {
  return decisions(sources)
    .map((d) => `### ADR-${d.number}: ${d.data.title}\n\n${d.body}`)
    .join('\n\n');
}

/**
 * The post-build check of the decision log (scripts/check-fragment-pages.mjs):
 * each decision whose index anchor names no heading id on the built page. That
 * catches an anchor adrAnchor computes differently from the page (a title shape
 * it does not reduce the way Astro does) and a page whose render was swallowed.
 * @param {string} html the built decisions page
 * @param {FragmentSource[]} sources
 * @returns {string[]}
 */
export function missingLogIds(html, sources) {
  const ids = new Set([...html.matchAll(/\sid="([^"]+)"/g)].map((m) => m[1]));
  return decisions(sources)
    .map((d) => ({ name: d.name, anchor: adrAnchor(d.number, d.data.title) }))
    .filter((d) => !ids.has(d.anchor))
    .map((d) => `${d.name}: the index links #${d.anchor}, but the built page has no such heading`);
}

/** Each directive, the fragment directory it reads (under src/content/), and its render. */
export const DIRECTIVES = {
  'build-log': { dir: 'build-log', render: renderBuildLog },
  'decision-index': { dir: 'decisions', render: renderDecisionIndex },
  'decision-log': { dir: 'decisions', render: renderDecisionLog },
};

const directiveLine = /^::([a-z-]+)[ \t]*$/;
const codeFence = /^ {0,3}(`{3,}|~{3,})/;

/**
 * Each line with the directive it carries, or undefined; a line inside a fenced
 * code block carries none, so a page can show a directive as an example.
 * @param {string} text
 * @returns {{ line: string, directive: string | undefined }[]}
 */
function scanLines(text) {
  let fence = '';
  return text.split('\n').map((line) => {
    const bare = line.replace(/\r$/, '');
    const f = codeFence.exec(bare);
    if (fence) {
      // A closing fence is the opener's character, at least as long, and nothing after.
      if (f && f[1][0] === fence[0] && f[1].length >= fence.length && bare.trim() === f[1]) fence = '';
      return { line, directive: undefined };
    }
    if (f) {
      fence = f[1];
      return { line, directive: undefined };
    }
    const name = directiveLine.exec(bare)?.[1];
    return { line, directive: name !== undefined && Object.hasOwn(DIRECTIVES, name) ? name : undefined };
  });
}

/**
 * The fragment directives a page uses, in page order.
 * @param {string} text
 * @returns {string[]}
 */
export function directivesIn(text) {
  return scanLines(text)
    .map((l) => l.directive)
    .filter((d) => d !== undefined);
}

/**
 * The page with each directive line replaced by its render.
 * @param {string} text the page body
 * @param {(dir: string) => FragmentSource[]} load the files of a fragment directory
 * @returns {string}
 */
export function expandFragments(text, load) {
  const lines = scanLines(text);
  if (!lines.some((l) => l.directive)) return text;
  return lines
    .map(({ line, directive }) => {
      if (!directive) return line;
      const { dir, render } = DIRECTIVES[directive];
      return render(load(dir));
    })
    .join('\n');
}
