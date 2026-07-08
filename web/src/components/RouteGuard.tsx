import { Show, type ParentComponent, createEffect, createMemo } from "solid-js";
import { useLocation, useNavigate } from "@solidjs/router";
import { useMe, can } from "../lib/auth";
import { routeTokens } from "../lib/nav";

// RouteGuard blocks a direct hit on a route the caller cannot read. Route access
// is derived from the same nav permission map that hides the sidebar button
// (routeTokens), so the two never diverge: a tab you cannot see is a URL you
// cannot reach. An ungated route (Home, Profile, the not-yet-built stubs) is
// always allowed. On a denied route it redirects to Home.
//
// This is UX and defence-in-depth, not the security boundary: the server gates
// every route and injects scope on every query, so it already refuses. The guard
// stops the console from rendering a page the caller cannot use and, under
// impersonation, from painting the previous principal's cached rows before the
// refetch is refused. It renders inside AuthGuard's authenticated Show, so
// me.data is set here; the loading gap is AuthGuard's to cover.
export const RouteGuard: ParentComponent = (props) => {
  const me = useMe();
  const location = useLocation();
  const navigate = useNavigate();

  const allowed = createMemo(() => {
    const m = me.data;
    if (!m) return true; // still resolving; AuthGuard holds the loader
    const tokens = routeTokens(location.pathname);
    return !tokens || can(m, ...tokens);
  });

  createEffect(() => {
    if (!allowed()) navigate("/", { replace: true });
  });

  return <Show when={allowed()}>{props.children}</Show>;
};
