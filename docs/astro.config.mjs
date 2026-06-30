// @ts-check
import mdx from '@astrojs/mdx';
import sitemap from '@astrojs/sitemap';
import starlight from '@astrojs/starlight';
import d2 from 'astro-d2';
import { defineConfig } from 'astro/config';

export default defineConfig({
  site: 'https://docs.omniglass.hyperscaleav.com',
  integrations: [
    // Diagrams are authored in D2 and rendered to inline SVG. ELK layout; dark theme
    // 200, light theme 0; inline so the SVG embeds in the page and the brand tokens in
    // custom.css can theme it with the light/dark toggle.
    //
    // skipGeneration: the SVGs are pre-rendered with the d2 binary and committed under
    // public/d2/, and the build only inlines them. This keeps the Cloudflare Pages build
    // hermetic (no d2 binary, and the D2 WASM mis-parses our source). Regenerate after
    // editing any diagram: `pnpm diagrams` (d2 binary on PATH). See /docs-diagram.
    d2({
      layout: 'elk',
      pad: 24,
      inline: true,
      theme: { default: '0', dark: '200' },
      skipGeneration: true,
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
      components: {
        // Render the page's sidebar.badge next to the H1 too, so the
        // built-vs-theory status shows on the page, not just in the nav.
        PageTitle: './src/components/PageTitle.astro',
        // Mount the diagram lightbox (click-to-expand) on every page.
        Head: './src/components/Head.astro',
      },
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
            { label: 'Implementation status', slug: 'architecture/status' },
            // the estate model, then the shapes it pins
            { label: 'Core entities', slug: 'architecture/core-entities' },
            { label: 'Templates', slug: 'architecture/templates' },
            // the journey, in the order the data travels
            { label: 'Data collection', slug: 'architecture/collection' },
            { label: 'Datapoints', slug: 'architecture/datapoints' },
            { label: 'Events', slug: 'architecture/events' },
            { label: 'Calculations', slug: 'architecture/calculations' },
            { label: 'Config & credentials', slug: 'architecture/variables' },
            { label: 'Cascade', slug: 'architecture/cascade' },
            { label: 'Groups', slug: 'architecture/groups' },
            { label: 'Health & KPIs', slug: 'architecture/health' },
            { label: 'Alarms and actions', slug: 'architecture/alarms-actions' },
            { label: 'UI', slug: 'architecture/ui' },
            { label: 'Views', slug: 'architecture/views' },
            { label: 'API', slug: 'architecture/api' },
            { label: 'Messaging', slug: 'architecture/messaging' },
            // the foundations underneath
            { label: 'Nodes', slug: 'architecture/nodes' },
            { label: 'Storage', slug: 'architecture/storage' },
            { label: 'Workers', slug: 'architecture/workers' },
            { label: 'Scaling and deployment', slug: 'architecture/scaling' },
            { label: 'Time', slug: 'architecture/time' },
            { label: 'Identity and access', slug: 'architecture/identity-access' },
            { label: 'Audit', slug: 'architecture/audit' },
            { label: 'Files and blobs', slug: 'architecture/files' },
            { label: 'AI', slug: 'architecture/ai' },
            { label: 'Expressions', slug: 'architecture/expressions' },
            { label: 'Glossary', slug: 'architecture/glossary' },
          ],
        },
        {
          label: 'Contributing',
          items: [
            { label: 'API first', slug: 'contributing/api-first' },
            { label: 'Test-driven', slug: 'contributing/test-driven' },
            { label: 'Docs with everything', slug: 'contributing/docs-with-everything' },
            { label: 'Learning tool', slug: 'contributing/learning-tool' },
            { label: 'Primitive first', slug: 'contributing/primitive-first' },
            { label: 'Design system', slug: 'contributing/design-system' },
            { label: 'Slice workflow', slug: 'contributing/slice-workflow' },
          ],
        },
        {
          label: 'Guides',
          items: [
            { label: 'The console', slug: 'guides/console' },
            { label: 'The CLI', slug: 'guides/cli' },
            { label: 'Container image', slug: 'guides/container-image' },
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
