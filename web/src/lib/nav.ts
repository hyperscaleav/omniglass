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
};

export type NavItem = {
  label: string;
  icon: Component<{ size?: number }>;
  hint: string;
  path?: string;
  live?: boolean;
  issue?: number;
  resource?: string;
  children?: NavChild[];
};

export const navItems: NavItem[] = [
  { label: "Home", path: "/", icon: Icons.Home, live: true, hint: "Your environment at a glance, and what needs attention right now." },
  { label: "Dashboards", path: "/dashboards", icon: Icons.LayoutDashboard, hint: "Official, shared, and your own dashboards." },
  { label: "Alarms", path: "/alarms", icon: Icons.Bell, hint: "What is firing now, with drill-down to the triggering datapoint." },
  {
    label: "Inventory", icon: Icons.Package, hint: "The entity graph: systems, components, locations, interfaces, nodes, and tasks.",
    children: [
      { label: "Components", path: "/components", live: true, resource: "component", hint: "The component inventory, with declared config, props, and tags." },
      { label: "Systems", path: "/systems", live: true, resource: "system", hint: "Location and system trees, navigable, with health at each level." },
      { label: "Locations", path: "/locations", live: true, resource: "location", hint: "The place tree: campuses, buildings, floors, and rooms." },
      { label: "Interfaces", path: "/interfaces", hint: "Connection endpoints on components, with their device credentials." },
      { label: "Nodes", path: "/nodes", hint: "Collection daemons: their assigned tasks, health, and enrollment." },
      { label: "Tasks", path: "/tasks", hint: "Collection task assignments across nodes." },
    ],
  },
  {
    label: "Catalog", icon: Icons.Layers, hint: "The authored model: templates, types, tags, and rules.",
    children: [
      { label: "Templates", path: "/templates", hint: "Author device shapes: component and system templates, versioned." },
      { label: "Types", path: "/types", hint: "Component, system, location, interface, datapoint, and event type registries." },
      { label: "Tags", path: "/tags", hint: "Tag definitions applied across the inventory." },
      { label: "Rules", path: "/rules", hint: "Transform, calc, and event rules, with CEL and blast-radius preview." },
    ],
  },
  { label: "Explore", path: "/explore", icon: Icons.Compass, hint: "Datapoint history, the event log, and the cascade resolve view." },
  { label: "Learn", path: "/learn", icon: Icons.GraduationCap, hint: "How collection turns a device into owned datapoints." },
  {
    label: "Settings", icon: Icons.Settings, hint: "Platform configuration and tenant administration.",
    children: [
      { label: "Config", path: "/config", hint: "Severity levels, schedules, retention, and platform settings." },
      { label: "Secrets", path: "/secrets", hint: "Shared device and platform credentials, with rotation and policy." },
      { label: "Users", path: "/users", live: true, resource: "principal", hint: "Users and service accounts: status, grants, and tokens." },
      { label: "Roles", path: "/roles", hint: "Custom roles: permission bundles and inheritance." },
      { label: "Groups", path: "/groups", hint: "User groups: membership and grants." },
      { label: "Audit", path: "/audit", hint: "The audit trail of every privileged mutation." },
    ],
  },
];

// filterNav drops the tabs a principal cannot read: a leaf with no resource is
// always kept; a leaf with a resource is kept only when canRead(resource); a group
// is kept only if it has at least one kept child (with its children filtered). The
// server gates the route regardless; this hides what the caller cannot use.
export function filterNav(items: NavItem[], canRead: (resource: string) => boolean): NavItem[] {
  const ok = (n: { resource?: string }) => !n.resource || canRead(n.resource);
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

// Flattened title + hint (+ icon + tracking issue) lookup by base-relative path,
// for the generic stub. A child inherits its parent group's icon (that is the icon
// the sidebar shows it under), so the placeholder matches the sidebar.
export type NavMeta = { label: string; hint: string; issue?: number; icon: Component<{ size?: number }> };
export const navByPath: Record<string, NavMeta> = (() => {
  const m: Record<string, NavMeta> = {};
  for (const item of navItems) {
    if (item.path) m[item.path] = { label: item.label, hint: item.hint, issue: item.issue, icon: item.icon };
    for (const child of item.children ?? []) m[child.path] = { label: child.label, hint: child.hint, issue: child.issue, icon: item.icon };
  }
  return m;
})();

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
// a detail route (/locations/hq) still resolves to "Locations".
export function sectionLabel(pathname: string): string {
  const path = relative(pathname);
  let label = "";
  let best = -1;
  for (const [p, meta] of Object.entries(navByPath)) {
    const hit = p === "/" ? path === "/" : path === p || path.startsWith(`${p}/`);
    if (hit && p.length > best) {
      label = meta.label;
      best = p.length;
    }
  }
  return label;
}
