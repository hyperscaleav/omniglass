import { api } from "../api/client";
import type { components } from "../api/schema.gen";
import { entityNameError } from "./names";
import { TRANSITIONS, type Transition } from "./subscriptions";

// The platform action rules data layer (#1346, the console half of #618): thin
// typed wrappers over /action-rules plus the pure rules the console's form
// judges a draft by. An action rule says WHEN a message is sent: on an alarm's
// raise, clear or both, when its predicate (the "action" expression site) holds
// over the alarm facts, it queues one notification of its type per target. It
// is a rule, so it rides rule:* (reads registry-shaped, writes all-scope).
// A person's own subscriptions are /auth/me/action-rules (lib/subscriptions),
// never listed here.

export type ActionRule = components["schemas"]["ActionRuleBody"];
export type Notification = components["schemas"]["NotificationBody"];

export const ACTION_RULES_KEY = ["action_rules"] as const;

export type ActionRuleDraft = {
  name: string;
  label: string;
  description: string;
  on: Transition[];
  when: string;
  targets: string[];
  // The notification type's name; empty sends the official one per transition.
  notificationType: string;
  optOutAllowed: boolean;
  enabled: boolean;
};

export function emptyActionRuleDraft(): ActionRuleDraft {
  return { name: "", label: "", description: "", on: ["raised"], when: "", targets: [], notificationType: "", optOutAllowed: false, enabled: true };
}

export function actionRuleDraftFrom(r: ActionRule): ActionRuleDraft {
  return {
    name: r.name,
    label: r.label ?? "",
    description: r.description ?? "",
    on: (r.on ?? []).filter((t): t is Transition => (TRANSITIONS as readonly string[]).includes(t)),
    when: r.when ?? "",
    targets: [...(r.targets ?? [])],
    notificationType: r.notification_type ?? "",
    optOutAllowed: r.opt_out_allowed,
    enabled: r.enabled,
  };
}

export type ActionRuleDraftErrors = Partial<Record<"name" | "on" | "targets", string>>;

// actionRuleDraftErrors is the form's rule (ADR-0113). The when is the
// server's to judge: the expression field checks it as it is typed.
export function actionRuleDraftErrors(d: ActionRuleDraft, editing = false): ActionRuleDraftErrors {
  const out: ActionRuleDraftErrors = {};
  if (!editing) {
    const name = d.name.trim();
    if (!name) out.name = "Name it: the API and CLI address a rule by its name.";
    else {
      const bad = entityNameError(name);
      if (bad) out.name = bad;
    }
  }
  if (d.on.length === 0) out.on = "Pick at least one: raised, cleared, or both.";
  if (d.targets.length === 0) out.targets = "Pick at least one target to send to.";
  return out;
}

export type CreateActionRule = components["schemas"]["CreateActionRuleInputBody"];
export type UpdateActionRule = components["schemas"]["UpdateActionRuleInputBody"];

export function createActionRuleBody(d: ActionRuleDraft): CreateActionRule {
  const body: CreateActionRule = { name: d.name.trim(), on: d.on, targets: d.targets, enabled: d.enabled, opt_out_allowed: d.optOutAllowed };
  if (d.label.trim()) body.label = d.label.trim();
  if (d.description.trim()) body.description = d.description.trim();
  if (d.when.trim()) body.when = d.when.trim();
  if (d.notificationType) body.notification_type = d.notificationType;
  return body;
}

// updateActionRuleBody is the PATCH: only what changed. An emptied when goes
// as "" (the rule then always holds) and an emptied type as "" (back to the
// official one per transition), since absent would leave either unchanged.
export function updateActionRuleBody(d: ActionRuleDraft, was: ActionRule): UpdateActionRule {
  const body: UpdateActionRule = {};
  if (d.label.trim() !== (was.label ?? "")) body.label = d.label.trim();
  if (d.description.trim() !== (was.description ?? "")) body.description = d.description.trim();
  if (d.on.join(",") !== (was.on ?? []).join(",")) body.on = d.on;
  if (d.when.trim() !== (was.when ?? "")) body.when = d.when.trim();
  if (d.targets.join(",") !== (was.targets ?? []).join(",")) body.targets = d.targets;
  if (d.notificationType !== (was.notification_type ?? "")) body.notification_type = d.notificationType;
  if (d.optOutAllowed !== was.opt_out_allowed) body.opt_out_allowed = d.optOutAllowed;
  if (d.enabled !== was.enabled) body.enabled = d.enabled;
  return body;
}

// ---- reads and writes --------------------------------------------------------

export async function listActionRules(): Promise<ActionRule[]> {
  const { data, error } = await api.GET("/action-rules");
  if (error) throw error;
  return (data?.action_rules ?? []) as ActionRule[];
}

export async function createActionRule(body: CreateActionRule): Promise<ActionRule> {
  const { data, error } = await api.POST("/action-rules", { body });
  if (error) throw error;
  return data as ActionRule;
}

export async function updateActionRule(name: string, body: UpdateActionRule): Promise<ActionRule> {
  const { data, error } = await api.PATCH("/action-rules/{name}", { params: { path: { name } }, body });
  if (error) throw error;
  return data as ActionRule;
}

export async function deleteActionRule(name: string): Promise<void> {
  const { error } = await api.DELETE("/action-rules/{name}", { params: { path: { name } } });
  if (error) throw error;
}

// ---- recent notifications ----------------------------------------------------

// The window a rule's recent notifications are read from: the newest this many
// the caller may see, the route's own ceiling. GET /notifications filters by
// alarm, target and status but not by rule, so a rule's are picked out of the
// window here (a rule quieter than the window shows none of its older ones).
export const RECENT_WINDOW = 500;
// How many a blade shows.
export const RECENT_SHOWN = 10;

export const NOTIFICATIONS_KEY = ["notifications"] as const;
export const recentNotificationsKey = (filter: { target?: string }) =>
  [...NOTIFICATIONS_KEY, "recent", filter.target ?? ""] as const;

export async function listRecentNotifications(filter: { target?: string } = {}): Promise<Notification[]> {
  // A target is narrowed by the server, so only what a panel shows is read;
  // a rule is picked out of the whole window.
  const limit = filter.target ? RECENT_SHOWN : RECENT_WINDOW;
  const { data, error } = await api.GET("/notifications", { params: { query: { limit, target: filter.target || undefined } } });
  if (error) throw error;
  return (data?.notifications ?? []) as Notification[];
}

// ofRule picks a platform rule's notifications out of a list, newest first as
// served. A person's own subscription may share a platform rule's name (names
// are unique per owner), and its rows travel through no target, so a row with
// no target is never a platform rule's.
export function ofRule(ns: Notification[], rule: string, n = RECENT_SHOWN): Notification[] {
  return ns.filter((x) => x.action_rule === rule && !!x.target).slice(0, n);
}

// usedBy names the rules that send to a target: the reason a delete of it is
// refused, said before it is tried.
export function usedBy(rules: ActionRule[], target: string): ActionRule[] {
  return rules.filter((r) => (r.targets ?? []).includes(target));
}
