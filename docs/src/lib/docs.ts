import { expandFragments } from '../plugins/fragments.mjs';

// Parsed docs content for the llms.txt / llms-full.txt artifacts.
// Reads the raw markdown at build time (no content-layer body dependency), so the
// generated files always match the docs. .mdx pages (live components) are excluded
// by the `*.md` glob.
const raw = import.meta.glob('/src/content/docs/**/*.md', {
  query: '?raw',
  import: 'default',
  eager: true,
}) as Record<string, string>;

// The build log and the decision log keep one file per entry (#1095); their
// pages carry a directive where the entries go, expanded here exactly as the
// page render expands it, so the full-text artifact carries every entry.
const fragmentFiles = import.meta.glob(['/src/content/build-log/*.md', '/src/content/decisions/*.md'], {
  query: '?raw',
  import: 'default',
  eager: true,
}) as Record<string, string>;

const fragmentDir = (dir: string) =>
  Object.entries(fragmentFiles)
    .filter(([path]) => path.startsWith(`/src/content/${dir}/`))
    .map(([path, text]) => ({ name: path.slice(path.lastIndexOf('/') + 1), text }));

export interface DocPage {
  slug: string;
  url: string;
  section: 'Overview' | 'Architecture' | 'Contributing';
  title: string;
  description: string;
  body: string;
}

function field(frontmatter: string, key: string): string {
  const m = frontmatter.match(new RegExp(`^${key}:\\s*(.*)$`, 'm'));
  let v = m ? m[1].trim() : '';
  if (/^".*"$/.test(v) || /^'.*'$/.test(v)) v = v.slice(1, -1);
  return v;
}

function parse(path: string, src: string): DocPage {
  const fm = src.match(/^---\r?\n([\s\S]*?)\r?\n---\r?\n?/);
  const frontmatter = fm ? fm[1] : '';
  const body = expandFragments(fm ? src.slice(fm[0].length) : src, fragmentDir).trim();
  const rel = path.replace('/src/content/docs/', '').replace(/\.md$/, '');
  const slug = rel === 'index' ? '' : rel.replace(/\/index$/, '');
  const url = '/' + (slug ? slug + '/' : '');
  const section = slug.startsWith('architecture')
    ? 'Architecture'
    : slug.startsWith('contributing')
      ? 'Contributing'
      : 'Overview';
  return {
    slug,
    url,
    section,
    title: field(frontmatter, 'title') || slug || 'Omniglass',
    description: field(frontmatter, 'description'),
    body,
  };
}

// Journey order: site index, then Why, then Overview, then the rest alphabetically.
const PRIORITY = ['', 'architecture/why', 'architecture'];
const rank = (slug: string) => {
  const i = PRIORITY.indexOf(slug);
  return i === -1 ? PRIORITY.length : i;
};

export const pages: DocPage[] = Object.entries(raw)
  .map(([path, src]) => parse(path, src))
  .sort((a, b) => rank(a.slug) - rank(b.slug) || a.slug.localeCompare(b.slug));
