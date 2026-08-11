import { describe, it, expect } from "vitest";
import { resolveInherited, type InheritedNode } from "./typechain";

// A three-deep chain where each fact is set at a different tier, so a walk that
// stopped at the first node or ran to the root would answer wrong on one of them.
const chain = new Map<string, InheritedNode & { stem?: string; icon?: string }>([
  ["device", { name: "device", stem: "device", icon: "box" }],
  ["display", { name: "display", parent: "device", stem: "display" }],
  ["video-wall", { name: "video-wall", parent: "display", icon: "monitor" }],
]);

describe("resolveInherited", () => {
  it("takes the node's own value when it has one", () => {
    expect(resolveInherited("display", chain, (t) => t.stem)).toBe("display");
  });

  it("walks to the nearest ancestor that has one", () => {
    expect(resolveInherited("video-wall", chain, (t) => t.stem)).toBe("display");
    expect(resolveInherited("display", chain, (t) => t.icon)).toBe("box");
  });

  it("returns undefined when nothing in the chain carries the fact", () => {
    const bare = new Map<string, InheritedNode & { stem?: string }>([["thing", { name: "thing" }]]);
    expect(resolveInherited("thing", bare, (t) => t.stem)).toBeUndefined();
  });

  it("returns undefined for a name the registry does not contain", () => {
    expect(resolveInherited("ghost", chain, (t) => t.stem)).toBeUndefined();
    expect(resolveInherited(undefined, chain, (t) => t.stem)).toBeUndefined();
  });

  it("treats an empty string as absent, the way the server's first-non-null walk does", () => {
    const blank = new Map<string, InheritedNode & { stem?: string }>([
      ["root", { name: "root", stem: "root" }],
      ["leaf", { name: "leaf", parent: "root", stem: "" }],
    ]);
    expect(resolveInherited("leaf", blank, (t) => t.stem)).toBe("root");
  });

  it("terminates on a cycle rather than hanging the console", () => {
    const cyclic = new Map<string, InheritedNode & { stem?: string }>([
      ["a", { name: "a", parent: "b" }],
      ["b", { name: "b", parent: "a" }],
    ]);
    expect(resolveInherited("a", cyclic, (t) => t.stem)).toBeUndefined();
  });
});
