import { uuidFor } from "./testids";
import { describe, it, expect, vi, afterEach } from "vitest";
import { api } from "../api/client";
import {
  createTargetBody,
  destination,
  emptyTargetDraft,
  parseAddresses,
  revealTargetUrl,
  targetDraftErrors,
  targetDraftFrom,
  testTarget,
  testVerdict,
  testable,
  updateTargetBody,
  type NotificationTarget,
} from "./notification_targets";

const at = "2026-10-29T00:00:00Z";
const hook: NotificationTarget = {
  id: uuidFor("t1"), name: "ops-chat", label: "Ops chat", kind: "webhook", url: "https://chat.example.com/…", auth: "hmac",
  secret: "ops-chat-signing", official: false, enabled: true, created_at: at, updated_at: at,
};

describe("targetDraftErrors", () => {
  it("passes a named webhook with a URL", () => {
    expect(targetDraftErrors({ ...emptyTargetDraft(), name: "ops-chat", url: "https://chat.example.com/hooks/ops" })).toEqual({});
  });
  it("asks for a name and judges it by the entity name rule", () => {
    expect(targetDraftErrors({ ...emptyTargetDraft(), url: "https://x.example" }).name).toMatch(/Name it/);
    expect(targetDraftErrors({ ...emptyTargetDraft(), name: "Ops Chat", url: "https://x.example" }).name).toMatch(/Lowercase/);
  });
  it("needs a webhook URL on create, refuses one that is not http(s), and keeps the stored one when blank on edit", () => {
    expect(targetDraftErrors({ ...emptyTargetDraft(), name: "x" }).url).toMatch(/needs the URL/);
    expect(targetDraftErrors({ ...emptyTargetDraft(), name: "x", url: "chat.example.com" }).url).toMatch(/http or https/);
    expect(targetDraftErrors({ ...emptyTargetDraft(), name: "x" }, true)).toEqual({});
  });
  it("needs at least one well-formed address on an email target", () => {
    const d = { ...emptyTargetDraft(), name: "box", kind: "email" as const };
    expect(targetDraftErrors(d).addresses).toMatch(/At least one/);
    expect(targetDraftErrors({ ...d, addresses: "ops@example.com, nope" }).addresses).toMatch(/"nope"/);
    expect(targetDraftErrors({ ...d, addresses: "ops@example.com" })).toEqual({});
  });
  it("needs a group or person on create, and a channel always, for people targets", () => {
    const g = { ...emptyTargetDraft(), name: "on-call", kind: "user_group" as const, channels: [] };
    expect(targetDraftErrors(g)).toEqual({ userGroup: expect.any(String), channels: expect.any(String) });
    expect(targetDraftErrors({ ...g, userGroup: "on-call", channels: ["inbox"] })).toEqual({});
    const p = { ...emptyTargetDraft(), name: "ada", kind: "user" as const };
    expect(targetDraftErrors(p).user).toBeTruthy();
    expect(targetDraftErrors(p, true)).toEqual({});
  });
});

describe("createTargetBody", () => {
  it("sends only what the kind reads", () => {
    const d = { ...emptyTargetDraft(), name: "on-call", label: " On call ", kind: "user_group" as const, url: "https://left.over", userGroup: "on-call", channels: ["inbox", "email"] as ("inbox" | "email")[] };
    expect(createTargetBody(d)).toEqual({ name: "on-call", label: "On call", kind: "user_group", enabled: true, user_group: "on-call", channels: ["inbox", "email"] });
  });
  it("binds a webhook's secret only when one is picked", () => {
    expect(createTargetBody({ ...emptyTargetDraft(), name: "h", url: "https://x.example/a" })).toEqual({ name: "h", kind: "webhook", enabled: true, url: "https://x.example/a" });
    expect(createTargetBody({ ...emptyTargetDraft(), name: "h", url: "https://x.example/a", secret: "k" }).secret).toBe("k");
  });
  it("splits typed addresses", () => {
    expect(createTargetBody({ ...emptyTargetDraft(), name: "b", kind: "email", addresses: "a@x.io,\n b@x.io;" }).addresses).toEqual(["a@x.io", "b@x.io"]);
  });
});

