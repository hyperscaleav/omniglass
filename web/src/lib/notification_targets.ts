import { api } from "../api/client";
import type { components } from "../api/schema.gen";
import { entityNameError } from "./names";
import { EMAIL_RE } from "./validate";

// The notification targets data layer (#1346, the console half of #618 and
// #1341): thin typed wrappers over /notification-targets plus the pure rules
// the console's form judges a draft by. A target says WHERE a message about an
// alarm goes: a webhook URL (masked on every read, revealed whole under an
// audit row of its own), a shared mailbox, a group or one person. Every route
// is admin-only (notification_target:*, all-scope): a target is an egress
// destination holding a credential.

export type NotificationTarget = components["schemas"]["NotificationTargetBody"];
export type TargetKind = NotificationTarget["kind"];
export type TargetTest = components["schemas"]["TestNotificationTargetOutputBody"];

export const NOTIFICATION_TARGETS_KEY = ["notification_targets"] as const;

export const TARGET_KINDS: readonly TargetKind[] = ["webhook", "email", "principal_group", "principal"];
export const PEOPLE_CHANNELS = ["inbox", "email"] as const;
export type PeopleChannel = (typeof PEOPLE_CHANNELS)[number];

// The words a kind reads as on the page: an operator picks "Group", the wire
// says principal_group.
export const KIND_WORDS: Record<TargetKind, string> = {
  webhook: "Webhook",
  email: "Email",
  principal_group: "Group",
  principal: "Person",
};

// testable says whether :test can reach a kind: a webhook or a mailbox takes a
// synthetic message, while people are told only about alarms (the route
// answers a group or person target with a 422).
export function testable(kind: TargetKind): boolean {
  return kind === "webhook" || kind === "email";
}

// destination is the one line a list row says about where a target sends:
// the masked URL, the mailbox addresses, the group or the person (by name,
// which a caller without the directory read is not shown).
export function destination(t: NotificationTarget): string {
  switch (t.kind) {
    case "webhook":
      return t.url ?? "";
    case "email":
      return (t.addresses ?? []).join(", ");
    case "principal_group":
      return t.principal_group ?? "";
    case "principal":
      return t.principal ?? "";
  }
}

// ---- the draft the forms edit ------------------------------------------------

export type TargetDraft = {
  name: string;
  label: string;
  kind: TargetKind;
  // A webhook's URL. Editing, blank keeps the stored one: the stored URL is
  // never read back whole into a form, so an edit only ever replaces it.
  url: string;
  // The bound platform secret's name; empty sends unsigned.
  secret: string;
  // An email target's addresses, as typed (commas, spaces or new lines).
  addresses: string;
  principalGroup: string;
  principal: string;
  channels: PeopleChannel[];
  enabled: boolean;
};

export function emptyTargetDraft(): TargetDraft {
  return { name: "", label: "", kind: "webhook", url: "", secret: "", addresses: "", principalGroup: "", principal: "", channels: ["inbox"], enabled: true };
}

export function targetDraftFrom(t: NotificationTarget): TargetDraft {
  return {
    name: t.name,
    label: t.label ?? "",
    kind: t.kind,
    url: "",
    secret: t.secret ?? "",
    addresses: (t.addresses ?? []).join(", "),
    principalGroup: t.principal_group ?? "",
    principal: t.principal ?? "",
    channels: (t.channels ?? []).filter((c): c is PeopleChannel => (PEOPLE_CHANNELS as readonly string[]).includes(c)),
    enabled: t.enabled,
  };
}

// parseAddresses splits what was typed into addresses, dropping the empties a
// trailing comma leaves.
export function parseAddresses(s: string): string[] {
  return s.split(/[\s,;]+/).map((a) => a.trim()).filter(Boolean);
}

export type TargetDraftErrors = Partial<Record<"name" | "url" | "addresses" | "principalGroup" | "principal" | "channels", string>>;

// targetDraftErrors is the form's rule (ADR-0113: TypeScript, never a native
// attribute). Whether a URL is one the egress contract admits is the server's
// call (it reads the allow list in force); the form only refuses what is not a
// URL at all. `editing` relaxes the URL: blank keeps the stored one.
export function targetDraftErrors(d: TargetDraft, editing = false): TargetDraftErrors {
  const out: TargetDraftErrors = {};
  if (!editing) {
    const name = d.name.trim();
    if (!name) out.name = "Name it: rules send to a target by its name.";
    else {
      const bad = entityNameError(name);
      if (bad) out.name = bad;
    }
  }
  switch (d.kind) {
    case "webhook": {
      const url = d.url.trim();
      if (!url && !editing) out.url = "A webhook needs the URL it posts to.";
      else if (url && !/^https?:\/\/[^\s/]+/i.test(url)) out.url = "An http or https URL, e.g. https://chat.example.com/hooks/ops.";
      break;
    }
    case "email": {
      const as = parseAddresses(d.addresses);
      if (as.length === 0) out.addresses = "At least one address.";
      else {
        const bad = as.find((a) => !EMAIL_RE.test(a));
        if (bad) out.addresses = `"${bad}" is not an email address.`;
      }
      break;
    }
    case "principal_group":
      if (!editing && !d.principalGroup) out.principalGroup = "Pick the group it tells.";
      if (d.channels.length === 0) out.channels = "Pick at least one way to tell them.";
      break;
    case "principal":
      if (!editing && !d.principal) out.principal = "Pick the person it tells.";
      if (d.channels.length === 0) out.channels = "Pick at least one way to tell them.";
      break;
  }
  return out;
}

