import { describe, it, expect } from "vitest";
import { commands, matches } from "./CommandPalette";

// The palette flattens the nav IA to destinations. A sectioned Catalog entry
// carries its section in the group tag ("Catalog · Locations"), so the two Types
// registries stay tellable apart, and the filter matches the section word too.
describe("command palette source", () => {
  it("tags a sectioned Catalog entry with Catalog · <section>", () => {
    expect(commands).toContainEqual(expect.objectContaining({ label: "Types", path: "/location-types", group: "Catalog · Locations" }));
    expect(commands).toContainEqual(expect.objectContaining({ label: "Types", path: "/secret-types", group: "Catalog · Secrets" }));
    expect(commands).toContainEqual(expect.objectContaining({ label: "Metrics", path: "/metrics", group: "Catalog · Telemetry" }));
  });

  it("keeps unsectioned entries' group tags unchanged", () => {
    expect(commands).toContainEqual(expect.objectContaining({ label: "Components", path: "/components", group: "Inventory" }));
    expect(commands).toContainEqual(expect.objectContaining({ label: "Users", path: "/users", group: "Admin" }));
  });

  // The Overview hub (#609) rides the same nav derivation: unsectioned, so its
  // group tag is the bare Catalog, with no special-casing in the palette.
  it("lists the Catalog Overview from the nav derivation", () => {
    expect(commands).toContainEqual(expect.objectContaining({ label: "Overview", path: "/catalog", group: "Catalog" }));
  });

  it("matches on label, group, and section: 'location' finds Catalog · Locations > Types", () => {
    const hits = commands.filter((c) => matches(c, "location"));
    expect(hits.map((c) => c.path)).toContain("/location-types");
    expect(hits.map((c) => c.path)).toContain("/locations"); // the Inventory twin still matches on its label
  });
});
