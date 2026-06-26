import type { APIRoute } from 'astro';
import { pages } from '../lib/docs';

export const prerender = true;

export const GET: APIRoute = () => {
  const list = (section: string) =>
    pages
      .filter((p) => p.section === section)
      .map((p) => `- [${p.title}](${p.url})${p.description ? `: ${p.description}` : ''}`)
      .join('\n');

  const out = [
    '# Omniglass',
    '',
    '> Open, AV-native observability and control plane for AV and IT estates: one Go binary over PostgreSQL. A proposed, forward-looking architecture, published ahead of the code.',
    '',
    'The full documentation as one file: [/llms-full.txt](/llms-full.txt).',
  ];

  for (const section of ['Overview', 'Architecture', 'Contributing']) {
    const items = list(section);
    if (items) out.push('', `## ${section}`, '', items);
  }
  out.push('');

  return new Response(out.join('\n'), {
    headers: { 'Content-Type': 'text/plain; charset=utf-8' },
  });
};