export type CreateTarget = components["schemas"]["CreateNotificationTargetInputBody"];
export type UpdateTarget = components["schemas"]["UpdateNotificationTargetInputBody"];

// createTargetBody sends only the fields the kind reads, so a webhook URL typed
// before switching to Group never travels.
export function createTargetBody(d: TargetDraft): CreateTarget {
  const body: CreateTarget = { name: d.name.trim(), kind: d.kind, enabled: d.enabled };
  if (d.label.trim()) body.label = d.label.trim();
  switch (d.kind) {
    case "webhook":
      body.url = d.url.trim();
      if (d.secret) body.secret = d.secret;
      break;
    case "email":
      body.addresses = parseAddresses(d.addresses);
      break;
    case "principal_group":
      body.principal_group = d.principalGroup;
      body.channels = d.channels;
      break;
    case "principal":
      body.principal = d.principal;
      body.channels = d.channels;
      break;
  }
  return body;
}

// updateTargetBody is the PATCH: only what changed, since an absent field is
// left alone. A blank URL keeps the stored one; an emptied secret unbinds it
// (the route's own rule). Kind, name and who a people target names are fixed
// at create.
export function updateTargetBody(d: TargetDraft, was: NotificationTarget): UpdateTarget {
  const body: UpdateTarget = {};
  if (d.label.trim() !== (was.label ?? "")) body.label = d.label.trim();
  if (d.enabled !== was.enabled) body.enabled = d.enabled;
  switch (was.kind) {
    case "webhook":
      if (d.url.trim()) body.url = d.url.trim();
      if (d.secret !== (was.secret ?? "")) body.secret = d.secret;
      break;
    case "email": {
      const as = parseAddresses(d.addresses);
      if (as.join(",") !== (was.addresses ?? []).join(",")) body.addresses = as;
      break;
    }
    case "principal_group":
    case "principal":
      if (d.channels.join(",") !== (was.channels ?? []).join(",")) body.channels = d.channels;
      break;
  }
  return body;
}

// ---- reads and writes --------------------------------------------------------

export async function listNotificationTargets(): Promise<NotificationTarget[]> {
  const { data, error } = await api.GET("/notification-targets");
  if (error) throw error;
  return (data?.notification_targets ?? []) as NotificationTarget[];
}

export async function createNotificationTarget(body: CreateTarget): Promise<NotificationTarget> {
  const { data, error } = await api.POST("/notification-targets", { body });
  if (error) throw error;
  return data as NotificationTarget;
}

export async function updateNotificationTarget(name: string, body: UpdateTarget): Promise<NotificationTarget> {
  const { data, error } = await api.PATCH("/notification-targets/{name}", { params: { path: { name } }, body });
  if (error) throw error;
  return data as NotificationTarget;
}

export async function deleteNotificationTarget(name: string): Promise<void> {
  const { error } = await api.DELETE("/notification-targets/{name}", { params: { path: { name } } });
  if (error) throw error;
}

// revealTargetUrl returns the URL whole. The server writes an audit row for
// every call, so the console asks only when the operator clicks Reveal.
export async function revealTargetUrl(name: string): Promise<string> {
  const { data, error } = await api.POST("/notification-targets/{name}:reveal", { params: { path: { name } } });
  if (error) throw error;
  return data?.url ?? "";
}

// testTarget sends one synthetic message and answers what the receiver said.
export async function testTarget(name: string): Promise<TargetTest> {
  const { data, error } = await api.POST("/notification-targets/{name}:test", { params: { path: { name } } });
  if (error) throw error;
  return data as TargetTest;
}

// testVerdict words a test's answer: the receiver's status when it answered,
// and the reason when it did not take the message.
export function testVerdict(r: TargetTest): string {
  if (r.ok) return r.status ? `Delivered: the receiver answered ${r.status}.` : "Delivered.";
  const why = r.reason || (r.status ? `the receiver answered ${r.status}` : "no answer came");
  return `Not delivered: ${why}.`;
}
