import { Show, type ParentComponent, createEffect } from "solid-js";
import { useLocation, useNavigate } from "@solidjs/router";
import { useMe } from "../lib/auth";

// AuthGuard gates the console chrome. It reads /auth/me; while pending it shows
// a loader, on 401 (me === null) it redirects to /login carrying the attempted
// path, and on success it renders the children (the App shell + page). Login is
// mounted outside this guard so the form has its own bare layout.
export const AuthGuard: ParentComponent = (props) => {
  const me = useMe();
  const location = useLocation();
  const navigate = useNavigate();

  createEffect(() => {
    if (me.isPending) return;
    if (me.data === null) {
      // Not authenticated (no/invalid session cookie): redirect to login carrying
      // the attempted path.
      const next = encodeURIComponent(location.pathname + location.search);
      navigate(next === "%2F" ? "/login" : `/login?next=${next}`, { replace: true });
    }
  });

  return (
    <Show when={!me.isPending && me.data !== null} fallback={<FullScreenLoader pending={me.isPending} />}>
      {props.children}
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
