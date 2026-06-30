import { describe, it, expect } from "vitest";
import { filterNav, navItems, type NavItem } from "./nav";

const Dummy = () => null;

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
    const out = filterNav(nav, (r) => r === "system");
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
    const out = filterNav(navItems, (r) => !["system", "component", "location"].includes(r));
    const inv = out.find((i) => i.label === "Inventory");
    const labels = inv?.children?.map((c) => c.label) ?? [];
    expect(labels).not.toContain("Systems");
    expect(labels).not.toContain("Components");
    expect(labels).not.toContain("Locations");
    expect(labels).toContain("Interfaces"); // a resource-less stub stays
  });
});
