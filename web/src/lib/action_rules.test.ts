import { uuidFor } from "./testids";
import { describe, it, expect, vi, afterEach } from "vitest";
import { api } from "../api/client";
import {
  RECENT_WINDOW,
  actionRuleDraftErrors,
  actionRuleDraftFrom,
  createActionRuleBody,
  emptyActionRuleDraft,
  listRecentNotifications,
  ofRule,
  updateActionRuleBody,
  usedBy,
  type ActionRule,
  type Notification,
} from "./action_rules";

const at = "2026-10-29T00:00:00Z";
const rule: ActionRule = {
  id: uuidFor("r1"), name: "page-ops", label: "Page ops", on: ["raised", "cleared"], when: "severity_rank >= 40", targets: ["ops-chat"],
  official: false, opt_out_allowed: false, enabled: true, created_at: at, updated_at: at,
};
const note = (over: Partial<Notification>): Notification => ({
  id: uuidFor("n"), alarm: "a", attempts: 1, channel: "webhook", component: "display-1", component_id: "c", created_at: at,
  notification_type: "alarm-raised", status: "sent", subject: "s", target: "ops-chat", text: "t", transition: "raised", action_rule: "page-ops",
  ...over,
});

describe("actionRuleDraftErrors", () => {
  it("passes a named draft with an edge and a target", () => {
    expect(actionRuleDraftErrors({ ...emptyActionRuleDraft(), name: "page-ops", targets: ["ops-chat"] })).toEqual({});
  });
  it("asks for a name, an edge and a target", () => {
    const e = actionRuleDraftErrors({ ...emptyActionRuleDraft(), on: [] });
    expect(Object.keys(e).sort()).toEqual(["name", "on", "targets"]);
  });
  it("does not judge the fixed name on edit", () => {
    expect(actionRuleDraftErrors({ ...emptyActionRuleDraft(), targets: ["x"] }, true)).toEqual({});
  });
});

describe("the bodies", () => {
  it("creates with only the optional fields that were filled", () => {
    expect(createActionRuleBody({ ...emptyActionRuleDraft(), name: " page-ops ", targets: ["ops-chat"] })).toEqual({
      name: "page-ops", on: ["raised"], targets: ["ops-chat"], enabled: true, opt_out_allowed: false,
    });
    expect(createActionRuleBody({ ...emptyActionRuleDraft(), name: "p", targets: ["t"], when: " severity_rank >= 40 ", notificationType: "page-oncall" })).toMatchObject({
      when: "severity_rank >= 40", notification_type: "page-oncall",
    });
  });
  it("patches nothing when nothing changed", () => {
    expect(updateActionRuleBody(actionRuleDraftFrom(rule), rule)).toEqual({});
  });
  it("sends an emptied when as empty, so the rule always holds", () => {
    expect(updateActionRuleBody({ ...actionRuleDraftFrom(rule), when: "  " }, rule)).toEqual({ when: "" });
  });
  it("sends the enabled switch and a changed target list", () => {
    expect(updateActionRuleBody({ ...actionRuleDraftFrom(rule), enabled: false, targets: ["ops-chat", "on-call"] }, rule)).toEqual({ enabled: false, targets: ["ops-chat", "on-call"] });
  });
});

describe("ofRule", () => {
  it("picks the rule's rows, newest first as served, capped", () => {
    const ns = [note({ id: "1" }), note({ id: "2", action_rule: "other" }), note({ id: "3" })];
    expect(ofRule(ns, "page-ops").map((n) => n.id)).toEqual(["1", "3"]);
    expect(ofRule(ns, "page-ops", 1).map((n) => n.id)).toEqual(["1"]);
  });
  it("never counts a personal subscription's row that shares the name (it travels through no target)", () => {
    expect(ofRule([note({ id: "p", target: "", channel: "inbox" })], "page-ops")).toEqual([]);
  });
});

describe("usedBy", () => {
  it("names the rules sending to a target", () => {
    expect(usedBy([rule, { ...rule, name: "quiet", targets: ["mailbox"] }], "ops-chat").map((r) => r.name)).toEqual(["page-ops"]);
  });
});

describe("listRecentNotifications", () => {
  afterEach(() => vi.restoreAllMocks());
  it("reads the route's whole window, narrowed to a target when given", async () => {
    const spy = vi.spyOn(api, "GET").mockResolvedValue({ data: { notifications: [note({})] }, error: undefined } as never);
    expect(await listRecentNotifications({ target: "ops-chat" })).toHaveLength(1);
    expect(spy).toHaveBeenCalledWith("/notifications", { params: { query: { limit: RECENT_WINDOW, target: "ops-chat" } } });
    await listRecentNotifications();
    expect(spy).toHaveBeenLastCalledWith("/notifications", { params: { query: { limit: RECENT_WINDOW, target: undefined } } });
  });
});
