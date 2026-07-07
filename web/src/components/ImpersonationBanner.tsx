import { Show } from "solid-js";
import { useQueryClient } from "@tanstack/solid-query";
import { actingAs, stopImpersonating } from "../lib/impersonation";

// A persistent banner shown while impersonating another principal, so the operator
// always knows whose eyes they are seeing through and can stop. The server
// enforces the actual read-only (view-as) and audit; this is the visible cue.
export default function ImpersonationBanner() {
  const qc = useQueryClient();
  return (
    <Show when={actingAs()}>
      {(a) => (
        <div class="flex items-center gap-3 border-b border-warning/40 bg-warning/15 px-8 py-2 text-sm" role="status">
          <span class="badge badge-warning badge-sm font-semibold uppercase tracking-wide">
            {a().mode === "view_as" ? "Viewing as" : "Acting as"}
          </span>
          <span class="font-medium">{a().target}</span>
          <span class="text-base-content/60">
            {a().mode === "view_as" ? "read-only" : "changes are attributed to both you and them"}
          </span>
          <span class="flex-1" />
          <button class="btn btn-warn btn-xs" onClick={() => stopImpersonating(qc)}>Stop</button>
        </div>
      )}
    </Show>
  );
}
