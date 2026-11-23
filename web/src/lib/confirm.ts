import { mountConfirmHost } from "../components/ConfirmHost";
import {
  type ConfirmRequest,
  type PendingConfirm,
  hasFallbackHost,
  isConfirmHosted,
  pendingConfirm,
  setFallbackHost,
  setPending,
} from "./confirm_store";

export { blockerHref, refusalOf, resetConfirm, type ConfirmRequest } from "./confirm_store";

// The console's one destructive confirm (#2463). Every "are you sure" goes
// through here and renders in the one ConfirmDialog the ConfirmHost draws:
// the title names the thing, the body says what goes with it, and the
// destructive button carries the verb. The browser's own confirm() is
// forbidden under web/src (native-confirm-guard.test.ts): it cannot name a
// blocker, cannot be styled, and blocks the page.
//
// A site stays one line:
//
//   if (!(await confirmDelete({ kind: "tag", name: t.name, consequence: "..." }))) return;
//
// A request with run hands the act itself to the dialog: it stays open while
// run is in flight, keeps open on a failure and says why, and on a refused
// delete that names its blockers (the 409 DeleteRefusal) lists them, each with
// its own link and delete, so the operator clears the way and retries in place.

// confirmAction asks; it resolves true when the operator confirms (and run,
// when given, succeeded), false when they cancel, close or press Escape. A
// second request while one is open cancels the first.
export function confirmAction(req: ConfirmRequest): Promise<boolean> {
  pendingConfirm()?.resolve(false);
  // Without a host under the router (a component test, a page rendered
  // alone) the first request mounts a bare one on document.body, so every
  // site works wherever it is rendered.
  if (!isConfirmHosted() && !hasFallbackHost() && typeof document !== "undefined") setFallbackHost(mountConfirmHost());
  return new Promise<boolean>((resolve) => {
    const entry: PendingConfirm = {
      ...req,
      resolve: (ok) => {
        if (pendingConfirm() === entry) setPending(null);
        resolve(ok);
      },
    };
    setPending(entry);
  });
}

// confirmDelete is the delete form: "Delete <kind> "<name>"?", the
// consequence as the body (else that it cannot be undone), the verb on the
// button. verb is for a delete the console words otherwise (Remove, Purge,
// Withdraw).
export function confirmDelete(opts: {
  kind: string;
  name: string;
  consequence?: string;
  verb?: string;
  run?: () => Promise<unknown>;
}): Promise<boolean> {
  const verb = opts.verb ?? "Delete";
  return confirmAction({
    title: `${verb} ${opts.kind} "${opts.name}"?`,
    body: opts.consequence ?? "This cannot be undone.",
    confirmLabel: `${verb} ${opts.kind}`,
    tone: "danger",
    run: opts.run,
  });
}
