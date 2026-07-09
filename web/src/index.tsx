/* @refresh reload */
import { type ParentComponent } from "solid-js";
import { render } from "solid-js/web";
import { Router, Route } from "@solidjs/router";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import "./app.css";
import App from "./App";
import { AuthGuard } from "./components/AuthGuard";
import { RouteGuard } from "./components/RouteGuard";
import Login from "./pages/Login";
import Home from "./pages/Home";
import Locations from "./pages/Locations";
import Systems from "./pages/Systems";
import Components from "./pages/Components";
import Profile from "./pages/Profile";
import Nodes from "./pages/Nodes";
import Interfaces from "./pages/Interfaces";
import Tasks from "./pages/Tasks";
import Users from "./pages/Users";
import Roles from "./pages/Roles";
import Groups from "./pages/Groups";
import Audit from "./pages/Audit";
import SectionStub from "./pages/SectionStub";
import NotFound from "./pages/NotFound";

const root = document.getElementById("root");
if (!root) throw new Error("missing #root element");

const queryClient = new QueryClient();

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

// Stubbed sections: backends not built yet. The design draws them as stubs too.
const STUBS = [
  "/dashboards", "/alarms",
  "/templates", "/types", "/tags", "/rules", "/explore", "/learn",
  "/config", "/secrets",
];

render(
  () => (
    <QueryClientProvider client={queryClient}>
      <Router base="/web">
        <Route path="/login" component={Login} />
        <Route path="/" component={ProtectedShell}>
          <Route path="/" component={Home} />
          {/* Inventory pages on the generic TreeList. The :name route opens the
              same page focused on one entity (the addressable full-page detail). */}
          <Route path="/locations" component={Locations} />
          <Route path="/locations/:name" component={Locations} />
          <Route path="/systems" component={Systems} />
          <Route path="/systems/:name" component={Systems} />
          <Route path="/components" component={Components} />
          <Route path="/components/:name" component={Components} />
          <Route path="/nodes" component={Nodes} />
          <Route path="/interfaces" component={Interfaces} />
          <Route path="/tasks" component={Tasks} />
          <Route path="/profile" component={Profile} />
          <Route path="/users" component={Users} />
          <Route path="/roles" component={Roles} />
          <Route path="/groups" component={Groups} />
          <Route path="/audit" component={Audit} />
          {STUBS.map((p) => <Route path={p} component={SectionStub} />)}
          <Route path="*" component={NotFound} />
        </Route>
      </Router>
    </QueryClientProvider>
  ),
  root,
);