describe("updateTargetBody", () => {
  it("is empty when nothing changed, the masked URL never sent back", () => {
    expect(updateTargetBody(targetDraftFrom(hook), hook)).toEqual({});
  });
  it("sends a new URL, an unbound secret and the enabled flag", () => {
    const d = { ...targetDraftFrom(hook), url: "https://chat.example.com/hooks/new", secret: "", enabled: false };
    expect(updateTargetBody(d, hook)).toEqual({ url: "https://chat.example.com/hooks/new", secret: "", enabled: false });
  });
  it("sends a people target's channels only when they changed", () => {
    const g: NotificationTarget = { ...hook, kind: "user_group", url: undefined, secret: undefined, user_group: "on-call", channels: ["inbox"] };
    expect(updateTargetBody(targetDraftFrom(g), g)).toEqual({});
    expect(updateTargetBody({ ...targetDraftFrom(g), channels: ["inbox", "email"] }, g)).toEqual({ channels: ["inbox", "email"] });
  });
  it("sends a role target's channels only when they changed (#1987)", () => {
    const r: NotificationTarget = { ...hook, kind: "role", url: undefined, secret: undefined, role: "admin", channels: ["inbox", "email"] };
    expect(updateTargetBody(targetDraftFrom(r), r)).toEqual({});
    expect(updateTargetBody({ ...targetDraftFrom(r), channels: ["inbox"] }, r)).toEqual({ channels: ["inbox"] });
  });
});

describe("the small readers", () => {
  it("tests only a webhook or a mailbox", () => {
    expect(testable("webhook")).toBe(true);
    expect(testable("email")).toBe(true);
    expect(testable("user_group")).toBe(false);
    expect(testable("user")).toBe(false);
    expect(testable("role")).toBe(false);
  });
  it("says where each kind sends", () => {
    expect(destination(hook)).toBe("https://chat.example.com/…");
    expect(destination({ ...hook, kind: "email", addresses: ["a@x.io", "b@x.io"] })).toBe("a@x.io, b@x.io");
    expect(destination({ ...hook, kind: "user", user: "ada" })).toBe("ada");
    expect(destination({ ...hook, kind: "role", role: "admin" })).toBe("admin");
  });
  it("parses addresses, dropping empties", () => {
    expect(parseAddresses(" a@x.io, ,b@x.io ")).toEqual(["a@x.io", "b@x.io"]);
  });
  it("words a test's answer", () => {
    expect(testVerdict({ ok: true, status: 200 })).toBe("Delivered: the receiver answered 200.");
    expect(testVerdict({ ok: false, status: 401, reason: "the receiver answered 401" })).toBe("Not delivered: the receiver answered 401.");
    expect(testVerdict({ ok: false, status: 0 })).toBe("Not delivered: no answer came.");
  });
});

describe("the reveal and test calls", () => {
  afterEach(() => vi.restoreAllMocks());
  it("reveals the URL whole through :reveal", async () => {
    const spy = vi.spyOn(api, "POST").mockResolvedValue({ data: { name: "ops-chat", url: "https://chat.example.com/hooks/ops?k=1" }, error: undefined } as never);
    expect(await revealTargetUrl("ops-chat")).toBe("https://chat.example.com/hooks/ops?k=1");
    expect(spy).toHaveBeenCalledWith("/notification-targets/{name}:reveal", { params: { path: { name: "ops-chat" } } });
  });
  it("throws the refusal of a test", async () => {
    vi.spyOn(api, "POST").mockResolvedValue({ data: undefined, error: { status: 422, detail: "people are told only about alarms" } } as never);
    await expect(testTarget("on-call")).rejects.toMatchObject({ status: 422 });
  });
});
