import { describe, it, expect, afterEach, vi } from "vitest";
import { render, screen, waitFor, fireEvent, within } from "@solidjs/testing-library";
import { Router, Route } from "@solidjs/router";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import Subscriptions from "./Subscriptions";
import { ME_KEY, type Me } from "../lib/auth";
import { SUBSCRIPTIONS_KEY, type RuleMatch, type Subscription } from "../lib/subscriptions";
import { severityLevelsKey } from "../lib/severity";

// My alarm subscriptions (#1355). The list is seeded into the query cache;
// fetch answers the writes, the :test preview and the expression check a test
// drives, and refuses anything else.
const viewer: Me = { principal: { id: "u-1", kind: "human" }, human: { username: "ada", email: "ada@example.com" }, permissions: ["*:read", "notification:subscribe"], grants: [] };
const noSubscribe: Me = { principal: { id: "u-2", kind: "human" }, human: { username: "bo" }, permissions: ["*:read"], grants: [] };

const outages: Subscription = {
  id: "r-1", name: "hq-outages", label: "HQ outages", on: ["raised", "cleared"], when: "severity_rank >= 40",
  channels: ["inbox", "email"], enabled: true, created_at: "2026-10-01T00:00:00Z", updated_at: "2026-10-01T00:00:00Z",
} as Subscription;
const everything: Subscription = {
  id: "r-2", name: "everything", on: ["raised"], when: "", channels: ["inbox"], enabled: false,
  created_at: "2026-10-01T00:00:00Z", updated_at: "2026-10-01T00:00:00Z",
} as Subscription;

const json = (body: unknown, status = 200) => new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
const validated = (ok = true) => json({ ok, site: "action", value_type: "bool", result_type: "bool", diagnostics: [], bindings: [{ name: "severity_rank", type: "int", doc: "The severity's rank" }], builtins: ["len"], budget: { iterations: 1, memory: 1, deadline: "5ms" } });

