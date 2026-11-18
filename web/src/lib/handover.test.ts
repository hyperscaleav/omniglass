import { describe, it, expect, beforeEach, vi } from "vitest";
import { addSubjects, alarmSubject, triageCall, clearTray, contextEntries, gathered, gatherer, handOverPage, handOverer, isHandOverKey, namePage, pageSubject, removeSubject, stamped, subjectOf, tray } from "./handover";

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

  it("names a detail page by its entity's name or address, in place of the uuid its route holds", () => {
    expect(pageSubject(`/systems/${UUID}`, "?tab=health", "auditorium")).toEqual({ kind: "page", ref: "/systems/auditorium?tab=health" });
    expect(pageSubject(`/systems/${UUID}`, "", UUID)).toBeUndefined();
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

describe("stamped", () => {
  it("is the subject a row stamps, or nothing for an empty reference or a uuid", () => {
    expect(stamped({ kind: "vendor", ref: "crestron" })).toEqual({ kind: "vendor", ref: "crestron" });
    expect(stamped({ kind: "vendor", ref: "" })).toBeUndefined();
    expect(stamped({ kind: "vendor", ref: UUID })).toBeUndefined();
    expect(stamped(undefined)).toBeUndefined();
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

  it("replaces a page handed over earlier and not yet sent with the page the person is on", () => {
    handOverPage("/alarms", "?state=active");
    handOverPage("/systems/auditorium", "");
    expect(tray()).toEqual([{ kind: "page", ref: "/systems/auditorium" }]);
  });

  it("puts the page in the tray and opens omnibot, and does nothing for anyone without assistant:use", () => {
    const here = () => ({ pathname: "/alarms", search: "?state=active" });
    const press = (may: boolean) => {
      const open = vi.fn();
      const e = new KeyboardEvent("keydown", { key: "j", ctrlKey: true, cancelable: true });
      handOverer(() => may, here, open)(e);
      return { open, prevented: e.defaultPrevented };
    };
    const denied = press(false);
    expect(denied.open).not.toHaveBeenCalled();
    expect(denied.prevented).toBe(false);
    expect(tray()).toEqual([]);
    const allowed = press(true);
    expect(allowed.open).toHaveBeenCalledTimes(1);
    expect(allowed.prevented).toBe(true);
    expect(tray()).toEqual([{ kind: "page", ref: "/alarms?state=active" }]);
  });

  it("hands over a detail page by the name the page shows", () => {
    namePage("hq.lobby.display-1");
    handOverPage(`/components/${UUID}`, "");
    namePage(undefined);
    expect(tray()).toEqual([{ kind: "page", ref: "/components/hq.lobby.display-1" }]);
  });
});

describe("alarmSubject", () => {
  it("is the component's name and the rule that raised it, else when it was raised", () => {
    expect(alarmSubject({ component: "hq.lobby.display-1", raised_by_rule: "offline", raised_at: "2026-11-18T10:00:00Z" })).toEqual({ kind: "alarm", ref: "hq.lobby.display-1/offline" });
    expect(alarmSubject({ component: "hq.lobby.display-1", raised_at: "2026-11-18T10:00:00Z" })).toEqual({ kind: "alarm", ref: "hq.lobby.display-1@2026-11-18T10:00:00Z" });
  });
});

describe("gathered", () => {
  const list = () => {
    const tbody = document.createElement("tbody");
    for (const name of ["a", "b", "c", "d"]) {
      const tr = document.createElement("tr");
      tr.dataset.subjectKind = "vendor";
      tr.dataset.subjectRef = name;
      const td = document.createElement("td");
      td.textContent = name;
      const btn = document.createElement("button");
      td.append(btn);
      tr.append(td);
      tbody.append(tr);
    }
    document.body.append(tbody);
    return [...tbody.children].map((tr) => tr.firstElementChild as HTMLElement);
  };

  it("is the stamped element a click lands in", () => {
    const cells = list();
    const g = gathered(cells[1], undefined, false);
    expect(g?.subjects).toEqual([{ kind: "vendor", ref: "b" }]);
    expect(g?.stamp).toBe(cells[1].parentElement);
  });

  it("is every stamped sibling from the last one gathered, either way, for a range", () => {
    const cells = list();
    const anchor = cells[3].parentElement!;
    expect(gathered(cells[1], anchor, true)?.subjects.map((s) => s.ref)).toEqual(["b", "c", "d"]);
    expect(gathered(cells[3], cells[0].parentElement!, true)?.subjects.map((s) => s.ref)).toEqual(["a", "b", "c", "d"]);
  });

  it("is the one element for a range with no anchor in the same list", () => {
    const cells = list();
    const other = list();
    expect(gathered(cells[2], other[0].parentElement!, true)?.subjects.map((s) => s.ref)).toEqual(["c"]);
  });

  it("is nothing off a stamp, or on a control inside one, which keeps its own meaning", () => {
    const cells = list();
    expect(gathered(document.body, undefined, false)).toBeUndefined();
    expect(gathered(cells[0].querySelector("button")!, undefined, false)).toBeUndefined();
  });
});

describe("gatherer", () => {
  beforeEach(() => clearTray());

  const rows = () => {
    const ul = document.createElement("ul");
    for (const name of ["hq.a", "hq.b", "hq.c"]) {
      const li = document.createElement("li");
      li.dataset.subjectKind = "component";
      li.dataset.subjectRef = name;
      li.textContent = name;
      ul.append(li);
    }
    document.body.append(ul);
    return [...ul.children] as HTMLElement[];
  };
  const listen = (may = true) => {
    const open = vi.fn();
    const onRow = vi.fn();
    const g = gatherer(() => may, open);
    window.addEventListener("click", g, true);
    document.addEventListener("click", onRow);
    return { open, onRow, done: () => (window.removeEventListener("click", g, true), document.removeEventListener("click", onRow)) };
  };
  const click = (el: Element, mods: MouseEventInit = {}) => el.dispatchEvent(new MouseEvent("click", { bubbles: true, cancelable: true, ...mods }));

  it("gathers an alt-clicked row, then a shift-alt range, opens omnibot, and keeps the row's own click from running", () => {
    const [a, , c] = rows();
    const { open, onRow, done } = listen();
    click(a, { altKey: true });
    click(c, { altKey: true, shiftKey: true });
    done();
    expect(tray().map((s) => s.ref)).toEqual(["hq.a", "hq.b", "hq.c"]);
    expect(open).toHaveBeenCalledTimes(2);
    expect(onRow).not.toHaveBeenCalled();
  });

  it("leaves a plain, a ⌘ or Ctrl click, and anyone who may not ask omnibot, alone", () => {
    const [a] = rows();
    let l = listen();
    click(a);
    click(a, { altKey: true, metaKey: true });
    click(a, { altKey: true, ctrlKey: true });
    l.done();
    expect(l.onRow).toHaveBeenCalledTimes(3);
    l = listen(false);
    click(a, { altKey: true });
    l.done();
    expect(l.onRow).toHaveBeenCalledTimes(1);
    expect(tray()).toEqual([]);
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
