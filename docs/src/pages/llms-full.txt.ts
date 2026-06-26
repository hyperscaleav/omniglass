import type { APIRoute } from 'astro';
import { pages } from '../lib/docs';

export const prerender = true;

export const GET: APIRoute = () => {
  const header = [
    '# Omniglass documentation (full text)',
    '',
    'Omniglass is an open, AV-native observability and control plane for AV and IT estates: one Go binary over PostgreSQL. This file concatenates the whole documentation site as one machine-readable artifact for LLM tools (NotebookLM and the like).',
    '',
    'This is a proposed, forward-looking architecture; per-page build status (Design / Partial / Built / Diverged) lives at /architecture/status/. Source: https://docs.omniglass.hyperscaleav.com/',
    '',
  ].join('\n');

  const body = pages
    .map((p) => {
      const head = `# ${p.title}\n\nURL: ${p.url}\n${p.description ? `\n${p.description}\n` : ''}`;
      return `${head}\n${p.body}\n`;
    })
    .join('\n---\n\n');

  return new Response(`${header}\n${body}`, {
    headers: { 'Content-Type': 'text/plain; charset=utf-8' },
  });
};
