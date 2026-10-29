import { uuidFor } from "../lib/testids";
import { describe, it, expect, afterEach, vi } from "vitest";
import { render, screen, waitFor, fireEvent, within } from "@solidjs/testing-library";
import { Router, Route } from "@solidjs/router";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import ActionRules from "./ActionRules";
import { ME_KEY, type Me } from "../lib/auth";
import { ACTION_RULES_KEY, recentNotificationsKey, type ActionRule, type Notification } from "../lib/action_rules";
import { NOTIFICATION_TARGETS_KEY, type NotificationTarget } from "../lib/notification_targets";
import { NOTIFICATION_TYPES_KEY } from "../lib/notification_types";

// Platform action rules (#1346): a FlatList over /action-rules. The blade
// switches a rule on and off and lists what it recently sent (picked from the
// newest notifications, a personal subscription's same-named rows left out);
// the form writes the when in the expression editor, whose check gates the
// save. The lists are seeded into the query cache; fetch answers the writes
// and the expression check, and refuses anything else.
const at = "2026-10-29T00:00:00Z";
const admin: Me = { principal: { id: uuidFor("u-root"), kind: "human" }, human: { username: "root" }, permissions: [">"], fleet_permissions: [">"], grants: [{ role: "owner", scope_kind: "all" }] };
const viewer: Me = { principal: { id: uuidFor("u-view"), kind: "human" }, human: { username: "viewer" }, permissions: ["*:read"], fleet_permissions: ["*:read"], grants: [{ role: "viewer", scope_kind: "all" }] };

const pageOps: ActionRule = {
  id: uuidFor("r-1"), name: "page-ops", label: "Page ops", on: ["raised", "cleared"], when: "severity_rank >= 40", targets: ["ops-chat"],
  official: false, opt_out_allowed: false, enabled: true, created_at: at, updated_at: at,
};
const quiet: ActionRule = {
  id: uuidFor("r-2"), name: "everything", on: ["raised"], targets: ["ops-mailbox"], official: false, opt_out_allowed: true, enabled: false,
  created_at: at, updated_at: at,
};
const target = (name: string): NotificationTarget => ({ id: uuidFor(name), name, kind: "webhook", auth: "none", enabled: true, created_at: at, updated_at: at });
const note = (over: Partial<Notification>): Notification => ({
  id: uuidFor("n"), alarm: "a", attempts: 1, channel: "webhook", component: "display-1", component_id: "c-1", created_at: at,
  notification_type: "alarm-raised", status: "sent", subject: "s", target: "ops-chat", text: "t", transition: "raised", action_rule: "page-ops",
  ...over,
});

const json = (body: unknown, status = 200) => new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
const validated = (ok = true) => json({ ok, site: "action", value_type: "bool", result_type: "bool", diagnostics: [], bindings: [{ name: "severity_rank", type: "int", doc: "The severity's rank" }], builtins: ["len"], budget: { iterations: 1, memory: 1, deadline: "5ms" } });

function mount(me: Me = admin, recent: Notification[] = []) {
  const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
  qc.setQueryData([...ME_KEY], me);
  qc.setQueryData([...ACTION_RULES_KEY], [pageOps, quiet]);
  qc.setQueryData([...NOTIFICATION_TARGETS_KEY], [target("ops-chat"), target("ops-mailbox")]);
  qc.setQueryData([...NOTIFICATION_TYPES_KEY], [{ id: uuidFor("ty"), name: "alarm-raised", official: true, subject: "", text: "", created_at: at, updated_at: at }]);
  qc.setQueryData([...recentNotificationsKey({})], recent);
  window.history.pushState({}, "", "/action-rules");
  render(() => (
    <QueryClientProvider client={qc}>
      <Router>
        <Route path="/action-rules" component={ActionRules} />
      </Router>
    </QueryClientProvider>
  ));
  return qc;
}

function stubFetch(handle: (req: Request) => Response | Promise<Response>) {
  return vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
    const req = input as Request;
    if (req.method === "POST" && req.url.endsWith("/expressions:validate")) return validated();
    return handle(req);
  });
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