function mount(opts: { me?: Me; rules?: Subscription[] } = {}) {
  const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
  qc.setQueryData([...ME_KEY], opts.me ?? viewer);
  if (opts.rules) qc.setQueryData([...SUBSCRIPTIONS_KEY], opts.rules);
  qc.setQueryData([...severityLevelsKey], []);
  window.history.pushState({}, "", "/profile/subscriptions");
  render(() => (
    <QueryClientProvider client={qc}>
      <Router>
        <Route path="/profile/subscriptions" component={Subscriptions} />
        <Route path="/profile" component={() => <div data-testid="profile-page" />} />
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

describe("Subscriptions", () => {
  afterEach(() => {
    vi.restoreAllMocks();
    window.history.pushState({}, "", "/");
  });

  it("lists each subscription: name, edges, when, channels and status", async () => {
    stubFetch(refuse);
    mount({ rules: [outages, everything] });
    const rows = await screen.findAllByTestId("sub-row");
    expect(rows).toHaveLength(2);
    const [a, b] = rows;
    expect(within(a).getByText("HQ outages")).toBeTruthy();
    expect(within(a).getByText("hq-outages")).toBeTruthy();
    expect(within(a).getByText("raised")).toBeTruthy();
    expect(within(a).getByText("cleared")).toBeTruthy();
    expect(within(a).getByText("severity_rank >= 40")).toBeTruthy();
    expect(within(a).getByText("email")).toBeTruthy();
    expect(within(a).getByTestId("sub-status").textContent).toBe("active");
    expect(within(b).getByText("every alarm you can see")).toBeTruthy();
    expect(within(b).getByTestId("sub-status").textContent).toBe("paused");
    expect(within(b).getByRole("button", { name: "Resume everything" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "New subscription" })).toBeTruthy();
  });

  it("teaches the empty page and offers the create", async () => {
    stubFetch(refuse);
    mount({ rules: [] });
    const empty = await screen.findByTestId("subs-empty");
    fireEvent.click(within(empty).getByRole("button", { name: "New subscription" }));
    expect(await screen.findByRole("dialog")).toBeTruthy();
  });

  it("pauses an active subscription", async () => {
    let body: unknown = null;
    stubFetch(async (r) => {
      if (r.method === "PATCH" && r.url.endsWith("/auth/me/action-rules/hq-outages")) {
        body = await r.clone().json();
        return json({ ...outages, enabled: false });
      }
      if (r.method === "GET" && r.url.endsWith("/auth/me/action-rules")) return json({ action_rules: [{ ...outages, enabled: false }, everything] });
      return refuse(r);
    });
    mount({ rules: [outages, everything] });
    fireEvent.click(await screen.findByRole("button", { name: "Pause HQ outages" }));
    await waitFor(() => expect(body).toEqual({ enabled: false }));
    await waitFor(() => expect(within(screen.getAllByTestId("sub-row")[0]).getByTestId("sub-status").textContent).toBe("paused"));
  });

  it("deletes after a confirm, and not without one", async () => {
    let deleted = 0;
    stubFetch((r) => {
      if (r.method === "DELETE" && r.url.endsWith("/auth/me/action-rules/everything")) {
        deleted++;
        return new Response(null, { status: 204 });
      }
      if (r.method === "GET" && r.url.endsWith("/auth/me/action-rules")) return json({ action_rules: [outages] });
      return refuse(r);
    });
    const ask = vi.spyOn(window, "confirm").mockReturnValueOnce(false).mockReturnValueOnce(true);
    mount({ rules: [outages, everything] });
    const del = await screen.findByRole("button", { name: "Delete everything" });
    fireEvent.click(del);
    expect(ask).toHaveBeenCalledTimes(1);
    expect(deleted).toBe(0);
    fireEvent.click(del);
    await waitFor(() => expect(deleted).toBe(1));
    await waitFor(() => expect(screen.getAllByTestId("sub-row")).toHaveLength(1));
  });

  it("tests the draft against my recent alarms and renders each verdict", async () => {
    let sent: { on?: string[]; when?: string } | null = null;
    const matches: RuleMatch[] = [
      { alarm: "a-1", component: "hq-boardroom-dsp", severity: "high", message: "DSP not answering", transition: "raised", at: new Date().toISOString(), matched: true },
      { alarm: "a-2", component: "hq-lobby-display", severity: "info", message: "Input lost", transition: "raised", at: new Date().toISOString(), matched: false },
      { alarm: "a-3", component: "hq-mic-1", severity: "high", message: "Battery low", transition: "raised", at: new Date().toISOString(), matched: false, error: "the when did not run: division by zero" },
    ] as RuleMatch[];
    stubFetch(async (r) => {
      if (r.method === "POST" && r.url.endsWith("/auth/me/action-rules:testDraft")) {
        sent = await r.clone().json();
        return json({ matches });
      }
      return refuse(r);
    });
    mount({ rules: [outages] });
    fireEvent.click(await screen.findByRole("button", { name: "Edit HQ outages" }));
    const dialog = await screen.findByRole("dialog");
    // Drop the cleared edge from the draft, then test: the draft is what is judged.
    fireEvent.change(within(dialog).getByRole("checkbox", { name: "cleared" }));
    const test = within(dialog).getByTestId("sub-test");
    await waitFor(() => expect((within(test).getByRole("button", { name: "Test" }) as HTMLButtonElement).disabled).toBe(false));
    fireEvent.click(within(test).getByRole("button", { name: "Test" }));
    await waitFor(() => expect(within(test).getAllByTestId("sub-test-match")).toHaveLength(3));
    expect(sent).toEqual({ on: ["raised"], when: "severity_rank >= 40", limit: 20 });
    expect(within(test).getByTestId("sub-test-summary").textContent).toContain("1 of 3 would have told you");
    expect(within(test).getByText("1 could not be judged")).toBeTruthy();
    const verdicts = within(test).getAllByTestId("sub-test-match").map((m) => m.dataset.verdict);
    expect(verdicts).toEqual(["matched", "missed", "error"]);
    expect(within(test).getByText("the when did not run: division by zero")).toBeTruthy();
    // Changing the draft marks the verdicts stale until the next run.
    fireEvent.change(within(dialog).getByRole("checkbox", { name: "cleared" }));
    expect(await within(test).findByText("stale")).toBeTruthy();
  });

  it("tests a new draft with no saved subscription at all", async () => {
    let sent: { on?: string[]; when?: string } | null = null;
    stubFetch(async (r) => {
      if (r.method === "POST" && r.url.endsWith("/auth/me/action-rules:testDraft")) {
        sent = await r.clone().json();
        return json({ matches: [{ alarm: "a-1", component: "hq-boardroom-dsp", severity: "high", message: "DSP not answering", transition: "raised", at: new Date().toISOString(), matched: true }] });
      }
      return refuse(r);
    });
    mount({ rules: [] });
    fireEvent.click(within(await screen.findByTestId("subs-empty")).getByRole("button", { name: "New subscription" }));
    const test = within(await screen.findByRole("dialog")).getByTestId("sub-test");
    const run = within(test).getByRole("button", { name: "Test" }) as HTMLButtonElement;
    expect(run.disabled).toBe(false);
    fireEvent.click(run);
    await waitFor(() => expect(within(test).getAllByTestId("sub-test-match")).toHaveLength(1));
    expect(sent).toEqual({ on: ["raised"], when: "", limit: 20 });
    expect(within(test).getByTestId("sub-test-summary").textContent).toContain("1 of 1 would have told you");
  });

  it("creates a subscription from the drawer", async () => {
    let created: Record<string, unknown> | null = null;
    stubFetch(async (r) => {
      if (r.method === "POST" && r.url.endsWith("/auth/me/action-rules")) {
        created = await r.clone().json();
        return json({ ...everything, name: "lobby", label: "Lobby", enabled: true }, 201);
      }
      if (r.method === "GET" && r.url.endsWith("/auth/me/action-rules")) return json({ action_rules: [outages] });
      return refuse(r);
    });
    mount({ rules: [] });
    fireEvent.click(within(await screen.findByTestId("subs-empty")).getByRole("button", { name: "New subscription" }));
    const dialog = await screen.findByRole("dialog");
    fireEvent.input(within(dialog).getByLabelText("Label"), { target: { value: "Lobby" } });
    expect((within(dialog).getByLabelText("Name") as HTMLInputElement).value).toBe("lobby");
    const submit = within(dialog).getByRole("button", { name: "Subscribe" }) as HTMLButtonElement;
    await waitFor(() => expect(submit.disabled).toBe(false));
    fireEvent.click(submit);
    await waitFor(() => expect(created).toEqual({ name: "lobby", label: "Lobby", on: ["raised"], channels: ["inbox"], enabled: true }));
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
  });

  it("puts a refused when back under the field", async () => {
    stubFetch(async (r) => {
      if (r.method === "PATCH") {
        return json({ status: 422, detail: "when: unknown name", errors: [{ location: "body.when", message: "unknown name sevrity", value: { line: 1, column: 1 } }] }, 422);
      }
      return refuse(r);
    });
    mount({ rules: [outages] });
    fireEvent.click(await screen.findByRole("button", { name: "Edit HQ outages" }));
    const dialog = await screen.findByRole("dialog");
    const save = within(dialog).getByRole("button", { name: "Save" }) as HTMLButtonElement;
    await waitFor(() => expect(save.disabled).toBe(false));
    fireEvent.click(save);
    expect((await within(dialog).findByTestId("when-refused")).textContent).toBe("Line 1, column 1: unknown name sevrity");
  });

  it("offers no write, and asks nothing, without notification:subscribe", async () => {
    const fetch = stubFetch(refuse);
    mount({ me: noSubscribe });
    expect(await screen.findByTestId("subs-denied")).toBeTruthy();
    expect(screen.queryByRole("button", { name: "New subscription" })).toBeNull();
    expect(screen.queryByTestId("subs-list")).toBeNull();
    expect(fetch).not.toHaveBeenCalled();
  });

  it("alerts on a failed read and retries", async () => {
    let calls = 0;
    stubFetch((r) => {
      if (r.method === "GET" && r.url.endsWith("/auth/me/action-rules")) {
        calls++;
        return calls === 1 ? json({ status: 500, detail: "the database is away" }, 500) : json({ action_rules: [outages] });
      }
      return refuse(r);
    });
    mount();
    const alert = await screen.findByTestId("subs-error");
    expect(alert.textContent).toContain("the database is away");
    fireEvent.click(within(alert).getByRole("button", { name: "Retry" }));
    expect(await screen.findAllByTestId("sub-row")).toHaveLength(1);
  });

  it("leads back to the profile from its breadcrumb", async () => {
    stubFetch(refuse);
    mount({ rules: [] });
    fireEvent.click(await screen.findByRole("button", { name: "Profile" }));
    expect(await screen.findByTestId("profile-page")).toBeTruthy();
  });
});
