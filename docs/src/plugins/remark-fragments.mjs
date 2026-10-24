// remarkFragments splices the fragment directories into a page (#1095): a line
// that is exactly `::build-log`, `::decision-index` or `::decision-log` is
// replaced by the rendered entries (fragments.mjs) BEFORE the page is parsed.
//
// Before the parse, not as a tree transform: Astro runs smartypants ahead of
// every user remark plugin, so entries spliced into the tree afterwards would
// keep straight quotes, and a list or a heading spliced in as its own subtree
// would neither join the page's list nor dedupe its anchor against the page's
// other headings. Wrapping the parser makes the page one document, exactly as
// it was when the entries were written into it by hand.
//
// The pre-build gate (scripts/check-fragments.mjs) renders every fragment
// directory first, because an error thrown here on a .md page is swallowed by
// the content loader (logged, page rendered empty, exit zero).
import { join } from 'node:path';
import { expandFragments, readFragmentDir } from './fragments.mjs';

/** @param {{ contentDir: string }} options the src/content directory */
export function remarkFragments({ contentDir }) {
  const parse = this.parser;
  if (typeof parse !== 'function') return;
  this.parser = (doc, file) =>
    parse(
      expandFragments(String(doc), (dir) => readFragmentDir(join(contentDir, dir))),
      file,
    );
}
