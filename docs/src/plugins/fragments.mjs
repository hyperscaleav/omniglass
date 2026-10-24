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
    return v;
  }
  if (raw.startsWith("'")) {
    if (raw.length < 2 || !raw.endsWith("'") || raw.slice(1, -1).replace(/''/g, '').includes("'")) {
      throw new Error(`${name}: ${key} is not a valid single-quoted string: ${raw}`);
    }
    return raw.slice(1, -1).replace(/''/g, "'");
  }
  if (raw.startsWith('[')) {
    let v;
    try {
      v = JSON.parse(raw);
    } catch {
      v = null;
    }
    if (!Array.isArray(v) || !v.every((n) => Number.isInteger(n) && n > 0)) {
      throw new Error(`${name}: ${key} must be a list of positive integers, like [12, 34]`);
    }
    return v;
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
      if (f.data.form === undefined) return `### ${f.data.title}\n\n${f.body}`;
      if (f.data.form !== 'bullet') throw new Error(`${name}: form is "bullet" or absent`);
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
    const taken = byNumber.get(m[1]);
    if (taken) {
      throw new Error(
        `ADR-${m[1]} is taken by both ${taken} and ${name}: renumber the one your branch added ` +
          `(make adr-renumber FROM=${m[1]} TO=<next free number>) before merging main`,
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

/** Each directive, the fragment directory it reads (under src/content/), and its render. */
export const DIRECTIVES = {
  'build-log': { dir: 'build-log', render: renderBuildLog },
  'decision-index': { dir: 'decisions', render: renderDecisionIndex },
  'decision-log': { dir: 'decisions', render: renderDecisionLog },
};

const directiveLine = /^::([a-z-]+)[ \t]*$/;

/**
 * The fragment directives a page uses, in page order.
 * @param {string} text
 * @returns {string[]}
 */
export function directivesIn(text) {
  return text
    .split('\n')
    .map((line) => directiveLine.exec(line.replace(/\r$/, ''))?.[1])
    .filter((name) => name !== undefined && Object.hasOwn(DIRECTIVES, name));
}

/**
 * The page with each directive line replaced by its render.
 * @param {string} text the page body
 * @param {(dir: string) => FragmentSource[]} load the files of a fragment directory
 * @returns {string}
 */
export function expandFragments(text, load) {
  if (directivesIn(text).length === 0) return text;
  return text
    .split('\n')
    .map((line) => {
      const name = directiveLine.exec(line.replace(/\r$/, ''))?.[1];
      if (name === undefined || !Object.hasOwn(DIRECTIVES, name)) return line;
      const { dir, render } = DIRECTIVES[name];
      return render(load(dir));
    })
    .join('\n');
}
