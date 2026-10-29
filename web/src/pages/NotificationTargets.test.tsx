import { uuidFor } from "../lib/testids";
import { describe, it, expect, afterEach, vi } from "vitest";
import { render, screen, waitFor, fireEvent, within } from "@solidjs/testing-library";
import { Router, Route } from "@solidjs/router";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import NotificationTargets from "./NotificationTargets";
import { ME_KEY, type Me } from "../lib/auth";
import { NOTIFICATION_TARGETS_KEY, type NotificationTarget } from "../lib/notification_targets";
import { ACTION_RULES_KEY, recentNotificationsKey, type ActionRule, type Notification } from "../lib/action_rules";
import { SECRETS_KEY, type Secret } from "../lib/secrets";
import { GROUPS_KEY } from "../lib/groups";

// Notification targets (#1346): a FlatList over /notification-targets, admin
// only. The blade reveals a webhook's URL (one audited call per click), tests
// a webhook or mailbox and shows what the receiver answered, names the rules
// sending here, and lists what was recently sent. The list and its neighbours
// are seeded into the query cache; fetch answers the writes and refuses
// anything else.
const at = "2026-10-29T00:00:00Z";
const admin: Me = { principal: { id: uuidFor("u-root"), kind: "human" }, human: { username: "root" }, permissions: [">"], fleet_permissions: [">"], grants: [{ role: "owner", scope_kind: "all" }] };
// Reads targets, may change nothing: a grant that carries only the read.
const reader: Me = { principal: { id: uuidFor("u-r"), kind: "human" }, human: { username: "auditor" }, permissions: ["notification_target:read"], fleet_permissions: ["notification_target:read"], grants: [] };

const opsChat: NotificationTarget = {
  id: uuidFor("t-1"), name: "ops-chat", label: "Ops chat", kind: "webhook", url: "https://chat.example.com/…", auth: "hmac",
  secret: "ops-chat-signing", enabled: true, created_at: at, updated_at: at,
};
const onCall: NotificationTarget = {
  id: uuidFor("t-2"), name: "on-call-page", label: "On-call page", kind: "principal_group", auth: "none", principal_group: "on-call",
  channels: ["inbox", "email"], enabled: false, created_at: at, updated_at: at,
};
const pageOps: ActionRule = {
  id: uuidFor("r-1"), name: "page-ops", label: "Page ops", on: ["raised"], when: "", targets: ["ops-chat"], official: false,
  opt_out_allowed: false, enabled: true, created_at: at, updated_at: at,
};
const sent: Notification = {
  id: uuidFor("n-1"), alarm: "a-1", attempts: 1, channel: "webhook", component: "display-1", component_id: "c-1", created_at: at,
  notification_type: "alarm-raised", status: "failed", last_error: "the receiver answered 503", subject: "s", target: "ops-chat",
  text: "t", transition: "raised", action_rule: "page-ops",
};
const signing = { id: uuidFor("s-1"), name: "ops-chat-signing", secret_type: "hmac-key", owner_kind: "platform", fields: [] } as Secret;

const json = (body: unknown, status = 200) => new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });

function mount(me: Me = admin) {
  const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
  qc.setQueryData([...ME_KEY], me);
  qc.setQueryData([...NOTIFICATION_TARGETS_KEY], [opsChat, onCall]);
  qc.setQueryData([...ACTION_RULES_KEY], [pageOps]);
  qc.setQueryData([...recentNotificationsKey({ target: "ops-chat" })], [sent]);
  qc.setQueryData([...recentNotificationsKey({ target: "on-call-page" })], []);
  qc.setQueryData([...SECRETS_KEY], [signing]);
  qc.setQueryData([...GROUPS_KEY], [{ id: uuidFor("g-1"), name: "on-call", label: "On call" }]);
  window.history.pushState({}, "", "/notification-targets");
  render(() => (
    <QueryClientProvider client={qc}>
      <Router>
        <Route path="/notification-targets" component={NotificationTargets} />
      </Router>
    </QueryClientProvider>
  ));
  return qc;
}

function stubFetch(handle: (req: Request) => Response | Promise<Response>) {
  return vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => handle(input as Request));
}
const refuse = (r: Request): Response => { throw new Error(`unexpected ${r.method} ${r.url}`); };

async function openBlade(label: string): Promise<HTMLElement> {
  fireEvent.click(screen.getByText(label));
  return await waitFor(() => {
    const el = document.querySelector("aside[data-blade]");
    if (!el) throw new Error("no blade yet");
    return el as HTMLElement;
  });
}

