import type { Component } from "solid-js";
import * as Icons from "../components/icons";

// The operator console IA, from the "Omniglass Console" design. Routes are flat
// and identity-based (a page addresses its entity, never its menu cluster); the
// sidebar groups them for browsing. `live` marks a route backed by a real page;
// everything else renders the generic SectionStub until its backend lands.
export type NavChild = {
  label: string;
  path: string;
  hint: string;
  live?: boolean;
  issue?: number; // tracking issue for a not-yet-built section, shown on its stub
  // The authorization resource this tab needs to read. When set, the sidebar hides
  // the tab from a principal without <resource>:read. Set it when the entity goes
  // live (its server route already gates on the same resource); leave unset while
  // the section is a stub so its "soon" entry stays visible.
  resource?: string;
  // The exact permission a sensitive tab requires, as a topic string, for when a
  // bare <resource>:read does not describe the route's gate (the audit route
  // requires the admin tier `audit:read:admin`). Takes precedence over `resource`,
  // so the sidebar hides the tab from exactly whoever the server would 403.
  perm?: string;
  // The section this entry sits under inside its group. A section is named for
  // the estate noun it serves; the entry keeps the registry's own word (and where
  // the registry's only word is "type", the entry is Types). The sidebar renders
  // a section header as a non-folding label before each run of children sharing a
  // section, never as a collapsible fold; filterNav still judges each entry on
  // its own gate, so a section whose entries are all hidden loses its header too.
  section?: string;
};

export type NavItem = {
  label: string;
  icon: Component<{ size?: number }>;
  hint: string;
  path?: string;
  live?: boolean;
  issue?: number;
  resource?: string;
  perm?: string;
  children?: NavChild[];
};

export const navItems: NavItem[] = [
  { label: "Home", path: "/", icon: Icons.Home, live: true, hint: "Your environment at a glance, and what needs attention right now." },
  { label: "Dashboards", path: "/dashboards", icon: Icons.LayoutDashboard, hint: "Official, shared, and your own dashboards." },
  { label: "Alarms", path: "/alarms", icon: Icons.Bell, hint: "What is firing now, with drill-down to the triggering sample." },
  {
    label: "Inventory", icon: Icons.Package, hint: "The monitored estate: components, systems, locations, and the collection nodes, each addressable with its health and configuration.",
    children: [
      { label: "Components", path: "/components", live: true, resource: "component", hint: "The component inventory, with declared config, props, and tags. Device interfaces are a panel on the component." },
      { label: "Systems", path: "/systems", live: true, resource: "system", hint: "Location and system trees, navigable, with health at each level." },
      { label: "Locations", path: "/locations", live: true, resource: "location", hint: "The place tree: campuses, buildings, floors, and rooms." },
      // An interface is an API on a component, named by its protocol, so it surfaces
      // as a panel on the component detail, not a separate nav entry. The tasks a
      // node runs are likewise a panel on the node.
      { label: "Nodes", path: "/nodes", live: true, resource: "node", hint: "Collection daemons: their health, enrollment, and the collection tasks assigned to each." },
    ],
  },
  {
    label: "Values", icon: Icons.Sliders, hint: "Operator-set values and content: interpolation variables, encrypted secrets, and reconciled component config, each resolved down the scope cascade, plus the files kept with the estate.",
    children: [
      { label: "Variables", path: "/variables", live: true, resource: "variable", hint: "Free interpolated values (macros), resolved down the scope cascade." },
      { label: "Secrets", path: "/secrets", live: true, resource: "secret", hint: "Shared device and platform credentials, resolved down the scope cascade." },
      { label: "Config", path: "/config", hint: "Reconciled component and system configuration: desired values operators set, optionally observed back from the device to detect drift and reconcile." },
      // Files are operator-uploaded content, not a cascaded value: a flat, tenant-wide
      // store of opaque bytes kept beside the estate. It sits in Values as the
      // non-cascading member (deliberately off the exclusive arc).
      { label: "Files", path: "/files", live: true, resource: "file", hint: "Firmware images, config dumps, runbooks, and captures kept with the estate, deduplicated and searchable." },
    ],
  },
  // Catalog is a single live entry opening the catalog area: a layout shell
  // (CatalogShell) whose subrail navigates between the registries' own routed
  // pages, with the /catalog Overview as its landing. Ungated like Home: the
  // subrail and the Overview permission-filter their entries (and count queries)
  // with the same can() this rail uses, so a floor viewer opens it and sees
  // exactly the registries it may read, while each registry page keeps its own
  // route-guard gate. The per-registry pages stay declared off-rail (OFF_RAIL
  // below): that carries their top-bar identity and their route-guard gates.
  { label: "Catalog", path: "/catalog", icon: Icons.Layers, live: true, hint: "Everything the estate is typed by: every registry, browsable in one place." },
  { label: "Explore", path: "/explore", icon: Icons.Compass, hint: "Sample history, the event log, and the cascade resolve view." },
  { label: "Learn", path: "/learn", icon: Icons.GraduationCap, hint: "How collection turns a device into owned samples." },
  {
    label: "Admin", icon: Icons.Settings, hint: "Platform administration: users, roles, groups, the audit trail, and platform settings.",
    children: [
      { label: "Users", path: "/users", live: true, perm: "principal:read:admin", hint: "Users and service accounts: status, grants, and tokens." },
      { label: "Roles", path: "/roles", live: true, perm: "role:read:admin", hint: "The built-in roles: what each permission bundle grants, and how they inherit." },
      { label: "Groups", path: "/groups", live: true, perm: "principal_group:read:admin", hint: "User groups: membership and the grants members inherit." },
      { label: "Audit", path: "/audit", live: true, perm: "audit:read:admin", hint: "The audit trail of every privileged action and sign-in." },
      { label: "Settings", path: "/settings", live: true, hint: "Platform preferences resolved down the cascade: the theme, defaults, and keybindings, each with its provenance and lock state." },
    ],
  },
];

