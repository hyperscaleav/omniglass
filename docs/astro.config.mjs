// @ts-check
import mdx from '@astrojs/mdx';
import sitemap from '@astrojs/sitemap';
import starlight from '@astrojs/starlight';
import mermaid from 'astro-mermaid';
import { defineConfig } from 'astro/config';

export default defineConfig({
  site: 'https://docs.omniglass.hyperscaleav.com',
  integrations: [
    mermaid({
      theme: 'dark',
      autoTheme: true,
    }),
    starlight({
      title: 'Omniglass',
      description:
        'Open observability and control plane for AV and IT estates, and a place to learn how one is built.',
      logo: {
        light: './public/logo-light.svg',
        dark: './public/logo-dark.svg',
        alt: 'Omniglass',
        replacesTitle: true,
      },
      favicon: '/favicon.svg',
      social: [
        {
          icon: 'github',
          label: 'GitHub',
          href: 'https://github.com/hyperscaleav/omniglass',
        },
      ],
      editLink: {
        baseUrl: 'https://github.com/hyperscaleav/omniglass/edit/main/docs/',
      },
      sidebar: [
        {
          label: 'Architecture',
          items: [
            { label: 'Why Omniglass', slug: 'architecture/why' },
            { label: 'Overview', slug: 'architecture' },
            { label: 'Taxonomy', slug: 'architecture/taxonomy' },
            { label: 'Variables', slug: 'architecture/variables' },
            { label: 'Identity and access', slug: 'architecture/identity-access' },
            { label: 'Components', slug: 'architecture/components' },
            { label: 'Collection', slug: 'architecture/collection' },
            { label: 'Cascade', slug: 'architecture/cascade' },
            { label: 'Alarms and actions', slug: 'architecture/alarms-actions' },
            { label: 'Health, SLI, and SLA', slug: 'architecture/health' },
            { label: 'Nodes', slug: 'architecture/nodes' },
            { label: 'Time', slug: 'architecture/time' },
            { label: 'Storage', slug: 'architecture/storage' },
            { label: 'Workers', slug: 'architecture/workers' },
            { label: 'Audit', slug: 'architecture/audit' },
            { label: 'Files and blobs', slug: 'architecture/files' },
            { label: 'AI', slug: 'architecture/ai' },
            { label: 'UI', slug: 'architecture/ui' },
            { label: 'Expressions', slug: 'architecture/expressions' },
          ],
        },
        {
          label: 'Contributing',
          items: [
            { label: 'API first', slug: 'contributing/api-first' },
            { label: 'Test-driven', slug: 'contributing/test-driven' },
            { label: 'Docs with everything', slug: 'contributing/docs-with-everything' },
            { label: 'Learning tool', slug: 'contributing/learning-tool' },
            { label: 'Design system', slug: 'contributing/design-system' },
          ],
        },
      ],
      customCss: ['./src/styles/custom.css'],
      expressiveCode: {
        themes: ['github-light', 'github-dark'],
      },
    }),
    mdx(),
    sitemap(),
  ],
});