describe("NotificationTargets", () => {
  afterEach(() => {
    vi.restoreAllMocks();
    window.history.pushState({}, "", "/");
  });

  it("lists each target with its kind, where it sends and its status", () => {
    stubFetch(refuse);
    mount();
    expect(screen.getByText("Ops chat")).toBeTruthy();
    expect(screen.getByText("https://chat.example.com/…")).toBeTruthy();
    expect(screen.getByText("Group")).toBeTruthy();
    expect(screen.getByText("enabled")).toBeTruthy();
    expect(screen.getByText("disabled")).toBeTruthy();
  });

  it("offers New target only to a caller who may create one", () => {
    stubFetch(refuse);
    mount(reader);
    expect(screen.queryByText("New target")).toBeNull();
  });

  it("reveals the whole URL with one audited call, and hides it again", async () => {
    const spy = stubFetch(async (req) => {
      if (req.method === "POST" && req.url.endsWith("/notification-targets/ops-chat:reveal")) return json({ name: "ops-chat", url: "https://chat.example.com/hooks/ops?token=abc" });
      return refuse(req);
    });
    mount();
    const blade = await openBlade("Ops chat");
    expect(within(blade).getByTestId("target-url").textContent).toBe("https://chat.example.com/…");
    expect(spy).not.toHaveBeenCalled();
    fireEvent.click(within(blade).getByText("Reveal"));
    await waitFor(() => expect(within(blade).getByTestId("target-url").textContent).toBe("https://chat.example.com/hooks/ops?token=abc"));
    expect(spy).toHaveBeenCalledTimes(1);
    fireEvent.click(within(blade).getByText("Hide"));
    expect(within(blade).getByTestId("target-url").textContent).toBe("https://chat.example.com/…");
  });

  it("tests a webhook and shows what the receiver answered", async () => {
    stubFetch(async (req) => {
      if (req.method === "POST" && req.url.endsWith("/notification-targets/ops-chat:test")) return json({ ok: false, status: 401, reason: "the receiver answered 401" });
      return refuse(req);
    });
    mount();
    const blade = await openBlade("Ops chat");
    fireEvent.click(within(blade).getByText("Send a test"));
    const result = await within(blade).findByTestId("target-test-result");
    expect(result.textContent).toBe("Not delivered: the receiver answered 401.");
    expect(result.getAttribute("data-ok")).toBe("false");
  });

  it("names the rules that send here and shows what was recently sent, with a failure's reason", async () => {
    stubFetch(refuse);
    mount();
    const blade = await openBlade("Ops chat");
    expect(within(blade).getByTestId("target-used-by").textContent).toContain("Page ops");
    const row = within(blade).getByTestId("recent-notification");
    expect(row.getAttribute("data-status")).toBe("failed");
    expect(within(row).getByTestId("recent-notification-error").textContent).toBe("the receiver answered 503");
  });

  it("offers no test for a group: people are told only about alarms", async () => {
    stubFetch(refuse);
    mount();
    const blade = await openBlade("On-call page");
    expect(within(blade).queryByTestId("target-test")).toBeNull();
    expect(within(blade).getByText("on-call")).toBeTruthy();
  });

  it("disables a target from its blade footer", async () => {
    let body: unknown;
    stubFetch(async (req) => {
      if (req.method === "PATCH" && req.url.endsWith("/notification-targets/ops-chat")) {
        body = await req.json();
        return json({ ...opsChat, enabled: false });
      }
      if (req.method === "GET" && req.url.endsWith("/notification-targets")) return json({ notification_targets: [{ ...opsChat, enabled: false }, onCall] });
      return refuse(req);
    });
    mount();
    const blade = await openBlade("Ops chat");
    fireEvent.click(within(blade).getByText("Disable"));
    await waitFor(() => expect(body).toEqual({ enabled: false }));
  });

  it("creates a group target with only the fields a group reads", async () => {
    let body: unknown;
    stubFetch(async (req) => {
      if (req.method === "POST" && req.url.endsWith("/notification-targets")) {
        body = await req.json();
        return json({ ...onCall, name: "night-shift", label: "Night shift", enabled: true });
      }
      if (req.method === "GET" && req.url.endsWith("/notification-targets")) return json({ notification_targets: [opsChat, onCall] });
      return refuse(req);
    });
    mount();
    fireEvent.click(screen.getByText("New target"));
    const kind = (await screen.findByTestId("target-kind-select")) as HTMLSelectElement;
    fireEvent.change(kind, { target: { value: "principal_group" } });
    fireEvent.input(screen.getByPlaceholderText("Ops chat"), { target: { value: "Night shift" } });
    const group = (await screen.findByText("Choose a group")).closest("select") as HTMLSelectElement;
    fireEvent.change(group, { target: { value: "on-call" } });
    const submit = screen.getByRole("button", { name: /Create target/ });
    await waitFor(() => expect((submit as HTMLButtonElement).disabled).toBe(false));
    fireEvent.click(submit);
    await waitFor(() =>
      expect(body).toEqual({ name: "night-shift", label: "Night shift", kind: "principal_group", enabled: true, principal_group: "on-call", channels: ["inbox"] }),
    );
  });

  it("opens a fresh form with nothing in red", async () => {
    stubFetch(refuse);
    mount();
    fireEvent.click(screen.getByText("New target"));
    await screen.findByPlaceholderText("ops-chat");
    expect(screen.queryByText("A webhook needs the URL it posts to.")).toBeNull();
  });

  it("refuses to create a webhook without a URL, saying why", async () => {
    stubFetch(refuse);
    mount();
    fireEvent.click(screen.getByText("New target"));
    fireEvent.input(await screen.findByPlaceholderText("ops-chat"), { target: { value: "ops-two" } });
    expect((screen.getByRole("button", { name: /Create target/ }) as HTMLButtonElement).disabled).toBe(true);
    expect(screen.getByText("A webhook needs the URL it posts to.")).toBeTruthy();
  });
});
