import { useQuery, useQueryClient } from "@tanstack/solid-query";
import { api } from "../api/client";
import type { components } from "../api/schema.gen";
import { can, useMe } from "./auth";
import { entityNameError } from "./names";

// The personal alarm subscription data layer (#1355, epic #1341): a person's
// own action rules, each telling them alone, on their own channels, about the
// alarms they can read. Self-scoped by the server (another person's rule is a
// non-disclosing 404) and gated by notification:subscribe, which the viewer
// floor holds.

export type Subscription = components["schemas"]["PersonalRuleBody"];
export type RuleMatch = components["schemas"]["RuleMatchBody"];

export const TRANSITIONS = ["raised", "cleared"] as const;
export type Transition = (typeof TRANSITIONS)[number];
export const CHANNELS = ["inbox", "email"] as const;
export type Channel = (typeof CHANNELS)[number];

// The pills a transition and a neutral fact wear, on every surface that lists
// action rules or what they sent (the subscriptions page, the platform action
// rules and their recent notifications).
export const PILL_NEUTRAL = "badge badge-sm badge-ghost";
export const TRANSITION_PILL: Record<string, string> = {
  raised: "badge badge-sm badge-soft badge-warning",
  cleared: "badge badge-sm badge-healthy",
};

export const SUBSCRIPTIONS_KEY = ["auth", "me", "action-rules"] as const;

// A person may own at most this many (the server refuses the next with a 409).
export const SUBSCRIPTION_CAP = 50;

// The draft the drawer edits. Name is create-only: the route addresses a rule
// by it and offers no rename.
export type SubscriptionDraft = {
  name: string;
  label: string;
  on: Transition[];
  when: string;
  channels: Channel[];
  enabled: boolean;
};

export function emptyDraft(): SubscriptionDraft {
  return { name: "", label: "", on: ["raised"], when: "", channels: ["inbox"], enabled: true };
}

export function draftFrom(s: Subscription): SubscriptionDraft {
  return {
    name: s.name,
    label: s.label ?? "",
    on: (s.on ?? []).filter((t): t is Transition => (TRANSITIONS as readonly string[]).includes(t)),
    when: s.when ?? "",
    channels: (s.channels ?? []).filter((c): c is Channel => (CHANNELS as readonly string[]).includes(c)),
    enabled: s.enabled,
  };
}

export type DraftErrors = Partial<Record<"name" | "on" | "channels", string>>;

// draftErrors is the form's rule, judged before sending (ADR-0113: a rule is
// TypeScript, never a native attribute). The when is the server's to judge
// (the expression field checks it as it is typed); an empty one always holds.
export function draftErrors(d: SubscriptionDraft): DraftErrors {
  const out: DraftErrors = {};
  const name = d.name.trim();
  if (!name) out.name = "Name it, so you can tell your subscriptions apart.";
  else {
    const bad = entityNameError(name);
    if (bad) out.name = bad;
  }
  if (d.on.length === 0) out.on = "Pick at least one: raised, cleared, or both.";
  if (d.channels.length === 0) out.channels = "Pick at least one way to be told.";
  return out;
}

// toggled adds a member to a set-like list, or takes it out, keeping the
// canonical order so a draft reads the same however it was clicked.
export function toggled<T extends string>(list: T[], v: T, order: readonly T[]): T[] {
  const has = list.includes(v);
  return order.filter((x) => (x === v ? !has : list.includes(x)));
}

// whenError reads a refusal of the when out of a 422: the API places it on
// body.when with its line and column. Anything else is not the when's fault.
export function whenError(e: unknown): string | null {
  const o = e as { detail?: string; errors?: { location?: string; message?: string; value?: { line?: number; column?: number } }[] } | null | undefined;
  const hit = (o?.errors ?? []).find((x) => x?.location === "body.when");
  if (!hit) return null;
  const at = hit.value?.line ? `Line ${hit.value.line}, column ${hit.value.column ?? 0}: ` : "";
  return `${at}${hit.message ?? o?.detail ?? "the when does not compile"}`;
}

// ---- reads and writes --------------------------------------------------------

export async function listSubscriptions(): Promise<Subscription[]> {
  const { data, error } = await api.GET("/auth/me/action-rules");
  if (error) throw error;
  return (data?.action_rules ?? []) as Subscription[];
}

export function useSubscriptions() {
  const me = useMe();
  return useQuery(() => ({
    queryKey: SUBSCRIPTIONS_KEY,
    queryFn: listSubscriptions,
    enabled: can(me.data, "notification", "subscribe"),
  }));
}

export async function createSubscription(d: SubscriptionDraft): Promise<Subscription> {
  const { data, error } = await api.POST("/auth/me/action-rules", {
    body: {
      name: d.name.trim(),
      label: d.label.trim() || undefined,
      on: d.on,
      when: d.when.trim() || undefined,
      channels: d.channels,
      enabled: d.enabled,
    },
  });
  if (error) throw error;
  return data as Subscription;
}

// updateSubscription sends every editable field, so the drawer's draft is what
// the rule becomes (an empty when returns it to always holding).
export async function updateSubscription(name: string, d: SubscriptionDraft): Promise<Subscription> {
  const { data, error } = await api.PATCH("/auth/me/action-rules/{name}", {
    params: { path: { name } },
    body: { label: d.label.trim(), on: d.on, when: d.when.trim(), channels: d.channels, enabled: d.enabled },
  });
  if (error) throw error;
  return data as Subscription;
}

export async function setSubscriptionEnabled(name: string, enabled: boolean): Promise<Subscription> {
  const { data, error } = await api.PATCH("/auth/me/action-rules/{name}", { params: { path: { name } }, body: { enabled } });
  if (error) throw error;
  return data as Subscription;
}

export async function deleteSubscription(name: string): Promise<void> {
  const { error } = await api.DELETE("/auth/me/action-rules/{name}", { params: { path: { name } } });
  if (error) throw error;
}

// testSubscription judges a draft's on and when against the caller's most
// recent alarms on components they can read (the keyless draft route, so a
// subscription not yet saved is tested the same as one being edited). It
// writes and sends nothing.
export async function testSubscription(draft: { on: Transition[]; when: string }, limit = 20): Promise<RuleMatch[]> {
  const { data, error } = await api.POST("/auth/me/action-rules:testDraft", {
    body: { on: draft.on, when: draft.when.trim(), limit },
  });
  if (error) throw error;
  return (data?.matches ?? []) as RuleMatch[];
}

export function useSubscriptionWrites() {
  const qc = useQueryClient();
  const refresh = () => qc.invalidateQueries({ queryKey: SUBSCRIPTIONS_KEY });
  return {
    create: async (d: SubscriptionDraft) => {
      const s = await createSubscription(d);
      await refresh();
      return s;
    },
    update: async (name: string, d: SubscriptionDraft) => {
      const s = await updateSubscription(name, d);
      await refresh();
      return s;
    },
    setEnabled: async (name: string, enabled: boolean) => {
      const s = await setSubscriptionEnabled(name, enabled);
      await refresh();
      return s;
    },
    remove: async (name: string) => {
      await deleteSubscription(name);
      await refresh();
    },
  };
}

// matchSummary counts a test's verdicts: how many transitions would have told
// you, how many would not, and how many the when could not judge.
export function matchSummary(ms: RuleMatch[]): { matched: number; missed: number; errored: number } {
  let matched = 0;
  let missed = 0;
  let errored = 0;
  for (const m of ms) {
    if (m.error) errored++;
    else if (m.matched) matched++;
    else missed++;
  }
  return { matched, missed, errored };
}
