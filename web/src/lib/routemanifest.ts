// The console route manifest: the single source the router renders from and the
// e2e smoke tier drives from (#760). routes.tsx maps each entry to its page
// component and index.tsx mounts the result, so a route cannot be added without
// an entry here; web/e2e/routes.spec.ts imports this same array and visits every
// entry's smoke address, so a route cannot exist without the browser tier
// rendering it. This module stays PURE DATA (no imports): the Playwright runner
// loads it in node, where a component import would not resolve.
//
// route-manifest-guard.test.ts pins the construction: every page key resolves,
// the stub entries equal the nav's own stub lists, every smoke address is
// concrete, paths are unique.

export type RouteEntry = {
  // The router pattern, base-relative (the router mounts under /web).
  path: string;
  // Which shell mounts it: the public sibling (login), the protected shell, or
  // the catalog layout inside the protected shell.
  shell: "public" | "protected" | "catalog";
  // The key into routes.tsx's PAGES registry.
  page: string;
  // False for the routes reachable without a session (login only).
  auth?: false;
  // The concrete address the smoke spec visits, base-relative. A parametrized
  // route substitutes the well-formed-unknown uuid: rendering the honest miss
  // face without a console error is exactly the smoke worth having.
  smoke: string;
};

// A uuid no row will ever carry: parametrized routes smoke their miss face.
export const UNKNOWN_UUID = "00000000-0000-4000-8000-000000000000";

export const ROUTE_MANIFEST: RouteEntry[] = [
  { path: "/login", shell: "public", page: "Login", auth: false, smoke: "/login" },

  { path: "/", shell: "protected", page: "Home", smoke: "/" },
  // Inventory pages on the generic TreeList. The :id route opens the same page
  // focused on one entity (the addressable full-page detail), addressed by uuid
  // (#627 Task 15c: name uniqueness is scoped to placement, so a name alone is
  // not a reliable route param); TreeList's focus effect resolves a name-shaped
  // link through a byAddr fallback, keeping the query string (#759).
  { path: "/locations", shell: "protected", page: "Locations", smoke: "/locations" },
  { path: "/locations/:id", shell: "protected", page: "Locations", smoke: `/locations/${UNKNOWN_UUID}` },
  { path: "/systems", shell: "protected", page: "Systems", smoke: "/systems" },
  { path: "/systems/:id", shell: "protected", page: "Systems", smoke: `/systems/${UNKNOWN_UUID}` },
  { path: "/components", shell: "protected", page: "Components", smoke: "/components" },
  { path: "/components/:id", shell: "protected", page: "Components", smoke: `/components/${UNKNOWN_UUID}` },
  { path: "/nodes", shell: "protected", page: "Nodes", smoke: "/nodes" },
  { path: "/files", shell: "protected", page: "Files", smoke: "/files" },
  { path: "/files/:id", shell: "protected", page: "Files", smoke: `/files/${UNKNOWN_UUID}` },
  { path: "/profile", shell: "protected", page: "Profile", smoke: "/profile" },
  { path: "/users", shell: "protected", page: "Users", smoke: "/users" },
  { path: "/roles", shell: "protected", page: "Roles", smoke: "/roles" },
  { path: "/groups", shell: "protected", page: "Groups", smoke: "/groups" },
  { path: "/secrets", shell: "protected", page: "Secrets", smoke: "/secrets" },
  { path: "/variables", shell: "protected", page: "Variables", smoke: "/variables" },
  { path: "/audit", shell: "protected", page: "Audit", smoke: "/audit" },
  { path: "/settings", shell: "protected", page: "Settings", smoke: "/settings" },
  // Stubbed sections outside the catalog (nav.ts's STUBS minus the catalog's
  // routed soon slots); the guard test pins this list to nav.ts, so a stub
  // added there without a row here fails the suite.
  { path: "/dashboards", shell: "protected", page: "SectionStub", smoke: "/dashboards" },
  { path: "/alarms", shell: "protected", page: "SectionStub", smoke: "/alarms" },
  { path: "/templates", shell: "protected", page: "SectionStub", smoke: "/templates" },
  { path: "/explore", shell: "protected", page: "SectionStub", smoke: "/explore" },
  { path: "/learn", shell: "protected", page: "SectionStub", smoke: "/learn" },
  { path: "/config", shell: "protected", page: "SectionStub", smoke: "/config" },
  { path: "/log-types", shell: "protected", page: "SectionStub", smoke: "/log-types" },
  // The not-found face is a route like any other; any unroutable address smokes it.
  { path: "*", shell: "protected", page: "NotFound", smoke: "/definitely-not-a-route" },

  { path: "/catalog", shell: "catalog", page: "CatalogOverview", smoke: "/catalog" },
  { path: "/metrics", shell: "catalog", page: "Metrics", smoke: "/metrics" },
  { path: "/properties", shell: "catalog", page: "Properties", smoke: "/properties" },
  { path: "/event-types", shell: "catalog", page: "EventTypes", smoke: "/event-types" },
  { path: "/command-types", shell: "catalog", page: "CommandTypes", smoke: "/command-types" },
  { path: "/vendors", shell: "catalog", page: "Vendors", smoke: "/vendors" },
  { path: "/products", shell: "catalog", page: "Products", smoke: "/products" },
  { path: "/drivers", shell: "catalog", page: "Drivers", smoke: "/drivers" },
  { path: "/component-types", shell: "catalog", page: "ComponentTypes", smoke: "/component-types" },
  { path: "/standards", shell: "catalog", page: "Standards", smoke: "/standards" },
  { path: "/system-types", shell: "catalog", page: "SystemTypes", smoke: "/system-types" },
  { path: "/location-types", shell: "catalog", page: "LocationTypes", smoke: "/location-types" },
  { path: "/secret-types", shell: "catalog", page: "SecretTypes", smoke: "/secret-types" },
  { path: "/tags", shell: "catalog", page: "Tags", smoke: "/tags" },
  // The catalog's routed soon slots (catalog.ts's CATALOG_STUB_PATHS), rendering
  // their stub inside the pane.
  { path: "/rules", shell: "catalog", page: "SectionStub", smoke: "/rules" },
  { path: "/notifications", shell: "catalog", page: "SectionStub", smoke: "/notifications" },
];
