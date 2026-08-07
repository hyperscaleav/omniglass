import { describe, it, expect } from "vitest";
import { filterNav, lookupNav, navItems, OFF_RAIL, routeTokens, sectionLabel, STUBS, type NavItem } from "./nav";
import { can, type Me } from "./auth";

const Dummy = () => null;

// A real nav gate: filter the live nav through the actual can() over a set of
// permission strings, exactly as the sidebar does, and read a section's children.
const meWith = (permissions: string[]): Me => ({ principal: { id: "p", kind: "human" }, permissions, grants: [] });
const section = (label: string, permissions: string[]): string[] =>
  filterNav(navItems, (tokens) => can(meWith(permissions), ...tokens)).find((i) => i.label === label)?.children?.map((c) => c.label) ?? [];
// The rail entries a principal keeps, by top-level label (the Catalog entry is a
// single leaf now, so it is addressed here rather than through children).
const rail = (permissions: string[]) =>
  filterNav(navItems, (tokens) => can(meWith(permissions), ...tokens)).map((i) => i.label);

describe("filterNav", () => {
  it("keeps every tab for a principal that can read everything", () => {
    const out = filterNav(navItems, () => true);
    expect(out.length).toBe(navItems.length);
  });

  it("keeps resource-less leaves and hides a leaf whose resource is unreadable", () => {
    const nav: NavItem[] = [
      { label: "Home", path: "/", icon: Dummy, hint: "" },
      { label: "Secrets", path: "/secrets", icon: Dummy, hint: "", resource: "secret" },
    ];
    expect(filterNav(nav, () => false).map((i) => i.label)).toEqual(["Home"]);
  });

  it("filters a group's children and drops a group with none readable", () => {
    const nav: NavItem[] = [
      { label: "Inv", icon: Dummy, hint: "", children: [
        { label: "Systems", path: "/systems", hint: "", resource: "system" },
        { label: "Locations", path: "/locations", hint: "", resource: "location" },
      ] },
      { label: "Empty", icon: Dummy, hint: "", children: [{ label: "X", path: "/x", hint: "", resource: "x" }] },
    ];
    const out = filterNav(nav, (tokens) => tokens[0] === "system");
    expect(out.map((i) => i.label)).toEqual(["Inv"]);
    expect(out[0].children!.map((c) => c.label)).toEqual(["Systems"]);
  });

  it("orders the inventory section Components, Systems, Locations, Nodes", () => {
    const inv = navItems.find((i) => i.label === "Inventory");
    expect(inv?.children?.map((c) => c.label)).toEqual([
      "Components", "Systems", "Locations", "Nodes",
    ]);
  });

  it("on the real nav, a principal without system/component/location read loses those tabs but keeps the stubs", () => {
    const out = filterNav(navItems, (tokens) => !["system", "component", "location"].includes(tokens[0]));
    const inv = out.find((i) => i.label === "Inventory");
    const labels = inv?.children?.map((c) => c.label) ?? [];
    expect(labels).not.toContain("Systems");
    expect(labels).not.toContain("Components");
    expect(labels).not.toContain("Locations");
    expect(labels).toContain("Nodes"); // gated on node:read, which this filter allows
  });

  // The owner regression (owner's only grant is the `>` tail): every gated tab must
  // return, driven through the real can() the sidebar uses.
  it("restores every Admin tab for the owner (`>`)", () => {
    expect(section("Admin", [">"])).toContain("Users");
    expect(section("Admin", [">"])).toContain("Roles");
    expect(section("Admin", [">"])).toContain("Audit");
  });

  // The Audit tab is gated on the admin tier, not a bare read: a viewer whose
  // `*:read` the server 403s at the 3-token audit route must not see the tab, while
  // an explicit `audit:read:admin` (admin) and `>` (owner) do.
  it("gates Audit on the admin tier, matching the server's audit:read:admin route", () => {
    expect(section("Admin", ["*:read"])).not.toContain("Audit");
    expect(section("Admin", ["audit:read:admin"])).toContain("Audit");
    expect(section("Admin", [">"])).toContain("Audit");
  });

  // The Users, Roles, and Groups directories are admin-tier reads
  // (<resource>:read:admin), matching the server routes: a viewer's *:read cannot
  // reach them, while admin's explicit read:admin grants (and owner's >) do.
  it("hides Users, Roles, and Groups from a *:read principal, keeps them for admin", () => {
    const floor = section("Admin", ["*:read"]);
    expect(floor).not.toContain("Users");
    expect(floor).not.toContain("Roles");
    expect(floor).not.toContain("Groups");
    const adm = section("Admin", ["principal:read:admin", "role:read:admin", "principal_group:read:admin"]);
    expect(adm).toContain("Users");
    expect(adm).toContain("Roles");
    expect(adm).toContain("Groups");
  });

  // Secrets is a sensitive resource: the server takes secret off the *:read floor,
  // so a viewer whose only grant is *:read does not read secrets and must not see
  // the tab, while an operator holding a literal secret:read (and owner's `>`) does.
  // Secrets lives under the Values group.
  it("hides Secrets from a *:read viewer, keeps it for an explicit secret:read and owner", () => {
    expect(section("Values", ["*:read"])).not.toContain("Secrets");
    expect(section("Values", ["*:*"])).not.toContain("Secrets");
    expect(section("Values", ["secret:read"])).toContain("Secrets");
    expect(section("Values", ["secret:read,reveal,create,update"])).toContain("Secrets");
    expect(section("Values", [">"])).toContain("Secrets");
  });

  // The Catalog rail entry is ungated (the browse page permission-filters its
  // own rows), so every principal keeps it, floor viewer included. The #598
  // guarantee moved with the gates: what a floor viewer must not reach is the
  // /secret-types ROUTE, and that gate now lives in the off-rail map (asserted
  // under routeTokens below), not a rail child.
  it("keeps the single Catalog entry for every principal, floor viewer included", () => {
    expect(rail(["*:read"])).toContain("Catalog");
    expect(rail([])).toContain("Catalog");
    expect(rail([">"])).toContain("Catalog");
  });
});

