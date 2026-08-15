import { useSearchParams } from "@solidjs/router";
import { createEffect, createSignal, on } from "solid-js";
import type { BladeEdit } from "./blades";

// The edit face is a URL fact (#759): ?edit=1 beside a detail route (or a blade's
// id param, `?u=<id>&edit=1`) requests edit mode, and leaving edit strips the param
// again. One hook owns both directions so every consumer gets the same contract:
// a deep link or a refresh lands editing (permission allowing), and Cancel or Save
// strips the param in place, so the history entry the operator is on no longer
// requests an edit they already left. This replaces the one-shot in-memory
// handoffs (pendingedit, openPrincipalInEdit), which no URL could express.
export function useEditParam(edit: BladeEdit | undefined, opts: { ready: () => boolean; canUpdate: () => boolean }): { request: () => void } {
  const [params, setParams] = useSearchParams();
  const requested = () => (Array.isArray(params.edit) ? params.edit[0] : params.edit) === "1";

  // The param is an intent consumed ONCE per appearance, not a state the mode is
  // derived from: begin() fires when the param shows up (or is present on mount)
  // and the body is ready, and never again until the param transitions back in.
  // Deriving the mode from the param directly would re-enter edit in the gap
  // between Cancel's setEditing(false) and the strip below; a transition that
  // lands while already editing (request() below begins synchronously, and the
  // router delivers the param a beat later) must not arm either, or the next
  // Cancel re-begins off the stale intent.
  const [pending, setPending] = createSignal(requested());
  createEffect(on(requested, (req) => setPending(req && !(edit?.editing() ?? false)), { defer: true }));
  createEffect(() => {
    if (pending() && opts.ready() && opts.canUpdate() && edit && !edit.editing()) {
      setPending(false);
      edit.begin();
    }
  });

  // Leaving edit (Cancel or a completed Save) strips the param via history
  // replace: the current entry stops requesting edit, so a refresh stays
  // read-only and Back lands on the face the operator actually left. The callback
  // only ever fires on an editing() CHANGE (defer skips the initial run), so
  // editing false here is always an exit; do not gate on the prev argument, which
  // is undefined on the first post-defer invocation (begin() can land before this
  // effect's first capture, making the exit that very invocation).
  createEffect(on(() => edit?.editing() ?? false, (editing) => {
    if (!editing && requested()) setParams({ edit: undefined }, { replace: true });
  }, { defer: true }));

  // The detail's own Edit affordance pushes the mode into the URL (a new history
  // entry, so Back returns to read-only) and begins in the same tick: the operator
  // (and every existing test) sees the edit face synchronously, not a router
  // roundtrip later.
  return {
    request: () => {
      setParams({ edit: "1" });
      if (edit && !edit.editing() && opts.ready() && opts.canUpdate()) edit.begin();
    },
  };
}
