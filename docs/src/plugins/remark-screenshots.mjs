// remarkScreenshots renders a `::screenshot{#id}` leaf directive into a figure,
// pulling the image and caption from the page's `screenshots` frontmatter. That
// frontmatter is the single source the capture generator also reads, so the
// embed and the captured PNG cannot drift: a `#id` with no matching frontmatter
// entry fails the build, and a renamed id breaks in both places at once.
//
// Requires remark-directive to run first (it parses the `::` syntax).
import { visit } from 'unist-util-visit';

export function remarkScreenshots() {
  return (tree, file) => {
    const specs = file.data?.astro?.frontmatter?.screenshots ?? [];
    const byId = new Map(specs.map((s) => [s.id, s]));

    visit(tree, (node) => {
      if (node.type !== 'leafDirective' || node.name !== 'screenshot') return;
      const id = node.attributes?.id;
      if (!id) {
        file.fail('::screenshot needs an id, e.g. ::screenshot{#secrets}', node);
      }
      const spec = byId.get(id);
      if (!spec) {
        file.fail(
          `::screenshot{#${id}} has no matching entry in this page's screenshots frontmatter`,
          node,
        );
      }
      const alt = String(spec.alt ?? '').replace(/"/g, '&quot;');
      // Static path under public/; the file is a generated resource (make docs-shots).
      // That the PNG exists is checked by `make docs-shots-verify`, not here (the
      // build render is cached, so a file check in-plugin would not run reliably).
      node.type = 'html';
      node.value =
        `<figure class="screenshot">` +
        `<img src="/screenshots/${id}.png" alt="${alt}" loading="lazy" />` +
        `<figcaption>${alt}</figcaption>` +
        `</figure>`;
      delete node.children;
      delete node.attributes;
      delete node.name;
    });
  };
}
