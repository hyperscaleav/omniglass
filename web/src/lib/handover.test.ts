import { describe, it, expect } from "vitest";
import { alarmSubject, contextEntries, namePage, pageHere, pageSubject, triageCall } from "./handover";

// The context builder (#2122, #2162): what omnibot is handed is the page the
// person is on as a query, and references, never what the console rendered
// and never a uuid.

const UUID = "3f2b8c1e-9a4d-4e6f-8b2a-1c5d7e9f0a3b";

describe("pageSubject", () => {
  it("is the route and its arguments", () => {
    expect(pageSubject("/alarms", "?state=active&sort=raised")).toEqual({ kind: "page", ref: "/alarms?state=active&sort=raised" });
    expect(pageSubject("/components", "")).toEqual({ kind: "page", ref: "/components" });
  });

  it("drops an argument that is a uuid, and a route that holds one", () => {
    expect(pageSubject("/audit", `?actor=${UUID}&verb=create`)).toEqual({ kind: "page", ref: "/audit?verb=create" });
    expect(pageSubject(`/things/${UUID}`, "")).toBeUndefined();
  });

  it("names a detail page by its entity's name or address, in place of the uuid its route holds", () => {
    expect(pageSubject(`/systems/${UUID}`, "?tab=health", "auditorium")).toEqual({ kind: "page", ref: "/systems/auditorium?tab=health" });
    expect(pageSubject(`/systems/${UUID}`, "", UUID)).toBeUndefined();
  });
});

describe("pageHere", () => {
  it("is the page the person is on, a detail page by the name it shows", () => {
    expect(pageHere("/alarms", "?state=active")).toEqual({ kind: "page", ref: "/alarms?state=active" });
    namePage("hq.lobby.display-1");
    expect(pageHere(`/components/${UUID}`, "")).toEqual({ kind: "page", ref: "/components/hq.lobby.display-1" });
    namePage(undefined);
    expect(pageHere(`/components/${UUID}`, "")).toBeUndefined();
  });
});

describe("contextEntries", () => {
  it("names each subject by kind and reference, nothing else", () => {
    const entries = contextEntries([
      { kind: "page", ref: "/alarms?state=active" },
      { kind: "alarm", ref: "hq.lobby.display-1/offline" },
    ]);
    expect(entries).toEqual([
      { description: "page", value: "/alarms?state=active" },
      { description: "alarm", value: "hq.lobby.display-1/offline" },
    ]);
  });

  it("keeps out a uuid, a duplicate and anything past what a run takes", () => {
    const many = Array.from({ length: 30 }, (_, i) => ({ kind: "component", ref: `hq.display-${i}` }));
    const entries = contextEntries([{ kind: "component", ref: UUID }, { kind: "component", ref: "hq.display-0" }, ...many]);
    expect(entries.some((e) => e.value.includes(UUID))).toBe(false);
    expect(entries.filter((e) => e.value === "hq.display-0")).toHaveLength(1);
    expect(entries).toHaveLength(20);
    expect(contextEntries([{ kind: "page", ref: "/x?q=" + "a".repeat(2000) }])).toEqual([]);
  });
});

describe("alarmSubject", () => {
  it("is the component's name and the rule that raised it, else when it was raised", () => {
    expect(alarmSubject({ component: "hq.lobby.display-1", raised_by_rule: "offline", raised_at: "2026-11-18T10:00:00Z" })).toEqual({ kind: "alarm", ref: "hq.lobby.display-1/offline" });
    expect(alarmSubject({ component: "hq.lobby.display-1", raised_at: "2026-11-18T10:00:00Z" })).toEqual({ kind: "alarm", ref: "hq.lobby.display-1@2026-11-18T10:00:00Z" });
  });
});

describe("triageCall", () => {
  it("runs triage-alarm on the alarm's component and the rule that raised it", () => {
    expect(triageCall({ kind: "alarm", ref: "hq.lobby.mic-1/icmp-unreachable" })).toEqual({
      name: "triage-alarm",
      arguments: { component: "hq.lobby.mic-1", alarm: "icmp-unreachable" },
    });
  });

  it("names only the component for an alarm known by when it was raised", () => {
    expect(triageCall({ kind: "alarm", ref: "mic-1@2026-08-15T14:20:00Z" })).toEqual({ name: "triage-alarm", arguments: { component: "mic-1" } });
  });

  it("is nothing for a subject that is not an alarm", () => {
    expect(triageCall({ kind: "component", ref: "mic-1" })).toBeUndefined();
    expect(triageCall({ kind: "alarm", ref: "/x" })).toBeUndefined();
  });
});
