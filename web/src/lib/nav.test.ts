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

  it("orders the inventory section Components, Systems, Locations, then the stubs", () => {
    const inv = navItems.find((i) => i.label === "Inventory");
    expect(inv?.children?.map((c) => c.label)).toEqual([
      "Components", "Systems", "Locations", "Interfaces", "Nodes", "Tasks",
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
  it("restores every Settings tab for the owner (`>`)", () => {
    expect(section("Settings", [">"])).toContain("Users");
    expect(section("Settings", [">"])).toContain("Roles");
    expect(section("Settings", [">"])).toContain("Audit");
  });

  // The Audit tab is gated on the admin tier, not a bare read: a viewer whose
  // `*:read` the server 403s at the 3-token audit route must not see the tab, while
  // an explicit `audit:read:admin` (admin) and `>` (owner) do.
  it("gates Audit on the admin tier, matching the server's audit:read:admin route", () => {
    expect(section("Settings", ["*:read"])).not.toContain("Audit");
    expect(section("Settings", ["audit:read:admin"])).toContain("Audit");
    expect(section("Settings", [">"])).toContain("Audit");
  });

  // The Users, Roles, and Groups directories are admin-tier reads
  // (<resource>:read:admin), matching the server routes: a viewer's *:read cannot
  // reach them, while admin's explicit read:admin grants (and owner's >) do.
  it("hides Users, Roles, and Groups from a *:read principal, keeps them for admin", () => {
    const floor = section("Settings", ["*:read"]);
    expect(floor).not.toContain("Users");
    expect(floor).not.toContain("Roles");
    expect(floor).not.toContain("Groups");
    const adm = section("Settings", ["principal:read:admin", "role:read:admin", "principal_group:read:admin"]);
    expect(adm).toContain("Users");
    expect(adm).toContain("Roles");
    expect(adm).toContain("Groups");
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
    const settings = navItems.find((i) => i.label === "Settings")!;
    for (const c of settings.children!) {
      const need = routeTokens(`/web${c.path}`);
      if (c.resource || c.perm) expect(need).not.toBeNull();
      else expect(need).toBeNull();
    }
  });
});
