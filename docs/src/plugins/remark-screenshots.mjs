// remarkScreenshots renders a `::screenshot{#id}` leaf directive into a figure,
// pulling the image and caption from the page's `screenshots` frontmatter. That
// frontmatter is the single source the capture generator also reads, so the
// embed and the captured PNG cannot drift: a `#id` with no matching frontmatter
// entry fails the build, and a renamed id breaks in both places at once.
//
// The directive resolution is the shared frontmatterDirective; its build gate is
// scripts/check-embeds.mjs, wired into `pnpm build` (#519).
import { frontmatterDirective } from './frontmatter-directive.mjs';

export function remarkScreenshots() {
  return frontmatterDirective('screenshot', 'screenshots', (spec) => {
    const alt = String(spec.alt ?? '').replace(/"/g, '&quot;');
    // Static path under public/; the file is a generated resource (make docs-shots).
    // That the PNG exists is checked by `make docs-shots-verify`, not here (the
    // build render is cached, so a file check in-plugin would not run reliably).
    return (
      `<figure class="screenshot">` +
      `<img src="/screenshots/${spec.id}.png" alt="${alt}" loading="lazy" />` +
      `<figcaption>${alt}</figcaption>` +
      `</figure>`
    );
  });
}
