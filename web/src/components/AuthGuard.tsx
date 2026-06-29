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
    <div style={{ "min-height": "100vh", display: "flex", "align-items": "center", "justify-content": "center", background: "var(--ground)" }}>
      <span style={{ "font-size": "13px", color: "var(--text-dim)" }}>{props.pending ? "Loading…" : "Redirecting…"}</span>
    </div>
  );
}
