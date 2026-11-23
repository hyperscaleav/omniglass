import { describe, expect, it } from "vitest";
import { alarmFilterKeys, chipsFromParams, paramsFromChips, queryFromChips } from "./alarmqueue";
import { buildPredicate } from "./predicate";
import type { Alarm } from "./alarms";

describe("the alarm queue's filter, in the address", () => {
  it("reads chips out of the address and writes the same back", () => {
    const chips = chipsFromParams({ state: "active", severity: "critical,high", location: "hq", ack: "no" });
    expect(chips).toEqual([
      { key: "severity", op: "eq", values: ["critical", "high"] },
      { key: "location", op: "eq", values: ["hq"] },
      { key: "ack", op: "eq", values: ["no"] },
    ]);
    expect(paramsFromChips(chips)).toEqual({ state: "active", severity: "critical,high", location: "hq", ack: "no", snoozed: undefined });
  });

  it("takes a repeated severity parameter as well as a comma list", () => {
    expect(chipsFromParams({ severity: ["critical", "high"] })[0].values).toEqual(["critical", "high"]);
  });

  it("drops what it does not know rather than failing", () => {
    expect(chipsFromParams({ ack: "maybe", snoozed: "yes", bogus: "1" })).toEqual([{ key: "snoozed", op: "eq", values: ["yes"] }]);
  });

  it("turns chips into the server's own filter", () => {
    expect(queryFromChips(chipsFromParams({ severity: "critical", location: "hq", ack: "no", snoozed: "yes" }))).toEqual({
      severity: ["critical"], location: "hq", acknowledged: false, snoozed: true,
    });
    expect(queryFromChips([])).toEqual({});
  });

  it("agrees with the server that a location chip keeps its whole subtree", () => {
    const chips = chipsFromParams({ location: "hq" });
    const keys = alarmFilterKeys(() => chips, () => []);
    const keep = buildPredicate(keys, chips);
    const room = { location: "hq-b1", location_path: "hq.hq-b1" } as Alarm;
    const other = { location: "annex", location_path: "annex" } as Alarm;
    expect(keep(room)).toBe(true);
    expect(keep(other)).toBe(false);
  });
});
