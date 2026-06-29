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
};

export type NavItem = {
  label: string;
  icon: Component<{ size?: number }>;
  hint: string;
  path?: string;
  live?: boolean;
  children?: NavChild[];
};

export const navItems: NavItem[] = [
  { label: "Home", path: "/", icon: Icons.Home, live: true, hint: "Your environment at a glance, and what needs attention right now." },
  { label: "Dashboards", path: "/dashboards", icon: Icons.LayoutDashboard, hint: "Official, shared, and your own dashboards." },
  { label: "Alarms", path: "/alarms", icon: Icons.Bell, hint: "What is firing now, with drill-down to the triggering datapoint." },
  {
    label: "Inventory", icon: Icons.Package, hint: "The entity graph: systems, components, locations, interfaces, nodes, and tasks.",
    children: [
      { label: "Systems", path: "/systems", live: true, hint: "Location and system trees, navigable, with health at each level." },
      { label: "Components", path: "/components", live: true, hint: "The component inventory, with declared config, props, and tags." },
      { label: "Locations", path: "/locations", live: true, hint: "The place tree: campuses, buildings, floors, and rooms." },
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
      { label: "Users", path: "/users", hint: "Users and service accounts: status, grants, and tokens." },
      { label: "Roles", path: "/roles", hint: "Custom roles: permission bundles and inheritance." },
      { label: "Groups", path: "/groups", hint: "User groups: membership and grants." },
      { label: "Audit", path: "/audit", hint: "The audit trail of every privileged mutation." },
    ],
  },
];

// Flattened title + hint lookup by base-relative path, for the generic stub.
export const navByPath: Record<string, { label: string; hint: string }> = (() => {
  const m: Record<string, { label: string; hint: string }> = {};
  for (const item of navItems) {
    if (item.path) m[item.path] = { label: item.label, hint: item.hint };
    for (const child of item.children ?? []) m[child.path] = { label: child.label, hint: child.hint };
  }
  return m;
})();

// The router base; nav paths are base-relative.
const BASE = "/web";
function relative(pathname: string): string {
  const p = pathname.startsWith(BASE) ? pathname.slice(BASE.length) : pathname;
  return p === "" ? "/" : p;
}

export function lookupNav(pathname: string): { label: string; hint: string } {
  return navByPath[relative(pathname)] ?? { label: "Coming soon", hint: "This section lands in a later slice." };
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
