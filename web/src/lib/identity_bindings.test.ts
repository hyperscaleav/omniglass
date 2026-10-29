import { describe, expect, it } from "vitest";
import { identityRowKey, ownerChoices, refusalWords } from "./identity_bindings";
import { uuidFor } from "./testids";
import type { Me } from "./auth";
import type { System } from "./systems";
import type { Component } from "./components";

const admin: Me = { principal: { id: "p", kind: "human" }, permissions: [">"], grants: [] };
const componentOnly: Me = { principal: { id: "p", kind: "human" }, permissions: ["*:read", "component:update"], grants: [] };

const systems: System[] = [
  { id: uuidFor("sys-b"), name: "briefing", label: "Briefing", member_count: 1, actions: ["update"] },
  { id: uuidFor("sys-a"), name: "annex", label: "Annex", member_count: 1, actions: ["update"] },
  { id: uuidFor("sys-ro"), name: "lobby", label: "Lobby", member_count: 1, actions: [] },
];
const components: Component[] = [
  { id: uuidFor("conduit"), name: "ucc-platform", system_count: 0, actions: ["update"] },
  { id: uuidFor("panel"), name: "panel-1", label: "Panel 1", system_count: 1, actions: ["update"] },
  { id: uuidFor("bar-ro"), name: "bar-1", system_count: 1, actions: ["read"] },
];

describe("ownerChoices", () => {
  it("offers only owners the caller may update, never the conduit itself, sorted by label", () => {
    const c = ownerChoices(systems, components, uuidFor("conduit"), admin);
    expect(c.systems.map((s) => s.name)).toEqual(["annex", "briefing"]);
    expect(c.components.map((s) => s.name)).toEqual(["panel-1"]);
  });

  it("drops a whole kind the caller holds no update on", () => {
    const c = ownerChoices(systems, components, uuidFor("conduit"), componentOnly);
    expect(c.systems).toEqual([]);
    expect(c.components.map((s) => s.name)).toEqual(["panel-1"]);
  });

  it("falls back to the permission when a row carries no actions", () => {
    const bare: System[] = [{ id: uuidFor("sys-x"), name: "x", member_count: 0 }];
    expect(ownerChoices(bare, [], "", admin).systems.map((s) => s.name)).toEqual(["x"]);
  });
});

describe("refusalWords", () => {
  it("says a 403 is a missing permission, then the server's reason", () => {
    expect(refusalWords({ status: 403, detail: "Ask for system:update over the system." })).toBe(
      "Not allowed: Ask for system:update over the system.",
    );
  });
  it("says a 404 is an owner gone or out of sight, without naming more", () => {
    expect(refusalWords({ status: 404, detail: "owner system not found" })).toBe(
      "Not found: the owner is gone, or outside what you can see.",
    );
  });
  it("says a 422 is a binding the rules refuse, then why", () => {
    expect(refusalWords({ status: 422, detail: "a place or a node owns no observed sample" })).toBe(
      "Not a valid binding: a place or a node owns no observed sample",
    );
  });
  it("falls back to the server's words for anything else", () => {
    expect(refusalWords({ status: 500, detail: "boom" })).toBe("boom");
  });
});

describe("identityRowKey", () => {
  it("keeps a key and a value apart even when one holds the other's separator", () => {
    expect(identityRowKey("a", "b c")).not.toBe(identityRowKey("a b", "c"));
  });
});