// can mirrors the server's Allows, including the sensitive-resource set: a bare `*`
// does not reach a sensitive resource in either the direct match or the :read floor,
// but a literal grant, a resource wildcard, and owner's `>` do. Mirrors
// internal/rbac/rbac_test.go so the console hides exactly what the server denies.
describe("can (sensitive resources)", () => {
  const me = (permissions: string[]): Me => ({ principal: { id: "p", kind: "human" }, permissions, grants: [] });
  it("keeps secret off the bare * wildcard but honors literal, resource-wildcard, and owner grants", () => {
    expect(can(me(["*:read"]), "secret", "read")).toBe(false);
    expect(can(me(["*:*"]), "secret", "read")).toBe(false);
    expect(can(me(["secret:read"]), "secret", "read")).toBe(true);
    expect(can(me(["secret:reveal"]), "secret", "read")).toBe(true); // the :read floor
    expect(can(me(["secret:*"]), "secret", "read")).toBe(true);
    expect(can(me([">"]), "secret", "read")).toBe(true);
    // A non-sensitive resource still floors on *:read.
    expect(can(me(["*:read"]), "variable", "read")).toBe(true);
    // A 2-token secret:* cannot reach the admin tier; secret:> does.
    expect(can(me(["secret:*"]), "secret", "reveal", "admin")).toBe(false);
    expect(can(me(["secret:>"]), "secret", "reveal", "admin")).toBe(true);
  });
});