describe("ActionRules", () => {
  afterEach(() => {
    vi.restoreAllMocks();
    window.history.pushState({}, "", "/");
  });

  it("lists each rule: its edges, its when, where it sends and its status", () => {
    stubFetch(refuse);
    mount();
    expect(screen.getByText("Page ops")).toBeTruthy();
    expect(screen.getByText("severity_rank >= 40")).toBeTruthy();
    expect(screen.getByText("every alarm")).toBeTruthy();
    expect(screen.getByText("ops-mailbox")).toBeTruthy();
    expect(screen.getByText("disabled")).toBeTruthy();
  });

  it("offers New action rule to a writer and hides it from a viewer", () => {
    stubFetch(refuse);
    mount(viewer);
    expect(screen.queryByText("New action rule")).toBeNull();
  });

  it("shows what the rule recently sent, leaving out a personal subscription's same-named rows", async () => {
    stubFetch(refuse);
    mount(admin, [
      note({ id: "1", component: "display-1", status: "sent" }),
      note({ id: "2", component: "dsp-1", target: "", channel: "inbox" }),
      note({ id: "3", component: "mic-1", action_rule: "everything", target: "ops-mailbox" }),
    ]);
    const blade = await openBlade("Page ops");
    const rows = within(blade).getAllByTestId("recent-notification");
    expect(rows).toHaveLength(1);
    expect(rows[0].textContent).toContain("display-1");
    expect(rows[0].textContent).toContain("ops-chat");
  });

  it("says when a rule has sent nothing yet", async () => {
    stubFetch(refuse);
    mount();
    const blade = await openBlade("Page ops");
    expect(within(blade).getByText("Nothing sent yet.")).toBeTruthy();
  });

  it("switches a rule back on from its blade footer", async () => {
    let body: unknown;
    stubFetch(async (req) => {
      if (req.method === "PATCH" && req.url.endsWith("/action-rules/everything")) {
        body = await req.json();
        return json({ ...quiet, enabled: true });
      }
      if (req.method === "GET" && req.url.endsWith("/action-rules")) return json({ action_rules: [pageOps, { ...quiet, enabled: true }] });
      return refuse(req);
    });
    mount();
    const blade = await openBlade("everything");
    fireEvent.click(within(blade).getByText("Enable"));
    await waitFor(() => expect(body).toEqual({ enabled: true }));
  });

  it("creates a rule once its when compiles, sending the targets picked", async () => {
    let body: unknown;
    stubFetch(async (req) => {
      if (req.method === "POST" && req.url.endsWith("/action-rules")) {
        body = await req.json();
        return json({ ...pageOps, name: "page-night", label: "Page night" });
      }
      if (req.method === "GET" && req.url.endsWith("/action-rules")) return json({ action_rules: [pageOps, quiet] });
      return refuse(req);
    });
    mount();
    fireEvent.click(screen.getByText("New action rule"));
    fireEvent.input(await screen.findByPlaceholderText("Page ops"), { target: { value: "Page night" } });
    const group = screen.getByRole("group", { name: "Send to" });
    fireEvent.click(within(group).getByLabelText("ops-chat"));
    const submit = screen.getByRole("button", { name: /Create action rule/ });
    await waitFor(() => expect((submit as HTMLButtonElement).disabled).toBe(false));
    fireEvent.click(submit);
    await waitFor(() =>
      expect(body).toEqual({ name: "page-night", label: "Page night", on: ["raised"], targets: ["ops-chat"], enabled: true, opt_out_allowed: false }),
    );
  });

  it("opens a fresh form with nothing in red", async () => {
    stubFetch(refuse);
    mount();
    fireEvent.click(screen.getByText("New action rule"));
    await screen.findByPlaceholderText("page-ops");
    expect(screen.queryByText("Pick at least one target to send to.")).toBeNull();
  });

  it("holds the save until a target is picked", async () => {
    stubFetch(refuse);
    mount();
    fireEvent.click(screen.getByText("New action rule"));
    fireEvent.input(await screen.findByPlaceholderText("page-ops"), { target: { value: "page-night" } });
    expect((screen.getByRole("button", { name: /Create action rule/ }) as HTMLButtonElement).disabled).toBe(true);
    expect(screen.getByText("Pick at least one target to send to.")).toBeTruthy();
  });

  it("places a refusal of the when beside the editor", async () => {
    stubFetch(async (req) => {
      if (req.method === "PATCH" && req.url.endsWith("/action-rules/page-ops")) {
        return json({ status: 422, detail: "when: bad", errors: [{ location: "body.when", message: "unknown name sevrity", value: { line: 1, column: 1 } }] }, 422);
      }
      return refuse(req);
    });
    mount();
    const blade = await openBlade("Page ops");
    fireEvent.click(within(blade).getByText("Edit"));
    const label = await screen.findByPlaceholderText("Page ops");
    fireEvent.input(label, { target: { value: "Page ops now" } });
    const save = screen.getByRole("button", { name: /Save action rule/ });
    await waitFor(() => expect((save as HTMLButtonElement).disabled).toBe(false));
    fireEvent.click(save);
    expect((await screen.findByTestId("action-rule-when-refused")).textContent).toBe("Line 1, column 1: unknown name sevrity");
  });
});
