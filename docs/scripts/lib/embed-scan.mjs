// @ts-check
// The one reading of "this page embeds ::<name>{#id}" the docs scripts share
// (check-embeds.mjs, for screenshots and walkthroughs alike), and the shape the
// Go lint mirrors (internal/docslint/walkthroughs.go). An embed is a leaf
// directive alone on its line, as remark-directive parses one: not an inline
// text directive (`:name{#id}`, or `::name{#id}` mid-line), not a `:::`
// container, and not a quote of the syntax in a fenced code block, an inline
// code span or an HTML comment, all of which teach the syntax rather than embed.

const codeFence = /^\s*(`{3,}|~{3,})/;

/**
 * Every embed of directive `name` in the page, with its 1-indexed line and its
 * id (null when the directive carries none).
 * @param {string[]} lines the page's lines
 * @param {string} name e.g. 'walkthrough'
 * @returns {{ line: number, id: string | null }[]}
 */
export function scanDirectives(lines, name) {
  const leaf = new RegExp(`^\\s*::${name}(?:\\{([^}]*)\\})?\\s*$`);
  const out = [];
  let i = 0;
  if (lines[0]?.trim() === '---') {
    const close = lines.findIndex((l, j) => j > 0 && l.trim() === '---');
    if (close > 0) i = close + 1;
  }
  let inCode = false;
  let delim = '';
  let inComment = false;
  for (; i < lines.length; i++) {
    let line = lines[i];
    if (inCode) {
      const t = line.trimStart();
      if (t.startsWith(delim) && /^[`~]*\s*$/.test(t.slice(delim.length))) inCode = false;
      continue;
    }
    if (!inComment) {
      const code = codeFence.exec(line);
      if (code) {
        inCode = true;
        delim = code[1];
        continue;
      }
    }
    // Strip HTML comments, which may open on one line and close on a later one.
    let kept = '';
    while (line.length) {
      if (inComment) {
        const close = line.indexOf('-->');
        if (close < 0) {
          line = '';
        } else {
          line = line.slice(close + 3);
          inComment = false;
        }
      } else {
        const open = line.indexOf('<!--');
        if (open < 0) {
          kept += line;
          line = '';
        } else {
          kept += line.slice(0, open);
          line = line.slice(open + 4);
          inComment = true;
        }
      }
    }
    const m = leaf.exec(kept.replace(/`[^`]*`/g, ''));
    if (!m) continue;
    const id = /(?:^|\s)#([A-Za-z0-9_-]+)/.exec(m[1] ?? '');
    out.push({ line: i + 1, id: id ? id[1] : null });
  }
  return out;
}
