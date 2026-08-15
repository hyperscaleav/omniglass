/* @refresh reload */
import { type ParentComponent } from "solid-js";
import { render } from "solid-js/web";
import { Router, Route } from "@solidjs/router";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import "./app.css";
import { setUnauthorizedHandler, clearToken } from "./api/client";
import { ME_KEY } from "./lib/auth";
import App from "./App";
import { AuthGuard } from "./components/AuthGuard";
import { RouteGuard } from "./components/RouteGuard";
import CatalogShell from "./components/CatalogShell";
import { ROUTE_MANIFEST, type RouteEntry } from "./lib/routemanifest";
import { PAGES } from "./routes";

const root = document.getElementById("root");
if (!root) throw new Error("missing #root element");

const queryClient = new QueryClient();

// When a protected request 401s, the session has ended (expired, reset, or locked
// out): drop any stale bearer and null the cached principal so the AuthGuard
// redirects to /login on the spot, carrying the current path to return to.
setUnauthorizedHandler(() => {
  clearToken();
  queryClient.setQueryData(ME_KEY, null);
});

// ProtectedShell gates the console: AuthGuard intercepts /auth/me and redirects
// unauthenticated callers to /login; App renders the rail + top bar around the
// page. Login is a sibling route outside the shell.
const ProtectedShell: ParentComponent = (props) => (
  <AuthGuard>
    <App>
      <RouteGuard>{props.children}</RouteGuard>
    </App>
  </AuthGuard>
);

// The router renders FROM the route manifest (#760): every <Route> below is a
// manifest entry resolved through the PAGES registry, so a route cannot be added
// without the entry the e2e smoke spec drives from. The manifest documents each
// route's shape (the :id details, the stubs, the catalog layout); the guard test
// (route-manifest-guard.test.ts) pins registry completeness and the stub lists.
// The Catalog area stays a layout route: CatalogShell draws the subrail around
// each registry's own page while the URL stays canonical (/products, /metrics,
// ...); each page keeps its own route-guard gate.
const routesFor = (shell: RouteEntry["shell"]) =>
  ROUTE_MANIFEST.filter((r) => r.shell === shell).map((r) => <Route path={r.path} component={PAGES[r.page]} />);

render(
  () => (
    <QueryClientProvider client={queryClient}>
      <Router base="/web">
        {routesFor("public")}
        <Route path="/" component={ProtectedShell}>
          {routesFor("protected")}
          <Route component={CatalogShell}>{routesFor("catalog")}</Route>
        </Route>
      </Router>
    </QueryClientProvider>
  ),
  root,
);