// routeTokens is the route guard's half of the same map that hides the sidebar
// button: a gated route returns the permission it needs, an ungated one returns
// null (always reachable), and a detail route inherits its section's gate.
describe("routeTokens", () => {
  it("returns the permission a gated route requires", () => {
    expect(routeTokens("/web/locations")).toEqual(["location", "read"]);
    expect(routeTokens("/web/components")).toEqual(["component", "read"]);
    expect(routeTokens("/web/systems")).toEqual(["system", "read"]);
    expect(routeTokens("/web/users")).toEqual(["principal", "read", "admin"]);
    expect(routeTokens("/web/roles")).toEqual(["role", "read", "admin"]);
    expect(routeTokens("/web/groups")).toEqual(["principal_group", "read", "admin"]);
    expect(routeTokens("/web/secrets")).toEqual(["secret", "read"]);
    expect(routeTokens("/web/audit")).toEqual(["audit", "read", "admin"]); // the admin tier
    // The split registries (#598): each page's URL carries its own gate, and
    // /secret-types requires the same secret resource its API route stamps, so
    // the route guard blocks a floor viewer from the URL the sidebar hid.
    expect(routeTokens("/web/location-types")).toEqual(["location_type", "read"]);
    expect(routeTokens("/web/secret-types")).toEqual(["secret", "read"]);
  });
  it("inherits a section's gate on its detail route (longest prefix)", () => {
    expect(routeTokens("/web/locations/hq")).toEqual(["location", "read"]);
    expect(routeTokens("/web/components/cmp_9f2")).toEqual(["component", "read"]);
  });
  it("returns null for an ungated route (Home, Profile, the stubs)", () => {
    expect(routeTokens("/web/")).toBeNull();
    expect(routeTokens("/web/profile")).toBeNull();
    expect(routeTokens("/web/dashboards")).toBeNull(); // a not-yet-built stub
  });
  it("gates exactly what the sidebar hides: routeTokens is set iff the nav entry has a resource/perm", () => {
    // Every gated nav child's route resolves to a permission; a resource-less stub does not.
    const admin = navItems.find((i) => i.label === "Admin")!;
    for (const c of admin.children!) {
      const need = routeTokens(`/web${c.path}`);
      if (c.resource || c.perm) expect(need).not.toBeNull();
      else expect(need).toBeNull();
    }
  });
});

describe("nav IA rework", () => {
  it("puts the estate entities under Inventory and the operator-set values under Values", () => {
    expect(section("Inventory", [">"])).toEqual(["Components", "Systems", "Locations", "Nodes"]);
    expect(section("Values", [">"])).toEqual(["Variables", "Secrets", "Config", "Files"]);
  });

  it("renames the Settings group to Admin and drops the Settings label", () => {
    const labels = filterNav(navItems, () => true).map((i) => i.label);
    expect(labels).toContain("Admin");
    expect(labels).not.toContain("Settings");
  });

  it("keeps governance plus the Settings soon-stub under Admin for an owner", () => {
    expect(section("Admin", [">"])).toEqual(["Users", "Roles", "Groups", "Audit", "Settings"]);
  });

  it("shows a bare *:read viewer only the ungated Settings soon-stub under Admin", () => {
    expect(section("Admin", ["*:read"])).toEqual(["Settings"]);
  });

  it("keeps moved entries' gates and leaves the stubs ungated", () => {
    expect(routeTokens("/web/secrets")).toEqual(["secret", "read"]);
    expect(routeTokens("/web/variables")).toEqual(["variable", "read"]);
    expect(routeTokens("/web/config")).toBeNull();
    expect(routeTokens("/web/settings")).toBeNull();
    expect(routeTokens("/web/nodes")).toEqual(["node", "read"]); // node directory is live, gated on node:read
  });
});

