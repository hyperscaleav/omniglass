import { describe, it, expect } from "vitest";
import { filterNav, navItems, routeTokens, type NavItem } from "./nav";
import { can, type Me } from "./auth";

const Dummy = () => null;

// A real nav gate: filter the live nav through the actual can() over a set of
// permission strings, exactly as the sidebar does, and read a section's children.
const meWith = (permissions: string[]): Me => ({ principal: { id: "p", kind: "human" }, permissions, grants: [] });
const section = (label: string, permissions: string[]): string[] =>
  filterNav(navItems, (tokens) => can(meWith(permissions), ...tokens)).find((i) => i.label === label)?.children?.map((c) => c.label) ?? [];

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

  it("orders the inventory section Components, Systems, Locations, the stubs, then the values band", () => {
    const inv = navItems.find((i) => i.label === "Inventory");
    expect(inv?.children?.map((c) => c.label)).toEqual([
      "Components", "Systems", "Locations", "Interfaces", "Nodes", "Tasks",
      "Variables", "Secrets", "Config",
    ]);
  });

  it("on the real nav, a principal without system/component/location read loses those tabs but keeps the stubs", () => {
    const out = filterNav(navItems, (tokens) => !["system", "component", "location"].includes(tokens[0]));
    const inv = out.find((i) => i.label === "Inventory");
    const labels = inv?.children?.map((c) => c.label) ?? [];
    expect(labels).not.toContain("Systems");
    expect(labels).not.toContain("Components");
    expect(labels).not.toContain("Locations");
    expect(labels).toContain("Interfaces"); // a resource-less stub stays
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
  it("hides Secrets from a *:read viewer, keeps it for an explicit secret:read and owner", () => {
    expect(section("Settings", ["*:read"])).not.toContain("Secrets");
    expect(section("Settings", ["*:*"])).not.toContain("Secrets");
    expect(section("Settings", ["secret:read"])).toContain("Secrets");
    expect(section("Settings", ["secret:read,reveal,create,update"])).toContain("Secrets");
    expect(section("Settings", [">"])).toContain("Secrets");
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
    const settings = navItems.find((i) => i.label === "Admin")!;
    for (const c of settings.children!) {
      const need = routeTokens(`/web${c.path}`);
      if (c.resource || c.perm) expect(need).not.toBeNull();
      else expect(need).toBeNull();
    }
  });
});

describe("nav IA rework", () => {
  it("groups the estate-attached values under Inventory", () => {
    const inv = section("Inventory", [">"]);
    expect(inv).toContain("Components");
    expect(inv).toContain("Variables");
    expect(inv).toContain("Secrets");
    expect(inv).toContain("Config");
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

  it("marks the two Inventory band leaders with a section header", () => {
    const inv = navItems.find((i) => i.label === "Inventory")!.children!;
    expect(inv.find((c) => c.label === "Components")!.section).toBe("Entities");
    expect(inv.find((c) => c.label === "Variables")!.section).toBe("Values");
  });

  it("keeps moved entries' gates and leaves the stubs ungated", () => {
    expect(routeTokens("/web/secrets")).toEqual(["secret", "read"]);
    expect(routeTokens("/web/variables")).toEqual(["variable", "read"]);
    expect(routeTokens("/web/config")).toBeNull();
    expect(routeTokens("/web/settings")).toBeNull();
  });
});
