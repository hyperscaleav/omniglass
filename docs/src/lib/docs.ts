// Parsed docs content for the llms.txt / llms-full.txt artifacts.
// Reads the raw markdown at build time (no content-layer body dependency), so the
// generated files always match the docs. .mdx pages (live components) are excluded
// by the `*.md` glob.
const raw = import.meta.glob('/src/content/docs/**/*.md', {
  query: '?raw',
  import: 'default',
  eager: true,
}) as Record<string, string>;

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
  const body = (fm ? src.slice(fm[0].length) : src).trim();
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
