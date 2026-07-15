import { createSignal } from "solid-js";

// A one-shot "open this entity's detail in edit mode" handoff, the inventory analog
// of openPrincipalInEdit for Users. Create-as-route sets it just before navigating
// from /<entity>/create to /<entity>/<newId>, and the row pencil sets it before
// navigating to a node's full page, so the detail lands already in edit. The detail
// body consumes it once when its node resolves. A module signal (not page state) so
// it survives the route change that remounts the page.
const [pending, setPending] = createSignal<string | null>(null);

// Mark an entity id to open in edit on its next detail render.
export function openInEdit(id: string): void {
  setPending(id);
}

// Consume the flag if it matches this id: returns true once, then clears it so a
// later remount does not re-enter edit.
export function consumePendingEdit(id: string): boolean {
  if (pending() === id) {
    setPending(null);
    return true;
  }
  return false;
}
