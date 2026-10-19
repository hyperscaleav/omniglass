import { docsLoader } from '@astrojs/starlight/loaders';
import { docsSchema } from '@astrojs/starlight/schema';
import { defineCollection, z } from 'astro:content';

// A page declares the screenshots it teaches with, right beside its prose. This
// frontmatter is the single source: the generator (web/e2e/docs-shots.mjs) reads
// it to know what to capture, and the `::screenshot{#id}` directive renders the
// image from the same entry, so the capture list and the embed can never drift
// apart. The PNGs themselves are a generated resource (see `make docs-shots`).
const screenshotStep = z.object({
  action: z.enum(['click', 'hover', 'fill', 'press']),
  selector: z.string().optional(),
  value: z.string().optional(),
});

const screenshot = z.object({
  // Stable key: the PNG is public/screenshots/<id>.png and the directive is ::screenshot{#<id>}.
  id: z.string(),
  // Console route to capture, e.g. /web/secrets.
  path: z.string(),
  // Alt text; also the caption the embed renders.
  alt: z.string(),
  // false for the signed-out login screen; otherwise the dev bearer token is injected.
  auth: z.boolean().default(true),
  // Optional interactions before the shot (open a blade, hover a tooltip).
  steps: z.array(screenshotStep).default([]),
  // Playwright selectors to mask (cover with a solid box) before the shot, for
  // non-deterministic regions like timestamps, so the image stays byte-stable and
  // the drift gate does not fire on data that legitimately changes every capture.
  mask: z.array(z.string()).default([]),
});

// A page declares the walkthroughs it embeds the same way (#926). The script is
// web/e2e/walkthroughs/<id>.yaml and everything the embed shows (title,
// captions, poster, stills) comes from the recorder's manifest, copied into
// public/walkthroughs/<id>/ by `make docs-walkthroughs`, so the declaration is
// only the id: restating a caption here would drift from the recording.
const walkthrough = z.object({
  // The walkthrough's id: its script, its out dir, and ::walkthrough{#<id>}.
  id: z.string().regex(/^[a-z0-9][a-z0-9-]*$/),
});

const docs = defineCollection({
  loader: docsLoader(),
  schema: docsSchema({
    extend: z.object({
      screenshots: z.array(screenshot).optional(),
      walkthroughs: z.array(walkthrough).optional(),
    }),
  }),
});

export const collections = { docs };
