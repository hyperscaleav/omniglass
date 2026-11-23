import { createSignal } from "solid-js";
import type { BladeDestructive } from "./blades";
import { confirmDelete } from "./confirm";

// The detail body's destructive action (#799, shared by both hosts since
// #2423): Delete addresses by uuid (a duplicate name under another parent is
// legal) and confirms first, in the one confirm dialog (#2463). The delete
// runs inside that dialog: a refusal keeps it open and names what is in the
// way (the 409's blockers, each with its own link and delete), so the
// operator clears them and retries without leaving. Only a success leaves,
// by the host's own way out (a blade closes its stack, a page goes back to
// Explore). err stays for a host that renders a failure of its own; the
// dialog owns the delete's.
export function useEntityDelete(opts: {
  kindLabel: string;
  id: string;
  name: () => string | undefined;
  actions: () => string[];
  remove: (id: string) => Promise<unknown>;
  invalidate: () => unknown;
  onDeleted: () => void;
  consequence?: string;
}) {
  const [err] = createSignal<string | null>(null);
  const destructive = (): BladeDestructive | undefined =>
    opts.actions().includes("delete")
      ? {
          label: "Delete",
          tone: "danger" as const,
          onClick: async () => {
            const name = opts.name();
            if (!name) return;
            const done = await confirmDelete({ kind: opts.kindLabel, name, consequence: opts.consequence, run: () => opts.remove(opts.id) });
            if (!done) return;
            opts.onDeleted();
            await opts.invalidate();
          },
        }
      : undefined;
  return { destructive, err };
}
