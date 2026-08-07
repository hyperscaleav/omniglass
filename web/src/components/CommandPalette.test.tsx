import { describe, it, expect } from "vitest";
import { commands, matches } from "./CommandPalette";

// The palette flattens the nav IA to destinations. The Catalog rail collapsed
// to a single browse entry (the single-surface prototype), so the palette lists
// Catalog itself and the per-registry entries left with their rail children;
// finding a registry is the browse page's search now.
describe("command palette source", () => {
  it("lists Catalog as a top-level destination", () => {
    expect(commands).toContainEqual(expect.objectContaining({ label: "Catalog", path: "/catalog" }));
  });

  it("no longer lists the per-registry pages the rail dropped", () => {
    expect(commands.find((c) => c.path === "/products")).toBeUndefined();
    expect(commands.find((c) => c.path === "/secret-types")).toBeUndefined();
    expect(commands.find((c) => c.path === "/metrics")).toBeUndefined();
  });

  it("keeps grouped entries' group tags unchanged", () => {
    expect(commands).toContainEqual(expect.objectContaining({ label: "Components", path: "/components", group: "Inventory" }));
    expect(commands).toContainEqual(expect.objectContaining({ label: "Users", path: "/users", group: "Admin" }));
  });

  it("matches on label and group: 'inventory' finds the Inventory pages", () => {
    const hits = commands.filter((c) => matches(c, "inventory"));
    expect(hits.map((c) => c.path)).toContain("/components");
    expect(hits.map((c) => c.path)).toContain("/locations");
  });
});