// filterNav drops the tabs a principal cannot reach: an ungated leaf is always
// kept; a gated leaf is kept only when `allow` passes its required permission
// tokens (an explicit `perm`, else `<resource>:read`); a group is kept only if it
// has at least one kept child (with its children filtered). The server gates the
// route regardless; this hides what the caller cannot use. `allow` takes the token
// path so a sensitive tab can require a deeper tier (e.g. `audit:read:admin`).
export function filterNav(items: NavItem[], allow: (tokens: string[]) => boolean): NavItem[] {
  const ok = (n: { resource?: string; perm?: string }) =>
    n.perm ? allow(n.perm.split(":")) : !n.resource || allow([n.resource, "read"]);
  const out: NavItem[] = [];
  for (const item of items) {
    if (item.children) {
      const children = item.children.filter(ok);
      if (children.length) out.push({ ...item, children });
    } else if (ok(item)) {
      out.push(item);
    }
  }
  return out;
}

// Off-rail pages: routed surfaces with no rail entry of their own. The Catalog
// rail collapsed to the single /catalog entry (the shell's subrail navigates
// the registries now), but every registry keeps its own routed page (where the
// create and edit surfaces live), its identity in the top bar, and its
// permission gate in the route guard, all declared here.
// /templates set the precedent when it left the rail (#608). Each entry's
// resource/perm feeds navPerms exactly as a rail entry's would, so collapsing
// the rail loosened no gate: /secret-types still demands secret:read. A
// not-yet-built page's tracking issue rides here too, shown on its stub
// through navByPath exactly as a rail entry's would be.
export const OFF_RAIL: { path: string; label: string; hint: string; resource?: string; perm?: string; issue?: number }[] = [
  { path: "/products", label: "Products", resource: "product", hint: "A concrete SKU: a vendor's product, its driver, kind, and the component type it is classified under." },
  { path: "/vendors", label: "Vendors", resource: "vendor", hint: "The organizations behind products: manufacturers, integrators, developers." },
  { path: "/drivers", label: "Drivers", resource: "driver", hint: "The implementations that get, emit, and set a product's signals." },
  { path: "/component-types", label: "Component types", resource: "component_type", hint: "The device-class genus registry: display, projector, mic, and your own, each carrying the icon, stem, and abbreviation its products inherit." },
  { path: "/standards", label: "Standards", resource: "standard", hint: "The blueprints a system conforms to, each declaring the properties every conforming system exposes." },
  { path: "/system-types", label: "System types", resource: "system_type", hint: "The coarse space registry: what kind of space a system is (a boardroom, a classroom, a video wall), each carrying the icon, stem, and abbreviation its systems inherit." },
  { path: "/location-types", label: "Location types", resource: "location_type", hint: "The place classifier registry: campus, building, floor, room, and your own." },
  { path: "/secret-types", label: "Secret types", resource: "secret", hint: "The shapes a secret can take, read-only reference data that ships with Omniglass." },
  { path: "/metrics", label: "Metrics", resource: "metric_type", hint: "The numeric signal catalog: the canonical series a sample measures, each carrying its unit and precision." },
  { path: "/properties", label: "Properties", resource: "property_type", hint: "The categorical signal catalog: the canonical properties a sample observes and a product contract declares." },
  { path: "/event-types", label: "Events", resource: "event_type", hint: "The occurrence catalog: the discrete happenings an event is typed by." },
  { path: "/command-types", label: "Commands", resource: "command_type", hint: "The do catalog: what a component can be told, with a target and a settle window." },
  { path: "/tags", label: "Tags", resource: "tag", hint: "The governed tag key vocabulary applied across the inventory." },
  { path: "/rules", label: "Rules", hint: "Transform, calc, and event rules, with CEL and blast-radius preview.", issue: 624 },
  { path: "/log-types", label: "Logs", hint: "The log shape catalog. The log lane arrives untyped today; typing what a line can be is still to come." },
  { path: "/notifications", label: "Notifications", hint: "The outbound notification shapes: how a firing event reaches an operator, still to come.", issue: 618 },
  { path: "/templates", label: "Templates", hint: "Example configurations you clone: start a location type, a standard, or a whole system from one, then own what you get." },
];

