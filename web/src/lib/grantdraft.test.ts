import { describe, it, expect } from "vitest";
import {
  grantKey,
  stageGrant,
  toggleGrant,
  chipStates,
  pendingDiff,
  isDirty,
  validateStage,
  type ExistingGrant,
  type GrantRef,
} from "./grantdraft";

// grantdraft is the pure staging core behind the grant builder: the admin builds a
// desired grant set (draft) on top of the current server grants, and nothing is
// sent until save. These pure functions compute the draft, its per-chip visual
// state, and the add/remove diff the save applies. No I/O, no framework.

const existing = (id: string, role: string, kind: GrantRef["scope_kind"], scope?: string): ExistingGrant =>
  ({ id, role, scope_kind: kind, scope_id: scope });
const ref = (role: string, kind: GrantRef["scope_kind"], scope?: string): GrantRef =>
  ({ role, scope_kind: kind, scope_id: scope });

describe("grantKey", () => {
  it("identifies a grant by role and scope, ignoring the server id and the all scope's phantom id", () => {
    expect(grantKey(ref("admin", "all"))).toBe(grantKey(ref("admin", "all", "ignored")));
    expect(grantKey(ref("admin", "location", "boi"))).toBe("admin@location:boi");
    expect(grantKey(ref("admin", "all"))).toBe("admin@all");
  });
  it("distinguishes the same role at different scopes and different roles at one scope", () => {
    expect(grantKey(ref("admin", "location", "boi"))).not.toBe(grantKey(ref("admin", "location", "sjc")));
    expect(grantKey(ref("admin", "location", "boi"))).not.toBe(grantKey(ref("viewer", "location", "boi")));
  });
});

describe("stageGrant", () => {
  it("appends a new grant to the draft", () => {
    const out = stageGrant([ref("viewer", "all")], ref("admin", "location", "boi"));
    expect(out).toHaveLength(2);
    expect(out[1]).toEqual(ref("admin", "location", "boi"));
  });
  it("is a no-op when the same role@scope is already staged (dedup by key)", () => {
    const draft = [ref("admin", "location", "boi")];
    expect(stageGrant(draft, ref("admin", "location", "boi"))).toEqual(draft);
  });
});

describe("toggleGrant", () => {
  const current = [existing("g1", "admin", "location", "boi")];
  it("marks an existing kept grant for removal (drops it from the draft)", () => {
    const draft = [ref("admin", "location", "boi")];
    expect(toggleGrant(current, draft, "admin@location:boi")).toEqual([]);
  });
  it("undoes a removal by re-adding the existing grant from the server set", () => {
    const draft: GrantRef[] = []; // admin@boi marked for removal
    const out = toggleGrant(current, draft, "admin@location:boi");
    expect(out).toHaveLength(1);
    expect(grantKey(out[0])).toBe("admin@location:boi");
  });
  it("cancels a pending add", () => {
    const draft = [ref("admin", "location", "boi"), ref("viewer", "all")];
    expect(toggleGrant(current, draft, "viewer@all")).toEqual([ref("admin", "location", "boi")]);
  });
});

describe("chipStates", () => {
  it("labels each chip unchanged, removed, or added, existing first then adds", () => {
    const current = [existing("g1", "admin", "location", "boi"), existing("g2", "viewer", "all")];
    // keep admin@boi, drop viewer@all, add operator@sjc.
    const draft = [ref("admin", "location", "boi"), ref("operator", "location", "sjc")];
    const states = chipStates(current, draft);
    expect(states.map((s) => s.kind)).toEqual(["unchanged", "removed", "added"]);
    expect(grantKey(states[2].grant)).toBe("operator@location:sjc");
  });
});

describe("pendingDiff / isDirty", () => {
  const current = [existing("g1", "admin", "location", "boi"), existing("g2", "viewer", "all")];
  it("is clean when the draft equals the current set regardless of order", () => {
    const draft = [ref("viewer", "all"), ref("admin", "location", "boi")];
    expect(pendingDiff(current, draft)).toEqual({ adds: [], removes: [] });
    expect(isDirty(current, draft)).toBe(false);
  });
  it("splits into adds (new refs) and removes (existing grants with ids)", () => {
    const draft = [ref("admin", "location", "boi"), ref("operator", "location", "sjc")];
    const { adds, removes } = pendingDiff(current, draft);
    expect(adds).toEqual([ref("operator", "location", "sjc")]);
    expect(removes).toEqual([existing("g2", "viewer", "all")]);
    expect(isDirty(current, draft)).toBe(true);
  });
});

describe("validateStage", () => {
  const draft = [ref("admin", "location", "boi")];
  it("requires a role", () => {
    expect(validateStage(draft, { scope_kind: "all" })).toBe("role-required");
  });
  it("requires an entity for a scoped kind, but not for all", () => {
    expect(validateStage(draft, { role: "viewer", scope_kind: "location" })).toBe("entity-required");
    expect(validateStage(draft, { role: "viewer", scope_kind: "all" })).toBeNull();
  });
  it("rejects a duplicate of an already-staged grant", () => {
    expect(validateStage(draft, { role: "admin", scope_kind: "location", scope_id: "boi" })).toBe("duplicate");
  });
  it("accepts a valid, novel grant", () => {
    expect(validateStage(draft, { role: "operator", scope_kind: "location", scope_id: "sjc" })).toBeNull();
  });
});
