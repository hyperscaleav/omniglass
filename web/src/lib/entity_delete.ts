import { createSignal } from "solid-js";
import type { BladeDestructive } from "./blades";
import { describeError } from "./format";

// The detail body's destructive action (#799, shared by both hosts since
// #2423): Delete addresses by uuid (a duplicate name under another parent is
// legal) and confirms first. A failed delete (a 409 on a still-referenced row)
// keeps the body where it is and says so; only a success leaves, by the
// host's own way out (a blade closes its stack, a page goes back to Explore).
export function useEntityDelete(opts: {
  kindLabel: string;
  id: string;
  name: () => string | undefined;
  actions: () => string[];
  remove: (id: string) => Promise<unknown>;
  invalidate: () => unknown;
  onDeleted: () => void;
}) {
  const [err, setErr] = createSignal<string | null>(null);
  const destructive = (): BladeDestructive | undefined =>
    opts.actions().includes("delete")
      ? {
          label: "Delete",
          tone: "danger" as const,
          onClick: () => {
            const name = opts.name();
            if (!name || !confirm(`Delete ${opts.kindLabel} "${name}"?`)) return;
            void opts.remove(opts.id).then(
              async () => {
                opts.onDeleted();
                await opts.invalidate();
              },
              (e: unknown) => setErr(describeError(e)),
            );
          },
        }
      : undefined;
  return { destructive, err };
}
