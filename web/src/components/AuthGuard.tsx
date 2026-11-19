import { Show, type ParentComponent, createEffect } from "solid-js";
import { useLocation, useNavigate } from "@solidjs/router";
import { useMe } from "../lib/auth";
import { useAuthStatus } from "../lib/authStatus";
import { encodeNext } from "../lib/next";
import ForceChangePassword from "./ForceChangePassword";

// AuthGuard gates the console chrome. It reads /auth/me; while pending it shows
// a loader, on 401 (me === null) it redirects to /login carrying the attempted
// path, and on success it renders the children (the App shell + page). Login is
// mounted outside this guard so the form has its own bare layout. It also holds
// the loader until /auth/status settles, fetched alongside /auth/me: the shell's
// lab banner hangs on that answer, and inserting it after the first paint would
// shift every screen down (#2003). A failed status read lets the shell in bare.
export const AuthGuard: ParentComponent = (props) => {
  const me = useMe();
  const status = useAuthStatus();
  const location = useLocation();
  const navigate = useNavigate();

  createEffect(() => {
    if (me.isPending) return;
    if (me.data === null) {
      // Not authenticated (no/invalid session cookie): redirect to login carrying
      // the attempted path. encodeNext and Login's resolveNext are inverses kept
      // in one module so they cannot drift apart again (#646).
      const next = encodeNext(location.pathname, location.search, location.hash);
      navigate(next === null ? "/login" : `/login?next=${next}`, { replace: true });
    }
  });

  return (
    <Show when={!me.isPending && !status.isPending && me.data !== null} fallback={<FullScreenLoader pending={me.isPending || status.isPending} />}>
      {/* An admin-reset account is gated to the change-password screen (the server
          refuses every other route until it clears), replacing the whole shell. */}
      <Show when={me.data?.person?.must_change_password} fallback={props.children}>
        <ForceChangePassword />
      </Show>
    </Show>
  );
};

function FullScreenLoader(props: { pending: boolean }) {
  return (
    <div class="flex min-h-screen items-center justify-center gap-3 bg-base-100 text-sm text-base-content/50">
      <Show when={props.pending} fallback={<span>Redirecting…</span>}>
        <span class="loading loading-spinner loading-md text-primary" />
      </Show>
    </div>
  );
}
