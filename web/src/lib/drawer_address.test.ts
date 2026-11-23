import { describe, it, expect } from "vitest";
import { DRAWER_PAGES, drawerHref, firstParam } from "./drawer_address";
import { ROUTE_MANIFEST } from "./routemanifest";

describe("drawerHref", () => {
  it("addresses a row on its list page", () => {
    expect(drawerHref("event_rule", "av-offline")).toBe("/event-rules?open=av-offline");
  });
  it("encodes the address and carries extra params after it", () => {
    expect(drawerHref("user", "a b/c", { edit: "1" })).toBe("/users?open=a+b%2Fc&edit=1");
  });
  it("never lets extra override the address", () => {
    expect(drawerHref("driver", "x", { open: "y" })).toBe("/drivers?open=x");
  });
  it("points every kind at a routed list page", () => {
    const paths = new Set(ROUTE_MANIFEST.map((r) => r.path));
    for (const [kind, path] of Object.entries(DRAWER_PAGES)) expect(paths.has(path), kind).toBe(true);
  });
});

describe("firstParam", () => {
  it("reads the first of a repeated key and treats empty as absent", () => {
    expect(firstParam(["a", "b"])).toBe("a");
    expect(firstParam("")).toBeUndefined();
    expect(firstParam(undefined)).toBeUndefined();
  });
});
