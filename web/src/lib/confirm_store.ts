import { createSignal } from "solid-js";
import type { DeleteBlocker, DeleteRefusal } from "../api/api.gen";

// The state behind the console's one destructive confirm (#2463): the open
// request, read by ConfirmHost and written by lib/confirm. Split from
// lib/confirm so the host can read it without importing the module that
// mounts the host.

export type ConfirmTone = "danger" | "action";

export type ConfirmRequest = {
  // The dialog's heading: a question that names the thing.
  title: string;
  // What goes with it, or what follows; plain text.
  body?: string;
  // The confirm button's label: the verb ("Delete", "Revoke", "Discard").
  confirmLabel: string;
  tone?: ConfirmTone;
  // The act, run inside the dialog. Resolves the request true on success;
  // a throw keeps the dialog open with the error (and any blockers) shown.
  run?: () => Promise<unknown>;
};

export type PendingConfirm = ConfirmRequest & { resolve: (ok: boolean) => void };

const [pending, setPending] = createSignal<PendingConfirm | null>(null);
export { pending as pendingConfirm };

export { setPending };

// A host mounted under the router (AppRoot) claims the dialog; lib/confirm
// self-mounts a bare one when none has.
let hosted = false;
export const isConfirmHosted = () => hosted;
let fallback: (() => void) | null = null;
export const hasFallbackHost = () => fallback !== null;
export function setFallbackHost(dispose: () => void) {
  fallback = dispose;
}
export function claimConfirmHost(): () => void {
  hosted = true;
  return () => {
    hosted = false;
  };
}

// refusalOf reads a thrown error as a refused delete that names what is in
// the way, or null when it is any other failure.
export function refusalOf(e: unknown): { blockers: DeleteBlocker[]; hidden: number } | null {
  const r = e as DeleteRefusal | null | undefined;
  if (!r || typeof r !== "object" || r.status !== 409) return null;
  const blockers = r.blockers ?? [];
  const hidden = r.blockers_hidden ?? 0;
  if (blockers.length === 0 && hidden === 0) return null;
  return { blockers, hidden };
}

// blockerHref is where a blocker's link leads: its own page. An endpoint has
// no page of its own (it opens in its component's blade), so it has none.
export function blockerHref(b: Pick<DeleteBlocker, "kind" | "id">): string | undefined {
  switch (b.kind) {
    case "component":
      return `/components/${b.id}`;
    case "system":
      return `/systems/${b.id}`;
    case "location":
      return `/locations/${b.id}`;
    default:
      return undefined;
  }
}


// For tests: drop any open request and the self-mounted host.
export function resetConfirm() {
  pending()?.resolve(false);
  setPending(null);
  fallback?.();
  fallback = null;
}
