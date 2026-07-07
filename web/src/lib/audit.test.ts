import { describe, it, expect } from "vitest";
import { auditFilterKeys, actorLabel, accountableLabel, type AuditEvent } from "./audit";
import { buildPredicate, type Chip } from "./predicate";

// The audit facets are the console's shared faceted search applied to the trail:
// who (actor), action (verb), resource (kind), id (resource id). These pin the
// filtering behavior the FilterBar drives, independent of the page render.
const ev = (p: Partial<AuditEvent>): AuditEvent => ({ id: "e", ts: "2026-07-07T00:00:00Z", verb: "read", resource: "auth", ...p });

const rows: AuditEvent[] = [
  ev({ id: "1", actor_name: "alice", verb: "login", resource: "auth" }),
  ev({ id: "2", actor_name: "bob", verb: "delete", resource: "component", resource_id: "cmp_9f2" }),
  ev({ id: "3", actor_name: "alice", verb: "logout", resource: "auth" }),
  ev({ id: "4", actor_name: "carol", verb: "create", resource: "principal_grant", resource_id: "grant_01" }),
];

const shown = (chips: Chip[]): string[] => rows.filter(buildPredicate(auditFilterKeys, chips)).map((e) => e.id);

describe("auditFilterKeys", () => {
  it("filters by who (actor substring)", () => {
    expect(shown([{ key: "who", op: "contains", values: ["ali"] }])).toEqual(["1", "3"]);
  });
  it("filters by action (verb, exact)", () => {
    expect(shown([{ key: "action", op: "eq", values: ["login"] }])).toEqual(["1"]);
    // Within one chip, values OR: login OR create.
    expect(shown([{ key: "action", op: "eq", values: ["login", "create"] }])).toEqual(["1", "4"]);
  });
  it("filters by resource (exact)", () => {
    expect(shown([{ key: "resource", op: "eq", values: ["auth"] }])).toEqual(["1", "3"]);
  });
  it("filters by id (resource_id substring), empty on rows without one", () => {
    expect(shown([{ key: "id", op: "contains", values: ["cmp_"] }])).toEqual(["2"]);
    expect(shown([{ key: "id", op: "contains", values: ["grant"] }])).toEqual(["4"]);
  });
  it("ANDs across chips", () => {
    expect(shown([
      { key: "who", op: "contains", values: ["alice"] },
      { key: "action", op: "eq", values: ["logout"] },
    ])).toEqual(["3"]);
  });
  it("offers a sorted, de-duplicated value catalog per facet", () => {
    const byKey = Object.fromEntries(auditFilterKeys.map((k) => [k.key, k]));
    expect(byKey.who.values!(rows)).toEqual(["alice", "bob", "carol"]);
    expect(byKey.action.values!(rows)).toEqual(["create", "delete", "login", "logout"]);
    expect(byKey.resource.values!(rows)).toEqual(["auth", "component", "principal_grant"]);
  });
  it("who falls back to system when the actor is unknown", () => {
    expect(actorLabel(ev({ actor_name: undefined, actor: undefined }))).toBe("system");
  });

  it("holds the impersonator accountable, and `who` matches either party", () => {
    // root acted while impersonating alice: the impersonator (root) is accountable.
    const imp = [ev({ id: "imp", actor_name: "alice", real_actor: "u-root", real_actor_name: "root", verb: "update", resource: "principal" })];
    expect(accountableLabel(imp[0])).toBe("root");
    const hit = (term: string) => imp.filter(buildPredicate(auditFilterKeys, [{ key: "who", op: "contains", values: [term] }])).map((e) => e.id);
    expect(hit("root")).toEqual(["imp"]); // the impersonator
    expect(hit("alice")).toEqual(["imp"]); // the assumed identity
  });
});
