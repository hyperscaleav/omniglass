import { describe, expect, it } from "vitest";
import { byName, mergeResolved } from "./resolution";

describe("mergeResolved and byName", () => {
  const tag = { key: "environment", value: "prod", owner_kind: "location", owner_name: "hq", band: 1, depth: 0, winner: true };
  const tagShadow = { key: "environment", value: "dev", owner_kind: "platform", band: 0, depth: 0, winner: false };
  const variable = { id: "v1", name: "poll_interval", value_type: "int", value: 30, owner_kind: "component", owner_name: "codec-1", band: 3, depth: 0, winner: true };
  const secret = { id: "s1", name: "device-login", secret_type: "basic-auth", owner_kind: "location", owner_name: "hq", band: 1, depth: 0, winner: true, fields: [] };

  it("carries the kind explicitly, because the three cascades have different bands", () => {
    const rows = mergeResolved([tag] as never, [variable] as never, [secret] as never);
    expect(rows.map((r) => r.kind).sort()).toEqual(["secret", "tag", "variable"]);
  });

  // A secret's fields are masked, so the panel shows provenance and no value.
  // Carrying a value here at all would invite a surface that leaks one.
  it("gives a secret no value", () => {
    const [row] = mergeResolved([] as never, [] as never, [secret] as never);
    expect(row.value).toBe("");
    expect(row.owner_name).toBe("hq");
  });

  // A variable's value is polymorphic; rendering it raw gives "[object Object]".
  it("renders a variable's value the way it was typed", () => {
    const [row] = mergeResolved([] as never, [variable] as never, [] as never);
    expect(row.value).toBe("30");
  });

  it("groups a winner with what it beat, per kind and name", () => {
    const groups = byName(mergeResolved([tag, tagShadow] as never, [variable] as never, [] as never));
    const env = groups.find((g) => g.name === "environment")!;
    expect(env.winner?.value).toBe("prod");
    expect(env.shadowed).toHaveLength(1);
    expect(env.shadowed[0].value).toBe("dev");
  });

  // Two kinds can share a name; they are separate rows, not one merged group.
  it("keeps the same name in two kinds apart", () => {
    const clash = { ...variable, name: "environment" };
    const groups = byName(mergeResolved([tag] as never, [clash] as never, [] as never));
    expect(groups.filter((g) => g.name === "environment")).toHaveLength(2);
  });
});
