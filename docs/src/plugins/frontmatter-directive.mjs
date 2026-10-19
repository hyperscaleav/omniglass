// frontmatterDirective is the shared shape of the docs' generated-resource
// embeds: a `::<name>{#id}` leaf directive resolved against an entry in the
// page's `<key>` frontmatter, then replaced by the HTML `render` returns. The
// screenshot (`::screenshot` / `screenshots`) and the walkthrough
// (`::walkthrough` / `walkthroughs`) embeds are its two consumers, so a directive
// with no id, or an id its page does not declare, fails the same way for both.
//
// The fails reach the dev overlay; on a .md page the content loader swallows
// them at build time (logged, page rendered empty, exit zero), so the build gate
// is scripts/check-embeds.mjs, which runs before `astro build` (#519).
//
// Requires remark-directive to run first (it parses the `::` syntax).
import { visit } from 'unist-util-visit';

/**
 * @param {string} name the directive name, e.g. 'screenshot'
 * @param {string} key the frontmatter list that declares its ids, e.g. 'screenshots'
 * @param {(spec: any, file: any, node: any) => string} render the HTML for one declared entry
 */
export function frontmatterDirective(name, key, render) {
  return (tree, file) => {
    const specs = file.data?.astro?.frontmatter?.[key] ?? [];
    const byId = new Map(specs.map((s) => [s.id, s]));

    visit(tree, (node) => {
      if (node.type !== 'leafDirective' || node.name !== name) return;
      const id = node.attributes?.id;
      if (!id) {
        file.fail(`::${name} needs an id, e.g. ::${name}{#some-id}`, node);
      }
      const spec = byId.get(id);
      if (!spec) {
        file.fail(`::${name}{#${id}} has no matching entry in this page's ${key} frontmatter`, node);
      }
      node.type = 'html';
      node.value = render(spec, file, node);
      delete node.children;
      delete node.attributes;
      delete node.name;
    });
  };
}
