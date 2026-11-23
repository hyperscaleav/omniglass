import { Show, type ParentComponent, createMemo } from "solid-js";
import { useLocation } from "@solidjs/router";
import { useMe, can } from "../lib/auth";
import { routeAllowed, routeNeeds, sectionLabel } from "../lib/nav";
import DeniedPage from "./DeniedPage";

// RouteGuard blocks a direct hit on a route the caller cannot read. Route access
// is derived from the same nav permission map that hides the sidebar button
// (routeTokens), so the two never diverge: a tab you cannot see is a URL you
// cannot reach. An ungated route (Home, Profile, the not-yet-built stubs) is
// always allowed. On a denied route it renders the denied face (DeniedPage, #2442):
// the section, the permission it needs, and the way back. It used to redirect to
// Home, which dropped the person there with no word.
//
// This is UX and defence-in-depth, not the security boundary: the server gates
// every route and injects scope on every query, so it already refuses. The guard
// stops the console from rendering a page the caller cannot use and, under
// impersonation, from painting the previous user's cached rows before the
// refetch is refused. It renders inside AuthGuard's authenticated Show, so
// me.data is set here; the loading gap is AuthGuard's to cover.
export const RouteGuard: ParentComponent = (props) => {
  const me = useMe();
  const location = useLocation();

  const allowed = createMemo(() => {
    const m = me.data;
    if (!m) return true; // still resolving; AuthGuard holds the loader
    return routeAllowed(location.pathname, (tokens) => can(m, ...tokens));
  });

  return (
    <Show when={allowed()} fallback={<DeniedPage section={sectionLabel(location.pathname) || "this page"} needs={routeNeeds(location.pathname)} />}>
      {props.children}
    </Show>
  );
};
