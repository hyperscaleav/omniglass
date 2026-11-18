import { describe, it, expect, beforeEach } from "vitest";
import { addSubjects, clearTray, contextEntries, handOverPage, isHandOverKey, pageSubject, removeSubject, subjectOf, tray } from "./handover";

// The context builder (#2122): what the person hands omnibot is references and
// the page as a query, never what the console rendered and never a uuid.

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
});

describe("subjectOf", () => {
  it("reads the stamp a primitive carries: a kind and a name or address", () => {
    const row = document.createElement("tr");
    row.dataset.subjectKind = "component";
    row.dataset.subjectRef = "hq.lobby.display-1";
    row.textContent = "Lobby display (offline: panel says HELLO WORLD)";
    expect(subjectOf(row)).toEqual({ kind: "component", ref: "hq.lobby.display-1" });
  });

  it("is nothing for an unstamped element or a uuid reference", () => {
    expect(subjectOf(document.createElement("div"))).toBeUndefined();
    const row = document.createElement("tr");
    row.dataset.subjectKind = "component";
    row.dataset.subjectRef = UUID;
    expect(subjectOf(row)).toBeUndefined();
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

describe("the tray", () => {
  beforeEach(() => clearTray());

  it("adds a subject once, removes one, and clears", () => {
    addSubjects([{ kind: "alarm", ref: "hq.a/offline" }, { kind: "system", ref: "hq.room-1" }]);
    addSubjects([{ kind: "alarm", ref: "hq.a/offline" }]);
    expect(tray()).toHaveLength(2);
    removeSubject({ kind: "alarm", ref: "hq.a/offline" });
    expect(tray()).toEqual([{ kind: "system", ref: "hq.room-1" }]);
    clearTray();
    expect(tray()).toEqual([]);
  });
});

describe("the hand-over key", () => {
  beforeEach(() => clearTray());

  const key = (k: Partial<KeyboardEvent>) => ({ key: "j", metaKey: false, ctrlKey: false, altKey: false, shiftKey: false, ...k });

  it("is ⌘J or Ctrl-J, and nothing else", () => {
    expect(isHandOverKey(key({ metaKey: true }))).toBe(true);
    expect(isHandOverKey(key({ ctrlKey: true, key: "J" }))).toBe(true);
    expect(isHandOverKey(key({}))).toBe(false);
    expect(isHandOverKey(key({ metaKey: true, key: "k" }))).toBe(false);
    expect(isHandOverKey(key({ ctrlKey: true, shiftKey: true }))).toBe(false);
  });

  it("hands over the page when nothing is gathered yet, and leaves gathered subjects alone", () => {
    handOverPage("/alarms", "?state=active");
    expect(tray()).toEqual([{ kind: "page", ref: "/alarms?state=active" }]);
    clearTray();
    addSubjects([{ kind: "alarm", ref: "hq.a/offline" }]);
    handOverPage("/alarms", "");
    expect(tray()).toEqual([{ kind: "alarm", ref: "hq.a/offline" }]);
  });
});