// The Catalog rail collapsed to a single browse entry (the single-surface
// prototype): one live, ungated, resource-less leaf at /catalog, declared the
// same way Home and Explore are. The registries left the rail but not the app:
// each keeps its route, its top-bar identity, and its gate through OFF_RAIL.
describe("Catalog single entry", () => {
  const cat = navItems.find((i) => i.label === "Catalog")!;

  it("is a live top-level leaf at /catalog with no children and no gate", () => {
    expect(cat.path).toBe("/catalog");
    expect(cat.live).toBe(true);
    expect(cat.children).toBeUndefined();
    expect(cat.resource).toBeUndefined();
    expect(cat.perm).toBeUndefined();
    expect(routeTokens("/web/catalog")).toBeNull();
  });

  it("keeps every former registry page reachable off-rail with its gate intact", () => {
    const paths = OFF_RAIL.map((o) => o.path);
    for (const p of ["/products", "/vendors", "/drivers", "/capabilities", "/component-types", "/standards", "/location-types", "/secret-types", "/metrics", "/properties", "/event-types", "/command-types", "/tags"]) {
      expect(paths).toContain(p);
    }
    // The gates did not loosen when the rail entries vanished: the sensitive
    // registry still demands secret:read (#598), the plain one its own read.
    expect(routeTokens("/web/secret-types")).toEqual(["secret", "read"]);
    expect(routeTokens("/web/products")).toEqual(["product", "read"]);
    expect(routeTokens("/web/tags")).toEqual(["tag", "read"]);
  });

  it("labels the off-rail pages by their own registry word in the top bar", () => {
    expect(sectionLabel("/web/catalog")).toBe("Catalog");
    expect(sectionLabel("/web/location-types")).toBe("Location types");
    expect(sectionLabel("/web/secret-types")).toBe("Secret types");
    expect(sectionLabel("/web/metrics")).toBe("Metrics");
    // A detail route resolves by longest prefix, as before.
    expect(sectionLabel("/web/metrics/temperature-c")).toBe("Metrics");
    // Rail entries are unchanged.
    expect(sectionLabel("/web/components")).toBe("Components");
    expect(sectionLabel("/web/users")).toBe("Users");
  });

  it("keeps every off-rail page's identity for bookmarks and stubs", () => {
    expect(lookupNav("/web/products").label).toBe("Products");
    expect(lookupNav("/web/rules").label).toBe("Rules");
    expect(lookupNav("/web/log-types").label).toBe("Logs");
  });

  it("surfaces each pending off-rail page's tracking issue on its stub", () => {
    expect(lookupNav("/web/notifications").issue).toBe(618);
    expect(lookupNav("/web/rules").issue).toBe(624);
  });

  // The shell is presentation over existing routes: every pre-shell URL keeps
  // its path and its exact gate. The whole catalog-area route set, pinned
  // literally so a route or gate change cannot ride in silently.
  it("pins the full catalog-area route set with its gates (no URL and no gate changed with the shell)", () => {
    const expected: Record<string, string[] | null> = {
      "/catalog": null,
      "/metrics": ["metric_type", "read"],
      "/properties": ["property_type", "read"],
      "/event-types": ["event_type", "read"],
      "/command-types": ["command_type", "read"],
      "/vendors": ["vendor", "read"],
      "/products": ["product", "read"],
      "/drivers": ["driver", "read"],
      "/capabilities": ["capability", "read"],
      "/component-types": ["component_type", "read"],
      "/standards": ["standard", "read"],
      "/location-types": ["location_type", "read"],
      "/secret-types": ["secret", "read"],
      "/tags": ["tag", "read"],
      "/rules": null,
      "/notifications": null,
      "/templates": null,
      "/log-types": null,
    };
    for (const [path, need] of Object.entries(expected)) {
      expect(routeTokens(`/web${path}`), path).toEqual(need);
    }
    // The set is complete: every off-rail page is accounted for above.
    for (const o of OFF_RAIL) expect(Object.keys(expected), o.path).toContain(o.path);
  });
});

describe("nav paths bind to routes (#608)", () => {
  it("resolves every unlive nav path to a registered stub, never NotFound", () => {
    const unlive = navItems.flatMap((i) => (i.children ?? []).filter((c) => !c.live).map((c) => c.path));
    for (const path of unlive) expect(STUBS, `${path} has a rail entry but no stub route`).toContain(path);
  });

  it("keeps the off-rail /templates stub its identity", () => {
    expect(STUBS).toContain("/templates");
    expect(lookupNav("/web/templates").label).toBe("Templates");
    expect(sectionLabel("/web/templates")).toBe("Templates");
  });
});
