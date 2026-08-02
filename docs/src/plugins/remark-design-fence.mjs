// remarkDesignFence renders a `:::design` container into a visually distinct
// aside marking its prose as target design, not built behavior. The fence is
// the machine-readable boundary the docs lint suite keys on: the strict
// existence lints skip fenced regions, the vocabulary lint does not
// (internal/docslint/regions.go holds the same contract on the Go side).
//
// Every fence must name the issue or ADR that tracks the gap in its label,
// e.g. `:::design[Target design, tracked in #434]` or `:::design[ADR-0071]`.
// A fence with neither fails the build, so unbuilt prose can never be fenced
// without an owner to chase it.
//
// Requires remark-directive to run first (it parses the `:::` syntax). Nesting
// follows remark-directive's rule: a fence containing another container (a
// `:::note` aside) uses more colons than what it contains, `::::design`.
import { visit } from 'unist-util-visit';

// The one definition of "has an owner": an issue number or an ADR. The
// pre-build gate (scripts/check-design-fences.mjs) imports this so the two
// checks cannot drift. The gate exists because a remark file.fail is swallowed
// by the content loader on .md pages (logged, page renders empty, build exits
// zero); the fail below still surfaces the error inline in dev.
export const trackerRef = /(#\d+|ADR-\d{4})/;

function textOf(node) {
  if (node.type === 'text' || node.type === 'inlineCode') return node.value;
  return (node.children ?? []).map(textOf).join('');
}

export function remarkDesignFence() {
  return (tree, file) => {
    visit(tree, (node) => {
      if (node.type !== 'containerDirective' || node.name !== 'design') return;
      const label = node.children?.find((c) => c.data?.directiveLabel);
      if (!label || !trackerRef.test(textOf(label))) {
        file.fail(
          ':::design needs a label naming the issue or ADR that tracks the gap, e.g. :::design[Target design, tracked in #434]',
          node,
        );
      }
      node.data = {
        ...node.data,
        hName: 'aside',
        hProperties: { class: 'design-fence', 'aria-label': 'Target design, not yet built' },
      };
      label.data = {
        ...label.data,
        hName: 'p',
        hProperties: { class: 'design-fence-title' },
      };
    });
  };
}
