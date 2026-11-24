import { describe, it, expect } from "vitest";
import { arrivedApart, fromEvent, fromLog, isFiltered, messageParts } from "./occurrence";
import { uuidFor } from "./testids";

const HEX = "a".repeat(32) + "0123456789abcdef0123456789abcdef";

describe("messageParts", () => {
  it("leaves a message with no id whole", () => {
    expect(messageParts("collection failed")).toEqual([{ text: "collection failed" }]);
  });

  it("turns a 64-hex id into a short labelled reference, folding in the word that names it", () => {
    expect(messageParts(`collection failed for task ${HEX}: timeout`)).toEqual([
      { text: "collection failed for " },
      { ref: HEX, label: "task", short: HEX.slice(0, 8) },
      { text: ": timeout" },
    ]);
    expect(messageParts(`task_id=${HEX}`)).toEqual([{ ref: HEX, label: "task", short: HEX.slice(0, 8) }]);
  });

  it("labels an unnamed id as id, and handles several", () => {
    const parts = messageParts(`${HEX} then ${HEX.toUpperCase()}`);
    expect(parts.filter((p) => "ref" in p)).toHaveLength(2);
    expect(parts[0]).toMatchObject({ label: "id" });
  });

  it("leaves a shorter hex string alone", () => {
    expect(messageParts("commit deadbeef12")).toEqual([{ text: "commit deadbeef12" }]);
  });
});

describe("normalizing", () => {
  it("maps an event to its key badge and the alarm it carries", () => {
    const o = fromEvent({ ts: "2026-11-24T00:00:00Z", key: "rule-fired", event_type_id: "x", origin: "derived", message: "m", provenance: "observed", alarm_id: "al-1", derived_by_rule: "r", derived_by_rule_id: uuidFor("rid") }, 0, "comp-1");
    expect(o).toMatchObject({ kind: "event", badge: "rule-fired", rule: { name: "r", id: uuidFor("rid") }, alarm: { id: "al-1", component: "comp-1" } });
  });

  it("takes a system event's alarm timeline from its component owner only", () => {
    const base = { ts: "t", key: "k", event_type_id: "x", origin: "caught", message: "m", provenance: "observed", alarm_id: "al-1", owner: "o", owner_id: "oid" };
    expect(fromEvent({ ...base, owner_kind: "component" }, 0).alarm).toEqual({ id: "al-1", component: "oid" });
    expect(fromEvent({ ...base, owner_kind: "system" }, 0).alarm).toBeUndefined();
  });

  it("colors a log by its severity and names its owner when a system lists it", () => {
    const o = fromLog({ ts: "t", message: "m", severity: "error", component: "bar-1" }, 0);
    expect(o).toMatchObject({ kind: "log", badge: "error", badgeClass: "badge-error", owner: "bar-1" });
  });

  it("says a line arrived apart only past a minute", () => {
    expect(arrivedApart({ ts: "2026-11-24T00:00:00Z", receivedAt: "2026-11-24T00:00:30Z" })).toBe(false);
    expect(arrivedApart({ ts: "2026-11-24T00:00:00Z", receivedAt: "2026-11-24T03:00:00Z" })).toBe(true);
  });

  it("reads a filter as narrowed only when a field is set", () => {
    expect(isFiltered({})).toBe(false);
    expect(isFiltered({ q: "x" })).toBe(true);
  });
});