// Flattened title + hint (+ icon + tracking issue) lookup by base-relative path,
// for the generic stub. A child inherits its parent group's icon (that is the icon
// the sidebar shows it under), so the placeholder matches the sidebar. A sectioned
// child also carries its group and section labels so the top bar can spell out the
// full address. Off-rail pages keep their identity here too: a bookmark to
// /products must still read "Products", not the anonymous fallback.
export type NavMeta = { label: string; hint: string; issue?: number; icon: Component<{ size?: number }>; group?: string; section?: string };
export const navByPath: Record<string, NavMeta> = (() => {
  const m: Record<string, NavMeta> = {};
  for (const item of navItems) {
    if (item.path) m[item.path] = { label: item.label, hint: item.hint, issue: item.issue, icon: item.icon };
    for (const child of item.children ?? [])
      m[child.path] = { label: child.label, hint: child.hint, issue: child.issue, icon: item.icon, group: item.label, section: child.section };
  }
  for (const o of OFF_RAIL) m[o.path] = { label: o.label, hint: o.hint, issue: o.issue, icon: Icons.Layers };
  return m;
})();

// Stubbed sections: backends not built yet, each rendering SectionStub in the
// router. /templates keeps its stub page though its rail entry is gone (#608).
// /rules and /notifications mount INSIDE the CatalogShell (their stubs render
// in the catalog pane). The router renders from the route manifest (#760), and
// route-manifest-guard.test.ts pins the manifest's stub entries to this list,
// so every unlive rail entry resolves to a registered stub rather than NotFound.
export const STUBS = [
  "/dashboards", "/alarms",
  "/templates", "/rules", "/explore", "/learn",
  "/config", "/log-types", "/notifications",
];

// The router base; nav paths are base-relative.
const BASE = "/web";
function relative(pathname: string): string {
  const p = pathname.startsWith(BASE) ? pathname.slice(BASE.length) : pathname;
  return p === "" ? "/" : p;
}

export function lookupNav(pathname: string): NavMeta {
  return navByPath[relative(pathname)] ?? { label: "Coming soon", hint: "This section is not built yet.", icon: Icons.Layers };
}

// sectionLabel resolves a pathname to its top-bar section by longest prefix, so
// a detail route (/locations/hq) still resolves to "Locations". A sectioned entry
// spells out its full address (group · section · entry) because a bare label
// would not identify it alone; instanceless today, since no nav child carries a
// section after the shell landed. An unsectioned entry keeps its single label.
export function sectionLabel(pathname: string): string {
  const path = relative(pathname);
  // Profile is reached from the sidebar footer, not a nav item, so it has no
  // navByPath entry; label it explicitly so the top bar reads "Profile".
  if (path === "/profile") return "Profile";
  let label = "";
  let best = -1;
  for (const [p, meta] of Object.entries(navByPath)) {
    const hit = p === "/" ? path === "/" : path === p || path.startsWith(`${p}/`);
    if (hit && p.length > best) {
      label = meta.section ? `${meta.group} · ${meta.section} · ${meta.label}` : meta.label;
      best = p.length;
    }
  }
  return label;
}

// navPerms maps a gated route path to the permission tokens it requires, from the
// SAME nav config that hides the sidebar button (an explicit `perm`, else
// `<resource>:read`), plus the off-rail pages, whose gates outlive their rail
// entries. An ungated path (Home, Profile, the stubs) is absent. This is the
// single source both the sidebar (hide the button) and the route guard (block
// the URL) read, so the two can never diverge.
const navPerms: Record<string, string[]> = (() => {
  const m: Record<string, string[]> = {};
  const add = (n: { path?: string; resource?: string; perm?: string }) => {
    if (!n.path) return;
    if (n.perm) m[n.path] = n.perm.split(":");
    else if (n.resource) m[n.path] = [n.resource, "read"];
  };
  for (const item of navItems) {
    add(item);
    for (const child of item.children ?? []) add(child);
  }
  for (const o of OFF_RAIL) add(o);
  return m;
})();

// routeTokens returns the permission a route requires, or null for an ungated
// route (always reachable). It resolves by longest prefix like sectionLabel, so a
// detail route (/locations/hq) inherits its section's gate (/locations). The route
// guard denies a route whose tokens the caller lacks; the server is still the
// authority, this only keeps the console from rendering a page the caller cannot
// use (and, under impersonation, from painting stale cross-principal data).
export function routeTokens(pathname: string): string[] | null {
  const path = relative(pathname);
  let tokens: string[] | null = null;
  let best = -1;
  for (const [p, need] of Object.entries(navPerms)) {
    const hit = path === p || path.startsWith(`${p}/`);
    if (hit && p.length > best) {
      tokens = need;
      best = p.length;
    }
  }
  return tokens;
}
