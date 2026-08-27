import { describe, it, expect } from "vitest";
import { buildCommands, matches } from "./CommandPalette";

// The palette flattens every destination the caller may reach to one searchable
// list: the nav IA plus the catalog registries, which left the rail for the
// shell's subrail (#625) and would otherwise be unfindable by name (#628). Both
// halves are judged through the same `allow` the sidebar and the subrail use, so
// the palette can never offer a jump the route guard would bounce.
const allowAll = () => true;
const deny = (...topics: string[]) => {
  const blocked = new Set(topics);
  return (tokens: string[]) => !blocked.has(tokens.join(":"));
};

describe("command palette source", () => {
  it("lists Catalog as a top-level destination", () => {
    expect(buildCommands(allowAll)).toContainEqual(expect.objectContaining({ label: "Catalog", path: "/catalog" }));
  });

  it("lists the per-registry pages the rail dropped, tagged by catalog group", () => {
    const commands = buildCommands(allowAll);
    expect(commands).toContainEqual(expect.objectContaining({ label: "Products", path: "/products", group: "Catalog · Components" }));
    expect(commands).toContainEqual(expect.objectContaining({ label: "Metrics", path: "/metrics", group: "Catalog · Telemetry" }));
    expect(commands).toContainEqual(expect.objectContaining({ label: "Tags", path: "/tags", group: "Catalog · Metadata" }));
  });

  it("keeps grouped entries' group tags unchanged", () => {
    const commands = buildCommands(allowAll);
    expect(commands).toContainEqual(expect.objectContaining({ label: "Components", path: "/components", group: "Fleet" }));
    expect(commands).toContainEqual(expect.objectContaining({ label: "Users", path: "/users", group: "Admin" }));
  });

  it("lists a routed soon registry, and no pathless slot", () => {
    const commands = buildCommands(allowAll);
    expect(commands).toContainEqual(expect.objectContaining({ label: "Rules", path: "/rules", group: "Catalog · Actions" }));
    expect(commands.filter((c) => c.label === "Templates")).toEqual([]);
    expect(commands.every((c) => c.path)).toBe(true);
  });

  // The standing ruling: secret types holds no nav slot. /secret-types stays
  // routed and reachable by URL under its gate, but it is not a destination the
  // palette offers.
  it("does not list secret types", () => {
    expect(buildCommands(allowAll).find((c) => c.path === "/secret-types")).toBeUndefined();
  });

  it("drops a destination the caller cannot read, rail or catalog", () => {
    const commands = buildCommands(deny("metric_type:read", "audit:read:admin"));
    expect(commands.find((c) => c.path === "/metrics")).toBeUndefined();
    expect(commands.find((c) => c.path === "/audit")).toBeUndefined();
    // Everything it may still read stands.
    expect(commands).toContainEqual(expect.objectContaining({ path: "/properties" }));
    expect(commands).toContainEqual(expect.objectContaining({ path: "/users" }));
  });

  it("drops a catalog group whose entries are all gated away", () => {
    const commands = buildCommands(deny("tag:read"));
    expect(commands.find((c) => c.group === "Catalog · Metadata")).toBeUndefined();
  });

  it("matches on label and group: 'fleet' finds the re-homed kind pages (#798)", () => {
    const hits = buildCommands(allowAll).filter((c) => matches(c, "fleet"));
    expect(hits.map((c) => c.path)).toContain("/components");
    expect(hits.map((c) => c.path)).toContain("/locations");
    expect(hits.map((c) => c.path)).toContain("/systems");
  });

  it("matches a registry by its own name: 'products' finds the registry page", () => {
    const hits = buildCommands(allowAll).filter((c) => matches(c, "products"));
    expect(hits.map((c) => c.path)).toContain("/products");
  });

  it("matches every registry on 'catalog', and a lane on its section", () => {
    const commands = buildCommands(allowAll);
    const catalog = commands.filter((c) => matches(c, "catalog")).map((c) => c.path);
    expect(catalog).toEqual(expect.arrayContaining(["/catalog", "/products", "/metrics", "/tags"]));
    const telemetry = commands.filter((c) => matches(c, "telemetry")).map((c) => c.path);
    expect(telemetry).toEqual(expect.arrayContaining(["/metrics", "/properties", "/event-types"]));
    expect(telemetry).not.toContain("/products");
  });
});
